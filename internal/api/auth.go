package api

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dhia/balise/internal/ratelimit"
)

// This file is the whole of Balise's owner authentication: one shared password, one
// signed session cookie, no accounts, no roles, no sessions table. 04 section 13 already
// specified this shape ("session cookie for the owner"); the design was approved as-is and
// deliberately kept this small. /mcp's bearer-token auth (auth.go/server.go in
// internal/mcp) is a completely separate system with its own lifetimes and revocation
// story -- nothing here reads or writes anything that package owns, and nothing there
// reads or writes anything this file owns.

const (
	// sessionCookieName is host-scoped (no Domain attribute is ever set on it), so it is
	// presented on any port on the same host -- including, deliberately, the frozen
	// fixture backend the Playwright visual suite runs on a different port. See the
	// completion report for why that is relied upon rather than worked around.
	sessionCookieName = "balise_session"

	// sessionTTL is how long a session cookie is valid for before it must be re-issued
	// by logging in again. Nothing in 04 pins an exact figure; 30 days matches "log in
	// once, stay in" for a single-owner local tool while still expiring eventually.
	sessionTTL = 30 * 24 * time.Hour

	// hkdfSalt and hkdfInfo are fixed, public context strings for the HKDF derivation
	// (RFC 5869) that turns the owner's password into the HMAC key sessions are signed
	// with. Neither is a secret -- HKDF's security comes from the password (the secret
	// input), not from these being hidden -- they only need to be stable across restarts
	// and distinct from any other HKDF use in this codebase, so old sessions keep
	// verifying after a restart and a changed password can never accidentally re-derive
	// the same key.
	hkdfSalt = "balise-owner-auth-v1"
	hkdfInfo = "balise-owner-session-signing-key"

	// loginRateLimitPerMinute deliberately sits far below internal/ratelimit's 60/min MCP
	// default: a human typing a password by hand will never need more than a handful of
	// attempts a minute, while a script guessing passwords benefits enormously from a
	// tight limit.
	loginRateLimitPerMinute = 5
)

// ownerAuth is present on *server exactly when an owner password was configured via
// WithOwnerPassword; a nil *ownerAuth (the zero value of the server.auth field) means no
// password was configured at all, and every /api request is left open -- the fail-open
// default for loopback-only local use that cmd/balise/main.go's fail-closed check (not
// this package) turns into a hard startup refusal once the bind address is not loopback.
type ownerAuth struct {
	// passwordDigest is sha256(password), compared in constant time against sha256 of
	// each login attempt -- never the raw password itself, and never compared with a
	// naive == or bytes.Equal, both of which can return in variable time depending on
	// where the first mismatched byte falls.
	passwordDigest [sha256.Size]byte

	// signingKey is derived from the password via HKDF, never persisted anywhere and
	// never itself stored on disk or in the database: it is recomputed fresh from
	// BALISE_PASSWORD every time the server starts. That gives two properties for free,
	// both wanted: a restart does not invalidate outstanding sessions (the same password
	// derives the same key), and changing the password invalidates every outstanding
	// session automatically (a different password derives a different key, so no old
	// cookie's signature verifies anymore) -- with no session store to clear by hand.
	signingKey []byte

	// limiter throttles login *attempts* (not authenticated calls -- those are the
	// concern of every other /api route, which requireSession gates on having a session
	// at all, not on a rate). Keyed by the caller's address (see clientKey): there is
	// exactly one password and one legitimate caller, so keying by anything
	// request-supplied (a header, a claimed username) would let an attacker simply
	// change that value to dodge the limit.
	limiter *ratelimit.RateLimiter
}

// newOwnerAuth derives everything ownerAuth needs from password once, at startup.
func newOwnerAuth(password string) *ownerAuth {
	digest := sha256.Sum256([]byte(password))
	key, err := hkdf.Key(sha256.New, []byte(password), []byte(hkdfSalt), hkdfInfo, sha256.Size)
	if err != nil {
		// hkdf.Key only fails when the requested key length exceeds what the hash can
		// expand to (255 * hash size); sha256.Size (32) is always satisfiable, so this
		// is unreachable in practice. Panicking rather than silently disabling auth is
		// the safe failure mode for a startup-time, deterministic computation like this.
		panic(fmt.Sprintf("derive owner session signing key: %v", err))
	}
	return &ownerAuth{
		passwordDigest: digest,
		signingKey:     key,
		limiter:        ratelimit.NewRateLimiter(loginRateLimitPerMinute),
	}
}

// checkPassword reports whether candidate is the configured owner password, comparing
// digests (not raw strings, not raw lengths) in constant time so neither a length nor a
// byte-position difference is observable from response timing.
func (a *ownerAuth) checkPassword(candidate string) bool {
	candidateDigest := sha256.Sum256([]byte(candidate))
	return subtle.ConstantTimeCompare(a.passwordDigest[:], candidateDigest[:]) == 1
}

// errInvalidSession covers every way a session cookie can fail to verify: malformed,
// tampered, signed under a different password (a different signingKey), or expired. It is
// deliberately one error, not several, so a caller (and 04's "expired/revoked -> 401"
// posture, reused here for the owner cookie) can never distinguish "wrong password once
// used to sign this" from "expired" from "corrupted" -- any distinction there would leak
// information for free to an attacker holding a stale or forged cookie.
var errInvalidSession = errors.New("invalid session")

// newSessionToken mints a fresh, signed session good for sessionTTL from now.
func (a *ownerAuth) newSessionToken() string {
	expiry := time.Now().Add(sessionTTL).Unix()
	payload := "owner." + strconv.FormatInt(expiry, 10)
	return a.signPayload(payload)
}

// signPayload returns payload plus an HMAC-SHA256 tag over it, both base64url-encoded and
// joined with ".": <base64url(payload)>.<base64url(hmac)>. The payload is encoded, not
// left raw, purely so it can never itself contain the "." separator.
func (a *ownerAuth) signPayload(payload string) string {
	mac := hmac.New(sha256.New, a.signingKey)
	mac.Write([]byte(payload))
	tag := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(tag)
}

// verifySessionToken reports errInvalidSession unless token is exactly a payload this
// ownerAuth's signingKey produced (so a token signed under a different password's derived
// key is rejected -- see the signingKey field's doc comment), unmodified since (so a
// tampered token is rejected), and not yet past its embedded expiry.
func (a *ownerAuth) verifySessionToken(token string) error {
	encPayload, encTag, ok := strings.Cut(token, ".")
	if !ok {
		return errInvalidSession
	}
	payload, err := base64.RawURLEncoding.DecodeString(encPayload)
	if err != nil {
		return errInvalidSession
	}
	gotTag, err := base64.RawURLEncoding.DecodeString(encTag)
	if err != nil {
		return errInvalidSession
	}

	mac := hmac.New(sha256.New, a.signingKey)
	mac.Write(payload)
	wantTag := mac.Sum(nil)
	if !hmac.Equal(gotTag, wantTag) {
		return errInvalidSession
	}

	kind, expiryStr, ok := strings.Cut(string(payload), ".")
	if !ok || kind != "owner" {
		return errInvalidSession
	}
	expiry, err := strconv.ParseInt(expiryStr, 10, 64)
	if err != nil {
		return errInvalidSession
	}
	if time.Now().Unix() > expiry {
		return errInvalidSession
	}
	return nil
}

// hasValidSession reports whether r carries a balise_session cookie that verifies against
// s.auth. It is nil-safe on s.auth itself only in the sense that callers must check
// s.auth != nil first (see requireSession) -- ownerAuth's methods all assume a live
// receiver, matching every other method on this type.
func (s *server) hasValidSession(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	return s.auth.verifySessionToken(cookie.Value) == nil
}

// isRequestSecure reports whether r arrived over TLS, directly (r.TLS set by Go's own TLS
// listener) or via a reverse proxy that terminates TLS and says so with the conventional
// X-Forwarded-Proto header (Tailscale serve, an nginx/Caddy tunnel, etc). It gates the
// cookie's Secure flag: setting Secure unconditionally would make the cookie unusable over
// plain HTTP, which is how this tool is used locally by default (04 section 19; also see
// the Makefile's own plaintext-password warning for exposed, non-TLS deployments).
func isRequestSecure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// sessionCookie builds the Set-Cookie value shared by login (a live token) and logout (an
// immediately-expired one), so the flags in the design brief -- HttpOnly always,
// SameSite=Lax always, Secure only over HTTPS -- can never drift between the two call
// sites.
func sessionCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   isRequestSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
}

// clientKey identifies the caller for login rate limiting: the connecting address's host,
// with the ephemeral port stripped (two attempts from the same machine on different local
// ports must share one bucket). Falls back to the raw RemoteAddr on the rare request where
// it is not in host:port form.
func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// authStatusResponse is what GET /api/auth/status, and the login/logout endpoints, all
// report back: whether a password is configured at all (auth_required -- the SPA never
// needs a login screen when this is false) and whether the current request is currently
// authenticated.
type authStatusResponse struct {
	AuthRequired  bool `json:"auth_required"`
	Authenticated bool `json:"authenticated"`
}

// handleAuthStatus lets the SPA ask, before rendering anything else, whether it needs to
// show a login screen. It is deliberately one of the few /api routes requireSession never
// gates -- see isPublicAuthPath -- since a caller with no session yet must be able to ask
// this without already having one.
func (s *server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeJSON(w, http.StatusOK, authStatusResponse{AuthRequired: false, Authenticated: true})
		return
	}
	writeJSON(w, http.StatusOK, authStatusResponse{AuthRequired: true, Authenticated: s.hasValidSession(r)})
}

// loginRequest is the JSON body POST /api/auth/login expects: {"password": "..."}.
type loginRequest struct {
	Password string `json:"password"`
}

// handleAuthLogin checks the submitted password (constant-time) and, on success, sets the
// signed session cookie. Rate limited per clientKey before the password is even parsed, so
// a caller already over the limit pays no extra cost for the attempt.
func (s *server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeJSON(w, http.StatusOK, authStatusResponse{AuthRequired: false, Authenticated: true})
		return
	}
	if !s.auth.limiter.Allow(clientKey(r)) {
		writeError(w, http.StatusTooManyRequests, "too many login attempts; wait a minute and try again")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !s.auth.checkPassword(req.Password) {
		writeError(w, http.StatusUnauthorized, "incorrect password")
		return
	}

	http.SetCookie(w, sessionCookie(r, s.auth.newSessionToken(), int(sessionTTL.Seconds())))
	writeJSON(w, http.StatusOK, authStatusResponse{AuthRequired: true, Authenticated: true})
}

// handleAuthLogout clears the session cookie unconditionally -- it never requires a valid
// session first (a stale, tampered, or already-expired cookie must still be clearable) --
// and reports the resulting (unauthenticated) status.
func (s *server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, sessionCookie(r, "", -1))
	writeJSON(w, http.StatusOK, authStatusResponse{AuthRequired: s.auth != nil, Authenticated: false})
}

// requireSession is the middleware that makes owner auth actually bind: it rejects every
// /api request with a plain 401 unless s.auth is nil (no password configured -- the
// fail-open loopback default), the path is one of the auth endpoints themselves (a caller
// with no session must still be able to check status, log in, or log out), or the request
// already carries a valid session cookie. Nothing downstream (any handler in this package)
// re-checks authentication itself; this is the one and only gate.
func (s *server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.auth == nil || isPublicAuthPath(r.URL.Path) || s.hasValidSession(r) {
			next.ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusUnauthorized, "authentication required")
	})
}

// publicAuthPaths is derived from apiRoutes' own public flag (see server.go) rather than
// hand-listed a second time here, so a route's public/protected status can never drift
// between the routing table and the middleware that enforces it.
var publicAuthPaths = buildPublicAuthPaths()

func buildPublicAuthPaths() map[string]bool {
	paths := map[string]bool{}
	for _, route := range apiRoutes {
		if route.public {
			paths[route.path()] = true
		}
	}
	return paths
}

func isPublicAuthPath(path string) bool {
	return publicAuthPaths[path]
}
