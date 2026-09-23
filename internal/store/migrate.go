// Package store owns everything that touches persistence.
package store

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate applies every migration in lexical order. Each is idempotent.
//
// Every migration file uses unqualified table names (`create table if not exists
// documents ...`), which Postgres resolves against whatever the connection's search_path
// says -- so before touching anything, and again once every file has applied, Migrate
// confirms that resolution actually lands in the schema the caller's DSN asked for. See
// verifyIsolatedSchema's doc comment for exactly what this closes: a caller isolating
// itself via "?search_path=<schema>,public" (as `make demo` does) must never be able to
// fall through to whatever schema comes next -- typically "public", the live index --
// just because <schema> turned out to be missing, dropped mid-run, or shadowed.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if err := verifyIsolatedSchema(ctx, pool); err != nil {
		return err
	}

	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	for _, entry := range entries {
		body, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", entry.Name(), err)
		}
	}

	// A schema dropped by a concurrent process mid-migration, or a search_path that
	// resolved correctly for DDL but not for this pool's other connections (pgxpool hands
	// out whichever connection is free; each was configured identically, but re-checking
	// against reality rather than trusting the first check costs one query), would
	// otherwise surface only as missing or misplaced data much later. Catch it here,
	// before Migrate returns a pool the caller is about to run real reads and writes
	// through.
	if err := verifyIsolatedSchema(ctx, pool); err != nil {
		return fmt.Errorf("after migrating: %w", err)
	}
	return nil
}
