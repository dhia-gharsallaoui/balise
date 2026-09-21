package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchRejectsUnauthenticatedCall(t *testing.T) {
	d := newTestDeps(t)
	_, _, err := d.fetch(context.Background(), reqWithToken("bogus"), FetchInput{Scope: "work", Slug: "widget"})
	require.ErrorIs(t, err, ErrUnauthenticated)
}

func TestFetchRequiresReadCapability(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "work", "widget", "claim text")
	raw := mintToken(t, d.pool, "writer-only", []string{"work"}, []string{"remember"})

	_, _, err := d.fetch(context.Background(), reqWithToken(raw), FetchInput{Scope: "work", Slug: "widget"})
	require.Error(t, err)
}

func TestFetchReturnsRequestedPage(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "work", "widget", "claim text")
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, out, err := d.fetch(context.Background(), reqWithToken(raw), FetchInput{Scope: "work", Slug: "widget"})
	require.NoError(t, err)
	require.Equal(t, "widget", out.Page.Slug)
	require.Equal(t, "work", out.Page.Scope)
	require.Len(t, out.Page.Claims, 1)
	require.Equal(t, "claim text", out.Page.Claims[0].Text)
}

func TestFetchRefusesScopeOutsideToken(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "client-globex", "secret", "claim text")
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, _, err := d.fetch(context.Background(), reqWithToken(raw), FetchInput{Scope: "client-globex", Slug: "secret"})
	require.ErrorIs(t, err, errNotFound)
}

func TestFetchUnknownSlugWithinAllowedScopeIsAlsoNotFound(t *testing.T) {
	d := newTestDeps(t)
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, _, err := d.fetch(context.Background(), reqWithToken(raw), FetchInput{Scope: "work", Slug: "does-not-exist"})
	require.ErrorIs(t, err, errNotFound)
}

// TestFetchNeverDistinguishesExistsElsewhere pins 04 section 12/14's "never
// distinguish 'exists elsewhere'": a slug that is real, but in a scope the
// token does not hold, must produce byte-for-byte the same error as a slug
// that does not exist anywhere.
func TestFetchNeverDistinguishesExistsElsewhere(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "client-globex", "secret", "claim text")
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, _, errOutOfScope := d.fetch(context.Background(), reqWithToken(raw), FetchInput{Scope: "client-globex", Slug: "secret"})
	_, _, errNonexistent := d.fetch(context.Background(), reqWithToken(raw), FetchInput{Scope: "work", Slug: "totally-made-up"})

	require.Error(t, errOutOfScope)
	require.Error(t, errNonexistent)
	require.Equal(t, errOutOfScope.Error(), errNonexistent.Error())
}

func TestFetchRecordsAnAuditRowOnEveryCall(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "work", "widget", "claim text")
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	before := countAuditRows(t, d.pool)
	_, _, err := d.fetch(context.Background(), reqWithToken(raw), FetchInput{Scope: "work", Slug: "widget"})
	require.NoError(t, err)
	require.Equal(t, before+1, countAuditRows(t, d.pool))
}
