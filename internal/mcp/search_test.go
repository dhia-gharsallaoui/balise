package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchRejectsUnauthenticatedCall(t *testing.T) {
	d := newTestDeps(t)
	_, _, err := d.search(context.Background(), reqWithToken("bogus"), SearchInput{Query: "gateway"})
	require.ErrorIs(t, err, ErrUnauthenticated)
}

func TestSearchRequiresReadCapability(t *testing.T) {
	d := newTestDeps(t)
	raw := mintToken(t, d.pool, "writer-only", []string{"work"}, []string{"remember"})

	_, out, err := d.search(context.Background(), reqWithToken(raw), SearchInput{Query: "gateway"})
	require.Error(t, err)
	require.Empty(t, out.Hits)
}

func TestSearchNarrowsToRequestedScope(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "work", "widget", "the widget gateway update")
	seedPage(t, d.pool, "client-globex", "widget2", "another gateway update")
	raw := mintToken(t, d.pool, "claude-code", []string{"work", "client-globex"}, []string{"read"})

	_, out, err := d.search(context.Background(), reqWithToken(raw), SearchInput{Query: "gateway", Scope: "work"})
	require.NoError(t, err)
	require.Len(t, out.Hits, 1)
	require.Equal(t, "work", out.Hits[0].Scope)
}

func TestSearchRefusesScopeOutsideToken(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "client-globex", "secret", "a client-globex gateway secret")
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, out, err := d.search(context.Background(), reqWithToken(raw), SearchInput{Query: "gateway", Scope: "client-globex"})
	require.Error(t, err, "a token holding only work must never be handed client-globex data via a scope argument")
	require.Empty(t, out.Hits)
}

func TestSearchWithNoScopeSearchesAllTokenScopes(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "work", "widget", "the widget gateway update")
	seedPage(t, d.pool, "client-globex", "widget2", "another gateway update")
	raw := mintToken(t, d.pool, "claude-code", []string{"work", "client-globex"}, []string{"read"})

	_, out, err := d.search(context.Background(), reqWithToken(raw), SearchInput{Query: "gateway"})
	require.NoError(t, err)
	require.Len(t, out.Hits, 2)
}

func TestSearchNeverReturnsHitsOutsideTokenScopes(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "work", "widget", "the widget gateway update")
	seedPage(t, d.pool, "client-globex", "secret", "a client-globex secret gateway update")
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, out, err := d.search(context.Background(), reqWithToken(raw), SearchInput{Query: "gateway"})
	require.NoError(t, err)
	require.NotEmpty(t, out.Hits)
	for _, h := range out.Hits {
		require.Equal(t, "work", h.Scope)
	}
}

func TestSearchRecordsAnAuditRowOnEveryCall(t *testing.T) {
	d := newTestDeps(t)
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	before := countAuditRows(t, d.pool)
	_, _, err := d.search(context.Background(), reqWithToken(raw), SearchInput{Query: "anything"})
	require.NoError(t, err)
	require.Equal(t, before+1, countAuditRows(t, d.pool))
}
