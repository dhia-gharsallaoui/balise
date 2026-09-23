package api

import (
	"context"
	"net/http"

	"github.com/dhia/balise/internal/search"
	"github.com/dhia/balise/internal/store"
)

func (s *server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	includeHistorical := r.URL.Query().Get("historical") == "true"

	hits, err := s.q().SearchClaims(r.Context(), query, includeHistorical, 30, s.semanticQuery(r.Context(), query))
	if err != nil {
		writeServerError(w, r, err)
		return
	}

	out := make([]map[string]any, 0, len(hits))
	for _, hit := range hits {
		out = append(out, map[string]any{
			"slug": hit.Slug, "title": hit.Title, "type": hit.Type, "status": hit.Status,
			"scope": hit.Scope, "matched_claims": hit.MatchedClaims, "why": hit.Why,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"hits": out, "coverage": search.AssessCoverage(query, hits),
	})
}

// semanticQuery embeds query through s.embedder, when one is configured, into the
// *store.SemanticQuery SearchClaims's third retrieval arm needs. It returns nil -- disabling
// that arm for this one request, never for the whole server -- both when no embedder is
// configured at all and when embedding this particular query text fails for any reason: a
// transient embedding failure must degrade to lexical-only search, not turn into a 500.
func (s *server) semanticQuery(ctx context.Context, query string) *store.SemanticQuery {
	if s.embedder == nil {
		return nil
	}
	vectors, err := s.embedder.Embed(ctx, []string{query})
	if err != nil || len(vectors) != 1 {
		return nil
	}
	return &store.SemanticQuery{Model: s.embedder.Model(), Vector: vectors[0]}
}
