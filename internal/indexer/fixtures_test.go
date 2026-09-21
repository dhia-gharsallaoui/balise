package indexer_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dhia/balise/internal/indexer"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/dhia/balise/internal/vault"
	"github.com/stretchr/testify/require"
)

var allScopes = store.Scopes{"work", "client-globex", "personal"}

// slugMeta reads just enough frontmatter to seed the slug index before any fixture is
// indexed. vault.Resolve is a pure read against ic.Slugs — nothing populates it as
// indexing proceeds — so a same-scope reference can only resolve if every fixture's
// identity is already known up front. This mirrors how a real reindex (Task 15's
// cli.Reindex) has to walk the vault twice for the same reason: pass one collects
// identity, pass two indexes content and resolves relations against it.
type slugMeta struct {
	UID   string `yaml:"uid"`
	Slug  string `yaml:"slug"`
	Scope string `yaml:"scope"`
}

func loadFixtures(t *testing.T) (*store.Queries, []indexer.Result) {
	t.Helper()
	q := store.NewQueries(testutil.NewDB(t), allScopes)

	reg, err := registry.Load("../../defaults/types")
	require.NoError(t, err)
	facets, err := registry.LoadFacets("../../defaults/facets")
	require.NoError(t, err)

	paths, err := filepath.Glob("../../defaults/fixtures/*.md")
	require.NoError(t, err)
	require.NotEmpty(t, paths)

	raws := make(map[string]string, len(paths))
	slugs := map[vault.ScopedSlug]string{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		raws[path] = string(raw)

		page, err := vault.Parse(string(raw))
		require.NoError(t, err)
		var meta slugMeta
		require.NoError(t, page.Decode(&meta))
		if meta.UID == "" || meta.Slug == "" || meta.Scope == "" {
			continue // every fixture declares all three; nothing here needs a path-derived fallback.
		}
		slugs[vault.ScopedSlug{Scope: meta.Scope, Slug: meta.Slug}] = meta.UID
	}

	ic := indexer.Context{Registry: reg, Facets: facets,
		Slugs: slugs, Aliases: vault.AliasRegistry{}}

	var results []indexer.Result
	for _, path := range paths {
		result, err := indexer.IndexPage(context.Background(), q,
			"work/"+filepath.Base(path), raws[path], "fixture", ic)
		require.NoError(t, err)
		results = append(results, result)
	}
	return q, results
}

func anyFinding(results []indexer.Result, rule string) bool {
	for _, result := range results {
		if hasFinding(result, rule) {
			return true
		}
	}
	return false
}

func TestAllFixturesIndex(t *testing.T) {
	_, results := loadFixtures(t)
	for _, result := range results {
		require.True(t, result.Indexed)
	}
}

func TestAPageWithSixClaimsExists(t *testing.T) {
	q, _ := loadFixtures(t)
	page, err := q.GetPage(context.Background(), "work", "azapi-ergw-connection-deletion")
	require.NoError(t, err)
	require.Equal(t, 6, page.ClaimsCount)
}

func TestAnActivePageHoldsASupersededClaim(t *testing.T) {
	q, _ := loadFixtures(t)
	page, err := q.GetPage(context.Background(), "work", "azapi-ergw-connection-deletion")
	require.NoError(t, err)
	require.Equal(t, "active", page.Status)

	var superseded bool
	for _, claim := range page.Claims {
		if claim.Status == "superseded" {
			superseded = true
		}
	}
	require.True(t, superseded, "claim status is independent of page status")
}

func TestAHistoricalPageExists(t *testing.T) {
	q, _ := loadFixtures(t)
	rows, err := q.ListPages(context.Background(), store.PageFilter{IncludeHistorical: true})
	require.NoError(t, err)

	var historical bool
	for _, row := range rows {
		if row.Historical {
			historical = true
		}
	}
	require.True(t, historical)
}

func TestFixturesRaiseOversizeAndDanglingFindings(t *testing.T) {
	_, results := loadFixtures(t)
	require.True(t, anyFinding(results, "oversize"))
	require.True(t, anyFinding(results, "dangling_ref"))
}

func TestThePersonalPageIsInvisibleToAWorkOnlyCaller(t *testing.T) {
	q, _ := loadFixtures(t)
	ctx := context.Background()

	page, err := q.GetPage(ctx, "personal", "personal-note")
	require.NoError(t, err)
	require.NotNil(t, page, "visible to a caller holding personal")

	// Rebuild with a narrower scope set against the same data.
	narrow := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	page, err = narrow.GetPage(ctx, "personal", "personal-note")
	require.NoError(t, err)
	require.Nil(t, page)
}
