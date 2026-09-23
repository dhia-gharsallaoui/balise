package guard_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Guards 2, 3, 4 and 5 — respectively in this file, queries_constructor_test.go,
// pool_getter_test.go and queries_constructor_test.go again — all resolve types by AST name:
// they look for the identifier "Queries", the selector "pgxpool.Pool", and so on, in the
// syntax tree of already-parsed source. None of them run a type checker. That is a real,
// deliberate limitation, not an oversight:
//
//   - `type Q = Queries` inside internal/store, with a widener spelled `func Wide(...) *Q`,
//     defeats guard 3: the AST never contains the identifier "Queries" at the declaration site.
//     `&Q{pool: p, scopes: ...}` defeats guard 5 for exactly the same reason.
//   - `type P = pgxpool.Pool`, or an import alias (`import pp "github.com/.../pgxpool"` then
//     `*pp.Pool`), defeats guard 4 the same way.
//   - `type Queries = impl` (aliasing the exported name to some other, unguarded type) defeats
//     every guard that matches on the name "Queries".
//
// Closing that gap properly requires resolving type identities rather than name spellings —
// go/types via golang.org/x/tools/go/packages with NeedTypes — which is a new dependency and
// adds real seconds to every test run. That is deliberately not done here.
//
// This is an acceptable gap because every one of these bypasses requires editing
// internal/store itself. Outside the package, Go already forbids setting an unexported field,
// and a zero-value store.Queries{} is nil-pool, nil-scopes, and fails closed. So these guards
// exist to catch a maintainer's mistake — a new exported field, a second constructor, a pool
// getter added without thinking about what it hands out. `type Q = Queries` is not a mistake;
// it is a deliberate act, visible in any diff that introduces it, and a reviewer reading that
// diff does not need a test to tell them what it does.
//
// In short: these are regression guards against accidental scope-boundary erosion, not a
// defence against a maintainer of this package who is deliberately trying to circumvent them.
// A green TestGuardsCatchKnownBypasses run is not proof of the latter.

var derivedTables = []string{"documents", "claims", "chunks", "edges", "lint_findings", "claim_embeddings"}

// sqlVerbRe matches a SQL verb followed by any whitespace — not just a literal space — so a
// query broken across lines ("select\n  uid, ...") is still recognised as SQL.
var sqlVerbRe = regexp.MustCompile(`(select|insert\s+into|update|delete\s+from|join)\s`)

func looksLikeSQL(text string) bool {
	return sqlVerbRe.MatchString(strings.ToLower(text))
}

// queriesGoPath is the one file allowed to name a derived table. It is matched as a full,
// repo-rooted path — not a basename — so a same-named file in another package (e.g. a
// hypothetical internal/indexer/queries.go) is not accidentally exempted too.
const queriesGoPath = "internal/store/queries.go"

// repoGoFiles walks the whole repository for .go files, not just one level under internal/,
// so cmd/ and any nested subpackage are covered rather than only internal/<pkg>/<file>.go.
func repoGoFiles(t *testing.T) (root string, files []string) {
	t.Helper()
	root, err := filepath.Abs("../..")
	require.NoError(t, err)

	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "web":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	require.NoError(t, err)
	return root, files
}

// Only internal/store/queries.go may name a derived table in SQL — 04 section 14.
func TestOnlyQueriesFileNamesDerivedTables(t *testing.T) {
	root, paths := repoGoFiles(t)

	var offenders []string
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)
		if filepath.ToSlash(rel) == queriesGoPath {
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
			value := strings.ToLower(lit.Value)
			if !looksLikeSQL(value) {
				return true
			}
			for _, table := range derivedTables {
				if strings.Contains(value, table) {
					offenders = append(offenders, fmt.Sprintf("%s:%d names %q in SQL",
						rel, fset.Position(lit.Pos()).Line, table))
				}
			}
			return true
		})
	}
	require.Empty(t, offenders,
		"only internal/store/queries.go may reach the derived tables: %s",
		strings.Join(offenders, "; "))
}

// sqlArgPosition maps a pgx SQL-executing method to the position of its SQL-text argument:
// Query, QueryRow and Exec all take (ctx, sql, args...); a *pgx.Batch's Queue takes
// (sql, args...) with no context. SendBatch is deliberately not in this map — its signature
// is (ctx, *pgx.Batch), so it never carries SQL text directly. The literal requirement instead
// falls on every Queue call that built that batch (or every direct pgx.QueuedQuery composite
// literal — see queuedQuerySQLViolations below), which this map already covers.
var sqlArgPosition = map[string]int{
	"Query":    1,
	"QueryRow": 1,
	"Exec":     1,
	"Queue":    0,
}

// migrateFile and migrateFunc name the one call allowed to carry a non-literal SQL argument:
// store.Migrate reads a whole, versioned .sql file out of an embed.FS and executes it
// verbatim. Its argument is an entire file's content, loaded wholesale, not a value built
// from parts, request data, or a scope — the one case where "the SQL" cannot be spelled as a
// literal because it is loaded, not authored, at that call site.
const (
	migrateFile = "internal/store/migrate.go"
	migrateFunc = "Migrate"
)

// testutilDBFile and testutilDBFunc name the other call allowed to carry a non-literal SQL
// argument: NewDB (see internal/testutil/db.go) creates, then later drops, a uniquely-named
// schema per test so that go test's default parallelism does not race two packages against one
// shared "public" schema. The schema name is generated internally from a fixed charset (see
// newSchemaName) and crypto/rand, never from external input — but it is still a Postgres
// identifier, and identifiers cannot be bound as pgx query parameters (only values can, via
// $1-style placeholders), so there is no literal-SQL form of "CREATE SCHEMA <generated name>"
// available at all. That is the same structural reason store.Migrate is exempted above: the SQL
// text cannot be spelled as a literal at the call site, not that this call site is special.
const (
	testutilDBFile = "internal/testutil/db.go"
	testutilDBFunc = "NewDB"
)

// noExemptions never exempts any function. It is the correct isExempt for a fixture (see
// TestGuardsCatchKnownBypasses) since no fixture is store.Migrate.
func noExemptions(string) bool { return false }

// sqlScopePackages is the set of packages guard 2 (TestSQLArgumentsAreAlwaysLiterals) walks.
// It starts from enginePackages (defined in neutral_test.go, shared with the unrelated
// domain-vocabulary guard in TestEnginePackagesNeverMentionATypeName) and adds every package
// that can reach a pgx call and was not yet covered: internal/testutil (already has real Exec
// calls, in NewDB — see testutilDBFile/testutilDBFunc above), and internal/api and cmd/balise
// (both empty today, wired up in later tasks, but a pgx call added there in the future must be
// caught from day one). Guard 2 gets its own list, built from enginePackages rather than by
// editing it, so that widening this guard's coverage never silently widens the vocabulary
// guard's coverage too.
var sqlScopePackages = append(append([]string{}, enginePackages...),
	"../testutil", "../api", "../../cmd/balise")

// exemptRange finds the one top-level function in file satisfying isExempt and returns its
// body's token.Pos range. token.Pos values are monotonic byte offsets within a file, so a later
// "is this call inside the exempt function" check needs only a start<=pos<=end containment
// test — no stack of enclosing-function context needs to be tracked during the single
// ast.Inspect pass over the whole file. ok is false if no such function exists (or it has no
// body), in which case no position will ever be reported as contained.
func exemptRange(file *ast.File, isExempt func(fnName string) bool) (start, end token.Pos, ok bool) {
	for _, decl := range file.Decls {
		fn, isFn := decl.(*ast.FuncDecl)
		if !isFn || fn.Body == nil || !isExempt(fn.Name.Name) {
			continue
		}
		return fn.Body.Pos(), fn.Body.End(), true
	}
	return 0, 0, false
}

func withinExemptRange(pos, start, end token.Pos, ok bool) bool {
	return ok && pos >= start && pos <= end
}

// methodValueBindings scans file for `ident := expr.TrackedMethod` or `var ident = expr.TrackedMethod`,
// where TrackedMethod is one of the tracked pgx methods (see sqlArgPosition) referenced but not
// called — a method value. It maps ident to the method name it was bound from, so a later bare
// call `ident(ctx, dyn)` can be resolved back to the tracked method even though call.Fun is an
// *ast.Ident there, not the *ast.SelectorExpr a naive check for "pool.Query(...)" looks for.
func methodValueBindings(file *ast.File) map[string]string {
	bindings := map[string]string{}
	record := func(lhs, rhs ast.Expr) {
		ident, isIdent := lhs.(*ast.Ident)
		selector, isSelector := rhs.(*ast.SelectorExpr)
		if !isIdent || !isSelector {
			return
		}
		if _, tracked := sqlArgPosition[selector.Sel.Name]; tracked {
			bindings[ident.Name] = selector.Sel.Name
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.AssignStmt:
			for i, rhs := range n.Rhs {
				if i < len(n.Lhs) {
					record(n.Lhs[i], rhs)
				}
			}
		case *ast.ValueSpec:
			for i, rhs := range n.Values {
				if i < len(n.Names) {
					record(n.Names[i], rhs)
				}
			}
		}
		return true
	})
	return bindings
}

// trackedCall reports whether call invokes a tracked pgx method (see sqlArgPosition), either
// directly through a selector (pool.Query(...)) or indirectly through a bare identifier
// previously bound to that method as a method value (run := pool.Query; run(...)). It returns
// the argument index that should carry the SQL text, whether the call is tracked at all, and a
// display name for the method used in violation messages.
func trackedCall(call *ast.CallExpr, methodValues map[string]string) (argIdx int, tracked bool, methodName string) {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		idx, ok := sqlArgPosition[fn.Sel.Name]
		return idx, ok, fn.Sel.Name
	case *ast.Ident:
		name, bound := methodValues[fn.Name]
		if !bound {
			return 0, false, ""
		}
		idx, ok := sqlArgPosition[name]
		return idx, ok, name
	default:
		return 0, false, ""
	}
}

// isQueuedQueryType reports whether expr names pgx.QueuedQuery (or a bare QueuedQuery, in case
// of a dot-import) — the type that *pgx.Batch.Queue constructs internally. Building one
// directly and appending it to the exported Batch.QueuedQueries slice runs exactly the same
// query without ever calling Queue, whether it is built as a composite literal (spelled or
// type-elided) or declared zero and filled in field by field afterwards.
func isQueuedQueryType(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name == "QueuedQuery"
	case *ast.Ident:
		return t.Name == "QueuedQuery"
	default:
		return false
	}
}

// queuedQuerySQLViolations reports every composite literal of pgx.QueuedQuery within node whose
// SQL field is not a string literal — the direct-construction bypass for guard 2's Queue
// tracking, which would otherwise only ever see calls to the Queue method itself.
//
// The literal's type comes from forEachCompositeLit, not from lit.Type directly, so the inner
// literal of &pgx.Batch{QueuedQueries: []*pgx.QueuedQuery{{SQL: dyn}}} is recognised too. There
// lit.Type is nil — the type is elided, inferred by the compiler from the enclosing slice's
// element type — and a check that read lit.Type would skip it, even though dropping the type
// name from an element literal changes nothing at all about what runs.
func queuedQuerySQLViolations(fset *token.FileSet, node ast.Node, isExempt func(pos token.Pos) bool) []string {
	var offenders []string
	forEachCompositeLit(node, func(lit *ast.CompositeLit, typ ast.Expr) {
		if typ == nil || !isQueuedQueryType(typ) {
			return
		}
		for _, elt := range lit.Elts {
			kv, isKV := elt.(*ast.KeyValueExpr)
			if !isKV {
				continue
			}
			key, isKey := kv.Key.(*ast.Ident)
			if !isKey || key.Name != "SQL" || isStringLiteral(kv.Value) {
				continue
			}
			if isExempt(lit.Pos()) {
				continue
			}
			offenders = append(offenders, fmt.Sprintf(
				"%d: constructs a pgx.QueuedQuery with a non-literal SQL field",
				fset.Position(lit.Pos()).Line))
		}
	})
	return offenders
}

// queuedQueryBindings reports the identifiers in node declared as a pgx.QueuedQuery, by AST name
// exactly as the rest of these guards resolve types: `var qq pgx.QueuedQuery` (a ValueSpec with
// an explicit type), `qq := pgx.QueuedQuery{}` and `var qq = &pgx.QueuedQuery{}` (a composite
// literal on the right-hand side). It exists so that a later `qq.SQL = dyn` can be attributed to
// the type without a type checker.
func queuedQueryBindings(node ast.Node) map[string]bool {
	bound := map[string]bool{}
	record := func(name ast.Expr, typ ast.Expr) {
		ident, isIdent := name.(*ast.Ident)
		if !isIdent || typ == nil {
			return
		}
		if isQueuedQueryType(unwrapStar(typ)) {
			bound[ident.Name] = true
		}
	}
	ast.Inspect(node, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.ValueSpec:
			for _, name := range decl.Names {
				record(name, decl.Type)
			}
			for i, value := range decl.Values {
				if i < len(decl.Names) {
					record(decl.Names[i], compositeLitType(value))
				}
			}
		case *ast.AssignStmt:
			for i, rhs := range decl.Rhs {
				if i < len(decl.Lhs) {
					record(decl.Lhs[i], compositeLitType(rhs))
				}
			}
		}
		return true
	})
	return bound
}

// queuedQueryFieldAssignViolations reports every assignment of a non-literal to the SQL field of
// a value declared as a pgx.QueuedQuery — `var qq pgx.QueuedQuery; qq.SQL = dyn`. No composite
// literal is ever written in that shape, so queuedQuerySQLViolations cannot see it, yet the
// batch it is appended to runs exactly the same hand-built SQL.
//
// An assignment whose right-hand side count does not line up with its left (qq.SQL, err = f())
// is reported rather than skipped: the value assigned there is a call result, which is never a
// string literal, so failing closed is also the correct answer.
func queuedQueryFieldAssignViolations(fset *token.FileSet, node ast.Node, isExempt func(pos token.Pos) bool) []string {
	bound := queuedQueryBindings(node)
	if len(bound) == 0 {
		return nil
	}

	var offenders []string
	ast.Inspect(node, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range assign.Lhs {
			selector, isSelector := lhs.(*ast.SelectorExpr)
			if !isSelector || selector.Sel.Name != "SQL" {
				continue
			}
			base, isIdent := selector.X.(*ast.Ident)
			if !isIdent || !bound[base.Name] {
				continue
			}
			if i < len(assign.Rhs) && isStringLiteral(assign.Rhs[i]) {
				continue
			}
			if isExempt(assign.Pos()) {
				continue
			}
			offenders = append(offenders, fmt.Sprintf(
				"%d: assigns a non-literal to a pgx.QueuedQuery SQL field",
				fset.Position(assign.Pos()).Line))
		}
		return true
	})
	return offenders
}

// sqlLiteralViolations reports every call in file to a tracked pgx method (see sqlArgPosition)
// whose SQL argument is not a string literal written at the call site, plus every direct
// construction of a pgx.QueuedQuery with a non-literal SQL field, plus every non-literal
// assigned to the SQL field of a pgx.QueuedQuery declared elsewhere. It walks the whole file with
// ast.Inspect rather than iterating file.Decls looking for *ast.FuncDecl, so it reaches a call
// inside a package-level function-literal variable (var Unscoped = func(...) {...}), inside any
// nested closure, and a call made through a method value bound earlier in the same file
// (run := pool.Query; run(ctx, dyn)) — none of which is a *ast.FuncDecl, and the last of which
// is not even a *ast.SelectorExpr call. It is a pure function of an already-parsed file, so the
// same check runs both against real package files (TestSQLArgumentsAreAlwaysLiterals) and
// against isolated testdata fixtures (TestGuardsCatchKnownBypasses) without either caller
// reimplementing the walk. isExempt names the one function, if any, allowed to break the rule.
//
// A constant identifier (const listSQL = "select ..."; pool.Query(ctx, listSQL)) is rejected
// here exactly like any other non-literal argument, even though hoisting a query string into a
// named constant is an otherwise unremarkable Go idiom. That rejection is intentional, not a
// gap: telling a `const` bound once, at compile time, to a literal apart from a `var` built at
// runtime from request data requires resolving the identifier, and resolving identifiers here
// would reopen exactly the reachability hole this round closes. So SQL stays inline at the call
// site. If a future change genuinely needs the constant-hoisting idiom, add a reviewed,
// narrowly-scoped exemption (the same way migrateFunc is exempted below) rather than loosening
// this predicate to accept identifiers generally.
func sqlLiteralViolations(fset *token.FileSet, file *ast.File, isExempt func(fnName string) bool) []string {
	start, end, hasExempt := exemptRange(file, isExempt)
	within := func(pos token.Pos) bool { return withinExemptRange(pos, start, end, hasExempt) }
	methodValues := methodValueBindings(file)

	var offenders []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		argIdx, tracked, methodName := trackedCall(call, methodValues)
		if !tracked || argIdx >= len(call.Args) || isStringLiteral(call.Args[argIdx]) {
			return true
		}
		if within(call.Pos()) {
			return true
		}
		offenders = append(offenders, fmt.Sprintf(
			"%d: calls %s with a non-literal SQL argument",
			fset.Position(call.Pos()).Line, methodName))
		return true
	})
	offenders = append(offenders, queuedQuerySQLViolations(fset, file, within)...)
	offenders = append(offenders, queuedQueryFieldAssignViolations(fset, file, within)...)
	return offenders
}

// Every SQL argument at every pgx call site, across every scope package (see sqlScopePackages),
// must be a string literal written at the call site. An identifier, a `+` concatenation, a
// fmt.Sprintf result, a bytes.Buffer or strings.Builder's output, a strings.Join — anything
// that is not a literal — is a violation, no matter how it was built or how the call is reached
// (a plain call, a call inside a var-bound function literal, a call through a method value, or
// a pgx.QueuedQuery built by hand). This one positive rule subsumes every way of hiding dynamic
// SQL behind a helper, rather than naming forbidden shapes one at a time.
// TestGuardsCatchKnownBypasses proves it against fixtures built from exactly those shapes.
func TestSQLArgumentsAreAlwaysLiterals(t *testing.T) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)

	var offenders []string
	for _, pkg := range sqlScopePackages {
		paths, err := filepath.Glob(filepath.Join(pkg, "*.go"))
		require.NoError(t, err)

		for _, path := range paths {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			require.NoError(t, err)

			abs, err := filepath.Abs(path)
			require.NoError(t, err)
			rel, err := filepath.Rel(root, abs)
			require.NoError(t, err)
			rel = filepath.ToSlash(rel)

			isExempt := func(fnName string) bool {
				return (rel == migrateFile && fnName == migrateFunc) ||
					(rel == testutilDBFile && fnName == testutilDBFunc)
			}
			for _, v := range sqlLiteralViolations(fset, file, isExempt) {
				offenders = append(offenders, rel+":"+v)
			}
		}
	}
	require.Empty(t, offenders, strings.Join(offenders, "; "))
}

func isStringLiteral(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING
}
