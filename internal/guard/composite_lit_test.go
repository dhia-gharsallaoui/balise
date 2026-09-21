package guard_test

import (
	"go/ast"
	"go/token"
)

// compositeLitVisitor implements ast.Visitor. It carries, for the children of the node it is
// visiting, the type a type-elided composite literal among those children would take — which
// is how []*pgx.QueuedQuery{{SQL: dyn}} is recognised as constructing a pgx.QueuedQuery even
// though the inner literal names no type at all.
type compositeLitVisitor struct {
	elem  ast.Expr
	visit func(lit *ast.CompositeLit, typ ast.Expr)
}

// Visit reports a composite literal with the type it constructs, then hands its children the
// element type they would elide to. A *ast.KeyValueExpr passes the inherited element type
// straight through, because a map literal's values sit one level down inside one
// (map[string]*pgx.QueuedQuery{"a": {SQL: dyn}}); every other node clears it, since eliding a
// type is legal only directly inside an array, slice or map literal.
func (v compositeLitVisitor) Visit(node ast.Node) ast.Visitor {
	if _, isKeyValue := node.(*ast.KeyValueExpr); isKeyValue {
		return v
	}
	lit, ok := node.(*ast.CompositeLit)
	if !ok {
		return compositeLitVisitor{visit: v.visit}
	}
	typ := lit.Type
	if typ == nil {
		typ = v.elem
	}
	v.visit(lit, typ)
	return compositeLitVisitor{elem: compositeElementType(typ), visit: v.visit}
}

// forEachCompositeLit walks node and calls visit once for every composite literal in it, paired
// with the type that literal constructs: lit.Type when it is spelled out, or the type inferred
// from the enclosing array, slice or map literal when it is elided. typ is nil when neither is
// available. Guards that match a composite literal by type name go through this rather than
// testing lit.Type directly, so that eliding the type — a purely cosmetic edit that changes no
// behaviour whatsoever — cannot make a literal invisible to them.
func forEachCompositeLit(node ast.Node, visit func(lit *ast.CompositeLit, typ ast.Expr)) {
	ast.Walk(compositeLitVisitor{visit: visit}, node)
}

// compositeElementType returns the type an elided element of a literal of type typ takes: the
// element type of an array or slice, the value type of a map, and nothing for any other type
// (a struct field's value may never elide its type). A leading pointer is stripped, since an
// elided element of a []*T is written as a T literal with the & implied.
func compositeElementType(typ ast.Expr) ast.Expr {
	switch t := typ.(type) {
	case *ast.ArrayType:
		return unwrapStar(t.Elt)
	case *ast.MapType:
		return unwrapStar(t.Value)
	}
	return nil
}

// unwrapStar removes one leading pointer from a type expression, leaving anything else alone.
func unwrapStar(expr ast.Expr) ast.Expr {
	if star, ok := expr.(*ast.StarExpr); ok {
		return star.X
	}
	return expr
}

// compositeLitType returns the type expression of expr when expr is a composite literal, with a
// leading & or * removed so that pgx.QueuedQuery{} and &pgx.QueuedQuery{} resolve alike, and
// nil when expr is anything else.
func compositeLitType(expr ast.Expr) ast.Expr {
	if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == token.AND {
		expr = unary.X
	}
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	return unwrapStar(lit.Type)
}
