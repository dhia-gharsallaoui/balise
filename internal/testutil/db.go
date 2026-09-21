// Package testutil provides a migrated, empty database for integration tests.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/dhia/balise/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const defaultDSN = "postgresql://balise:balise@localhost:5432/balise"

var nonSchemaChar = regexp.MustCompile(`[^a-z0-9_]+`)

// extensionsMu guards extensionsDone: a sync.Once would cache a transient bootstrap
// failure (a lock wait, a connection blip) forever, poisoning every later test in the
// process. A mutex plus a flag set only on success lets the next caller retry — the
// extension statements are all "create extension if not exists", so retrying is free.
var (
	extensionsMu   sync.Mutex
	extensionsDone bool
)

// extensionLockKey is an arbitrary constant used with pg_advisory_lock to serialise
// ensureExtensions across concurrently-running test binaries: each Go test package runs as
// its own process, so the in-process extensionsOnce only protects one of them at a time —
// the advisory lock protects all of them against each other too.
const extensionLockKey = 869412007

// NewDB returns a pool scoped to a freshly created, uniquely named schema, migrated and
// empty. Skips the test when no database is reachable, so unit runs stay offline.
//
// Each test gets its own schema, rather than every test dropping and recreating one shared
// "public" schema: go test runs packages in parallel, so a shared schema lets one package's
// drop race another package's still-running queries against the same physical database.
// A unique per-test schema (via search_path) removes that race without serialising the
// suite — Tasks 13-15 will add more database-using packages, so this needs to hold under
// growing concurrency, not just today's three.
func NewDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("BALISE_TEST_DSN")
	if dsn == "" {
		dsn = defaultDSN
	}
	ctx := context.Background()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}

	probe, err := pgxpool.NewWithConfig(ctx, cfg.Copy())
	if err != nil {
		t.Skipf("no database: %v", err)
	}
	if err := probe.Ping(ctx); err != nil {
		probe.Close()
		t.Skipf("no database: %v", err)
	}

	extensionsMu.Lock()
	if !extensionsDone {
		if err := ensureExtensions(ctx, t, probe); err != nil {
			extensionsMu.Unlock()
			probe.Close()
			t.Fatalf("install required extensions: %v", err)
		}
		extensionsDone = true
	}
	extensionsMu.Unlock()

	schema := newSchemaName(t)
	_, err = probe.Exec(ctx, fmt.Sprintf("create schema %s", schema))
	probe.Close()
	require.NoError(t, err, "create schema %s", schema)

	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	// public stays on the search_path, after the test's own schema: migrations still create
	// tables in the isolated schema (it is searched first), while functions and operators
	// from the extensions ensureExtensions installs — always into public, never into a
	// per-test schema — stay resolvable (e.g. immutable_unaccent's call to unaccent()).
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)

	// Registered immediately after the schema exists, and before store.Migrate runs: if
	// migration fails partway (or for any other reason the test aborts below), the schema
	// this test owns must still be dropped. Registering the cleanup only after a successful
	// Migrate would leak schema on every migration failure, since require.NoError below calls
	// t.FailNow and never returns to reach a later Cleanup registration.
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), fmt.Sprintf("drop schema if exists %s cascade", schema)); err != nil {
			t.Logf("drop schema %s: %v", schema, err)
		}
		pool.Close()
	})

	require.NoError(t, store.Migrate(ctx, pool))
	return pool
}

// ensureExtensions installs the Postgres extensions every migration assumes are already
// present (see internal/store/migrations/00001_initial.sql: ltree, pg_trgm, unaccent) into
// "public", guarded by a session-level advisory lock so that two test binaries bootstrapping
// at the same instant do not race each other.
//
// It must run over pool's own search_path, never a per-test schema's: CREATE EXTENSION with
// no explicit SCHEMA clause installs into the first schema on the current search_path, and a
// per-test schema is dropped in t.Cleanup — installing an extension there would delete it out
// from under every other test the moment that one test finished, and which test "wins" that
// race is exactly the kind of nondeterminism per-test schemas exist to remove.
//
// The lock and every install run over one acquired *pgxpool.Conn, not pool directly: advisory
// locks are held by the backend session that took them, and pool.Exec alone may hand two
// unrelated calls to two different pooled connections, in which case an unlock call could land
// on a session that never held the lock at all.
func ensureExtensions(ctx context.Context, t *testing.T, pool *pgxpool.Pool) error {
	t.Helper()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for extension bootstrap: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "select pg_advisory_lock($1)", extensionLockKey); err != nil {
		return fmt.Errorf("acquire extension bootstrap lock: %w", err)
	}
	defer func() {
		if _, err := conn.Exec(context.Background(), "select pg_advisory_unlock($1)", extensionLockKey); err != nil {
			t.Logf("release extension bootstrap lock: %v", err)
		}
	}()

	if _, err := conn.Exec(ctx, "create extension if not exists ltree"); err != nil {
		return fmt.Errorf("create extension ltree: %w", err)
	}
	if _, err := conn.Exec(ctx, "create extension if not exists pg_trgm"); err != nil {
		return fmt.Errorf("create extension pg_trgm: %w", err)
	}
	if _, err := conn.Exec(ctx, "create extension if not exists unaccent"); err != nil {
		return fmt.Errorf("create extension unaccent: %w", err)
	}
	return nil
}

// newSchemaName derives a valid, collision-resistant unqualified Postgres identifier from
// the test's own name (for easy identification of an orphaned schema) plus random bytes
// (for uniqueness across the concurrently-running packages and processes go test starts).
func newSchemaName(t *testing.T) string {
	t.Helper()
	sanitized := nonSchemaChar.ReplaceAllString(strings.ToLower(t.Name()), "_")
	if len(sanitized) > 24 {
		sanitized = sanitized[:24]
	}
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatalf("random schema suffix: %v", err)
	}
	return fmt.Sprintf("t_%s_%s", sanitized, hex.EncodeToString(suffix))
}
