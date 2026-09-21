package guard_test

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// mentionsQueriesType reports whether expr's AST subtree contains the identifier "Queries"
// anywhere — as a bare type, under a *ast.StarExpr, inside an *ast.ArrayType, an *ast.MapType's
// key or value, an *ast.ChanType, a nested *ast.FuncType (a function returning a function), or
// a field of an inline *ast.StructType. One recursive containment check replaces what would
// otherwise be a shape-specific matcher per wrapper — bare pointer, slice-of-pointer,
// map-of-pointer, and so on — each of which would need to be added by hand as a new wrapper
// shape was discovered.
func mentionsQueriesType(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found {
			return false
		}
		if ident, ok := node.(*ast.Ident); ok && ident.Name == "Queries" {
			found = true
			return false
		}
		return true
	})
	return found
}

// funcTypeMentionsQueries reports whether any parameter or result of ft mentions the Queries
// type anywhere in its shape. Scanning Params as well as Results is what catches an
// out-parameter (func Fill(dst **Queries, pool *pgxpool.Pool)) that a Results-only check would
// never see, since the constructed *Queries there is never actually returned.
func funcTypeMentionsQueries(ft *ast.FuncType) bool {
	if ft.Params != nil {
		for _, field := range ft.Params.List {
			if mentionsQueriesType(field.Type) {
				return true
			}
		}
	}
	if ft.Results != nil {
		for _, field := range ft.Results.List {
			if mentionsQueriesType(field.Type) {
				return true
			}
		}
	}
	return false
}

// queriesConstructorViolations reports the name of every function-like declaration in file —
// any *ast.FuncDecl (with or without a receiver) or a var/const-bound *ast.FuncLit, via
// forEachFuncLike — whose parameters or results mention the Queries type anywhere in their
// shape. It intentionally does not exclude NewQueries: TestNewQueriesIsTheOnlyConstructor
// instead asserts that NewQueries is the *only* name this ever reports, which both proves the
// detector actually fires on a real constructor and proves nothing else does. A bare *Queries
// return is only the simplest shape this catches: []*Queries, map[string]*Queries,
// func() *Queries, <-chan *Queries, a struct{ Q *Queries } field, and a **Queries
// out-parameter all hand out a *Queries just as effectively, and mentionsQueriesType catches
// every one of them without a matcher per shape.
func queriesConstructorViolations(file *ast.File) []string {
	var offenders []string
	forEachFuncLike(file, func(fn funcLike) {
		if funcTypeMentionsQueries(fn.typ) {
			offenders = append(offenders, fn.name)
		}
	})
	return offenders
}

// Only store.NewQueries may construct, or otherwise hand out, a *store.Queries. Any other
// function, method (on any receiver, including a foreign one), or function-literal variable
// whose signature mentions Queries in any shape is itself a second constructor — 04 section 14.
func TestNewQueriesIsTheOnlyConstructor(t *testing.T) {
	var offenders []string
	for _, sf := range parseStorePackage(t) {
		offenders = append(offenders, queriesConstructorViolations(sf.file)...)
	}
	require.Equal(t, []string{"NewQueries"}, offenders,
		"NewQueries must be the only function-like declaration whose signature mentions Queries")
}

// newQueriesFunc is the one function whose body may contain a Queries composite literal.
const newQueriesFunc = "NewQueries"

// isQueriesType reports whether expr names the Queries type exactly — as a bare identifier
// inside internal/store, or as a qualified store.Queries. It is deliberately stricter than
// mentionsQueriesType above, which answers "does this type expression mention Queries anywhere
// in its tree": a []*Queries literal collecting two already-constructed Queries mentions the
// type but constructs nothing, whereas Queries{...} constructs one.
func isQueriesType(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name == "Queries"
	case *ast.SelectorExpr:
		return t.Sel.Name == "Queries"
	default:
		return false
	}
}

// queriesLiteralViolations reports every composite literal of type Queries in file that is not
// inside NewQueries' body — Queries{...} and &Queries{...} alike, and wherever it appears: in a
// return, as a field of an enclosing struct literal, inside a nested closure, or as an element
// of a slice or map (forEachCompositeLit infers the type of an elided element literal, so
// []*Queries{{pool: p}} is caught as readily as the spelled-out form). The walk is a whole-file
// ast.Walk, so no declaration shape can hide one.
//
// An empty Queries{} outside NewQueries is reported exactly like a fully populated one. That is
// not an oversight: a zero Queries is fail-closed today only by accident of its nil pool and
// nil scopes, and the invariant this guard exists to state is unconditional — nothing but
// NewQueries may build a Queries — not "nothing but NewQueries may build a dangerous one".
//
// This is the invariant the three preceding guards do not state. Each of those asks what a
// declaration hands back; none asks what builds a Queries in the first place. A carrier type
// (type Carrier struct{ Q *Queries }) whose constructor returns *Carrier mentions Queries
// nowhere in its signature, so guard 3 never fires on it — yet its body can populate pool and
// scopes field by field and hand out an every-scope Queries that NewQueries never saw.
func queriesLiteralViolations(fset *token.FileSet, file *ast.File) []string {
	start, end, hasExempt := exemptRange(file, func(fnName string) bool { return fnName == newQueriesFunc })

	var offenders []string
	forEachCompositeLit(file, func(lit *ast.CompositeLit, typ ast.Expr) {
		if typ == nil || !isQueriesType(typ) {
			return
		}
		if withinExemptRange(lit.Pos(), start, end, hasExempt) {
			return
		}
		offenders = append(offenders, fmt.Sprintf("%d: constructs a Queries composite literal outside %s",
			fset.Position(lit.Pos()).Line, newQueriesFunc))
	})
	return offenders
}

// Only store.NewQueries may construct a store.Queries. Its fields are unexported, so outside
// the package the compiler already enforces this; inside internal/store nothing did, and a
// composite literal written anywhere else in the package binds whatever scope set its author
// typed — the actual scope-escalation vector these guards exist to close — 04 section 14.
func TestOnlyNewQueriesConstructsQueries(t *testing.T) {
	var offenders []string
	for _, sf := range parseStorePackage(t) {
		for _, violation := range queriesLiteralViolations(sf.fset, sf.file) {
			offenders = append(offenders, sf.path+":"+violation)
		}
	}
	require.Empty(t, offenders,
		"only NewQueries may construct a Queries: %s", strings.Join(offenders, "; "))
}
