package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// The four pure unit tests that used to live in this file (refusing the (perMin+1)th
// call, the window rolling under an injected clock, independent per-token buckets, and
// race-safety under concurrent callers) moved to internal/ratelimit/ratelimit_test.go when
// RateLimiter itself moved there (see internal/ratelimit/ratelimit.go's doc comment). This
// test stays behind: it drives a real HTTP round trip through NewHandler's full stack
// (authMiddleware wrapping the MCP transport), so it belongs to this package, not to
// internal/ratelimit.

// TestRateLimiterRefusalSurfacesAs429OverRealHTTP is judgment call (b): the refusal has to
// actually reach a caller as 04 section 12's 429, not just as RateLimiter.Allow returning
// false in isolation -- so this drives a real httptest.Server running NewHandler's full
// stack (authMiddleware wrapping the MCP transport, exactly as server.go wires it), the
// same way e2e_test.go's TestEndToEndOverRealHTTPAndMCPClient does.
func TestRateLimiterRefusalSurfacesAs429OverRealHTTP(t *testing.T) {
	d := newTestDeps(t)
	raw := mintToken(t, d.pool, "rate-limited-agent", []string{"work"}, []string{"read"})

	// ratePerMinute: 1 -- the smallest limit that still lets the test distinguish
	// "allowed" from "refused" in exactly two requests.
	handler := NewHandler(d.pool, d.pages, d.order, 1)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	doRequest := func() *http.Response {
		req, err := http.NewRequest(http.MethodPost, srv.URL, http.NoBody)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+raw)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		return resp
	}

	first := doRequest()
	_ = first.Body.Close()
	require.NotEqual(t, http.StatusTooManyRequests, first.StatusCode,
		"the first call within the limit must not be rate limited")
	require.NotEqual(t, http.StatusUnauthorized, first.StatusCode,
		"a live token must authenticate")

	second := doRequest()
	defer second.Body.Close()
	require.Equal(t, http.StatusTooManyRequests, second.StatusCode,
		"the call past the per-minute limit must surface as 429")
}
