package search_test

import (
	"testing"

	"github.com/dhia/balise/internal/search"
	"github.com/dhia/balise/internal/store"
	"github.com/stretchr/testify/require"
)

func TestCoverage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
		hits  []store.Hit
		want  string
	}{
		{"empty results", "anything at all", nil, "low"},
		{"shared term", "gateway connection loss",
			[]store.Hit{{MatchedClaims: []string{"Updating the gateway removes every connection"}}}, "ok"},
		{"no shared term", "quarterly revenue forecast",
			[]store.Hit{{MatchedClaims: []string{"Pod IPs never appear on the VNet"}}}, "low"},
		{"stopwords alone", "what is the and of it",
			[]store.Hit{{MatchedClaims: []string{"The gateway is in the hub"}}}, "low"},
		{"case insensitive", "gateway",
			[]store.Hit{{MatchedClaims: []string{"GATEWAY behaviour"}}}, "ok"},
		{"falls back to title", "gateway",
			[]store.Hit{{Title: "Gateway update deletes connections"}}, "ok"},
		{"short words ignored", "an id",
			[]store.Hit{{MatchedClaims: []string{"an id is not a term"}}}, "low"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, search.AssessCoverage(tc.query, tc.hits))
		})
	}
}
