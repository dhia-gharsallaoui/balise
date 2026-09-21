package guard_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// parseStorePackage parses every non-test .go file in internal/store and returns their ASTs.
// It skips _test.go files deliberately: internal/store's own test files (queries_test.go in
// particular) legitimately construct or return a *Queries to build fixtures for their own
// assertions, and globbing those in — as the pre-round-3 version of this helper did — makes
// guards 3 and 4 fail against the store package's own tests rather than only against a real
// bypass in production code. Shared by TestNewQueriesIsTheOnlyConstructor,
// TestQueriesHasNoExportedOrEmbeddedFields and TestNoQueriesMethodReturnsThePool.
// storeFile pairs a parsed internal/store file with the FileSet it was parsed under and the
// path it came from, so a guard can report a violation as path:line instead of as a bare shape
// name. The FileSet is per file, which is all any of these guards needs: none of them compares
// positions across two files.
type storeFile struct {
	path string
	fset *token.FileSet
	file *ast.File
}

func parseStorePackage(t *testing.T) []storeFile {
	t.Helper()
	paths, err := filepath.Glob("../store/*.go")
	require.NoError(t, err)

	var files []storeFile
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		require.NoError(t, err)
		files = append(files, storeFile{path: filepath.ToSlash(path), fset: fset, file: file})
	}
	return files
}
