package registry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dhia/balise/internal/registry"
	"github.com/stretchr/testify/require"
)

const vocab = `
name: vendor
values:
  azure:
    aliases: [az, microsoft-azure]
    children:
      expressroute: {aliases: [er, ergw]}
      aks: {}
  fortinet: {}
`

func loadFacets(t *testing.T) *registry.Facets {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vendor.yaml"), []byte(vocab), 0o644))
	facets, err := registry.LoadFacets(dir)
	require.NoError(t, err)
	return facets
}

func TestSlashBecomesDot(t *testing.T) {
	got, ok := loadFacets(t).ToLtree("vendor/azure/expressroute")
	require.True(t, ok)
	require.Equal(t, "vendor.azure.expressroute", got)
}

func TestAliasResolvesToTheFullPath(t *testing.T) {
	facets := loadFacets(t)
	require.Equal(t, "vendor/azure/expressroute", facets.ResolveAlias("er"))
	require.Equal(t, "vendor/azure/expressroute", facets.ResolveAlias("ergw"))
	require.Equal(t, "vendor/azure", facets.ResolveAlias("az"))
}

func TestUnknownTagIsReturnedUnchanged(t *testing.T) {
	require.Equal(t, "layer/network", loadFacets(t).ResolveAlias("layer/network"))
}

func TestKnownValuesAreRecognised(t *testing.T) {
	facets := loadFacets(t)
	require.True(t, facets.IsKnown("vendor/azure/aks"))
	require.False(t, facets.IsKnown("vendor/azure/nonsense"))
}

func TestMalformedTagsAreRejected(t *testing.T) {
	facets := loadFacets(t)
	for _, tag := range []string{"a/b/c/d/e", "vendor/Azure!", "", "vendor//azure"} {
		_, ok := facets.ToLtree(tag)
		require.False(t, ok, tag)
	}
}

// TestHyphenatedTagIsRejectedNotMangled is a regression test for a real bug found running
// Task 15's acceptance suite against the real corpus: the page
// alloy-duplicate-component-kills-telemetry.md carries the tag `vendor/grafana-alloy`.
// facetSegment used to allow hyphens (`^[a-z0-9-]+$`), so ToLtree converted this to
// "vendor.grafana-alloy" — syntax Postgres's ltree type rejects outright ("ltree syntax
// error"), which failed the whole INSERT instead of surfacing as an out_of_vocabulary
// finding as 04 section 7 step 9 requires for any out-of-vocabulary tag. A hyphenated
// segment must fail ToLtree here, at the boundary, not at the database.
func TestHyphenatedTagIsRejectedNotMangled(t *testing.T) {
	_, ok := loadFacets(t).ToLtree("vendor/grafana-alloy")
	require.False(t, ok, "vendor/grafana-alloy")
}

func TestAnAliasIsExpandedBeforeConversion(t *testing.T) {
	got, ok := loadFacets(t).ToLtree("er")
	require.True(t, ok)
	require.Equal(t, "vendor.azure.expressroute", got)
}

// A file with no .example sibling (vendor.yaml here) loads exactly as it always did.
func TestLoadFacetsLoadsAFileWithNoExampleSibling(t *testing.T) {
	facets := loadFacets(t)
	require.True(t, facets.IsKnown("vendor/fortinet"))
}

// customer.example.yaml is loaded only when customer.yaml (the real, gitignored file) is
// absent from the directory.
func TestLoadFacetsFallsBackToExampleWhenRealFileIsAbsent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "customer.example.yaml"),
		[]byte("name: customer\nvalues:\n  globex: {}\n"), 0o644))

	facets, err := registry.LoadFacets(dir)
	require.NoError(t, err)
	require.True(t, facets.IsKnown("customer/globex"))
}

// customer.yaml, when present, is used instead of customer.example.yaml — the real file
// always wins.
func TestLoadFacetsPrefersRealFileOverExample(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "customer.yaml"),
		[]byte("name: customer\nvalues:\n  umbrella: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "customer.example.yaml"),
		[]byte("name: customer\nvalues:\n  initech: {}\n"), 0o644))

	facets, err := registry.LoadFacets(dir)
	require.NoError(t, err)
	require.True(t, facets.IsKnown("customer/umbrella"))
	require.False(t, facets.IsKnown("customer/initech"))
}
