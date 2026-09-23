package cli_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/dhia/balise/internal/cli"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/stretchr/testify/require"
)

// pageAt renders a minimal valid page for a vault fixture.
func pageAt(scope, slug, uid string) store.Change {
	return store.Change{
		Path: fmt.Sprintf("%s/gotchas/%s.md", scope, slug),
		Data: []byte(fmt.Sprintf(
			"---\nuid: %s\nslug: %s\ntype: gotcha\nscope: %s\ntitle: %s\n"+
				"claims:\n  - text: a claim\n    status: active\n---\nbody\n",
			uid, slug, scope, slug)),
	}
}

func slugScopes(t *testing.T, q *store.Queries) map[string]string {
	t.Helper()
	rows, err := q.ListPages(context.Background(), store.PageFilter{IncludeHistorical: true})
	require.NoError(t, err)
	out := map[string]string{}
	for _, row := range rows {
		require.NotContains(t, out, row.Slug, "a slug must not exist in two scopes at once")
		out[row.Slug] = row.Scope
	}
	return out
}

func newVault(t *testing.T) (*store.GitPageStore, *store.Queries) {
	t.Helper()
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	_, err = pages.Commit([]store.Change{
		pageAt("work", "alpha", "01J8Z3K9V6Q2M4N7P8R9S0T1A1"),
		pageAt("work", "beta", "01J8Z3K9V6Q2M4N7P8R9S0T1B2"),
	}, "Dhia <d@x>", "seed")
	require.NoError(t, err)
	return pages, store.NewQueries(testutil.NewDB(t), store.Scopes{"work", "client-globex"})
}

// TestReindexPrunesAPageThatLeftTheVault: nothing in the system deleted a document before
// this, so a page removed from the vault stayed in the index — with its body and claims —
// served by /api/pages, /api/search and /api/graph forever.
func TestReindexPrunesAPageThatLeftTheVault(t *testing.T) {
	pages, q := newVault(t)
	ctx := context.Background()

	report, err := cli.Reindex(ctx, q, pages, "../../defaults", nil)
	require.NoError(t, err)
	require.Equal(t, 2, report.Pages)
	require.Zero(t, report.Pruned)

	_, err = pages.Commit([]store.Change{{Path: "work/gotchas/beta.md", Delete: true}},
		"Dhia <d@x>", "retire beta")
	require.NoError(t, err)

	report, err = cli.Reindex(ctx, q, pages, "../../defaults", nil)
	require.NoError(t, err)
	require.Equal(t, 1, report.Pruned)
	require.Equal(t, map[string]string{"alpha": "work"}, slugScopes(t, q))
}

// TestReindexAfterAScopeCorrectionLeavesOneCopy is the index half of the correction workflow:
// a page whose tenant was corrected must end up in exactly one scope. With its uid preserved
// the row moves in place; the prune is what guarantees the outcome either way.
func TestReindexAfterAScopeCorrectionLeavesOneCopy(t *testing.T) {
	pages, q := newVault(t)
	ctx := context.Background()
	_, err := cli.Reindex(ctx, q, pages, "../../defaults", nil)
	require.NoError(t, err)

	_, err = pages.Commit([]store.Change{
		pageAt("client-globex", "beta", "01J8Z3K9V6Q2M4N7P8R9S0T1B2"),
		{Path: "work/gotchas/beta.md", Delete: true},
	}, "Dhia <d@x>", "beta belongs to client-globex")
	require.NoError(t, err)

	_, err = cli.Reindex(ctx, q, pages, "../../defaults", nil)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"alpha": "work", "beta": "client-globex"}, slugScopes(t, q))
}

// TestReindexPrunesAMovedPageThatWasGivenANewIdentity is the same correction with the uid not
// preserved — the shape the importer had before it learned to keep one. The new row inserts
// cleanly under the new scope because (scope, slug) is free there, so without a prune the
// customer's content exists in both tenants' scopes at once.
func TestReindexPrunesAMovedPageThatWasGivenANewIdentity(t *testing.T) {
	pages, q := newVault(t)
	ctx := context.Background()
	_, err := cli.Reindex(ctx, q, pages, "../../defaults", nil)
	require.NoError(t, err)

	_, err = pages.Commit([]store.Change{
		pageAt("client-globex", "beta", "01J8Z3K9V6Q2M4N7P8R9S0T1B9"),
		{Path: "work/gotchas/beta.md", Delete: true},
	}, "Dhia <d@x>", "beta re-minted under client-globex")
	require.NoError(t, err)

	report, err := cli.Reindex(ctx, q, pages, "../../defaults", nil)
	require.NoError(t, err)
	require.Equal(t, 1, report.Pruned)
	require.Equal(t, map[string]string{"alpha": "work", "beta": "client-globex"}, slugScopes(t, q))
}

// TestReindexIsNoopForABodyIdenticalPageInAnotherScope pins the isNoop fix. gitVersion is a
// per-file blob hash, so two byte-identical pages with the same slug in different scopes used
// to compare equal through a slug-only lookup: the second reported Indexed=true,
// Changed=false and was never actually indexed, with no finding to say so.
func TestReindexIsNoopForABodyIdenticalPageInAnotherScope(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	// Same slug, same body, same bytes but for the scope line — two different customers.
	_, err = pages.Commit([]store.Change{
		pageAt("work", "vpn", "01J8Z3K9V6Q2M4N7P8R9S0T1C1"),
		pageAt("client-globex", "vpn", "01J8Z3K9V6Q2M4N7P8R9S0T1C2"),
	}, "Dhia <d@x>", "one slug, two scopes")
	require.NoError(t, err)

	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work", "client-globex"})
	ctx := context.Background()
	report, err := cli.Reindex(ctx, q, pages, "../../defaults", nil)
	require.NoError(t, err)
	require.Equal(t, 2, report.Changed, "both pages must actually be indexed")

	rows, err := q.ListPages(ctx, store.PageFilter{IncludeHistorical: true})
	require.NoError(t, err)
	require.Len(t, rows, 2)

	second, err := cli.Reindex(ctx, q, pages, "../../defaults", nil)
	require.NoError(t, err)
	require.Zero(t, second.Changed, "and re-indexing them unchanged must still be a no-op")
	require.Zero(t, second.Pruned)
}
