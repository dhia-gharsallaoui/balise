package registry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dhia/balise/internal/registry"
	"github.com/stretchr/testify/require"
)

// A missing or empty tenants list must fail loudly: silently falling back to "nothing is a
// tenant" (or worse, "everything is") would quietly change the security partition with no
// error anywhere.
func TestLoadTenantsRejectsAMissingTenantsKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tenants.yaml")
	require.NoError(t, os.WriteFile(path, []byte("other: value\n"), 0o644))

	_, err := registry.LoadTenants(path)
	require.Error(t, err)
}

// A null value is as ambiguous as an absent key -- `tenants:` with nothing after it is far
// more likely to be a truncated edit than a deliberate statement -- so it is refused too.
func TestLoadTenantsRejectsANullTenantsValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tenants.yaml")
	require.NoError(t, os.WriteFile(path, []byte("tenants:\n"), 0o644))

	_, err := registry.LoadTenants(path)
	require.Error(t, err)
}

// An explicit empty list is the opposite of a silent fallback: it is the owner declaring
// that this vault has no external customers at all, which is the normal case for a
// personal or single-owner vault. Rejecting it made that vault unrepresentable -- the
// registry would not load and /api/settings failed outright -- while protecting nothing,
// since the security risk the guard exists for is an *unnoticed* empty partition.
func TestLoadTenantsAcceptsAnExplicitlyEmptyList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tenants.yaml")
	require.NoError(t, os.WriteFile(path, []byte("tenants: []\n"), 0o644))

	tenants, err := registry.LoadTenants(path)
	require.NoError(t, err)
	require.False(t, tenants.Is("globex"), "an empty list means nothing is a tenant")
	require.False(t, tenants.Is(""), "including the empty string")
}

// A self-contained fixture, not the repo's own defaults/tenants.yaml: that file holds the
// owner's real (gitignored) customer list locally, so a test asserting on its exact
// contents would be asserting on private data that is absent from a fresh clone.
func TestLoadTenantsAcceptsAPopulatedList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tenants.yaml")
	require.NoError(t, os.WriteFile(path, []byte("tenants: [globex, acme]\n"), 0o644))

	tenants, err := registry.LoadTenants(path)
	require.NoError(t, err)
	require.True(t, tenants.Is("globex"))
	require.True(t, tenants.Is("acme"))
	require.False(t, tenants.Is("colo"), "colo is a datacenter, not a tenant")
	require.False(t, tenants.Is("platform"), "platform is the owner's own internal category, not a tenant")
}

// LoadTenants falls back to the tracked .example.yaml sibling when the real file is
// absent, so a fresh clone still has a working, generic default.
func TestLoadTenantsFallsBackToExampleWhenRealFileIsAbsent(t *testing.T) {
	dir := t.TempDir()
	example := filepath.Join(dir, "tenants.example.yaml")
	require.NoError(t, os.WriteFile(example, []byte("tenants: [initech]\n"), 0o644))

	tenants, err := registry.LoadTenants(filepath.Join(dir, "tenants.yaml"))
	require.NoError(t, err)
	require.True(t, tenants.Is("initech"))
}

// The real file, when present, always wins over its .example sibling.
func TestLoadTenantsPrefersRealFileOverExample(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tenants.yaml"), []byte("tenants: [umbrella]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tenants.example.yaml"), []byte("tenants: [initech]\n"), 0o644))

	tenants, err := registry.LoadTenants(filepath.Join(dir, "tenants.yaml"))
	require.NoError(t, err)
	require.True(t, tenants.Is("umbrella"))
	require.False(t, tenants.Is("initech"))
}
