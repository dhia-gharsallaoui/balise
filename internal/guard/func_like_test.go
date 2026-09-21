package guard_test

import "go/ast"

// funcLike unifies a *ast.FuncDecl (with or without a receiver) and an identifier bound to a
// *ast.FuncLit by a var or const declaration (var Unscoped = func(...) {...}) under one shape,
// so guard 3 (queriesConstructorViolations, in queries_constructor_test.go) and guard 4
// (queriesPoolGetterViolations, in pool_getter_test.go) can each walk both kinds of
// function-shaped declaration with a single shared helper instead of two separate,
// independently drifting traversals.
type funcLike struct {
	name string
	typ  *ast.FuncType
}

// forEachFuncLike calls visit once for every *ast.FuncDecl in file — regardless of whether it
// has a receiver — and once for every identifier bound to a *ast.FuncLit by a var or const
// declaration, at any depth.
//
// The walk is a whole-file ast.Inspect, not a loop over file.Decls. That distinction is the
// whole point: a file.Decls loop sees only top-level declarations, so a function literal
// declared by a var inside a function body — `func Register(p *pgxpool.Pool) { var wide =
// func() *Queries {...}; Registry = wide }` — is invisible to it, even though the literal's
// signature widens exactly as much as a top-level one. ast.Inspect reaches the *ast.ValueSpec
// wherever it is declared: in a top-level var block, in a grouped const block, or nested inside
// any function body or closure.
//
// Anonymous function literals bound to no identifier are deliberately not visited: they have no
// name to report, cannot be referenced by anything outside the expression they appear in, and
// any pgx call inside one is caught directly by guard 2's whole-file walk in sql_test.go.
func forEachFuncLike(file *ast.File, visit func(funcLike)) {
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.FuncDecl:
			visit(funcLike{name: n.Name.Name, typ: n.Type})
		case *ast.ValueSpec:
			for i, value := range n.Values {
				if i >= len(n.Names) {
					continue
				}
				if lit, isLit := value.(*ast.FuncLit); isLit {
					visit(funcLike{name: n.Names[i].Name, typ: lit.Type})
				}
			}
		}
		return true
	})
}
