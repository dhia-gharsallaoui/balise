package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dhia/balise/internal/store"
)

// FetchInput deviates from 04 section 12's abbreviated schema
// ({"slug":"string?","uid":"string?","range":"[int,int]?"}) in one
// deliberate way: Scope is required here. store.Queries.GetPage(ctx,
// scope, slug) mechanically needs an explicit scope -- a slug is only
// unique per (scope, slug) pair, not globally, so there is no safe way to
// guess which of the token's scopes a bare slug belongs to. uid and range
// are dropped because GetPage supports neither.
type FetchInput struct {
	Scope string `json:"scope"`
	Slug  string `json:"slug"`
}

// FetchClaim is one claim attached to a fetched page.
type FetchClaim struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

// FetchPage mirrors the fields of store.PageRow that 04 section 12's
// output schema calls for.
type FetchPage struct {
	Slug   string       `json:"slug"`
	Title  string       `json:"title"`
	Type   string       `json:"type"`
	Status string       `json:"status"`
	Scope  string       `json:"scope"`
	Body   string       `json:"body"`
	Claims []FetchClaim `json:"claims"`
}

// Attachment matches 04 section 12's {"name":"","mime":"","url":""} shape.
// balise's PageStore has no attachment concept yet, so this is always
// empty; the field exists so a client parsing the documented output
// schema does not have to special-case its absence.
type Attachment struct {
	Name string `json:"name"`
	MIME string `json:"mime"`
	URL  string `json:"url"`
}

// FetchOutput matches 04 section 12's {"page":"Page","attachments":[...]}
// shape.
type FetchOutput struct {
	Page        FetchPage    `json:"page"`
	Attachments []Attachment `json:"attachments"`
}

// errNotFound is returned for both "no such slug" and "slug exists, but
// outside this token's scopes" -- 04 section 14: "never distinguish
// 'exists elsewhere'."
var errNotFound = errors.New("not found")

func (d *deps) fetch(ctx context.Context, req *sdk.CallToolRequest, in FetchInput) (*sdk.CallToolResult, FetchOutput, error) {
	start := time.Now()
	out := FetchOutput{Attachments: []Attachment{}}

	tok, err := authenticate(ctx, d.pool, req)
	if err != nil {
		return nil, out, err
	}

	if !tok.HasCapability("read") {
		recordAudit(ctx, d.pool, tok.ID, "fetch", tok.Scopes, in.Slug, nil, 0, start)
		return nil, out, fmt.Errorf("missing capability: read")
	}

	// A scope the token does not hold is refused identically to a
	// genuinely nonexistent slug below -- 04 section 12: "404 unknown
	// slug within allowed scopes (never distinguish 'exists
	// elsewhere')."
	if !tok.AllowsScope(in.Scope) {
		recordAudit(ctx, d.pool, tok.ID, "fetch", tok.Scopes, in.Slug, nil, 0, start)
		return nil, out, errNotFound
	}

	// Reuse store.Queries for every read, per the task brief: scoped to
	// exactly the one requested scope, constructed fresh via the sole
	// constructor NewQueries.
	q := store.NewQueries(d.pool, store.Scopes{in.Scope})
	page, err := q.GetPage(ctx, in.Scope, in.Slug)
	if err != nil {
		recordAudit(ctx, d.pool, tok.ID, "fetch", []string{in.Scope}, in.Slug, nil, 0, start)
		return nil, out, fmt.Errorf("fetch: %w", err)
	}
	if page == nil {
		recordAudit(ctx, d.pool, tok.ID, "fetch", []string{in.Scope}, in.Slug, nil, 0, start)
		return nil, out, errNotFound
	}

	claims := make([]FetchClaim, 0, len(page.Claims))
	for _, c := range page.Claims {
		claims = append(claims, FetchClaim{Text: c.Text, Status: c.Status})
	}
	out.Page = FetchPage{
		Slug:   page.Slug,
		Title:  page.Title,
		Type:   page.Type,
		Status: page.Status,
		Scope:  page.Scope,
		Body:   page.BodyMD,
		Claims: claims,
	}

	recordAudit(ctx, d.pool, tok.ID, "fetch", []string{in.Scope}, in.Slug, []string{page.UID}, len(claims), start)
	return nil, out, nil
}
