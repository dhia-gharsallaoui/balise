package mcp

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// leakFixtureScopes are the scopes seeded before every leak-test fixture
// runs. Each scope gets several pages whose claim text names the scope
// itself, so any hit or fetched page can be checked against exactly the
// scope it reports, and against every fixture's own allowed-scope set.
var leakFixtureScopes = []string{"work", "client-globex", "client-acme"}

func seedLeakFixtureVault(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	for _, scope := range leakFixtureScopes {
		for p := 0; p < 3; p++ {
			slug := fmt.Sprintf("page-%d", p)
			seedPage(t, pool, scope, slug,
				fmt.Sprintf("this %s page %d discusses gateway migration secrets", scope, p))
		}
	}
}

func assertScopeAllowed(t *testing.T, allowed map[string]bool, scope, where string) {
	t.Helper()
	if !allowed[scope] {
		t.Fatalf("leak: %s returned scope %q, which is outside the allowed set", where, scope)
	}
}

// TestLeakTestZeroReturnedUIDsOutsideAllowedScopes is 04 section 14's
// required leak test: for each of three token fixtures (a single-scope
// token, a multi-scope token, and a token with no remember capability),
// 200 queries spread across remember, search, and fetch, asserting zero
// returned uids/slugs/paths outside that token's allowed scopes -- and
// that every capability or scope violation fails closed rather than ever
// returning or writing data.
func TestLeakTestZeroReturnedUIDsOutsideAllowedScopes(t *testing.T) {
	type fixture struct {
		name         string
		scopes       []string
		capabilities []string
	}
	fixtures := []fixture{
		{"single-scope token", []string{"work"}, []string{"read", "remember"}},
		{"multi-scope token", []string{"work", "client-globex"}, []string{"read", "remember"}},
		{"no-remember-capability token", []string{"work", "client-globex", "client-acme"}, []string{"read"}},
	}

	queries := []string{"gateway", "migration", "secrets", "page", "discusses", "nonexistent-term"}

	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			d := newTestDeps(t)
			seedLeakFixtureVault(t, d.pool)
			raw := mintToken(t, d.pool, "fixture-agent", fx.scopes, fx.capabilities)

			allowed := map[string]bool{}
			for _, s := range fx.scopes {
				allowed[s] = true
			}
			hasRemember := false
			for _, c := range fx.capabilities {
				if c == "remember" {
					hasRemember = true
				}
			}

			ctx := context.Background()
			req := reqWithToken(raw)

			for i := 0; i < 200; i++ {
				q := queries[i%len(queries)]
				scopeForCall := leakFixtureScopes[i%len(leakFixtureScopes)]

				// search, narrowed to a scope that cycles across the whole
				// vault (not just the fixture's own scopes): a token asking
				// for a scope it does not hold must come back refused, never
				// with that scope's data.
				_, sOut, sErr := d.search(ctx, req, SearchInput{Query: q, Scope: scopeForCall, TopK: 50})
				if !allowed[scopeForCall] {
					require.Error(t, sErr, "round %d: search into disallowed scope %q must be refused", i, scopeForCall)
					require.Empty(t, sOut.Hits)
				} else {
					require.NoError(t, sErr)
					for _, h := range sOut.Hits {
						assertScopeAllowed(t, allowed, h.Scope, fmt.Sprintf("round %d narrowed search hit", i))
					}
				}

				// unnarrowed search across the token's own bound scopes --
				// must never surface a scope outside the fixture's own set.
				_, wideOut, wideErr := d.search(ctx, req, SearchInput{Query: q, TopK: 50})
				require.NoError(t, wideErr)
				for _, h := range wideOut.Hits {
					assertScopeAllowed(t, allowed, h.Scope, fmt.Sprintf("round %d unscoped search hit", i))
				}

				// fetch, cycling through every seeded (scope, slug) pair in
				// the whole vault, including ones the fixture does not hold.
				slug := fmt.Sprintf("page-%d", i%3)
				_, fOut, fErr := d.fetch(ctx, req, FetchInput{Scope: scopeForCall, Slug: slug})
				if !allowed[scopeForCall] {
					require.ErrorIs(t, fErr, errNotFound, "round %d: an out-of-scope fetch must read as not found, never as data", i)
				} else {
					require.NoError(t, fErr)
					require.Equal(t, scopeForCall, fOut.Page.Scope)
				}

				// pages, narrowed to a scope that cycles across the whole
				// vault: a disallowed scope must come back refused, never
				// with that scope's listing.
				_, pOut, pErr := d.listPages(ctx, req, PagesInput{Scope: scopeForCall})
				if !allowed[scopeForCall] {
					require.Error(t, pErr, "round %d: pages into disallowed scope %q must be refused", i, scopeForCall)
					require.Empty(t, pOut.Pages)
				} else {
					require.NoError(t, pErr)
					for _, p := range pOut.Pages {
						assertScopeAllowed(t, allowed, p.Scope, fmt.Sprintf("round %d pages listing", i))
					}
				}

				// related, cycling through every seeded (scope, slug) pair
				// in the whole vault, including ones the fixture does not
				// hold -- an out-of-scope slug must read as not found, and
				// any neighbour surfaced for an allowed slug must itself
				// stay within the allowed set.
				_, relOut, relErr := d.related(ctx, req, RelatedInput{Scope: scopeForCall, Slug: slug})
				if !allowed[scopeForCall] {
					require.ErrorIs(t, relErr, errNotFound, "round %d: related into disallowed scope %q must read as not found", i, scopeForCall)
					require.Empty(t, relOut.Outbound)
					require.Empty(t, relOut.Backlinks)
				} else {
					require.NoError(t, relErr)
					for _, group := range relOut.Outbound {
						for _, n := range group {
							assertScopeAllowed(t, allowed, n.Scope, fmt.Sprintf("round %d related outbound %s", i, n.Slug))
						}
					}
					for _, n := range relOut.Backlinks {
						assertScopeAllowed(t, allowed, n.Scope, fmt.Sprintf("round %d related backlink %s", i, n.Slug))
					}
				}

				// context, narrowed to a scope that cycles across the
				// whole vault: a disallowed scope must be refused outright,
				// and every packed page's own scope must stay allowed.
				_, cOut, cErr := d.context(ctx, req, ContextInput{Query: q, Scope: scopeForCall})
				if !allowed[scopeForCall] {
					require.Error(t, cErr, "round %d: context into disallowed scope %q must be refused", i, scopeForCall)
					require.Empty(t, cOut.Pages)
				} else {
					require.NoError(t, cErr)
					for _, p := range cOut.Pages {
						assertScopeAllowed(t, allowed, p.Scope, fmt.Sprintf("round %d context page", i))
					}
				}

				// remember, cycling through scopes: only ever allowed to
				// land under the fixture's own scopes, and only when the
				// fixture holds the remember capability at all.
				_, rOut, rErr := d.remember(ctx, req, RememberInput{
					Text:  "leak probe " + strconv.Itoa(i),
					Scope: scopeForCall,
				})
				switch {
				case !hasRemember:
					require.Error(t, rErr, "round %d: a token with no remember capability must never be allowed to write", i)
					require.Empty(t, rOut.Path)
				case !allowed[scopeForCall]:
					require.Error(t, rErr, "round %d: remember must refuse a scope the token does not hold", i)
					require.Empty(t, rOut.Path)
				default:
					require.NoError(t, rErr)
					require.True(t, strings.HasPrefix(rOut.Path, scopeForCall+"/memory/fixture-agent/"))
				}
			}

			// After 200 rounds, walk the vault's own file listing and
			// confirm every written memory file sits under an allowed
			// scope's own prefix -- the write-side mirror of the read-side
			// assertions above.
			all, err := d.pages.List("")
			require.NoError(t, err)
			for _, p := range all {
				scope := strings.SplitN(p, "/", 2)[0]
				assertScopeAllowed(t, allowed, scope, "on-disk memory file "+p)
			}
		})
	}
}
