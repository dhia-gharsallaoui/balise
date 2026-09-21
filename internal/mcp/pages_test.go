package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPagesRejectsUnauthenticatedCall(t *testing.T) {
	d := newTestDeps(t)
	_, _, err := d.listPages(context.Background(), reqWithToken("bogus"), PagesInput{})
	require.ErrorIs(t, err, ErrUnauthenticated)
}

func TestPagesRequiresReadCapability(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "work", "widget", "claim text")
	raw := mintToken(t, d.pool, "writer-only", []string{"work"}, []string{"remember"})

	_, _, err := d.listPages(context.Background(), reqWithToken(raw), PagesInput{})
	require.Error(t, err)
}

func TestPagesListsWithinAllowedScope(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "work", "widget", "claim text")
	seedPage(t, d.pool, "work", "gadget", "claim text")
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, out, err := d.listPages(context.Background(), reqWithToken(raw), PagesInput{})
	require.NoError(t, err)
	require.Len(t, out.Pages, 2)
	for _, p := range out.Pages {
		require.Equal(t, "work", p.Scope)
	}
}

// TestPagesRefusesScopeOutsideToken is the hostile cross-scope input: a
// scope argument narrows, never widens (04 section 14) -- a token confined
// to "work" asking pages to narrow to a scope it does not hold must be
// refused outright, never quietly served from a scope it never held.
func TestPagesRefusesScopeOutsideToken(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "client-globex", "secret", "claim text")
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, out, err := d.listPages(context.Background(), reqWithToken(raw), PagesInput{Scope: "client-globex"})
	require.Error(t, err)
	require.Empty(t, out.Pages)
}

// TestPagesUnknownTypeReturnsEmptyNotError is the hostile "unknown type"
// input: pages has no closed enum of valid types to validate Type against
// (the registry's own type list lives in defaults/types/, not in this
// package), so an unrecognised type must behave exactly like a type that
// simply has no matching pages -- an empty result, never an error, and
// never a crash.
func TestPagesUnknownTypeReturnsEmptyNotError(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "work", "widget", "claim text")
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, out, err := d.listPages(context.Background(), reqWithToken(raw), PagesInput{Type: "not-a-real-type"})
	require.NoError(t, err)
	require.Empty(t, out.Pages)
}

func TestPagesLimitIsClampedToMax(t *testing.T) {
	d := newTestDeps(t)
	for i := 0; i < 5; i++ {
		seedPage(t, d.pool, "work", "page-"+string(rune('a'+i)), "claim text")
	}
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	// A caller asking for far more than maxPagesLimit must never force an
	// unbounded query result -- Limit is clamped, not honoured literally.
	_, out, err := d.listPages(context.Background(), reqWithToken(raw), PagesInput{Limit: 1_000_000})
	require.NoError(t, err)
	require.Len(t, out.Pages, 5)
}

func TestPagesRecordsAnAuditRowOnEveryCall(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "work", "widget", "claim text")
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	before := countAuditRows(t, d.pool)
	_, _, err := d.listPages(context.Background(), reqWithToken(raw), PagesInput{})
	require.NoError(t, err)
	require.Equal(t, before+1, countAuditRows(t, d.pool))
}
