package registry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/vault"
	"github.com/stretchr/testify/require"
)

// Unlike tenants and order, an empty declared-scopes list is the normal starting state, not
// an error: defaults/scopes.yaml ships with `scopes: []` and nothing has been added yet.
func TestLoadDeclaredScopesAcceptsAnEmptyList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scopes.yaml")
	require.NoError(t, os.WriteFile(path, []byte("scopes: []\n"), 0o644))

	declared, err := registry.LoadDeclaredScopes(path)
	require.NoError(t, err)
	require.Empty(t, declared.Names())
}

func TestLoadDeclaredScopesRejectsAMissingFile(t *testing.T) {
	_, err := registry.LoadDeclaredScopes(filepath.Join(t.TempDir(), "missing.yaml"))
	require.Error(t, err)
}

func TestLoadDeclaredScopesReadsThePopulatedList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scopes.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`scopes: ["sandbox", "incubator"]`+"\n"), 0o644))

	declared, err := registry.LoadDeclaredScopes(path)
	require.NoError(t, err)
	require.Equal(t, []string{"sandbox", "incubator"}, declared.Names())
	require.True(t, declared.Has("sandbox"))
	require.False(t, declared.Has("client-globex"))
}

func TestAppendDeclaredScopeAddsAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scopes.yaml")
	require.NoError(t, os.WriteFile(path, []byte("# a comment that must survive\nscopes: []\n"), 0o644))

	declared, err := registry.AppendDeclaredScope(path, "sandbox")
	require.NoError(t, err)
	require.Equal(t, []string{"sandbox"}, declared.Names())

	// The append must be durable — a fresh load from disk sees it too.
	reloaded, err := registry.LoadDeclaredScopes(path)
	require.NoError(t, err)
	require.Equal(t, []string{"sandbox"}, reloaded.Names())

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(body), "a comment that must survive", "the header must not be dropped by a rewrite")

	declared2, err := registry.AppendDeclaredScope(path, "incubator")
	require.NoError(t, err)
	require.Equal(t, []string{"sandbox", "incubator"}, declared2.Names())
}

func TestAppendDeclaredScopeRejectsADuplicate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scopes.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`scopes: ["sandbox"]`+"\n"), 0o644))

	_, err := registry.AppendDeclaredScope(path, "sandbox")
	require.ErrorIs(t, err, registry.ErrScopeAlreadyDeclared)
}

// AppendDeclaredScope must reuse vault.ValidateSegment, not a second hand-rolled check, so
// hostile input is rejected identically everywhere a scope or agent name is accepted.
func TestAppendDeclaredScopeRejectsHostileNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scopes.yaml")
	require.NoError(t, os.WriteFile(path, []byte("scopes: []\n"), 0o644))

	for _, name := range []string{"", "..", ".", "../escape", "a/b", "a\\b", "   "} {
		_, err := registry.AppendDeclaredScope(path, name)
		require.Errorf(t, err, "expected %q to be rejected", name)
		require.ErrorIsf(t, err, vault.ErrInvalidSegment, "expected %q to fail ValidateSegment specifically", name)
	}
}
