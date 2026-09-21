package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dhia/balise/internal/store"
)

// ErrUnauthenticated means the call carried no bearer token that resolves
// to a live agent_tokens row: missing header, unknown token, expired, or
// revoked. 04 section 12: "expired/revoked -> 401."
var ErrUnauthenticated = errors.New("unauthenticated")

const bearerPrefix = "Bearer "

// touchToken is a package-level variable, not a direct call to
// store.TouchToken, purely so a test in this package can substitute a fake
// that returns an error -- proving authenticate() never fails a call just
// because recording last_used_at failed -- without needing a live database
// failure to induce one. Production code never reassigns it.
var touchToken = store.TouchToken

// bearerToken extracts the raw token value from an "Authorization: Bearer
// <token>" header, or "" if the header is absent or not a bearer token.
func bearerToken(h http.Header) string {
	if h == nil {
		return ""
	}
	v := h.Get("Authorization")
	if !strings.HasPrefix(v, bearerPrefix) {
		return ""
	}
	return strings.TrimPrefix(v, bearerPrefix)
}

// hashToken returns the hex-encoded sha256 of raw, matching how
// internal/store's CreateToken/LookupToken hash and store the raw token
// value: "32 random bytes, base64url, shown once; sha256 stored" (04
// section 14).
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// lookupLiveToken resolves raw's hash to an AgentToken that is neither
// unknown, revoked, nor expired. Both the HTTP-level auth middleware and
// every tool handler call this, so a token that goes bad mid-connection
// (revoked between two calls in the same session) is refused on the very
// next call, not just at connection time.
func lookupLiveToken(ctx context.Context, pool *pgxpool.Pool, raw string) (*store.AgentToken, error) {
	if raw == "" {
		return nil, fmt.Errorf("%w: missing bearer token", ErrUnauthenticated)
	}
	tok, err := store.LookupToken(ctx, pool, hashToken(raw))
	if err != nil {
		return nil, fmt.Errorf("look up token: %w", err)
	}
	if tok == nil {
		return nil, fmt.Errorf("%w: unknown token", ErrUnauthenticated)
	}
	if tok.Revoked() {
		return nil, fmt.Errorf("%w: token revoked", ErrUnauthenticated)
	}
	if tok.Expired(time.Now()) {
		return nil, fmt.Errorf("%w: token expired", ErrUnauthenticated)
	}
	return tok, nil
}

// authenticate resolves the bearer token carried on req's HTTP header (via
// req.Extra.Header -- RequestExtra is populated by the streamable HTTP
// transport from the incoming request) into a live AgentToken.
//
// Tool handlers call this independently of the HTTP-level auth middleware
// in server.go, rather than threading a pre-resolved token through
// context: it keeps each handler independently testable by constructing a
// *sdk.CallToolRequest directly (no HTTP transport needed), and it is the
// only path by which a handler ever learns which token is calling it --
// there is no second, uncontrolled way for a handler to identify its
// caller. That makes it the one choke point common to all six tool
// handlers (context, fetch, pages, related, remember, search), and so the
// right place to record that the token was just used successfully: doing
// it here, once, covers every handler uniformly instead of requiring each
// one to remember to call store.TouchToken itself (previously only
// remember.go did, which is why last_used_at never moved for the five
// read-only tools).
//
// Touching the token is deliberately best-effort in two ways, both
// intentional:
//
//   - A failure to record "last used" must never turn a successful
//     authentication into a failed tool call -- see store.TouchToken's own
//     doc comment ("a courtesy, not part of the security boundary"). The
//     error is logged, not swallowed outright and not returned: logging
//     satisfies "never silently swallow errors" while still leaving the
//     call itself unaffected.
//   - The extra write is unconditional, not throttled or batched. At this
//     scale (human- or agent-paced tool calls, already capped by
//     RateLimiter at 60/token/minute) it is a single UPDATE by primary
//     key -- negligible load -- so throttling it further would add
//     complexity, and a second layer of staleness on top of
//     last_used_at, for no real benefit.
func authenticate(ctx context.Context, pool *pgxpool.Pool, req *sdk.CallToolRequest) (*store.AgentToken, error) {
	var header http.Header
	if req != nil && req.Extra != nil {
		header = req.Extra.Header
	}
	tok, err := lookupLiveToken(ctx, pool, bearerToken(header))
	if err != nil {
		return nil, err
	}
	if err := touchToken(ctx, pool, tok.ID); err != nil {
		slog.Warn("touch token: recording last_used_at failed; auth still succeeds", "token_id", tok.ID, "error", err)
	}
	return tok, nil
}
