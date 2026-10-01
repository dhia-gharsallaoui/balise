package cli

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dhia/balise/internal/compile"
	"github.com/dhia/balise/internal/embed"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
)

// IndexCandidateFinder is compile.CandidateFinder over the index: the same SearchClaims
// fusion agents' search uses (lexical, trigram, and semantic when an embedder is loaded),
// run through a store.Queries bound to exactly the one scope asked about, so a note can
// only ever be matched against pages in its own scope. Pages of a log-trait type are left
// out: a note is never attached to another agent's notes.
type IndexCandidateFinder struct {
	pool     *pgxpool.Pool
	reg      *registry.Registry
	embedder *embed.Embedder // nil means lexical-only matching
}

// NewIndexCandidateFinder builds the finder; embedder may be nil.
func NewIndexCandidateFinder(pool *pgxpool.Pool, reg *registry.Registry, embedder *embed.Embedder) *IndexCandidateFinder {
	return &IndexCandidateFinder{pool: pool, reg: reg, embedder: embedder}
}

// Candidates returns up to limit pages in scope most related to text, with their active
// claims so the model can see what each page already says.
func (f *IndexCandidateFinder) Candidates(ctx context.Context, scope, text string, limit int) ([]compile.CandidatePage, error) {
	q := store.NewQueries(f.pool, store.Scopes{scope})
	// Over-fetch: some hits are log pages, which are dropped below.
	hits, err := q.SearchClaims(ctx, text, false, limit*3, f.semanticQuery(ctx, text))
	if err != nil {
		return nil, fmt.Errorf("search %s: %w", scope, err)
	}
	var out []compile.CandidatePage
	for _, h := range hits {
		if len(out) >= limit {
			break
		}
		if f.reg.Traits(h.Type)["log"] {
			continue
		}
		page, err := q.GetPage(ctx, h.Scope, h.Slug)
		if err != nil {
			return nil, fmt.Errorf("get page %s/%s: %w", h.Scope, h.Slug, err)
		}
		if page == nil {
			continue
		}
		var claims []string
		for _, c := range page.Claims {
			if c.Status == "active" {
				claims = append(claims, c.Text)
			}
		}
		out = append(out, compile.CandidatePage{
			Path: page.Path, Slug: page.Slug, Title: page.Title, Type: page.Type, Claims: claims,
		})
	}
	return out, nil
}

// semanticQuery embeds text when an embedder is loaded. A failed embedding degrades this one
// lookup to lexical matching rather than failing the run, the same contract mcp's search has.
func (f *IndexCandidateFinder) semanticQuery(ctx context.Context, text string) *store.SemanticQuery {
	if f.embedder == nil {
		return nil
	}
	vectors, err := f.embedder.Embed(ctx, []string{text})
	if err != nil || len(vectors) != 1 {
		return nil
	}
	return &store.SemanticQuery{Model: f.embedder.Model(), Vector: vectors[0]}
}
