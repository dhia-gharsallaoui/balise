package store_test

import (
	"context"
	"sort"
	"testing"

	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestDiscoverVaultScopesFindsTopLevelDirsWithMarkdown(t *testing.T) {
	s := newStore(t)
	_, err := s.Commit([]store.Change{
		{Path: "work/a.md", Data: []byte("a")},
		{Path: "client-globex/b.md", Data: []byte("b")},
		{Path: "work/c.md", Data: []byte("c")},
	}, "Dhia <d@x>", "seed")
	require.NoError(t, err)

	scopes, err := store.DiscoverVaultScopes(s)
	require.NoError(t, err)
	require.Equal(t, []string{"client-globex", "work"}, scopes)
}

func TestDiscoverVaultScopesIgnoresReservedTopLevelDirs(t *testing.T) {
	s := newStore(t)
	_, err := s.Commit([]store.Change{
		{Path: ".balise/types/note.md", Data: []byte("type")},
		{Path: "review/01ABC.md", Data: []byte("proposal")},
		{Path: "work/a.md", Data: []byte("a")},
	}, "Dhia <d@x>", "seed")
	require.NoError(t, err)

	scopes, err := store.DiscoverVaultScopes(s)
	require.NoError(t, err)
	require.Equal(t, []string{"work"}, scopes)
}

func TestDiscoverVaultScopesIgnoresNonMarkdown(t *testing.T) {
	s := newStore(t)
	_, err := s.Commit([]store.Change{
		{Path: "work/notes.txt", Data: []byte("not markdown")},
	}, "Dhia <d@x>", "seed")
	require.NoError(t, err)

	scopes, err := store.DiscoverVaultScopes(s)
	require.NoError(t, err)
	require.Empty(t, scopes)
}

func TestUnionScopesDeduplicatesPreservingFirstSeenOrder(t *testing.T) {
	got := store.UnionScopes([]string{"b", "a"}, []string{"a", "c"}, []string{"d"})
	require.Equal(t, []string{"b", "a", "c", "d"}, got)
}

func TestUnionScopesHandlesNoGroups(t *testing.T) {
	require.Empty(t, store.UnionScopes())
}

func TestEffectiveScopesFallsBackWhenNothingIsKnown(t *testing.T) {
	pool := testutil.NewDB(t)
	s := newStore(t) // empty vault, no commits

	got, err := store.EffectiveScopes(context.Background(), pool, s, nil)
	require.NoError(t, err)
	sort.Strings(got)
	want := []string{"client-globex", "personal", "work"}
	sort.Strings(want)
	require.Equal(t, want, got)
}

// A scope declared in defaults/scopes.yaml but backed by zero database rows and zero vault
// pages must still show up — that is the entire point of declaring one ahead of time.
func TestEffectiveScopesIncludesADeclaredScopeWithNoPagesAndNoRows(t *testing.T) {
	pool := testutil.NewDB(t)
	s := newStore(t)

	got, err := store.EffectiveScopes(context.Background(), pool, s, []string{"sandbox"})
	require.NoError(t, err)
	require.Contains(t, got, "sandbox")
}

func TestEffectiveScopesUnionsDatabaseVaultAndDeclared(t *testing.T) {
	pool := testutil.NewDB(t)
	s := newStore(t)
	_, err := s.Commit([]store.Change{
		{Path: "vault-only/a.md", Data: []byte("a")},
	}, "Dhia <d@x>", "seed")
	require.NoError(t, err)

	got, err := store.EffectiveScopes(context.Background(), pool, s, []string{"declared-only"})
	require.NoError(t, err)
	require.Contains(t, got, "vault-only")
	require.Contains(t, got, "declared-only")
}
