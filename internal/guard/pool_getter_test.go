package guard_test

import (
	"fmt"
	"go/ast"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// queriesFieldViolations reports every field of a store.Queries struct type that is either
// exported or embedded (anonymous). Queries deliberately holds only unexported, named fields:
// an exported field could be read or replaced from outside the package, and an embedded field
// promotes its methods straight onto *Queries — embedding *pgxpool.Pool, for instance, would
// hand every caller Query/Exec/etc. directly, unscoped.
//
// Like every other walker here it inspects the whole file rather than looping over file.Decls,
// so a Queries declared anywhere — including inside a function body, where its *ast.TypeSpec is
// not a top-level declaration at all — has its fields checked.
func queriesFieldViolations(file *ast.File) []string {
	var offenders []string
	ast.Inspect(file, func(node ast.Node) bool {
		typeSpec, ok := node.(*ast.TypeSpec)
		if !ok || typeSpec.Name.Name != "Queries" {
			return true
		}
		structType, isStruct := typeSpec.Type.(*ast.StructType)
		if !isStruct {
			return true
		}
		for _, field := range structType.Fields.List {
			if len(field.Names) == 0 {
				offenders = append(offenders, fmt.Sprintf("embeds %s", embeddedTypeName(field)))
				continue
			}
			for _, name := range field.Names {
				if name.IsExported() {
					offenders = append(offenders, fmt.Sprintf("exports field %s", name.Name))
				}
			}
		}
		return true
	})
	return offenders
}

// embeddedTypeName renders an embedded field's type for a violation message: "*pgxpool.Pool"
// for a pointer to a selector type, the bare identifier for a local type, or the selector name
// for a qualified non-pointer type.
func embeddedTypeName(field *ast.Field) string {
	switch t := field.Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if ident, ok := t.X.(*ast.Ident); ok {
			return "*" + ident.Name
		}
		if selector, ok := t.X.(*ast.SelectorExpr); ok {
			if pkgIdent, ok := selector.X.(*ast.Ident); ok {
				return "*" + pkgIdent.Name + "." + selector.Sel.Name
			}
			return "*" + selector.Sel.Name
		}
	case *ast.SelectorExpr:
		return t.Sel.Name
	}
	return fmt.Sprintf("%T", field.Type)
}

// The store.Queries struct must have no exported fields and no embedded fields — 04 section 14.
func TestQueriesHasNoExportedOrEmbeddedFields(t *testing.T) {
	var offenders []string
	for _, sf := range parseStorePackage(t) {
		offenders = append(offenders, queriesFieldViolations(sf.file)...)
	}
	require.Empty(t, offenders, strings.Join(offenders, "; "))
}

// connTypeNames is every raw connection-shaped type a Queries method (or a plain function
// taking a *Queries) must never hand back: the pool itself, a checked-out pool connection, and
// a raw pgx connection or transaction — each of which can run arbitrary, unscoped SQL exactly
// as freely as the pool it came from.
var connTypeNames = map[string]bool{
	"pgxpool.Pool": true,
	"pgxpool.Conn": true,
	"pgx.Conn":     true,
	"pgx.Tx":       true,
}

// mentionsConnType reports whether expr — after unwrapping at most one leading *ast.StarExpr,
// since pgx.Tx is itself an interface value with no pointer form but *pgxpool.Pool always
// appears as a pointer — is a qualified selector naming one of connTypeNames.
func mentionsConnType(expr ast.Expr) bool {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkgIdent, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}
	return connTypeNames[pkgIdent.Name+"."+selector.Sel.Name]
}

// funcTypeReturnsConn reports whether any result of ft mentions a raw connection type. Only
// Results are scanned — a function or method that merely takes a *pgxpool.Pool as a parameter
// (as NewQueries legitimately does) is not handing anything back to its caller.
func funcTypeReturnsConn(ft *ast.FuncType) bool {
	if ft.Results == nil {
		return false
	}
	for _, field := range ft.Results.List {
		if mentionsConnType(field.Type) {
			return true
		}
	}
	return false
}

// queriesPoolGetterViolations reports the name of every function-like declaration in file —
// via forEachFuncLike, so any *ast.FuncDecl regardless of receiver, plus any var-bound
// *ast.FuncLit — that returns a raw connection type. Being receiver-agnostic is what catches a
// plain function (func RawPool(q *Queries) *pgxpool.Pool), which a check requiring
// fn.Recv != nil let straight through before this round; checking pgx.Tx and *pgxpool.Conn
// alongside *pgxpool.Pool is what catches a transaction or a checked-out connection handed back
// instead of the pool itself.
func queriesPoolGetterViolations(file *ast.File) []string {
	var offenders []string
	forEachFuncLike(file, func(fn funcLike) {
		if funcTypeReturnsConn(fn.typ) {
			offenders = append(offenders, fmt.Sprintf("%s returns a raw connection type", fn.name))
		}
	})
	return offenders
}

// No function or method reachable from store.Queries — any receiver, or none — may return the
// pool, a pooled connection, a raw connection, or a transaction. Every SQL-executing call must
// go through a Queries method that has already applied the scope filter — 04 section 14.
func TestNoQueriesMethodReturnsThePool(t *testing.T) {
	var offenders []string
	for _, sf := range parseStorePackage(t) {
		offenders = append(offenders, queriesPoolGetterViolations(sf.file)...)
	}
	require.Empty(t, offenders, strings.Join(offenders, "; "))
}
