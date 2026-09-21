package guard_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// parseFixture parses a single isolated testdata/<name> file — never built by `go build` or
// `go test` (the .go.txt extension keeps the Go toolchain from compiling it, and testdata/ is
// ignored by tooling regardless) — so a bypass shape can be exercised in total isolation from
// the real internal/store package.
func parseFixture(t *testing.T, name string) (*token.FileSet, *ast.File) {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, data, 0)
	require.NoError(t, err)
	return fset, file
}

func containsSubstring(offenders []string, want string) bool {
	for _, offender := range offenders {
		if strings.Contains(offender, want) {
			return true
		}
	}
	return false
}

// checkSQLLiteral, checkQueriesField, checkQueriesConstructor, checkPoolGetter and
// checkQueriesLiteral adapt each guard's real predicate to one shared signature so
// guardBypassCases can drive all of them through a single table. checkSQLLiteral and
// checkQueriesLiteral are the two that need fset, for the line numbers in their messages;
// checkSQLLiteral additionally passes noExemptions, since no fixture is store.Migrate.
func checkSQLLiteral(fset *token.FileSet, file *ast.File) []string {
	return sqlLiteralViolations(fset, file, noExemptions)
}

func checkQueriesField(_ *token.FileSet, file *ast.File) []string {
	return queriesFieldViolations(file)
}

func checkQueriesConstructor(_ *token.FileSet, file *ast.File) []string {
	return queriesConstructorViolations(file)
}

func checkPoolGetter(_ *token.FileSet, file *ast.File) []string {
	return queriesPoolGetterViolations(file)
}

func checkQueriesLiteral(fset *token.FileSet, file *ast.File) []string {
	return queriesLiteralViolations(fset, file)
}

type guardBypassCase struct {
	name     string
	fixture  string
	check    func(fset *token.FileSet, file *ast.File) []string
	wantName string
}

// guardBypassCases is the fixed catalogue of known bypass shapes each guard must catch. Every
// entry is a real, isolated Go file under testdata/ — never a description of a shape — parsed
// and run through the same predicate function the real-package tests use. Twenty entries
// predate this round: seven original shapes (bypass_buffer_builder through bypass_value_return)
// and thirteen reachability shapes — a package-level function-literal variable, a method value,
// a direct pgx.QueuedQuery composite literal, and, for guards 3 and 4, every wrapper shape
// (slice, map, channel, function-returning-function, struct field, out-parameter) and receiver
// shape (foreign receiver, no receiver, pgx.Tx/*pgxpool.Conn results).
//
// Five were added this round: a type-elided pgx.QueuedQuery element literal and a SQL field
// assigned after the fact (guard 2), a function literal declared by a var inside a function
// body (guard 3), a Queries struct declared inside a function body (guard 4), and — the shape
// no guard checked at all until now — a carrier type whose constructor builds a fully scoped
// Queries by hand without ever calling NewQueries (guard 5).
var guardBypassCases = []guardBypassCase{
	// Guard 2 — sqlLiteralViolations / queuedQuerySQLViolations.
	{
		name:     "buffer builder hides concatenation behind bytes.Buffer",
		fixture:  "bypass_buffer_builder.go.txt",
		check:    checkSQLLiteral,
		wantName: "calls Exec with a non-literal SQL argument",
	},
	{
		name:     "strings.Join hides concatenation behind a helper",
		fixture:  "bypass_strings_join.go.txt",
		check:    checkSQLLiteral,
		wantName: "calls QueryRow with a non-literal SQL argument",
	},
	{
		name:     "hoisting literal pieces into a local before concatenating",
		fixture:  "bypass_hoisted_literal.go.txt",
		check:    checkSQLLiteral,
		wantName: "calls Query with a non-literal SQL argument",
	},
	{
		name:     "package-level function-literal variable (the reviewer's exact shape)",
		fixture:  "bypass_var_func_literal.go.txt",
		check:    checkSQLLiteral,
		wantName: "calls Exec with a non-literal SQL argument",
	},
	{
		name:     "method value bound to an identifier before being called",
		fixture:  "bypass_method_value.go.txt",
		check:    checkSQLLiteral,
		wantName: "calls Query with a non-literal SQL argument",
	},
	{
		name:     "direct pgx.QueuedQuery composite literal bypassing Queue",
		fixture:  "bypass_queued_query_literal.go.txt",
		check:    checkSQLLiteral,
		wantName: "constructs a pgx.QueuedQuery with a non-literal SQL field",
	},
	{
		name:     "type-elided inner literal inside []*pgx.QueuedQuery",
		fixture:  "bypass_elided_queued_query.go.txt",
		check:    checkSQLLiteral,
		wantName: "constructs a pgx.QueuedQuery with a non-literal SQL field",
	},
	{
		name:     "SQL field assigned after the fact, never a composite literal",
		fixture:  "bypass_queued_query_assign.go.txt",
		check:    checkSQLLiteral,
		wantName: "assigns a non-literal to a pgx.QueuedQuery SQL field",
	},

	// Guard 3 — queriesConstructorViolations.
	{
		name:     "plain function returning a Queries value with every scope",
		fixture:  "bypass_value_return.go.txt",
		check:    checkQueriesConstructor,
		wantName: "Wide",
	},
	{
		name:     "method on a foreign receiver returning *Queries",
		fixture:  "bypass_foreign_receiver.go.txt",
		check:    checkQueriesConstructor,
		wantName: "All",
	},
	{
		name:     "package-level function-literal variable returning *Queries",
		fixture:  "bypass_var_constructor.go.txt",
		check:    checkQueriesConstructor,
		wantName: "WideVar",
	},
	{
		name:     "slice of *Queries",
		fixture:  "bypass_slice_queries.go.txt",
		check:    checkQueriesConstructor,
		wantName: "AllQueries",
	},
	{
		name:     "map of *Queries",
		fixture:  "bypass_map_queries.go.txt",
		check:    checkQueriesConstructor,
		wantName: "ByScope",
	},
	{
		name:     "receive-only channel of *Queries",
		fixture:  "bypass_chan_queries.go.txt",
		check:    checkQueriesConstructor,
		wantName: "Stream",
	},
	{
		name:     "function returning a function returning *Queries",
		fixture:  "bypass_func_return_queries.go.txt",
		check:    checkQueriesConstructor,
		wantName: "Factory",
	},
	{
		name:     "anonymous struct carrying a *Queries field",
		fixture:  "bypass_struct_field_carrier.go.txt",
		check:    checkQueriesConstructor,
		wantName: "Bundle",
	},
	{
		name:     "**Queries out-parameter instead of a result",
		fixture:  "bypass_out_param.go.txt",
		check:    checkQueriesConstructor,
		wantName: "Fill",
	},
	{
		name:     "function literal declared by a var inside a function body",
		fixture:  "bypass_nested_var_constructor.go.txt",
		check:    checkQueriesConstructor,
		wantName: "wideNested",
	},

	// Guard 4 — queriesFieldViolations (the field half) and queriesPoolGetterViolations (the
	// getter half). Both walk the whole file with ast.Inspect.
	{
		name:     "Queries struct embeds *pgxpool.Pool",
		fixture:  "bypass_embedded_field.go.txt",
		check:    checkQueriesField,
		wantName: "embeds *pgxpool.Pool",
	},
	{
		name:     "Queries struct declared inside a function body embeds *pgxpool.Pool",
		fixture:  "bypass_nested_embedded_field.go.txt",
		check:    checkQueriesField,
		wantName: "embeds *pgxpool.Pool",
	},
	{
		name:     "method on *Queries hands back the raw pool",
		fixture:  "bypass_pool_getter.go.txt",
		check:    checkPoolGetter,
		wantName: "Unwrap",
	},
	{
		name:     "plain function, no receiver, hands back the raw pool",
		fixture:  "bypass_plain_pool_getter.go.txt",
		check:    checkPoolGetter,
		wantName: "RawPool",
	},
	{
		name:     "method on *Queries hands back a pgx.Tx",
		fixture:  "bypass_tx_getter.go.txt",
		check:    checkPoolGetter,
		wantName: "Tx",
	},
	{
		name:     "method on *Queries hands back a *pgxpool.Conn",
		fixture:  "bypass_conn_getter.go.txt",
		check:    checkPoolGetter,
		wantName: "Acquire",
	},

	// Guard 5 — queriesLiteralViolations.
	{
		name:     "carrier struct builds a Queries by hand, bypassing NewQueries",
		fixture:  "bypass_carrier_literal.go.txt",
		check:    checkQueriesLiteral,
		wantName: "constructs a Queries composite literal outside NewQueries",
	},
}

// Every known bypass shape must still be caught by the guard it targets. This is what proves
// the guards' predicates are reachable, not merely sound: TestGuardsCatchKnownBypasses fails
// loudly if a future change to a walker — for example, reverting to a file.Decls loop instead
// of ast.Inspect — silently stops seeing one of these shapes again.
func TestGuardsCatchKnownBypasses(t *testing.T) {
	for _, c := range guardBypassCases {
		t.Run(c.name, func(t *testing.T) {
			fset, file := parseFixture(t, c.fixture)
			offenders := c.check(fset, file)
			require.True(t, containsSubstring(offenders, c.wantName),
				"expected an offender mentioning %q, got: %v", c.wantName, offenders)
		})
	}
}
