package mcp

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestRememberRejectsUnauthenticatedCall(t *testing.T) {
	d := newTestDeps(t)
	_, _, err := d.remember(context.Background(), reqWithToken("bogus"), RememberInput{Text: "note", Scope: "work"})
	require.ErrorIs(t, err, ErrUnauthenticated)
}

func TestRememberRequiresRememberCapability(t *testing.T) {
	d := newTestDeps(t)
	raw := mintToken(t, d.pool, "read-only", []string{"work"}, []string{"read"})

	_, out, err := d.remember(context.Background(), reqWithToken(raw), RememberInput{Text: "note", Scope: "work"})
	require.Error(t, err)
	require.Empty(t, out.Path)

	all, listErr := d.pages.List("")
	require.NoError(t, listErr)
	require.Empty(t, all, "a capability violation must fail closed: nothing gets written")
}

func TestRememberRejectsScopeOutsideToken(t *testing.T) {
	d := newTestDeps(t)
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read", "remember"})

	_, out, err := d.remember(context.Background(), reqWithToken(raw), RememberInput{Text: "note", Scope: "client-globex"})
	require.Error(t, err)
	require.Empty(t, out.Path)

	all, listErr := d.pages.List("")
	require.NoError(t, listErr)
	require.Empty(t, all, "a token holding work must never be allowed to write under client-globex")
}

func TestRememberRejectsEmptyScope(t *testing.T) {
	d := newTestDeps(t)
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read", "remember"})

	_, _, err := d.remember(context.Background(), reqWithToken(raw), RememberInput{Text: "note", Scope: ""})
	require.Error(t, err)
}

func TestRememberRejectsEmptyOrBlankText(t *testing.T) {
	d := newTestDeps(t)
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read", "remember"})

	for _, text := range []string{"", "   ", "\t\n"} {
		_, _, err := d.remember(context.Background(), reqWithToken(raw), RememberInput{Text: text, Scope: "work"})
		require.Error(t, err, "remember must refuse blank text %q", text)
	}
}

// TestRememberRefusesTokenWithHostileAgentName defends the write side of the
// belt-and-braces path check as a second, independent layer. store.CreateToken now
// validates a token's name and scopes at mint time (internal/store/tokens.go,
// TestCreateTokenRejectsHostileAgentName in tokens_test.go) -- so a hostile name can no
// longer reach the table via the normal path. This test proves that even if one did --
// a stale row from before that check existed, a direct database edit, a future insert
// path that forgets to call CreateToken -- remember's own path-building still
// independently refuses it. Defense in depth means neither layer trusts the other: the
// token here is inserted directly (mintTokenBypassingNameValidation), never through
// CreateToken, precisely so this test cannot be satisfied by creation-time validation
// alone.
func TestRememberRefusesTokenWithHostileAgentName(t *testing.T) {
	d := newTestDeps(t)
	ctx := context.Background()
	raw := mintTokenBypassingNameValidation(t, d.pool, "../escape", []string{"work"}, []string{"read", "remember"})

	_, out, err := d.remember(ctx, reqWithToken(raw), RememberInput{Text: "note", Scope: "work"})
	require.Error(t, err)
	require.Empty(t, out.Path)

	all, listErr := d.pages.List("")
	require.NoError(t, listErr)
	require.Empty(t, all, "a hostile agent name must produce no write at all, not a sanitised one")
}

// TestRememberWritesExactlyTheExpectedPathAndNowhereElse is the memory-file
// exact-path test the task brief calls for.
func TestRememberWritesExactlyTheExpectedPathAndNowhereElse(t *testing.T) {
	d := newTestDeps(t)
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read", "remember"})

	_, out, err := d.remember(context.Background(), reqWithToken(raw), RememberInput{Text: "first note", Scope: "work"})
	require.NoError(t, err)

	const wantPrefix = "work/memory/claude-code/"
	require.True(t, strings.HasPrefix(out.Path, wantPrefix), "path %q must sit under %q", out.Path, wantPrefix)
	require.True(t, strings.HasSuffix(out.Path, ".md"))

	all, err := d.pages.List("")
	require.NoError(t, err)
	require.Equal(t, []string{out.Path}, all, "remember must write exactly one file, at exactly the expected path, and nothing else")

	data, _, err := d.pages.Read(out.Path)
	require.NoError(t, err)
	require.Contains(t, string(data), "first note")
	require.Contains(t, string(data), "claude-code")

	history, err := d.pages.History(out.Path, 1)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Contains(t, history[0].Author, "agent:claude-code")
}

func TestRememberAppendsToTheSameDaysFile(t *testing.T) {
	d := newTestDeps(t)
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read", "remember"})
	ctx := context.Background()

	_, out1, err := d.remember(ctx, reqWithToken(raw), RememberInput{Text: "first", Scope: "work"})
	require.NoError(t, err)
	_, out2, err := d.remember(ctx, reqWithToken(raw), RememberInput{Text: "second", Scope: "work"})
	require.NoError(t, err)

	require.Equal(t, out1.Path, out2.Path, "same-day remembers append to one file")

	data, _, err := d.pages.Read(out2.Path)
	require.NoError(t, err)
	require.Contains(t, string(data), "first")
	require.Contains(t, string(data), "second")

	all, err := d.pages.List("")
	require.NoError(t, err)
	require.Len(t, all, 1, "appending must not create a second file")
}

func TestRememberMultiScopeTokenCanWriteToEachOfItsOwnScopes(t *testing.T) {
	d := newTestDeps(t)
	raw := mintToken(t, d.pool, "claude-code", []string{"work", "client-globex"}, []string{"read", "remember"})
	ctx := context.Background()

	_, out1, err := d.remember(ctx, reqWithToken(raw), RememberInput{Text: "work note", Scope: "work"})
	require.NoError(t, err)
	_, out2, err := d.remember(ctx, reqWithToken(raw), RememberInput{Text: "client note", Scope: "client-globex"})
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(out1.Path, "work/memory/claude-code/"))
	require.True(t, strings.HasPrefix(out2.Path, "client-globex/memory/claude-code/"))
}

// TestRememberEveryCallWritesOneAuditRow pins 04 section 12's "every call
// writes one audit_log row," on both the success and the refusal path.
func TestRememberEveryCallWritesOneAuditRow(t *testing.T) {
	d := newTestDeps(t)
	ctx := context.Background()
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read", "remember"})

	before := countAuditRows(t, d.pool)

	_, _, err := d.remember(ctx, reqWithToken(raw), RememberInput{Text: "ok note " + strconv.Itoa(1), Scope: "work"})
	require.NoError(t, err)
	_, _, err = d.remember(ctx, reqWithToken(raw), RememberInput{Text: "refused", Scope: "client-globex"})
	require.Error(t, err)

	after := countAuditRows(t, d.pool)
	require.Equal(t, before+2, after, "both the success and the refusal must each write exactly one audit_log row")
}

func countAuditRows(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), "select count(*) from audit_log").Scan(&n))
	return n
}
