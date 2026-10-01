package cli_test

import (
	"context"
	"testing"

	"github.com/dhia/balise/internal/cli"
	"github.com/dhia/balise/internal/store"
	"github.com/stretchr/testify/require"
)

// TestRememberedNotesAreSearchableAfterReindex pins the promise remember makes: a note an
// agent saves can be found again. It runs the real path, a daily memory file committed to a
// git vault in remember's own format, indexed by Reindex against the shipped registry, then
// searched across the scope the token holds.
func TestRememberedNotesAreSearchableAfterReindex(t *testing.T) {
	pages, q := newVault(t)
	ctx := context.Background()
	_, err := pages.Commit([]store.Change{{
		Path: "work/memory/dapple-controlplane/2026-10-01.md",
		Data: []byte("---\nuid: 01J8Z3K9V6Q2M4N7P8R9S0T1M1\nslug: \"2026-10-01\"\ntype: memory\n" +
			"scope: work\ntitle: dapple-controlplane -- 2026-10-01\nagent: dapple-controlplane\n" +
			"date: \"2026-10-01\"\n---\n" +
			"- **06:52:27 UTC** Dell Enterprise SONiC holds config in sonic-cli; SSH the mgmt IP instead\n"),
	}}, "dapple-controlplane <agent@balise>", "remember: dapple-controlplane")
	require.NoError(t, err)

	_, err = cli.Reindex(ctx, q, pages, "../../defaults", nil)
	require.NoError(t, err)

	hits, err := q.SearchClaims(ctx, "SONiC sonic-cli", false, 10, nil)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.Equal(t, "memory", hits[0].Type)
	require.Equal(t, "dapple-controlplane -- 2026-10-01", hits[0].Title)
	require.Equal(t, []string{"Dell Enterprise SONiC holds config in sonic-cli; SSH the mgmt IP instead"},
		hits[0].MatchedClaims)
}
