package mcp

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dhia/balise/internal/store"
)

// PagesInput is pages' argument shape, per 04 section 12's abbreviated
// schema. as_of is deliberately omitted: documents.as_of/valid_from/valid_to
// are real columns, but the indexer never populates them (confirmed across
// every migration and the indexer itself), so accepting the field would
// silently do nothing -- the same reasoning search.go's SearchInput doc
// comment already documents for its own omissions. Omit and say why, rather
// than accept and ignore.
type PagesInput struct {
	// Type narrows to one page type. Empty means every type.
	Type string `json:"type,omitempty"`
	// Scope narrows to one scope, which must already be one of the
	// token's own scopes -- 04 section 14: "a scope argument narrows,
	// never widens." Empty means every scope the token holds.
	Scope string `json:"scope,omitempty"`
	// Tags, when non-empty, keeps only pages carrying at least one of
	// them (store.Queries.ListPages: "tags && ...", an overlap match).
	Tags []string `json:"tags,omitempty"`
	// Status, when non-empty, keeps only pages whose status is one of
	// these values (e.g. "active", "deprecated").
	Status []string `json:"status,omitempty"`
	// Limit caps the number of pages returned. Non-positive falls back
	// to defaultPagesLimit; anything above maxPagesLimit is clamped
	// there -- an untrusted caller asking for an unbounded listing must
	// not be able to force an unbounded query result.
	Limit int `json:"limit,omitempty"`
}

// PagesPage mirrors one row of 04 section 12's output schema. Unlike
// fetch.go's FetchPage, it carries no body or claims: pages is a listing
// tool, not a single-page fetch, so it does not return full content for
// every match in the list.
type PagesPage struct {
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Scope  string `json:"scope"`
}

// PagesOutput matches 04 section 12's {"pages":[...]} shape.
type PagesOutput struct {
	Pages []PagesPage `json:"pages"`
}

const (
	defaultPagesLimit = 50
	maxPagesLimit     = 200
)

// listPages implements the "pages" tool. It is named listPages, not pages,
// because deps already has a field named pages (the store.PageStore) --
// the tool's registered name in server.go is still "pages".
func (d *deps) listPages(ctx context.Context, req *sdk.CallToolRequest, in PagesInput) (*sdk.CallToolResult, PagesOutput, error) {
	start := time.Now()
	out := PagesOutput{Pages: []PagesPage{}}
	descr := describePagesQuery(in)

	tok, err := authenticate(ctx, d.pool, req)
	if err != nil {
		return nil, out, err
	}

	if !tok.HasCapability("read") {
		recordAudit(ctx, d.pool, tok.ID, "pages", tok.Scopes, descr, nil, 0, start)
		return nil, out, fmt.Errorf("missing capability: read")
	}

	// Scopes from the token are the only source of scopes_allowed; a
	// scope argument narrows, never widens (04 section 14), enforced by
	// constructing a fresh, narrower store.Queries below -- never by
	// post-filtering a wider query's results.
	scopes := tok.Scopes
	if in.Scope != "" {
		if !tok.AllowsScope(in.Scope) {
			recordAudit(ctx, d.pool, tok.ID, "pages", tok.Scopes, descr, nil, 0, start)
			return nil, out, fmt.Errorf("scope %q not allowed for this token", in.Scope)
		}
		scopes = []string{in.Scope}
	}

	limit := in.Limit
	switch {
	case limit <= 0:
		limit = defaultPagesLimit
	case limit > maxPagesLimit:
		limit = maxPagesLimit
	}

	filter := store.PageFilter{Tags: in.Tags, Status: in.Status, Limit: limit}
	if in.Type != "" {
		filter.Types = []string{in.Type}
	}

	// Reuse store.Queries for every read, per the task brief: it binds
	// scopes at construction, and internal/store's own guard tests
	// enforce that nothing else touches the derived tables directly.
	q := store.NewQueries(d.pool, store.Scopes(scopes))
	rows, err := q.ListPages(ctx, filter)
	if err != nil {
		recordAudit(ctx, d.pool, tok.ID, "pages", scopes, descr, nil, 0, start)
		return nil, out, fmt.Errorf("list pages: %w", err)
	}

	uids := make([]string, 0, len(rows))
	out.Pages = make([]PagesPage, 0, len(rows))
	for _, r := range rows {
		out.Pages = append(out.Pages, PagesPage{
			Slug: r.Slug, Title: r.Title, Type: r.Type, Status: r.Status, Scope: r.Scope,
		})
		uids = append(uids, r.UID)
	}

	recordAudit(ctx, d.pool, tok.ID, "pages", scopes, descr, uids, len(out.Pages), start)
	return nil, out, nil
}

// describePagesQuery renders a short, human-readable summary of the
// filters a pages call used, for the audit log's query column (which
// search.go and fetch.go populate with their own single query/slug
// string) -- pages has no single free-text query, so this stands in for
// one, truncated the same way by store.RecordAudit regardless.
func describePagesQuery(in PagesInput) string {
	return fmt.Sprintf("type=%q tags=%v status=%v", in.Type, in.Tags, in.Status)
}
