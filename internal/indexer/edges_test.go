package indexer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dhia/balise/internal/indexer"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/vault"
	"github.com/stretchr/testify/require"
)

func edgeRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "state.yaml"), []byte(
		"name: state\ntraits: [indexed]\nfields:\n"+
			"  instance_of: {type: ref, to: entity, role: specializes}\n"+
			"  superseded_by: {type: ref, to: state, role: supersedes}\n"+
			"  about: {type: \"ref[]\", to: any, role: about}\n"), 0o644))
	reg, err := registry.Load(dir)
	require.NoError(t, err)
	return reg
}

func edgesOf(t *testing.T, source string) []indexer.Edge {
	t.Helper()
	page, err := vault.Parse(source)
	require.NoError(t, err)
	return indexer.EdgesFrom(page, edgeRegistry(t))
}

func TestRefFieldBecomesATypedEdge(t *testing.T) {
	edges := edgesOf(t, "---\ntype: state\ninstance_of: expressroute\n---\nbody\n")
	require.Equal(t, []indexer.Edge{
		{ToRef: "expressroute", Kind: "specializes", Field: "instance_of"},
	}, edges)
}

func TestRefListFieldYieldsOneEdgePerTarget(t *testing.T) {
	edges := edgesOf(t, "---\ntype: state\nabout: [a, b]\n---\nbody\n")
	require.Len(t, edges, 2)
	for _, edge := range edges {
		require.Equal(t, "about", edge.Kind)
	}
}

func TestBodyWikilinkBecomesAnUntypedLink(t *testing.T) {
	edges := edgesOf(t, "---\ntype: state\n---\nsee [[other-page]]\n")
	require.Equal(t, []indexer.Edge{{ToRef: "other-page", Kind: "link"}}, edges)
}

func TestUnknownFrontmatterKeyIsNotAnEdge(t *testing.T) {
	require.Empty(t, edgesOf(t, "---\ntype: state\noriginSessionId: bf52\n---\nbody\n"))
}

func TestEmptyRefIsSkipped(t *testing.T) {
	require.Empty(t, edgesOf(t, "---\ntype: state\ninstance_of:\n---\nbody\n"))
}

func TestEdgesAreDeduplicated(t *testing.T) {
	edges := edgesOf(t, "---\ntype: state\n---\n[[a]] and again [[a]]\n")
	require.Len(t, edges, 1)
}

func TestUnknownTypeYieldsOnlyBodyLinks(t *testing.T) {
	edges := edgesOf(t, "---\ntype: nosuchtype\ninstance_of: x\n---\n[[a]]\n")
	require.Equal(t, []indexer.Edge{{ToRef: "a", Kind: "link"}}, edges)
}

// TestEdgesFromSortOrderIsDeterministic pins the exact output order of
// EdgesFrom, not just membership. def.Fields is a map, so without sorting,
// the three edges that share ToRef "alpha" (a typed ref, an "about" list
// member, and a body link) would come back in whatever order Go's map
// iteration happens to visit fields — different from run to run. This test
// has four distinct ToRef values (alpha, beta, delta, gamma) so the first
// comparator term (ToRef) is exercised, and three edges sharing ToRef
// "alpha" with different Kind/Field so the second and third comparator
// terms (Kind, then Field) are exercised too.
func TestEdgesFromSortOrderIsDeterministic(t *testing.T) {
	edges := edgesOf(t, "---\ntype: state\ninstance_of: alpha\nsuperseded_by: beta\n"+
		"about: [alpha, gamma]\n---\nsee [[alpha]] and [[delta]]\n")

	require.Equal(t, []indexer.Edge{
		{ToRef: "alpha", Kind: "about", Field: "about"},
		{ToRef: "alpha", Kind: "link"},
		{ToRef: "alpha", Kind: "specializes", Field: "instance_of"},
		{ToRef: "beta", Kind: "supersedes", Field: "superseded_by"},
		{ToRef: "delta", Kind: "link"},
		{ToRef: "gamma", Kind: "about", Field: "about"},
	}, edges)
}
