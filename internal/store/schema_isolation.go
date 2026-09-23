package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// verifyIsolatedSchema closes the failure mode behind `make demo`'s worst bug: a caller
// isolates itself from the live index by putting a schema ahead of "public" on the DSN's
// search_path (exactly what DEMO_DSN in the Makefile does: "?search_path=demo_vault,public"),
// but Postgres resolves every *unqualified* table name in CREATE/SELECT/INSERT by walking
// that search_path and silently using the first schema that actually exists. If the intended
// schema was never created, was dropped between runs, or a role/session setting shadows it,
// every one of those statements resolves into whatever schema comes next -- "public" here,
// the owner's real 139-page index -- and nothing about that is loud. A reindex proceeds,
// reports success, and has just pruned and replaced someone else's data.
//
// This function does not create the schema itself: `make demo` already does that explicitly
// (see the Makefile's `demo` target), and the one thing worse than failing loudly on a
// missing schema is guessing what the caller meant and creating it for them. Instead this
// asks Postgres directly -- not the DSN string, which only says what was *requested* --
// which schema unqualified statements would actually resolve to right now, via
// current_schemas(false) (the same function Postgres itself consults for name resolution),
// and refuses to continue unless that first-resolved schema is exactly the one the DSN asked
// for. A schema that is merely present somewhere on the path but not first is exactly as
// dangerous as one that is entirely missing, so both are rejected the same way.
//
// A DSN with no search_path override at all (the ordinary live-vault case: `make up`/`make
// demo`'s VAULT target) asks for nothing here and is left alone -- that path is supposed to
// land in "public", the normal Postgres default, and always has.
func verifyIsolatedSchema(ctx context.Context, pool *pgxpool.Pool) error {
	target := requestedPrimarySchema(pool)
	if target == "" || target == "public" {
		return nil
	}

	var resolved []string
	if err := pool.QueryRow(ctx, "select current_schemas(false)").Scan(&resolved); err != nil {
		return fmt.Errorf("verify schema isolation for %q: %w", target, err)
	}

	if len(resolved) == 0 || resolved[0] != target {
		return fmt.Errorf(
			"schema isolation failed: this connection's search_path asked for %q to come first, "+
				"but Postgres currently resolves unqualified tables to %v instead -- refusing to run "+
				"migrations or queries, since that would silently read and write whichever schema *is* "+
				"first (often \"public\", the live index). Create %q explicitly (e.g. `create schema %q`) "+
				"and rerun; do not remove \"public\" from search_path instead, since the extensions "+
				"(ltree, pg_trgm, unaccent) this schema's generated columns depend on live there",
			target, resolved, target, target)
	}
	return nil
}

// requestedPrimarySchema reads back the schema search_path was configured to try first, from
// the pool's own connection config -- i.e. what a DSN's "?search_path=..." query parameter
// (or an explicit RuntimeParams override, as internal/testutil/db.go sets for per-test
// isolation) actually asked for, not a guess. "" means no override was requested at all.
func requestedPrimarySchema(pool *pgxpool.Pool) string {
	cfg := pool.Config()
	if cfg == nil || cfg.ConnConfig == nil {
		return ""
	}
	searchPath := cfg.ConnConfig.RuntimeParams["search_path"]
	if searchPath == "" {
		return ""
	}
	first := strings.TrimSpace(strings.Split(searchPath, ",")[0])
	// Schema names containing upper case or special characters come back quoted in the DSN
	// (`"Demo Vault"`); current_schemas() reports the unquoted name, so strip matching quotes
	// here to compare like with like.
	first = strings.Trim(first, `"`)
	return first
}
