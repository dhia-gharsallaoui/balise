package mcp

import (
	"context"
	"fmt"
	"sort"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
)

// ContextInput is context's argument shape, per 04 section 12's abbreviated
// schema and section 11's context-assembly algorithm.
//
// Two fields the spec's abbreviated schema also lists are deliberately
// omitted, for the same reason search.go's SearchInput and pages.go's
// PagesInput omit as_of: nothing in this environment backs them.
//   - as_of: documents.as_of/valid_from/valid_to are real columns the
//     indexer never populates.
//   - default_space (a per-caller "preferred space" boost): there is no
//     per-token or per-request notion of a default space anywhere in the
//     schema or the token model to source this from.
//
// Presigned attachment URLs are likewise not produced anywhere in this
// output: balise's PageStore has no attachment concept yet (see fetch.go's
// Attachment, always empty).
type ContextInput struct {
	Query string `json:"query"`
	// Scope narrows to one scope, which must already be one of the
	// token's own scopes -- 04 section 14: "a scope argument narrows,
	// never widens." Empty means every scope the token holds.
	Scope string `json:"scope,omitempty"`
	// BudgetTokens is a pointer so an explicit 0 (a real, if degenerate,
	// request -- "pack to a zero-token budget") can be told apart from an
	// omitted field (nil), which falls back to defaultContextBudget. A
	// negative value is refused outright rather than silently reinterpreted
	// as either 0 or the default: it is not a meaningful budget under any
	// reading, and guessing which of the two the caller "really meant"
	// would be dishonest.
	BudgetTokens *int `json:"budget_tokens,omitempty"`
	// IncludeHistorical also widens edge expansion to follow "supersedes"
	// edges, not just "specializes" (04 section 11 step 2), and stops
	// historical candidates being dropped in step 4.
	IncludeHistorical bool `json:"include_historical,omitempty"`
}

// ContextPage is one page packed into the returned ContextPack. Why records
// how this page was found -- a search hit's own lexical/trigram/semantic
// reason (store.Hit.Why, exactly as search.go's SearchHit already surfaces
// it, now including "semantic" when a configured embedder's vector arm won
// the RRF fusion for that hit), or the edge kind
// ("specializes"/"supersedes") that pulled it in as an expansion neighbour
// of a search hit. There is still no numeric score field: Why already says
// which retrieval arm produced the hit, and printing a raw RRF score here
// would invite a confidence-magnitude misreading the task brief warns
// against.
type ContextPage struct {
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Scope  string `json:"scope"`
	Body   string `json:"body"`
	Tokens int    `json:"tokens"`
	Why    string `json:"why"`
}

// UnloadedPage is one candidate that did not make it into the pack. Reason
// is "oversize" when the page's own token count exceeds the entire budget
// (it could never fit, regardless of what else was packed), or "budget"
// when it would fit in a fresh budget but the budget was already spent on
// higher-ranked candidates. 04 section 11 step 4: a page is never silently
// dropped and never truncated mid-page to make it fit.
type UnloadedPage struct {
	Slug   string `json:"slug"`
	Reason string `json:"reason"`
}

// ContextOutput is the ContextPack 04 section 12 describes.
//
// Coverage is set honestly, the same way SearchOutput's is: "ok" when at
// least one page was actually packed, "low" otherwise -- never a semantic
// confidence score, because none is computed. Ranking behind Pages' order
// reuses SearchClaims (search.go's own RRF fusion of tsvector, pg_trgm, and
// -- when a model is configured on this deployment -- an in-process
// embedding arm; see deps.semanticQuery and internal/store's
// SemanticQuery). Without a configured model, ranking here is lexical
// only, exactly as before, with no error and no behaviour change.
type ContextOutput struct {
	Pages      []ContextPage  `json:"pages"`
	Unloaded   []UnloadedPage `json:"unloaded"`
	Coverage   string         `json:"coverage"`
	TokensUsed int            `json:"tokens_used"`
}

const (
	defaultContextBudget = 6000
	contextSearchTopK    = 40
)

// contextCandidate is one page under consideration for packing, whether it
// came directly from search or was pulled in by edge expansion.
type contextCandidate struct {
	uid, slug, title, typ, status, scope, body string
	tokens                                     int
	historical                                 bool
	score                                      float64
	why                                        string
}

func (d *deps) context(ctx context.Context, req *sdk.CallToolRequest, in ContextInput) (*sdk.CallToolResult, ContextOutput, error) {
	start := time.Now()
	out := ContextOutput{Pages: []ContextPage{}, Unloaded: []UnloadedPage{}}

	tok, err := authenticate(ctx, d.pool, req)
	if err != nil {
		return nil, out, err
	}

	if !tok.HasCapability("read") {
		recordAuditCoverage(ctx, d.pool, tok.ID, "context", tok.Scopes, in.Query, nil, 0, "", start)
		return nil, out, fmt.Errorf("missing capability: read")
	}

	// Scopes from the token are the only source of scopes_allowed; a scope
	// argument narrows, never widens (04 section 14), enforced by
	// constructing a fresh, narrower store.Queries below.
	scopes := tok.Scopes
	if in.Scope != "" {
		if !tok.AllowsScope(in.Scope) {
			recordAuditCoverage(ctx, d.pool, tok.ID, "context", tok.Scopes, in.Query, nil, 0, "", start)
			return nil, out, fmt.Errorf("scope %q not allowed for this token", in.Scope)
		}
		scopes = []string{in.Scope}
	}

	budget := defaultContextBudget
	if in.BudgetTokens != nil {
		budget = *in.BudgetTokens
	}
	if budget < 0 {
		recordAuditCoverage(ctx, d.pool, tok.ID, "context", scopes, in.Query, nil, 0, "", start)
		return nil, out, fmt.Errorf("budget_tokens must not be negative")
	}

	q := store.NewQueries(d.pool, store.Scopes(scopes))

	candidates, err := d.contextCandidates(ctx, q, in)
	if err != nil {
		recordAuditCoverage(ctx, d.pool, tok.ID, "context", scopes, in.Query, nil, 0, "", start)
		return nil, out, fmt.Errorf("context: %w", err)
	}

	ordered := orderContextCandidates(candidates, d.order)
	tokensUsed, uids := packContext(&out, ordered, budget)
	out.TokensUsed = tokensUsed

	out.Coverage = "low"
	if len(out.Pages) > 0 {
		out.Coverage = "ok"
	}

	recordAuditCoverage(ctx, d.pool, tok.ID, "context", scopes, in.Query, uids, tokensUsed, out.Coverage, start)
	return nil, out, nil
}

// contextCandidates implements 04 section 11 steps 1-2: search for
// candidates, then expand via specializes (+supersedes when
// IncludeHistorical) edges one hop out from those candidates.
func (d *deps) contextCandidates(ctx context.Context, q *store.Queries, in ContextInput) ([]contextCandidate, error) {
	hits, err := q.SearchClaims(
		ctx, in.Query, in.IncludeHistorical, contextSearchTopK, d.semanticQuery(ctx, in.Query),
	)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	var candidates []contextCandidate
	seen := map[string]bool{}
	var seedUIDs []string

	for _, h := range hits {
		page, err := q.GetPage(ctx, h.Scope, h.Slug)
		if err != nil {
			return nil, fmt.Errorf("get page %s/%s: %w", h.Scope, h.Slug, err)
		}
		if page == nil || seen[page.UID] {
			// A candidate search cannot outrun the scope it was searched
			// in, but a page can vanish between the search and the
			// fetch (concurrent edit); skip rather than fail the whole
			// call over one stale hit.
			continue
		}
		seen[page.UID] = true
		candidates = append(candidates, contextCandidate{
			uid: page.UID, slug: page.Slug, title: page.Title, typ: page.Type,
			status: page.Status, scope: page.Scope, body: page.BodyMD,
			tokens: page.Tokens, historical: page.Historical,
			score: h.Score, why: h.Why,
		})
		seedUIDs = append(seedUIDs, page.UID)
	}

	kinds := []string{"specializes"}
	if in.IncludeHistorical {
		kinds = append(kinds, "supersedes")
	}
	neighbors, err := q.GetOutboundNeighbors(ctx, seedUIDs, kinds)
	if err != nil {
		return nil, fmt.Errorf("expand relations: %w", err)
	}
	var expandUIDs []string
	for _, n := range neighbors {
		if !seen[n.UID] {
			expandUIDs = append(expandUIDs, n.UID)
		}
	}
	neighborKind := map[string]string{}
	for _, n := range neighbors {
		if _, ok := neighborKind[n.UID]; !ok {
			neighborKind[n.UID] = n.Kind
		}
	}

	expanded, err := q.GetPagesByUIDs(ctx, expandUIDs)
	if err != nil {
		return nil, fmt.Errorf("get expansion pages: %w", err)
	}
	for _, page := range expanded {
		if seen[page.UID] {
			continue
		}
		seen[page.UID] = true
		candidates = append(candidates, contextCandidate{
			uid: page.UID, slug: page.Slug, title: page.Title, typ: page.Type,
			status: page.Status, scope: page.Scope, body: page.BodyMD,
			tokens: page.Tokens, historical: page.Historical,
			score: 0, why: neighborKind[page.UID],
		})
	}

	// 04 section 11 step 4: drop historical unless included. Filtered
	// here, in Go, rather than only relying on SearchClaims's own
	// includeHistorical flag, because GetPagesByUIDs (used for edge
	// expansion above) applies no such filter at the SQL level.
	if in.IncludeHistorical {
		return candidates, nil
	}
	kept := candidates[:0]
	for _, c := range candidates {
		if !c.historical {
			kept = append(kept, c)
		}
	}
	return kept, nil
}

// orderContextCandidates implements 04 section 11 step 3: order by type
// rank then score, historical last. Historical is treated as the primary
// partition -- every non-historical candidate before every historical one
// -- because "historical last" only has one honest reading: at the very
// end of the whole list, not merely within each type-rank bucket. Within
// each partition, candidates are ordered by registry type rank, then by
// score descending; an expansion-only candidate (score 0, see
// contextCandidates) sorts after every real search hit of the same type.
func orderContextCandidates(candidates []contextCandidate, order *registry.Order) []contextCandidate {
	ordered := append([]contextCandidate(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.historical != b.historical {
			return !a.historical
		}
		ra, rb := order.Rank(a.typ), order.Rank(b.typ)
		if ra != rb {
			return ra < rb
		}
		return a.score > b.score
	})
	return ordered
}

// packContext implements 04 section 11 step 4's budget packing, using each
// candidate's real store.PageRow.Tokens count (never a character estimate).
// A page is never truncated mid-body to make it fit: it is either packed
// whole or reported in unloaded, never both.
func packContext(out *ContextOutput, ordered []contextCandidate, budget int) (int, []string) {
	tokensUsed := 0
	var uids []string
	for _, c := range ordered {
		switch {
		case c.tokens > budget:
			out.Unloaded = append(out.Unloaded, UnloadedPage{Slug: c.slug, Reason: "oversize"})
		case tokensUsed+c.tokens > budget:
			out.Unloaded = append(out.Unloaded, UnloadedPage{Slug: c.slug, Reason: "budget"})
		default:
			out.Pages = append(out.Pages, ContextPage{
				Slug: c.slug, Title: c.title, Type: c.typ, Status: c.status,
				Scope: c.scope, Body: c.body, Tokens: c.tokens, Why: c.why,
			})
			tokensUsed += c.tokens
			uids = append(uids, c.uid)
		}
	}
	return tokensUsed, uids
}
