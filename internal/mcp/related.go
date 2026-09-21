package mcp

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dhia/balise/internal/store"
)

// RelatedInput is related's argument shape. Scope is required for the same
// reason fetch.go's FetchInput requires it: a slug is only unique per
// (scope, slug), so there is no safe way to guess which of the token's
// scopes a bare slug names.
type RelatedInput struct {
	Scope string `json:"scope"`
	Slug  string `json:"slug"`
	// Depth controls how many outbound hops to follow past the starting
	// page. Non-positive falls back to defaultRelatedDepth (1); anything
	// above maxRelatedDepth is silently clamped there -- an untrusted
	// caller must never be able to turn a cyclic or densely connected
	// graph into an unbounded traversal.
	Depth int `json:"depth,omitempty"`
}

// RelatedPage is one neighbouring page, trimmed to what a caller needs to
// decide whether to fetch it.
type RelatedPage struct {
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Scope  string `json:"scope"`
}

// RelatedOutput groups outbound relations by edge kind ("specializes",
// "supersedes", etc. -- whatever kinds the vault's edges actually carry),
// alongside inbound backlinks. Depth echoes the effective depth actually
// used, after clamping, so a caller that requested more than
// maxRelatedDepth can tell its request was capped rather than silently
// satisfied in full.
//
// Backlinks are always exactly one hop (inbound edges into the starting
// page only), regardless of Depth: store.Queries has no batched inbound
// neighbour query analogous to GetOutboundNeighbors, and multi-hop inbound
// traversal is not something any tool in this package needs yet. This is a
// deliberate simplification, not an oversight.
type RelatedOutput struct {
	Outbound  map[string][]RelatedPage `json:"outbound"`
	Backlinks []RelatedPage            `json:"backlinks"`
	Depth     int                      `json:"depth"`
}

const (
	defaultRelatedDepth = 1
	maxRelatedDepth     = 5
	// maxRelatedNodes bounds total outbound expansion regardless of
	// depth: a depth cap alone does not bound the number of nodes
	// visited at a given hop, so a small number of richly connected hub
	// pages could still make even a capped-depth traversal expensive on
	// a much larger vault than this one. Once this many distinct nodes
	// have been visited, expansion stops early; already-found relations
	// are still returned, never discarded.
	maxRelatedNodes = 500
)

func (d *deps) related(ctx context.Context, req *sdk.CallToolRequest, in RelatedInput) (*sdk.CallToolResult, RelatedOutput, error) {
	start := time.Now()
	out := RelatedOutput{Outbound: map[string][]RelatedPage{}, Backlinks: []RelatedPage{}}

	tok, err := authenticate(ctx, d.pool, req)
	if err != nil {
		return nil, out, err
	}

	if !tok.HasCapability("read") {
		recordAudit(ctx, d.pool, tok.ID, "related", tok.Scopes, in.Slug, nil, 0, start)
		return nil, out, fmt.Errorf("missing capability: read")
	}

	// A scope the token does not hold is refused identically to a
	// genuinely nonexistent slug below -- 04 section 12/14: never
	// distinguish "exists elsewhere" from "does not exist".
	if !tok.AllowsScope(in.Scope) {
		recordAudit(ctx, d.pool, tok.ID, "related", tok.Scopes, in.Slug, nil, 0, start)
		return nil, out, errNotFound
	}

	depth := in.Depth
	if depth <= 0 {
		depth = defaultRelatedDepth
	}
	if depth > maxRelatedDepth {
		depth = maxRelatedDepth
	}
	out.Depth = depth

	// Reuse store.Queries for every read, scoped to exactly the one
	// requested scope, constructed fresh via the sole constructor
	// NewQueries.
	q := store.NewQueries(d.pool, store.Scopes{in.Scope})
	page, err := q.GetPage(ctx, in.Scope, in.Slug)
	if err != nil {
		recordAudit(ctx, d.pool, tok.ID, "related", []string{in.Scope}, in.Slug, nil, 0, start)
		return nil, out, fmt.Errorf("related: %w", err)
	}
	if page == nil {
		recordAudit(ctx, d.pool, tok.ID, "related", []string{in.Scope}, in.Slug, nil, 0, start)
		return nil, out, errNotFound
	}

	// Backlinks come from GetRelations, which -- like GetOutboundNeighbors
	// -- inner-joins to documents, so a dangling edge (to_uid null) or one
	// pointing outside the allowed scope is silently excluded, never a
	// crash and never surfaced as an error.
	_, backlinks, err := q.GetRelations(ctx, page.UID)
	if err != nil {
		recordAudit(ctx, d.pool, tok.ID, "related", []string{in.Scope}, in.Slug, nil, 0, start)
		return nil, out, fmt.Errorf("related: %w", err)
	}
	for _, b := range backlinks {
		out.Backlinks = append(out.Backlinks, RelatedPage{Slug: b.Slug, Title: b.Title, Status: b.Status, Scope: in.Scope})
	}

	visited := map[string]bool{page.UID: true}
	frontier := []string{page.UID}
	resultCount := len(out.Backlinks)

	for hop := 0; hop < depth && len(frontier) > 0 && len(visited) < maxRelatedNodes; hop++ {
		neighbors, err := q.GetOutboundNeighbors(ctx, frontier, nil)
		if err != nil {
			recordAudit(ctx, d.pool, tok.ID, "related", []string{in.Scope}, in.Slug, nil, resultCount, start)
			return nil, out, fmt.Errorf("related: %w", err)
		}

		var next []string
		for _, n := range neighbors {
			if visited[n.UID] {
				continue
			}
			if len(visited) >= maxRelatedNodes {
				break
			}
			visited[n.UID] = true
			out.Outbound[n.Kind] = append(out.Outbound[n.Kind], RelatedPage{
				Slug: n.Slug, Title: n.Title, Status: n.Status, Scope: in.Scope,
			})
			resultCount++
			next = append(next, n.UID)
		}
		frontier = next
	}

	uids := make([]string, 0, len(visited))
	for uid := range visited {
		uids = append(uids, uid)
	}

	// tokensOut here counts returned relation entries, not text tokens --
	// the same reuse of the field fetch.go makes with its claim count,
	// rather than an actual token count that has no meaning for this tool.
	recordAudit(ctx, d.pool, tok.ID, "related", []string{in.Scope}, in.Slug, uids, resultCount, start)
	return nil, out, nil
}
