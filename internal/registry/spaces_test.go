package registry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dhia/balise/internal/registry"
	"github.com/stretchr/testify/require"
)

const decl = `
- name: Globex
  scope: client-globex
  filter: {tags: ["customer/globex/**"]}
  children:
    - name: Azure
      filter: {tags: ["vendor/azure/**"]}
      children:
        - name: ExpressRoute
          filter: {tags: ["vendor/azure/expressroute"]}
`

func loadSpaces(t *testing.T) *registry.Spaces {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spaces.yaml")
	require.NoError(t, os.WriteFile(path, []byte(decl), 0o644))
	spaces, err := registry.LoadSpaces(path)
	require.NoError(t, err)
	return spaces
}

func TestTreeIsNested(t *testing.T) {
	root := loadSpaces(t).Tree()[0]
	require.Equal(t, "Globex", root.Name)
	require.Equal(t, "ExpressRoute", root.Children[0].Children[0].Name)
}

func TestChildInheritsTheParentFilter(t *testing.T) {
	spaces := loadSpaces(t)
	require.True(t, spaces.Matches("Azure", []string{"customer/globex/consumer", "vendor/azure/aks"}))
	require.False(t, spaces.Matches("Azure", []string{"vendor/azure/aks"}),
		"the inherited customer filter must still apply")
}

func TestGlobMatchesDescendants(t *testing.T) {
	require.True(t, loadSpaces(t).Matches("Globex", []string{"customer/globex/labs"}))
}

func TestExactFilterDoesNotMatchADescendant(t *testing.T) {
	require.False(t, loadSpaces(t).Matches("ExpressRoute",
		[]string{"customer/globex", "vendor/azure/expressroute/direct"}))
}

func TestEverySpaceCarriesItsRootScope(t *testing.T) {
	require.Equal(t, "client-globex", loadSpaces(t).ScopeOf("ExpressRoute"))
}

func TestUnknownSpaceMatchesNothing(t *testing.T) {
	require.False(t, loadSpaces(t).Matches("Nope", []string{"customer/globex"}))
	require.Empty(t, loadSpaces(t).ScopeOf("Nope"))
}

func TestTreeReturnsACopyOfTheTree(t *testing.T) {
	spaces := loadSpaces(t)
	roots := spaces.Tree()
	roots[0].Name = "mutated"
	roots[0].TagFilters[0] = "mutated"
	roots[0].Children[0].Name = "mutated-child"

	again := spaces.Tree()
	require.Equal(t, "Globex", again[0].Name, "the registry's own root must not be mutated")
	require.Equal(t, "customer/globex/**", again[0].TagFilters[0], "the registry's own tag filters must not be mutated")
	require.Equal(t, "Azure", again[0].Children[0].Name, "the registry's own children must not be mutated")
}
