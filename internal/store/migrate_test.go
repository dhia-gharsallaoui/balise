package store_test

import (
	"context"
	"testing"

	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestMigrateCreatesExpectedTables(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	// testutil.NewDB isolates this test into its own schema (search_path <schema>,public),
	// not literal "public" -- asserting against schemaname = 'public' here would actually be
	// checking the shared dev database's real, pre-existing tables (e.g. the live vault this
	// same Postgres instance holds), and would keep passing even if per-test isolation were
	// completely broken. current_schema() asks Postgres which schema this connection's
	// unqualified statements actually landed in, the same way verifyIsolatedSchema does.
	var schema string
	require.NoError(t, pool.QueryRow(ctx, `select current_schema()`).Scan(&schema))
	require.NotEqual(t, "public", schema,
		"expected this test's own isolated schema, not the shared \"public\" -- per-test isolation appears broken")

	rows, err := pool.Query(ctx,
		`select tablename from pg_tables where schemaname = $1 order by tablename`, schema)
	require.NoError(t, err)
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.Equal(t, []string{"admin_audit_log", "agent_tokens", "audit_log", "chunks", "claim_embeddings", "claims", "documents", "edges", "lint_findings"}, names)
}

func TestMigrateIsIdempotent(t *testing.T) {
	pool := testutil.NewDB(t)
	// NewDB already migrated once; a second pass must not error.
	require.NoError(t, storeMigrate(t, pool))
}

func TestClaimsTsvIsGenerated(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx,
		`insert into documents (uid, slug, scope, type, path, title, body_md, body_hash, tokens, git_version)
		 values ('u1','s','work','note','p.md','t','b','h',1,'g')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`insert into claims (doc_uid, claim_id, ord, text) values ('u1','c1',0,'gateway deletes connections')`)
	require.NoError(t, err)

	var hits int
	require.NoError(t, pool.QueryRow(ctx,
		`select count(*) from claims where tsv @@ websearch_to_tsquery('simple','gateway')`).Scan(&hits))
	require.Equal(t, 1, hits)
}

func storeMigrate(t *testing.T, pool *pgxpool.Pool) error {
	t.Helper()
	return store.Migrate(context.Background(), pool)
}

// TestMigrateDedupesGlobalFindingDuplicates simulates a database that ran lint before
// migration 00002 existed: two global findings (doc_uid is null) for the same (rule, detail)
// were free to coexist, because the base unique(rule, doc_uid, detail) constraint never
// matches when doc_uid is null. NewDB already applied 00002's index, so the index is dropped
// first to reopen that pre-fix window, the duplicate is inserted, and then Migrate is re-run
// to prove it both dedupes the existing rows and successfully recreates the index afterward.
func TestMigrateDedupesGlobalFindingDuplicates(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `drop index lint_findings_global_idx`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`insert into lint_findings (rule, severity, detail) values ($1, 'warn', $2)`,
		"dup-rule", "dup-detail")
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`insert into lint_findings (rule, severity, detail) values ($1, 'warn', $2)`,
		"dup-rule", "dup-detail")
	require.NoError(t, err)

	require.NoError(t, storeMigrate(t, pool))

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`select count(*) from lint_findings where rule = $1 and detail = $2 and doc_uid is null`,
		"dup-rule", "dup-detail").Scan(&count))
	require.Equal(t, 1, count)
}
