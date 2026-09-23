package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/dhia/balise/internal/api"
	"github.com/dhia/balise/internal/mcp"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
)

// testOwnerPassword is the single fixed password every auth test in this file signs in
// with, except the tests that deliberately use a different or wrong one.
const testOwnerPassword = "correct horse battery staple"

// newAuthServer is newServer's password-gated twin (same empty-DB, empty-git-vault, real
// spaces.yaml/order.yaml fixtures) built with api.WithOwnerPassword, so every route but the
// three auth endpoints requires a valid session cookie.
func newAuthServer(t *testing.T, password string) *httptest.Server {
	t.Helper()
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work", "client-globex"})
	spaces, err := registry.LoadSpaces("../../defaults/spaces.yaml")
	require.NoError(t, err)
	order, err := registry.LoadOrder("../../defaults/order.yaml")
	require.NoError(t, err)
	server := httptest.NewServer(api.New(q, newGitVault(t), spaces, order, "", api.WithOwnerPassword(password)))
	t.Cleanup(server.Close)
	return server
}

// newComboServer mounts /api and /mcp on one outer mux, exactly as cmd/balise/main.go does,
// so the two cross-system tests below exercise the real boundary between them rather than a
// reconstruction of it. It returns the shared pool too, since minting an MCP bearer token
// needs it directly (mcp.NewHandler takes the pool, not a *store.Queries).
func newComboServer(t *testing.T, password string) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewDB(t)
	q := store.NewQueries(pool, store.Scopes{"work", "client-globex"})
	spaces, err := registry.LoadSpaces("../../defaults/spaces.yaml")
	require.NoError(t, err)
	order, err := registry.LoadOrder("../../defaults/order.yaml")
	require.NoError(t, err)
	pages := newGitVault(t)

	outer := http.NewServeMux()
	outer.Handle("/", api.New(q, pages, spaces, order, "", api.WithOwnerPassword(password)))
	outer.Handle("/mcp", mcp.NewHandler(pool, pages, order, 0, nil))
	server := httptest.NewServer(outer)
	t.Cleanup(server.Close)
	return server, pool
}

// routeParam matches a "{name}" path-parameter segment in a mux pattern.
var routeParam = regexp.MustCompile(`\{[^}]+\}`)

// concreteRequest turns a routing-table pattern such as "GET /api/pages/{scope}/{slug}"
// into a method and a real, request-able path ("/api/pages/x/x"). The substituted value
// never needs to resolve to anything real: requireSession (auth.go) checks the raw request
// path against a small, literal set of public paths before the mux ever routes the request
// at all, so an unauthenticated call 401s regardless of whether the path would otherwise
// 404.
func concreteRequest(t *testing.T, pattern string) (method, path string) {
	t.Helper()
	parts := strings.SplitN(pattern, " ", 2)
	require.Len(t, parts, 2, "route pattern must be \"METHOD /path\": %q", pattern)
	return parts[0], routeParam.ReplaceAllString(parts[1], "x")
}

// newClientWithJar gives a test its own cookie jar, so login and subsequent calls behave
// exactly as a browser's fetch(..., {credentials: "include"}) would -- the session cookie
// set by one response is presented automatically on the next request to the same server.
func newClientWithJar(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	return &http.Client{Jar: jar}
}

// loginAs POSTs the given password to /api/auth/login and returns the raw response, letting
// each test decide what status/body to assert. Marshaled through encoding/json rather than
// a hand-built string so an unusual password (quotes, backslashes) is never at risk of
// producing invalid JSON.
func loginAs(t *testing.T, client *http.Client, serverURL, password string) *http.Response {
	t.Helper()
	body, err := json.Marshal(struct {
		Password string `json:"password"`
	}{Password: password})
	require.NoError(t, err)
	resp, err := client.Post(serverURL+"/api/auth/login", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	return resp
}

// TestEveryProtectedRouteRequiresSession is the brief's headline test: every route
// api.ProtectedRoutePatterns() enumerates (i.e. every route except the three auth
// endpoints) must 401 a caller with no session cookie at all. Enumerated from the route
// table itself, not hand-listed, so a future route is covered automatically instead of
// silently shipping unauthenticated by omission.
func TestEveryProtectedRouteRequiresSession(t *testing.T) {
	server := newAuthServer(t, testOwnerPassword)
	patterns := api.ProtectedRoutePatterns()
	require.NotEmpty(t, patterns)

	for _, pattern := range patterns {
		method, path := concreteRequest(t, pattern)
		t.Run(pattern, func(t *testing.T) {
			req, err := http.NewRequest(method, server.URL+path, nil)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusUnauthorized, resp.StatusCode,
				"%s %s must 401 without a session", method, path)
		})
	}
}

// TestSourcesUploadRequiresSessionWithRealBody pins the one route the brief calls out by
// name: the Sources drop zone's POST /api/sources/pages is the route that, before this
// feature, let anyone who could reach the port write into the vault with no auth check at
// all. A real body and real query parameters are sent here (unlike the placeholder-path
// sweep above) so nothing about carrying a payload changes the 401.
func TestSourcesUploadRequiresSessionWithRealBody(t *testing.T) {
	server := newAuthServer(t, testOwnerPassword)
	req, err := http.NewRequest(http.MethodPost,
		server.URL+"/api/sources/pages?scope=work&slug=unauthorized-write",
		strings.NewReader("# Should never land\n\nno session, no write."))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestAuthStatusIsPublicAndReportsRequired proves the flip side of the sweep above: the
// three auth endpoints themselves must stay reachable with no session, since a caller with
// none yet is the exact caller who needs to ask "do you need a password" and then supply
// one.
func TestAuthStatusIsPublicAndReportsRequired(t *testing.T) {
	server := newAuthServer(t, testOwnerPassword)
	resp, err := http.Get(server.URL + "/api/auth/status")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		AuthRequired  bool `json:"auth_required"`
		Authenticated bool `json:"authenticated"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.True(t, body.AuthRequired)
	require.False(t, body.Authenticated)
}

// TestLoginSetsUsableSessionCookie is the happy path: a correct password sets a cookie that
// then actually authenticates a protected route, not merely a 200 from /api/auth/login
// itself.
func TestLoginSetsUsableSessionCookie(t *testing.T) {
	server := newAuthServer(t, testOwnerPassword)
	client := newClientWithJar(t)

	resp := loginAs(t, client, server.URL, testOwnerPassword)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	treeResp, err := client.Get(server.URL + "/api/tree")
	require.NoError(t, err)
	defer treeResp.Body.Close()
	require.Equal(t, http.StatusOK, treeResp.StatusCode)
}

// TestLoginWithWrongPasswordIsRejectedAndSetsNoUsableCookie confirms the failure path sets
// nothing that later authenticates anything.
func TestLoginWithWrongPasswordIsRejectedAndSetsNoUsableCookie(t *testing.T) {
	server := newAuthServer(t, testOwnerPassword)
	client := newClientWithJar(t)

	resp := loginAs(t, client, server.URL, "definitely not the password")
	defer resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	treeResp, err := client.Get(server.URL + "/api/tree")
	require.NoError(t, err)
	defer treeResp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, treeResp.StatusCode)
}

// TestTamperedSessionCookieIsRejected flips one character of a real, freshly issued session
// cookie's value (part of its base64url-encoded HMAC tag) and confirms the tampered cookie
// no longer authenticates anything -- hmac.Equal must reject it, not merely a coincidental
// re-encoding mismatch.
func TestTamperedSessionCookieIsRejected(t *testing.T) {
	server := newAuthServer(t, testOwnerPassword)
	client := newClientWithJar(t)

	resp := loginAs(t, client, server.URL, testOwnerPassword)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	cookies := client.Jar.Cookies(u)
	require.Len(t, cookies, 1)
	tampered := *cookies[0]
	tampered.Value = flipLastChar(t, tampered.Value)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/tree", nil)
	require.NoError(t, err)
	req.AddCookie(&tampered)
	resp2, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp2.StatusCode)
}

// TestSessionSignedUnderDifferentPasswordIsRejected proves the HKDF-derived-key half of the
// design: a session cookie that is otherwise perfectly well-formed, issued by a server
// configured with a different owner password, must not authenticate against this server --
// the two servers' signing keys are derived from different passwords and so never agree.
func TestSessionSignedUnderDifferentPasswordIsRejected(t *testing.T) {
	server := newAuthServer(t, testOwnerPassword)
	otherPassword := "an entirely different password"
	otherServer := newAuthServer(t, otherPassword)

	client := newClientWithJar(t)
	resp := loginAs(t, client, otherServer.URL, otherPassword)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	otherURL, err := url.Parse(otherServer.URL)
	require.NoError(t, err)
	cookies := client.Jar.Cookies(otherURL)
	require.Len(t, cookies, 1)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/tree", nil)
	require.NoError(t, err)
	req.AddCookie(cookies[0])
	resp2, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp2.StatusCode)
}

// TestLogoutClearsSession drives a real net/http.CookieJar through login, a successful
// authenticated call, logout, and a subsequent call that must be unauthenticated again --
// exercising the jar's own handling of logout's Set-Cookie (MaxAge -1), not just asserting
// on the response header's literal text.
func TestLogoutClearsSession(t *testing.T) {
	server := newAuthServer(t, testOwnerPassword)
	client := newClientWithJar(t)

	resp := loginAs(t, client, server.URL, testOwnerPassword)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	treeResp, err := client.Get(server.URL + "/api/tree")
	require.NoError(t, err)
	treeResp.Body.Close()
	require.Equal(t, http.StatusOK, treeResp.StatusCode)

	logoutResp, err := client.Post(server.URL+"/api/auth/logout", "application/json", nil)
	require.NoError(t, err)
	logoutResp.Body.Close()
	require.Equal(t, http.StatusOK, logoutResp.StatusCode)

	afterResp, err := client.Get(server.URL + "/api/tree")
	require.NoError(t, err)
	defer afterResp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, afterResp.StatusCode)
}

// TestRepeatedFailedLoginsAreRateLimited proves login attempts are throttled per caller.
// The exact configured limit (auth.go's loginRateLimitPerMinute) is an implementation
// detail this test does not hard-code; it simply sends comfortably more failed attempts
// than any reasonable limit and requires a 429 to appear before giving up.
func TestRepeatedFailedLoginsAreRateLimited(t *testing.T) {
	server := newAuthServer(t, testOwnerPassword)
	client := &http.Client{}

	var sawTooManyRequests bool
	for i := 0; i < 25; i++ {
		resp := loginAs(t, client, server.URL, "definitely not the password")
		status := resp.StatusCode
		resp.Body.Close()
		require.Contains(t, []int{http.StatusUnauthorized, http.StatusTooManyRequests}, status)
		if status == http.StatusTooManyRequests {
			sawTooManyRequests = true
			break
		}
	}
	require.True(t, sawTooManyRequests, "repeated failed logins from one caller must eventually be rate limited")
}

// TestOwnerSessionCookieDoesNotAuthenticateMCP is one direction of the brief's "add a test
// for both directions": a valid owner session cookie, which does authenticate /api, must
// not authenticate /mcp. authMiddleware (internal/mcp/server.go) only ever looks at the
// Authorization header, never at cookies, so this holds structurally -- this test pins that
// nobody accidentally wires the two together later.
func TestOwnerSessionCookieDoesNotAuthenticateMCP(t *testing.T) {
	server, _ := newComboServer(t, testOwnerPassword)
	client := newClientWithJar(t)

	resp := loginAs(t, client, server.URL, testOwnerPassword)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Sanity check: the cookie really does authenticate /api on this same combo server.
	treeResp, err := client.Get(server.URL + "/api/tree")
	require.NoError(t, err)
	treeResp.Body.Close()
	require.Equal(t, http.StatusOK, treeResp.StatusCode)

	mcpResp, err := client.Get(server.URL + "/mcp")
	require.NoError(t, err)
	defer mcpResp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, mcpResp.StatusCode)
}

// TestMCPBearerTokenDoesNotAuthenticateAPI is the other direction: a live MCP agent bearer
// token, minted the same way `balise token create` does, must not authenticate /api --
// requireSession (auth.go) only ever looks at the session cookie, never at the
// Authorization header.
func TestMCPBearerTokenDoesNotAuthenticateAPI(t *testing.T) {
	server, pool := newComboServer(t, testOwnerPassword)

	_, raw, err := store.CreateToken(context.Background(), pool, "cross-system-test-agent",
		[]string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)

	// Sanity check: the token really does authenticate /mcp on this same combo server.
	mcpReq, err := http.NewRequest(http.MethodGet, server.URL+"/mcp", nil)
	require.NoError(t, err)
	mcpReq.Header.Set("Authorization", "Bearer "+raw)
	mcpResp, err := http.DefaultClient.Do(mcpReq)
	require.NoError(t, err)
	mcpResp.Body.Close()
	require.NotEqual(t, http.StatusUnauthorized, mcpResp.StatusCode)

	apiReq, err := http.NewRequest(http.MethodGet, server.URL+"/api/tree", nil)
	require.NoError(t, err)
	apiReq.Header.Set("Authorization", "Bearer "+raw)
	apiResp, err := http.DefaultClient.Do(apiReq)
	require.NoError(t, err)
	defer apiResp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, apiResp.StatusCode)
}

// flipLastChar changes a character near the end of a base64url-encoded string to a
// different valid base64url character, keeping the value syntactically well-formed (still
// decodes) while guaranteeing its signature no longer verifies.
//
// It deliberately targets the second-to-last character rather than the very last one: in
// unpadded base64 (RawURLEncoding), when the encoded byte count isn't a multiple of 3, the
// final character's low-order bits are unused padding that a lenient decoder discards. For a
// fixed-length value (our HMAC-SHA256 tag is always 32 bytes), that makes the true last
// character's low bits a coin flip that sometimes decodes identically either way -- flipping
// it can silently be a no-op and let a "tampered" cookie still verify. The second-to-last
// character never falls on a padding-only boundary, so flipping it always changes the
// decoded bytes.
func flipLastChar(t *testing.T, value string) string {
	t.Helper()
	require.GreaterOrEqual(t, len(value), 2)
	b := []byte(value)
	i := len(b) - 2
	if b[i] == 'A' {
		b[i] = 'B'
	} else {
		b[i] = 'A'
	}
	return string(b)
}
