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

func TestLoadTenantsRejectsAnEmptyTenantsList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tenants.yaml")
	require.NoError(t, os.WriteFile(path, []byte("tenants: []\n"), 0o644))

	_, err := registry.LoadTenants(path)
	require.Error(t, err)
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
