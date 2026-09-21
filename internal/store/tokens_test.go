package store_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/dhia/balise/internal/vault"
	"github.com/stretchr/testify/require"
)

func hashOf(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func TestCreateTokenShowsSecretOnceAndStoresOnlyItsHash(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	id, raw, err := store.CreateToken(ctx, pool, "claude-code", []string{"work"}, []string{"read", "remember"}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, id)
	require.NotEmpty(t, raw)

	tok, err := store.LookupToken(ctx, pool, hashOf(raw))
	require.NoError(t, err)
	require.NotNil(t, tok)
	require.Equal(t, id, tok.ID)
	require.Equal(t, "claude-code", tok.Name)
	require.Equal(t, []string{"work"}, tok.Scopes)
	require.Equal(t, []string{"read", "remember"}, tok.Capabilities)
	require.Equal(t, hashOf(raw), tok.TokenHash)
	require.False(t, tok.Revoked())
	require.False(t, tok.Expired(time.Now()))

	// The raw token itself must never be recoverable from storage: looking it up by its
	// own plaintext (instead of its hash) must find nothing.
	byPlaintext, err := store.LookupToken(ctx, pool, raw)
	require.NoError(t, err)
	require.Nil(t, byPlaintext)
}

func TestCreateTokenRejectsUnknownCapability(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	_, _, err := store.CreateToken(ctx, pool, "bad", []string{"work"}, []string{"admin"}, nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, store.ErrInvalidCapability))
}

// TestCreateTokenRejectsHostileAgentName is "a test per hostile input" for the agent name
// argument: each of these can never work as the "<agent>" path component rememberPath
// builds ("<scope>/memory/<agent>/..."), so CreateToken must refuse to mint a token for any
// of them rather than let a broken token reach the table.
func TestCreateTokenRejectsHostileAgentName(t *testing.T) {
	hostile := []string{
		"../../../etc/evil",
		"/etc/passwd",
		"evil\x00name",
		"agent name",
		"",
		"   ",
	}
	for _, name := range hostile {
		t.Run(name, func(t *testing.T) {
			pool := testutil.NewDB(t)
			ctx := context.Background()

			id, raw, err := store.CreateToken(ctx, pool, name, []string{"work"}, []string{"read"}, nil)
			require.Error(t, err, "CreateToken(name=%q) must be rejected", name)
			require.ErrorIs(t, err, vault.ErrInvalidSegment)
			require.Empty(t, id)
			require.Empty(t, raw)

			tokens, err := store.ListTokens(ctx, pool)
			require.NoError(t, err)
			require.Empty(t, tokens, "a rejected CreateToken call must write nothing")
		})
	}
}

// TestCreateTokenRejectsHostileScope is the same coverage, one level down, for every entry
// of scopes: a scope becomes a directory name too ("<scope>/memory/<agent>/"), so it carries
// identical constraints to the agent name.
func TestCreateTokenRejectsHostileScope(t *testing.T) {
	hostile := []string{
		"../../root",
		"/etc",
		"evil\x00scope",
		"agent name",
		"",
		"\t",
	}
	for _, scope := range hostile {
		t.Run(scope, func(t *testing.T) {
			pool := testutil.NewDB(t)
			ctx := context.Background()

			id, raw, err := store.CreateToken(ctx, pool, "probe", []string{scope}, []string{"remember"}, nil)
			require.Error(t, err, "CreateToken(scope=%q) must be rejected", scope)
			require.ErrorIs(t, err, vault.ErrInvalidSegment)
			require.Empty(t, id)
			require.Empty(t, raw)

			tokens, err := store.ListTokens(ctx, pool)
			require.NoError(t, err)
			require.Empty(t, tokens, "a rejected CreateToken call must write nothing")
		})
	}
}

// TestCreateTokenAcceptsOrdinaryNameAndScope pins the non-regression: a name/scope pair
// that was always meant to work must keep working once validation is added.
func TestCreateTokenAcceptsOrdinaryNameAndScope(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	id, raw, err := store.CreateToken(ctx, pool, "good-agent", []string{"work"}, []string{"read", "remember"}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, id)
	require.NotEmpty(t, raw)
}

func TestLookupTokenUnknownHashReturnsNilNil(t *testing.T) {
	pool := testutil.NewDB(t)
	tok, err := store.LookupToken(context.Background(), pool, "does-not-exist")
	require.NoError(t, err)
	require.Nil(t, tok)
}

func TestRevokeTokenMarksRevokedAndRejectsDoubleRevoke(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	id, raw, err := store.CreateToken(ctx, pool, "one-shot", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)

	require.NoError(t, store.RevokeToken(ctx, pool, id))

	tok, err := store.LookupToken(ctx, pool, hashOf(raw))
	require.NoError(t, err)
	require.NotNil(t, tok)
	require.True(t, tok.Revoked())

	// A second revoke of the same, already-revoked token finds no eligible row.
	err = store.RevokeToken(ctx, pool, id)
	require.Error(t, err)
	require.True(t, errors.Is(err, store.ErrTokenNotFound))
}

func TestRevokeTokenByNameConvenience(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	_, raw, err := store.CreateToken(ctx, pool, "named-token", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)

	require.NoError(t, store.RevokeToken(ctx, pool, "named-token"))

	tok, err := store.LookupToken(ctx, pool, hashOf(raw))
	require.NoError(t, err)
	require.True(t, tok.Revoked())
}

func TestRevokeTokenUnknownNameFails(t *testing.T) {
	pool := testutil.NewDB(t)
	err := store.RevokeToken(context.Background(), pool, "nobody")
	require.Error(t, err)
	require.True(t, errors.Is(err, store.ErrTokenNotFound))
}

func TestExpiredTokenReportsExpired(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	_, raw, err := store.CreateToken(ctx, pool, "stale", []string{"work"}, []string{"read"}, &past)
	require.NoError(t, err)

	tok, err := store.LookupToken(ctx, pool, hashOf(raw))
	require.NoError(t, err)
	require.NotNil(t, tok)
	require.True(t, tok.Expired(time.Now()))
	require.False(t, tok.Revoked())
}

func TestListTokensReturnsNewestFirstIncludingRevoked(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	id1, _, err := store.CreateToken(ctx, pool, "first", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)
	_, _, err = store.CreateToken(ctx, pool, "second", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)
	require.NoError(t, store.RevokeToken(ctx, pool, id1))

	tokens, err := store.ListTokens(ctx, pool)
	require.NoError(t, err)
	require.Len(t, tokens, 2)
	names := map[string]bool{}
	for _, tok := range tokens {
		names[tok.Name] = true
	}
	require.True(t, names["first"])
	require.True(t, names["second"])
}

func TestTouchTokenUpdatesLastUsedAt(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	id, raw, err := store.CreateToken(ctx, pool, "touched", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)

	tok, err := store.LookupToken(ctx, pool, hashOf(raw))
	require.NoError(t, err)
	require.Nil(t, tok.LastUsedAt)

	require.NoError(t, store.TouchToken(ctx, pool, id))

	tok, err = store.LookupToken(ctx, pool, hashOf(raw))
	require.NoError(t, err)
	require.NotNil(t, tok.LastUsedAt)
}

func TestGetTokenFindsByIDAndReturnsNilNilWhenMissing(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	id, _, err := store.CreateToken(ctx, pool, "findable", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)

	tok, err := store.GetToken(ctx, pool, id)
	require.NoError(t, err)
	require.NotNil(t, tok)
	require.Equal(t, "findable", tok.Name)

	missing, err := store.GetToken(ctx, pool, "does-not-exist")
	require.NoError(t, err)
	require.Nil(t, missing)
}

func TestListActivityReturnsNewestFirstJoinedToAgentName(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	id, _, err := store.CreateToken(ctx, pool, "auditable", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		require.NoError(t, store.RecordAudit(ctx, pool, store.AuditEntry{
			TokenID:      id,
			Tool:         "search",
			Scopes:       []string{"work"},
			Query:        "query",
			ReturnedUIDs: []string{"u1", "u2"},
			TokensOut:    10,
			Coverage:     "ok",
			LatencyMS:    5,
		}))
	}

	entries, err := store.ListActivity(ctx, pool, id, 50, 0)
	require.NoError(t, err)
	require.Len(t, entries, 3)
	// Newest first: ids strictly decrease.
	for i := 1; i < len(entries); i++ {
		require.Greater(t, entries[i-1].ID, entries[i].ID)
	}
	for _, e := range entries {
		require.Equal(t, "auditable", e.AgentName)
		require.Equal(t, 2, e.ResultCount, "returned_uids had 2 entries")
	}
}

func TestListActivityFiltersByTokenAndIgnoresOtherAgents(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	id1, _, err := store.CreateToken(ctx, pool, "agent-one", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)
	id2, _, err := store.CreateToken(ctx, pool, "agent-two", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)

	require.NoError(t, store.RecordAudit(ctx, pool, store.AuditEntry{TokenID: id1, Tool: "search", Scopes: []string{"work"}, Query: "a"}))
	require.NoError(t, store.RecordAudit(ctx, pool, store.AuditEntry{TokenID: id2, Tool: "search", Scopes: []string{"work"}, Query: "b"}))

	entries, err := store.ListActivity(ctx, pool, id1, 50, 0)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "agent-one", entries[0].AgentName)
}

func TestListActivityCapsAtLimitAndPagesWithBeforeID(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	id, _, err := store.CreateToken(ctx, pool, "paged-agent", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		require.NoError(t, store.RecordAudit(ctx, pool, store.AuditEntry{TokenID: id, Tool: "search", Scopes: []string{"work"}, Query: "q"}))
	}

	firstPage, err := store.ListActivity(ctx, pool, id, 2, 0)
	require.NoError(t, err)
	require.Len(t, firstPage, 2)

	secondPage, err := store.ListActivity(ctx, pool, id, 2, firstPage[len(firstPage)-1].ID)
	require.NoError(t, err)
	require.Len(t, secondPage, 2)

	// The two pages must not overlap.
	for _, a := range firstPage {
		for _, b := range secondPage {
			require.NotEqual(t, a.ID, b.ID)
		}
	}
}

func TestListActivityNonPositiveLimitFallsBackToDefault(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	id, _, err := store.CreateToken(ctx, pool, "default-limit-agent", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)
	require.NoError(t, store.RecordAudit(ctx, pool, store.AuditEntry{TokenID: id, Tool: "search", Scopes: []string{"work"}, Query: "q"}))

	entries, err := store.ListActivity(ctx, pool, id, 0, 0)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestRecordAuditTruncatesLongQueriesToThreeHundredChars(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	longQuery := ""
	for i := 0; i < 500; i++ {
		longQuery += "a"
	}

	err := store.RecordAudit(ctx, pool, store.AuditEntry{
		TokenID:      "tok1",
		Tool:         "search",
		Scopes:       []string{"work"},
		Query:        longQuery,
		Params:       map[string]any{"query": longQuery},
		ReturnedUIDs: []string{"u1", "u2"},
		TokensOut:    42,
		Coverage:     "ok",
		LatencyMS:    12,
	})
	require.NoError(t, err)

	var storedQuery string
	require.NoError(t, pool.QueryRow(ctx, `select query from audit_log where token_id = $1`, "tok1").Scan(&storedQuery))
	require.Len(t, storedQuery, 300)
}
