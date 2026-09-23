package registry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dhia/balise/internal/registry"
	"github.com/stretchr/testify/require"
)

const defaults = "../../defaults/types"

func TestLoadsTheEightShippedTypes(t *testing.T) {
	reg, err := registry.Load(defaults)
	require.NoError(t, err)
	require.Equal(t, []string{
		"decision", "gotcha", "incident", "issue", "memory", "note", "procedure", "state",
	}, reg.Names())
}

func TestTraitsComeFromTheFile(t *testing.T) {
	reg, err := registry.Load(defaults)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"indexed": true, "cited": true, "temporal": true},
		reg.Traits("gotcha"))
}

func TestLimitsDefaultWhenAbsent(t *testing.T) {
	dir := writeTypes(t, map[string]string{"bare.yaml": "name: bare\ntraits: [indexed]\n"})
	reg, err := registry.Load(dir)
	require.NoError(t, err)

	maxTokens, maxClaims := reg.Limits("bare")
	require.Equal(t, 1200, maxTokens)
	require.Equal(t, 6, maxClaims)
}

func TestUnknownTypeIsNotFound(t *testing.T) {
	reg, _ := registry.Load(defaults)
	_, ok := reg.Get("nope")
	require.False(t, ok)
}

func TestUserFileOverridesAShippedDefault(t *testing.T) {
	dir := writeTypes(t, map[string]string{
		"gotcha.yaml": "name: gotcha\ntraits: [indexed]\nmax_tokens: 999\n",
	})
	reg, err := registry.Load(defaults, dir)
	require.NoError(t, err)

	maxTokens, _ := reg.Limits("gotcha")
	require.Equal(t, 999, maxTokens)

	def, ok := reg.Get("gotcha")
	require.True(t, ok)
	require.True(t, def.OverridesDefault)
}

func TestInvalidTraitIsReportedNotFatal(t *testing.T) {
	dir := writeTypes(t, map[string]string{"bad.yaml": "name: bad\ntraits: [teleporting]\n"})
	reg, err := registry.Load(dir)
	require.NoError(t, err, "a bad file must not fail the whole load")

	_, ok := reg.Get("bad")
	require.False(t, ok)
	require.Len(t, reg.Errors(), 1)
	require.Contains(t, reg.Errors()[0], "teleporting")
}

func TestOneBadFileDoesNotStopTheGoodOnes(t *testing.T) {
	dir := writeTypes(t, map[string]string{
		"bad.yaml":  "name: bad\ntraits: [teleporting]\n",
		"good.yaml": "name: good\ntraits: [indexed]\n",
	})
	reg, err := registry.Load(dir)
	require.NoError(t, err)

	_, ok := reg.Get("good")
	require.True(t, ok)
	require.Len(t, reg.Errors(), 1)
}

func writeTypes(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}
	return dir
}

func TestTraitsReturnsACopy(t *testing.T) {
	reg, err := registry.Load(defaults)
	require.NoError(t, err)

	traits := reg.Traits("gotcha")
	traits["mutated"] = true

	require.False(t, reg.Traits("gotcha")["mutated"], "the registry's own trait set must not be mutated")
}

func TestGetReturnsATypeDefThatCannotMutateTheRegistry(t *testing.T) {
	reg, err := registry.Load(defaults)
	require.NoError(t, err)

	def, ok := reg.Get("gotcha")
	require.True(t, ok)
	def.Fields["mutated"] = registry.FieldDef{Type: "string"}
	def.Traits["mutated"] = true

	again, ok := reg.Get("gotcha")
	require.True(t, ok)
	_, fieldExists := again.Fields["mutated"]
	require.False(t, fieldExists, "the registry's own field map must not be mutated")
	require.False(t, again.Traits["mutated"], "the registry's own trait map must not be mutated")
}
