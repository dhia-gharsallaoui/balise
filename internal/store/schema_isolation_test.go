package store_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/dhia/balise/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// This is the regression test for the bug that actually happened: `make demo` reindexes
// against "?search_path=demo_vault,public". When demo_vault does not exist, Postgres
// resolves every unqualified table in the migration files, and later every query the
// reindex issues, straight into "public" -- the next schema on the path, and the one
// holding the owner's real, 139-page index. The reindex proceeds, reports success, and has
// just pruned and replaced someone else's data. It must fail instead, before touching
// anything.
//
// Both tests below run against the same shared dev Postgres instance testutil.NewDB uses,
// which is the live database these tests must never be able to write to. Every SQL
// statement here is a literal written at its call site (never built with fmt.Sprintf or
// passed through a variable) both to satisfy internal/guard's
// TestSQLArgumentsAreAlwaysLiterals, which scans this package's _test.go files too, and
// because a schema identifier cannot be bound as a query parameter in the first place --
// so a fixed, hardcoded schema name is used rather than a generated one, with defensive
// "if exists"/"if not exists" cleanup around it in case a previous run crashed before its
// own cleanup ran. Neither test ever opens a pool without a search_path override, and
// neither issues a write of any kind against a schema it didn't itself create.

func testDSN() string {
	if dsn := os.Getenv("BALISE_TEST_DSN"); dsn != "" {
		return dsn
	}
	return "postgresql://balise:balise@localhost:5432/balise"
}

func TestMigrateFailsWhenTargetSchemaDoesNotExist(t *testing.T) {
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(testDSN())
	require.NoError(t, err)

	probe, err := pgxpool.NewWithConfig(ctx, cfg.Copy())
	if err != nil {
		t.Skipf("no database: %v", err)
	}
	if pingErr := probe.Ping(ctx); pingErr != nil {
		probe.Close()
		t.Skipf("no database: %v", pingErr)
	}
	probe.Close()

	// Deliberately never created: that omission is exactly the "demo_vault schema does not
	// exist" scenario that let a stray reindex silently fall through to and prune the real
	// vault in public. A random suffix (not SQL -- this only ever appears in Go-side
	// connection config, never in a query argument) keeps this collision-proof without
	// needing any cleanup.
	suffix := make([]byte, 6)
	_, err = rand.Read(suffix)
	require.NoError(t, err)
	missing := "t_missing_" + hex.EncodeToString(suffix)

	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = missing + ",public"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	before := publicRowCounts(t)

	err = store.Migrate(ctx, pool)
	require.Error(t, err,
		"Migrate must refuse to run against a search_path whose first schema does not exist, "+
			"not silently fall through to whatever schema comes next")
	require.Contains(t, err.Error(), missing, "the failure should name the schema that was missing")

	after := publicRowCounts(t)
	require.Equal(t, before, after, "a rejected Migrate call must never have touched public")
}

// TestMigrateFailsWhenTargetSchemaDroppedMidRun covers the other half of "must hold even
// when the target schema is missing, half-created, or was dropped between runs": a schema
// that existed and was correctly migrated once, then disappeared (dropped by a concurrent
// process, a failed cleanup, an operator error) before the next command ran against it.
func TestMigrateFailsWhenTargetSchemaDroppedMidRun(t *testing.T) {
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(testDSN())
	require.NoError(t, err)

	probe, err := pgxpool.NewWithConfig(ctx, cfg.Copy())
	if err != nil {
		t.Skipf("no database: %v", err)
	}
	if pingErr := probe.Ping(ctx); pingErr != nil {
		probe.Close()
		t.Skipf("no database: %v", pingErr)
	}
	defer probe.Close()

	const schema = "t_store_migrate_droptest"
	_, err = probe.Exec(ctx, "drop schema if exists t_store_migrate_droptest cascade")
	require.NoError(t, err)
	_, err = probe.Exec(ctx, "create schema t_store_migrate_droptest")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = probe.Exec(context.Background(), "drop schema if exists t_store_migrate_droptest cascade")
	})

	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, store.Migrate(ctx, pool), "first Migrate, against a schema that genuinely exists, must succeed")

	_, err = probe.Exec(ctx, "drop schema t_store_migrate_droptest cascade")
	require.NoError(t, err)

	before := publicRowCounts(t)

	err = store.Migrate(ctx, pool)
	require.Error(t, err,
		"Migrate must refuse to run once its target schema has been dropped out from under it, "+
			"not silently fall through to public")
	require.Contains(t, err.Error(), schema)

	after := publicRowCounts(t)
	require.Equal(t, before, after, "a rejected Migrate call must never have touched public")
}

// publicRowCounts snapshots row counts for every derived table over a freshly opened pool
// with no search_path override at all, so it can only ever read literal "public" regardless
// of what the pool under test asked for. Every query is a literal naming its one table, both
// because the SQL-literal guard requires it and to keep this simple to audit by eye given
// how sensitive "did this touch public" is for this pair of tests. A table that doesn't
// exist yet (a fresh database with no live vault loaded) is recorded as -1 rather than
// failing the test -- what matters is that the snapshot taken before Migrate is rejected
// equals the one taken after, not what the live vault's counts happen to be today.
func publicRowCounts(t *testing.T) map[string]int {
	t.Helper()
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, testDSN())
	require.NoError(t, err)
	defer pool.Close()

	// Each query is written out in full at its own call site, rather than built from a
	// name + a shared helper: internal/guard's TestSQLArgumentsAreAlwaysLiterals requires
	// the SQL argument at a Query/QueryRow/Exec call site to be a literal, not merely
	// derived from one a few frames up, so a scan(name, query string) helper taking the SQL
	// as a parameter would itself be the violation even though every caller passes a
	// literal in.
	counts := map[string]int{}
	var n int

	n = -1
	_ = pool.QueryRow(ctx, "select count(*) from public.documents").Scan(&n)
	counts["documents"] = n

	n = -1
	_ = pool.QueryRow(ctx, "select count(*) from public.claims").Scan(&n)
	counts["claims"] = n

	n = -1
	_ = pool.QueryRow(ctx, "select count(*) from public.chunks").Scan(&n)
	counts["chunks"] = n

	n = -1
	_ = pool.QueryRow(ctx, "select count(*) from public.edges").Scan(&n)
	counts["edges"] = n

	n = -1
	_ = pool.QueryRow(ctx, "select count(*) from public.lint_findings").Scan(&n)
	counts["lint_findings"] = n

	n = -1
	_ = pool.QueryRow(ctx, "select count(*) from public.claim_embeddings").Scan(&n)
	counts["claim_embeddings"] = n

	n = -1
	_ = pool.QueryRow(ctx, "select count(*) from public.agent_tokens").Scan(&n)
	counts["agent_tokens"] = n

	n = -1
	_ = pool.QueryRow(ctx, "select count(*) from public.audit_log").Scan(&n)
	counts["audit_log"] = n

	n = -1
	_ = pool.QueryRow(ctx, "select count(*) from public.admin_audit_log").Scan(&n)
	counts["admin_audit_log"] = n

	return counts
}
