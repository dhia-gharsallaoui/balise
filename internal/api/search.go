package api

import (
	"net/http"

	"github.com/dhia/balise/internal/search"
)

func (s *server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	includeHistorical := r.URL.Query().Get("historical") == "true"

	hits, err := s.q().SearchClaims(r.Context(), query, includeHistorical, 30)
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
