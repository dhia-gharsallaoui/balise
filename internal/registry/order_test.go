package registry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dhia/balise/internal/registry"
	"github.com/stretchr/testify/require"
)

// A missing or empty agent_order must fail loudly: Rank falls back to "unknown type,
// sort last" for every name when names is empty, which would silently drop the 04
// section 11 step 3 ordering (most specific first) with no error anywhere.
func TestLoadOrderRejectsAMissingAgentOrderKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "order.yaml")
	require.NoError(t, os.WriteFile(path, []byte("other: value\n"), 0o644))

	_, err := registry.LoadOrder(path)
	require.Error(t, err)
}

func TestLoadOrderRejectsAnEmptyAgentOrderList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "order.yaml")
	require.NoError(t, os.WriteFile(path, []byte("agent_order: []\n"), 0o644))

	_, err := registry.LoadOrder(path)
	require.Error(t, err)
}

func TestLoadOrderAcceptsAPopulatedList(t *testing.T) {
	order, err := registry.LoadOrder("../../defaults/order.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, order.Names())
}
