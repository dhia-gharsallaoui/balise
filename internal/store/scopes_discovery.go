package store

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dhia/balise/internal/vault"
)

// DiscoverVaultScopes walks pages (every non-reserved .md file's top-level directory) and
// returns the distinct scope names it finds, sorted. This is the same logic that used to
// live as an unexported helper in cmd/balise/main.go; it moved here so both the CLI's serve
// command and internal/api's live scope-set rebuild (EffectiveScopes, below) share one
// implementation instead of two copies drifting apart.
func DiscoverVaultScopes(pages PageStore) ([]string, error) {
	paths, err := pages.List("")
	if err != nil {
		return nil, fmt.Errorf("list vault: %w", err)
	}
	seen := map[string]bool{}
	var scopes []string
	for _, p := range paths {
		if !strings.HasSuffix(p, ".md") || vault.IsReservedPath(p) {
			continue
		}
		scope := vault.ScopeOf(p)
		if scope != "" && !seen[scope] {
			seen[scope] = true
			scopes = append(scopes, scope)
		}
	}
	sort.Strings(scopes)
	return scopes, nil
}

// UnionScopes merges any number of scope-name groups into one deduplicated list, preserving
// first-seen order across the groups in the order given. It replaces what used to be an
// unexported two-argument unionScopes in cmd/balise/main.go; EffectiveScopes below needs to
// merge three sources (database-discovered, vault-discovered, declared), not two.
func UnionScopes(groups ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, group := range groups {
		for _, scope := range group {
			if !seen[scope] {
				seen[scope] = true
				out = append(out, scope)
			}
		}
	}
	return out
}

// defaultScopes is the fallback used only when nothing — not the database, not the vault,
// not defaults/scopes.yaml — has ever named a scope, e.g. a brand-new install. It mirrors
// the constant that used to live inline in cmd/balise/main.go's openQueries.
var defaultScopes = []string{"work", "client-globex", "personal"}

// EffectiveScopes computes the full scope set a running server should enforce: declared ∪
// discovered, where "discovered" covers both the database (DiscoverScopes) and the vault
// (DiscoverVaultScopes). A scope declared in defaults/scopes.yaml but backed by zero pages
// and zero documents rows is still included — that is the entire point of declaring one
// ahead of time (see defaults/scopes.yaml's header and 02 §8's "add a space" flow). If the
// union would otherwise be empty (nothing discovered, nothing declared), defaultScopes is
// used instead, matching this codebase's pre-existing bootstrap behaviour.
func EffectiveScopes(ctx context.Context, pool *pgxpool.Pool, pages PageStore, declared []string) ([]string, error) {
	fromDB, err := DiscoverScopes(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("discover database scopes: %w", err)
	}
	fromVault, err := DiscoverVaultScopes(pages)
	if err != nil {
		return nil, fmt.Errorf("discover vault scopes: %w", err)
	}
	union := UnionScopes(fromDB, fromVault, declared)
	if len(union) == 0 {
		return append([]string(nil), defaultScopes...), nil
	}
	sort.Strings(union)
	return union, nil
}
