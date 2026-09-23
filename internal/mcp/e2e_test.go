package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/dhia/balise/internal/store"
)

// authRoundTripper injects a fixed "Authorization: Bearer <token>" header
// on every outgoing request. go-sdk v1.0.0's StreamableClientTransport has
// no public field for a custom auth header (its only such mechanism is an
// unexported, hardcoded test-only toggle in the SDK's own source), so a
// custom http.RoundTripper is the standard way to add one.
type authRoundTripper struct {
	token string
	next  http.RoundTripper
}

func (rt authRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+rt.token)
	return rt.next.RoundTrip(req)
}

// TestEndToEndOverRealHTTPAndMCPClient is the one true wiring test: a real
// httptest.Server running NewHandler's full stack (the HTTP auth
// middleware, the streamable HTTP transport, the MCP server, and all three
// tools), driven by a real sdk.Client over a real StreamableClientTransport.
// Every other test in this package calls tool handlers directly, in
// process, to stay fast and to construct hostile inputs precisely; this
// test exists purely to confirm the transport and auth wiring in server.go
// actually holds together end to end.
func TestEndToEndOverRealHTTPAndMCPClient(t *testing.T) {
	d := newTestDeps(t)
	ctx := context.Background()
	_, raw, err := store.CreateToken(ctx, d.pool, "e2e-agent", []string{"work"}, []string{"read", "remember"}, nil)
	require.NoError(t, err)

	// ratePerMinute: 0 exercises NewHandler's own default-fallback path
	// (RateLimiter's default of 60/min), which comfortably covers this
	// test's two sequential calls without tripping the limiter.
	handler := NewHandler(d.pool, d.pages, d.order, 0, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	httpClient := &http.Client{Transport: authRoundTripper{token: raw, next: http.DefaultTransport}}
	transport := &sdk.StreamableClientTransport{Endpoint: srv.URL, HTTPClient: httpClient}

	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	result, err := session.CallTool(ctx, &sdk.CallToolParams{
		Name:      "remember",
		Arguments: map[string]any{"text": "hello from the real transport", "scope": "work"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "remember over the real transport must not report a tool error")
	require.NotNil(t, result.StructuredContent)

	raw2, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var out RememberOutput
	require.NoError(t, json.Unmarshal(raw2, &out))
	require.Contains(t, out.Path, "work/memory/e2e-agent/")

	data, _, err := d.pages.Read(out.Path)
	require.NoError(t, err)
	require.Contains(t, string(data), "hello from the real transport")

	// And an unauthenticated call over the same real transport is rejected
	// before the MCP layer ever runs -- 04 section 12's "expired/revoked ->
	// 401" applies just as much to a wholly unknown token.
	badClient := sdk.NewClient(&sdk.Implementation{Name: "bad-client", Version: "0.0.1"}, nil)
	badHTTPClient := &http.Client{Transport: authRoundTripper{token: "not-a-real-token", next: http.DefaultTransport}}
	badTransport := &sdk.StreamableClientTransport{Endpoint: srv.URL, HTTPClient: badHTTPClient}
	_, err = badClient.Connect(ctx, badTransport, nil)
	require.Error(t, err, "a request with an unknown bearer token must never reach the MCP protocol layer")
}
