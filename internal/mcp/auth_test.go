package mcp

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/dhia/balise/internal/store"
)

func TestLookupLiveTokenRejectsEmptyToken(t *testing.T) {
	d := newTestDeps(t)
	_, err := lookupLiveToken(context.Background(), d.pool, "")
	require.ErrorIs(t, err, ErrUnauthenticated)
}

func TestLookupLiveTokenRejectsUnknownToken(t *testing.T) {
	d := newTestDeps(t)
	_, err := lookupLiveToken(context.Background(), d.pool, "not-a-real-token")
	require.ErrorIs(t, err, ErrUnauthenticated)
}

func TestLookupLiveTokenRejectsRevokedToken(t *testing.T) {
	d := newTestDeps(t)
	ctx := context.Background()
	raw := mintToken(t, d.pool, "revoked-one", []string{"work"}, []string{"read"})
	require.NoError(t, store.RevokeToken(ctx, d.pool, "revoked-one"))

	_, err := lookupLiveToken(ctx, d.pool, raw)
	require.ErrorIs(t, err, ErrUnauthenticated)
}

func TestLookupLiveTokenRejectsExpiredToken(t *testing.T) {
	d := newTestDeps(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Hour)
	_, raw, err := store.CreateToken(ctx, d.pool, "stale-one", []string{"work"}, []string{"read"}, &past)
	require.NoError(t, err)

	_, err = lookupLiveToken(ctx, d.pool, raw)
	require.ErrorIs(t, err, ErrUnauthenticated)
}

func TestLookupLiveTokenAcceptsLiveToken(t *testing.T) {
	d := newTestDeps(t)
	raw := mintToken(t, d.pool, "live-one", []string{"work"}, []string{"read"})

	tok, err := lookupLiveToken(context.Background(), d.pool, raw)
	require.NoError(t, err)
	require.Equal(t, "live-one", tok.Name)
}

func TestBearerTokenExtractsRawValue(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer abc123")
	require.Equal(t, "abc123", bearerToken(h))
}

func TestBearerTokenIgnoresNonBearerScheme(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Basic abc123")
	require.Equal(t, "", bearerToken(h))
}

func TestBearerTokenHandlesNilAndEmptyHeader(t *testing.T) {
	require.Equal(t, "", bearerToken(nil))
	require.Equal(t, "", bearerToken(http.Header{}))
}

func TestAuthenticateResolvesTokenFromRequestExtraHeader(t *testing.T) {
	d := newTestDeps(t)
	raw := mintToken(t, d.pool, "via-request", []string{"work"}, []string{"read"})

	tok, err := authenticate(context.Background(), d.pool, reqWithToken(raw))
	require.NoError(t, err)
	require.Equal(t, "via-request", tok.Name)
}

func TestAuthenticateRejectsNilRequestAndNilExtra(t *testing.T) {
	d := newTestDeps(t)

	_, err := authenticate(context.Background(), d.pool, nil)
	require.ErrorIs(t, err, ErrUnauthenticated)

	_, err = authenticate(context.Background(), d.pool, reqWithToken(""))
	require.ErrorIs(t, err, ErrUnauthenticated)
}

// TestAuthenticateRecordsLastUsedAt is Fix 1's central claim: a successful
// authenticate() call must move last_used_at, so the Agents screen's "Never
// used" badge actually reflects real activity instead of never updating.
// Previously only remember.go called store.TouchToken, so the five
// read-only tools (context, fetch, pages, related, search) never touched
// it at all -- routing the touch through authenticate(), the one choke
// point common to every handler, fixes all six at once.
func TestAuthenticateRecordsLastUsedAt(t *testing.T) {
	d := newTestDeps(t)
	ctx := context.Background()
	raw := mintToken(t, d.pool, "touch-me", []string{"work"}, []string{"read"})

	// lookupLiveToken alone (unlike authenticate) never touches the token,
	// so this is a clean "before" snapshot -- a freshly minted token has
	// never been used.
	before, err := lookupLiveToken(ctx, d.pool, raw)
	require.NoError(t, err)
	require.Nil(t, before.LastUsedAt)

	tok, err := authenticate(ctx, d.pool, reqWithToken(raw))
	require.NoError(t, err)

	after, err := store.GetToken(ctx, d.pool, tok.ID)
	require.NoError(t, err)
	require.NotNil(t, after.LastUsedAt, "authenticate must record last_used_at on success")
}

// TestAuthenticateSurvivesTouchTokenFailure is Fix 1's other half: a
// bookkeeping failure while recording last_used_at must never turn an
// otherwise-successful authentication into a failed call -- store.
// TouchToken's own doc comment calls this "a courtesy, not part of the
// security boundary." touchToken is swapped for a fake that always errors,
// proving authenticate() logs and moves on rather than propagating it.
func TestAuthenticateSurvivesTouchTokenFailure(t *testing.T) {
	d := newTestDeps(t)
	raw := mintToken(t, d.pool, "touch-fails", []string{"work"}, []string{"read"})

	original := touchToken
	touchToken = func(ctx context.Context, pool *pgxpool.Pool, id string) error {
		return errors.New("simulated touch failure")
	}
	t.Cleanup(func() { touchToken = original })

	tok, err := authenticate(context.Background(), d.pool, reqWithToken(raw))
	require.NoError(t, err, "a failed touch must not fail authentication")
	require.Equal(t, "touch-fails", tok.Name)
}
