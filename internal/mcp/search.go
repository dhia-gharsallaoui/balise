package mcp

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dhia/balise/internal/store"
)

// SearchInput is search's argument shape, per 04 section 12's abbreviated
// schema. Type/tags/status/as_of/include_raw filters described there are not
// implemented by store.Queries.SearchClaims (which takes only a query
// string, an include-historical flag, and a limit), so they are omitted
// here rather than accepted-and-ignored.
type SearchInput struct {
	Query string `json:"query"`
	// Scope narrows the search to a single scope. It must already be one
	// of the token's own scopes -- 04 section 14: "a scope argument
	// narrows, never widens." Left empty, the search runs across every
	// scope the token holds.
	Scope             string `json:"scope,omitempty"`
	IncludeHistorical bool   `json:"include_historical,omitempty"`
	TopK              int    `json:"top_k,omitempty"`
}

// SearchHit mirrors store.Hit, trimmed to what 04 section 12's output
// schema lists. store.Hit carries no document UID, so the audit log and
// leak test use "<scope>/<slug>" as this hit's identifier instead (see
// the uids slice built in search below) -- a deliberate simplification,
// not a gap: scope+slug is already how GetPage uniquely addresses a page.
type SearchHit struct {
	Slug          string   `json:"slug"`
	Title         string   `json:"title"`
	Type          string   `json:"type"`
	Status        string   `json:"status"`
	Scope         string   `json:"scope"`
	MatchedClaims []string `json:"matched_claims"`
	Why           string   `json:"why"`
}

// SearchOutput matches 04 section 12's {"hits":[...],"coverage":"ok|low"}
// shape.
type SearchOutput struct {
	Hits     []SearchHit `json:"hits"`
	Coverage string      `json:"coverage"`
}

const defaultSearchTopK = 10

func (d *deps) search(ctx context.Context, req *sdk.CallToolRequest, in SearchInput) (*sdk.CallToolResult, SearchOutput, error) {
	start := time.Now()
	out := SearchOutput{Hits: []SearchHit{}}

	tok, err := authenticate(ctx, d.pool, req)
	if err != nil {
		return nil, out, err
	}

	if !tok.HasCapability("read") {
		recordAudit(ctx, d.pool, tok.ID, "search", tok.Scopes, in.Query, nil, 0, start)
		return nil, out, fmt.Errorf("missing capability: read")
	}

	// Scopes from the token are the only source of scopes_allowed; a
	// scope argument narrows, never widens (04 section 14). This is
	// enforced by constructing a fresh, narrower store.Queries below --
	// never by post-filtering a wider query's results.
	scopes := tok.Scopes
	if in.Scope != "" {
		if !tok.AllowsScope(in.Scope) {
			recordAudit(ctx, d.pool, tok.ID, "search", tok.Scopes, in.Query, nil, 0, start)
			return nil, out, fmt.Errorf("scope %q not allowed for this token", in.Scope)
		}
		scopes = []string{in.Scope}
	}

	topK := in.TopK
	if topK <= 0 {
		topK = defaultSearchTopK
	}

	// Reuse store.Queries for every read, per the task brief: it binds
	// scopes at construction, and internal/store's own guard tests
	// enforce that nothing else touches the derived tables. NewQueries
	// is the sole constructor.
	q := store.NewQueries(d.pool, store.Scopes(scopes))
	hits, err := q.SearchClaims(ctx, in.Query, in.IncludeHistorical, topK)
	if err != nil {
		recordAudit(ctx, d.pool, tok.ID, "search", scopes, in.Query, nil, 0, start)
		return nil, out, fmt.Errorf("search: %w", err)
	}

	uids := make([]string, 0, len(hits))
	out.Hits = make([]SearchHit, 0, len(hits))
	tokensOut := 0
	for _, h := range hits {
		out.Hits = append(out.Hits, SearchHit{
			Slug:          h.Slug,
			Title:         h.Title,
			Type:          h.Type,
			Status:        h.Status,
			Scope:         h.Scope,
			MatchedClaims: h.MatchedClaims,
			Why:           h.Why,
		})
		uids = append(uids, h.Scope+"/"+h.Slug)
		tokensOut += len(h.MatchedClaims)
	}
	out.Coverage = "low"
	if len(out.Hits) > 0 {
		out.Coverage = "ok"
	}

	recordAudit(ctx, d.pool, tok.ID, "search", scopes, in.Query, uids, tokensOut, start)
	return nil, out, nil
}
