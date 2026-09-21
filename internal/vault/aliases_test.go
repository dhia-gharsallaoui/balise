package vault_test

import (
	"testing"

	"github.com/dhia/balise/internal/vault"
	"github.com/stretchr/testify/require"
)

func TestRegisterReturnsANewRegistry(t *testing.T) {
	empty := vault.AliasRegistry{}
	one := empty.Register("ergw", "u1", "work")
	require.Empty(t, empty.LookupExact("ergw", "work"))
	require.Equal(t, "u1", one.LookupExact("ergw", "work"))
}

func TestLookupIsScoped(t *testing.T) {
	reg := vault.AliasRegistry{}.Register("ergw", "u1", "work")
	require.Empty(t, reg.LookupExact("ergw", "personal"))
}

func TestSameAliasInTwoScopesIsNotACollision(t *testing.T) {
	reg := vault.AliasRegistry{}.Register("ergw", "u1", "work").Register("ergw", "u2", "personal")
	require.Empty(t, reg.Collisions())
}

func TestSameAliasTwiceInOneScopeIsACollision(t *testing.T) {
	reg := vault.AliasRegistry{}.Register("ergw", "u1", "work").Register("ergw", "u2", "work")
	collisions := reg.Collisions()
	require.Len(t, collisions, 1)
	require.Equal(t, "ergw", collisions[0].Alias)
	require.ElementsMatch(t, []string{"u1", "u2"}, collisions[0].UIDs)
}

func TestRegisteringTheSameUIDTwiceIsNotACollision(t *testing.T) {
	reg := vault.AliasRegistry{}.Register("ergw", "u1", "work").Register("ergw", "u1", "work")
	require.Empty(t, reg.Collisions())
}

func TestAnAmbiguousAliasResolvesToNothing(t *testing.T) {
	reg := vault.AliasRegistry{}.Register("ergw", "u1", "work").Register("ergw", "u2", "work")
	require.Empty(t, reg.LookupExact("ergw", "work"),
		"a colliding alias must resolve to nothing rather than guess")
}

func TestNormalisedLookupMatchesAcrossSnakeAndKebab(t *testing.T) {
	reg := vault.AliasRegistry{}.Register("azapi_ergw_connection_deletion", "u1", "work")
	require.Equal(t, "u1", reg.LookupNormalised("azapi-ergw-connection-deletion", "work"))
	require.Empty(t, reg.LookupExact("azapi-ergw-connection-deletion", "work"))
}
