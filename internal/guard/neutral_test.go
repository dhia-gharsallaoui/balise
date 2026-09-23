package guard_test

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dhia/balise/internal/indexer"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/dhia/balise/internal/vault"
	"github.com/stretchr/testify/require"
)

var typeNames = []string{
	"gotcha", "decision", "state", "procedure", "incident", "issue", "memory",
}

var enginePackages = []string{
	"../indexer", "../search", "../store", "../vault", "../compile",
}

// Engine packages must switch on traits, never on a type name. Domain vocabulary
// belongs in defaults/types/*.yaml — 04 section 8.
func TestEnginePackagesNeverMentionATypeName(t *testing.T) {
	var offenders []string

	for _, pkg := range enginePackages {
		files, err := filepath.Glob(filepath.Join(pkg, "*.go"))
		require.NoError(t, err)

		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			require.NoError(t, err)

			ast.Inspect(file, func(node ast.Node) bool {
				lit, ok := node.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				value := strings.ToLower(strings.Trim(lit.Value, "`\""))
				for _, name := range typeNames {
					if value == name {
						offenders = append(offenders,
							fmt.Sprintf("%s:%d mentions %q", path, fset.Position(lit.Pos()).Line, name))
					}
				}
				return true
			})
		}
	}

	require.Empty(t, offenders,
		"engine packages must switch on traits, not type names: %s", strings.Join(offenders, "; "))
}

// 04 section 8: delete every type file except note.yaml and the engine still works.
func TestIndexingWorksWithOnlyTheNoteType(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "note.yaml"),
		[]byte("name: note\ntraits: [indexed]\n"), 0o644))

	reg, err := registry.Load(dir)
	require.NoError(t, err)
	require.Equal(t, []string{"note"}, reg.Names())

	facets, err := registry.LoadFacets(dir)
	require.NoError(t, err)

	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	raw := "---\nuid: 01J8Z3K9V6Q2M4N7P8R9S0T1U2\nslug: x\ntype: gotcha\nscope: work\n" +
		"title: A page whose type no longer exists\nclaims:\n  - text: It still indexes\n    status: active\n---\nbody\n"

	result, err := indexer.IndexPage(context.Background(), q, "work/x.md", raw, "v",
		indexer.Context{Registry: reg, Facets: facets,
			Slugs: map[vault.ScopedSlug]string{}, Aliases: vault.AliasRegistry{}})
	require.NoError(t, err)
	require.True(t, result.Indexed)

	page, err := q.GetPage(context.Background(), "work", "x")
	require.NoError(t, err)
	require.NotNil(t, page, "a page whose type was deleted must still index and be readable")
}
