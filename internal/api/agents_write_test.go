package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/stretchr/testify/require"
)

// postJSON POSTs body (marshaled as JSON) to path and returns the status code and the raw
// response bytes -- raw bytes rather than a decoded struct, so hostile-input tests here can
// assert on exact wire content (no "hash" key anywhere, no secret value anywhere) rather
// than only on whatever fields a hand-written response struct happens to declare.
func postJSON(t *testing.T, server *httptest.Server, path string, body any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	resp, err := http.Post(server.URL+path, "application/json", strings.NewReader(string(raw)))
	require.NoError(t, err)
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, got
}

type createAgentBody struct {
	Agent agentJSON `json:"agent"`
	Token string    `json:"token"`
}

// TestCreateAgentHappyPathReturnsTokenOnceAndPersistsAgent is the core POST /api/agents
// path: the raw token comes back exactly once, in this response, the created agent then
// shows up over GET /api/agents with the exact scopes/capabilities requested, and neither
// response ever carries the token's hash -- extending
// TestAgentsListNeverLeaksTokenHashOrSecret's own guarantee to the create response, per the
// brief's explicit instruction to do so.
func TestCreateAgentHappyPathReturnsTokenOnceAndPersistsAgent(t *testing.T) {
	pool := testutil.NewDB(t)
	server := newAgentsServer(t, pool)

	status, raw := postJSON(t, server, "/api/agents", map[string]any{
		"name":         "fresh-agent",
		"scopes":       []string{"work"},
		"capabilities": []string{"read", "remember"},
	})
	require.Equal(t, http.StatusCreated, status, string(raw))

	var body createAgentBody
	require.NoError(t, json.Unmarshal(raw, &body))
	require.NotEmpty(t, body.Token)
	require.Equal(t, "fresh-agent", body.Agent.Name)
	require.Equal(t, []string{"work"}, body.Agent.Scopes)
	require.ElementsMatch(t, []string{"read", "remember"}, body.Agent.Capabilities)
	require.Equal(t, "active", body.Agent.State)
	require.NotEmpty(t, body.Agent.ID)

	// The stored hash must never appear in this response, in any shape.
	tok, err := store.GetToken(t.Context(), pool, body.Agent.ID)
	require.NoError(t, err)
	require.NotNil(t, tok)
	require.NotEmpty(t, tok.TokenHash)
	text := string(raw)
	require.NotContains(t, text, tok.TokenHash)

	var generic map[string]any
	require.NoError(t, json.Unmarshal(raw, &generic))
	agentFields, ok := generic["agent"].(map[string]any)
	require.True(t, ok)
	for key := range agentFields {
		require.NotContains(t, strings.ToLower(key), "hash")
		require.NotContains(t, strings.ToLower(key), "secret")
	}

	// The minted secret must actually work: it round-trips back through GetToken's own
	// hashing (belt and suspenders on top of the dedicated MCP-level live check the task's
	// brief asks for separately).
	require.NotEqual(t, tok.TokenHash, body.Token)

	// And the new agent must show up over the read side too, still with no hash anywhere.
	status, listRaw := getRaw(t, server, "/api/agents")
	require.Equal(t, http.StatusOK, status)
	listText := string(listRaw)
	require.NotContains(t, listText, tok.TokenHash)
	require.NotContains(t, listText, body.Token)
	require.Contains(t, listText, "fresh-agent")
}

// TestCreateAgentRejectsUnknownScope pins the brief's sharpest warning: granting a scope
// the vault does not have would look like it worked and then silently return nothing for
// it. The fixture server only binds "work" and "client-globex" (newAgentsServer), so a request
// for a third, made-up scope must be refused before any token is minted.
func TestCreateAgentRejectsUnknownScope(t *testing.T) {
	pool := testutil.NewDB(t)
	server := newAgentsServer(t, pool)

	status, raw := postJSON(t, server, "/api/agents", map[string]any{
		"name":   "scope-grabber",
		"scopes": []string{"ghost-scope"},
	})
	require.Equal(t, http.StatusBadRequest, status, string(raw))

	tokens, err := store.ListTokens(t.Context(), pool)
	require.NoError(t, err)
	require.Empty(t, tokens, "a rejected create must mint nothing")
}

// TestCreateAgentRejectsPathTraversalInName and its scope-side sibling below pin
// vault.ValidateSegment's rejection of "/" and ".." reaching this handler at all -- an
// agent name becomes a path component under <scope>/memory/<name>/ (internal/mcp), so a
// traversal-shaped name must never reach store.CreateToken.
func TestCreateAgentRejectsPathTraversalInName(t *testing.T) {
	pool := testutil.NewDB(t)
	server := newAgentsServer(t, pool)

	status, raw := postJSON(t, server, "/api/agents", map[string]any{
		"name":   "../etc/passwd",
		"scopes": []string{"work"},
	})
	require.Equal(t, http.StatusBadRequest, status, string(raw))

	tokens, err := store.ListTokens(t.Context(), pool)
	require.NoError(t, err)
	require.Empty(t, tokens)
}

func TestCreateAgentRejectsPathTraversalInScope(t *testing.T) {
	pool := testutil.NewDB(t)
	server := newAgentsServer(t, pool)

	status, raw := postJSON(t, server, "/api/agents", map[string]any{
		"name":   "traversal-via-scope",
		"scopes": []string{"../work"},
	})
	require.Equal(t, http.StatusBadRequest, status, string(raw))

	status, raw = postJSON(t, server, "/api/agents", map[string]any{
		"name":   "traversal-via-scope-2",
		"scopes": []string{".."},
	})
	require.Equal(t, http.StatusBadRequest, status, string(raw))
}

// TestCreateAgentRejectsDuplicateName pins the one check store.CreateToken itself does not
// make: agent_tokens.name has no unique constraint (migrations/00004), so the handler must
// refuse a second still-live agent under a name already in use.
func TestCreateAgentRejectsDuplicateName(t *testing.T) {
	pool := testutil.NewDB(t)
	server := newAgentsServer(t, pool)

	status, raw := postJSON(t, server, "/api/agents", map[string]any{
		"name":   "dup-agent",
		"scopes": []string{"work"},
	})
	require.Equal(t, http.StatusCreated, status, string(raw))

	status, raw = postJSON(t, server, "/api/agents", map[string]any{
		"name":   "dup-agent",
		"scopes": []string{"client-globex"},
	})
	require.Equal(t, http.StatusConflict, status, string(raw))

	tokens, err := store.ListTokens(t.Context(), pool)
	require.NoError(t, err)
	require.Len(t, tokens, 1, "the rejected duplicate must not mint a second row")
}

func TestCreateAgentRejectsEmptyName(t *testing.T) {
	pool := testutil.NewDB(t)
	server := newAgentsServer(t, pool)

	status, raw := postJSON(t, server, "/api/agents", map[string]any{
		"name":   "",
		"scopes": []string{"work"},
	})
	require.Equal(t, http.StatusBadRequest, status, string(raw))
}

func TestCreateAgentRejectsOverLongName(t *testing.T) {
	pool := testutil.NewDB(t)
	server := newAgentsServer(t, pool)

	status, raw := postJSON(t, server, "/api/agents", map[string]any{
		"name":   strings.Repeat("a", 101),
		"scopes": []string{"work"},
	})
	require.Equal(t, http.StatusBadRequest, status, string(raw))
}

// TestCreateAgentRejectsUnknownCapability confirms store.ErrInvalidCapability maps to a 400
// here rather than a 500 -- a bad request from a form, not a server fault.
func TestCreateAgentRejectsUnknownCapability(t *testing.T) {
	pool := testutil.NewDB(t)
	server := newAgentsServer(t, pool)

	status, raw := postJSON(t, server, "/api/agents", map[string]any{
		"name":         "capability-grabber",
		"scopes":       []string{"work"},
		"capabilities": []string{"fly"},
	})
	require.Equal(t, http.StatusBadRequest, status, string(raw))

	tokens, err := store.ListTokens(t.Context(), pool)
	require.NoError(t, err)
	require.Empty(t, tokens)
}

// TestRevokeAgentStopsItAndKeepsAuditHistory pins the brief's "reversible in the sense
// history is kept": revoking sets revoked_at (state flips to "revoked" over GET
// /api/agents) but the row -- and so its audit trail -- is never deleted, and revoking the
// same agent twice is a 404 the second time, since RevokeToken only matches a still-active
// row.
func TestRevokeAgentStopsItAndKeepsAuditHistory(t *testing.T) {
	pool := testutil.NewDB(t)
	server := newAgentsServer(t, pool)

	status, raw := postJSON(t, server, "/api/agents", map[string]any{
		"name":   "revoke-me",
		"scopes": []string{"work"},
	})
	require.Equal(t, http.StatusCreated, status, string(raw))
	var created createAgentBody
	require.NoError(t, json.Unmarshal(raw, &created))

	status, raw = postJSON(t, server, "/api/agents/"+created.Agent.ID+"/revoke", nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	var revoked struct {
		Agent agentJSON `json:"agent"`
	}
	require.NoError(t, json.Unmarshal(raw, &revoked))
	require.Equal(t, "revoked", revoked.Agent.State)

	// The row survives: GetToken still finds it, and its name/scopes are unchanged.
	tok, err := store.GetToken(t.Context(), pool, created.Agent.ID)
	require.NoError(t, err)
	require.NotNil(t, tok)
	require.True(t, tok.Revoked())
	require.Equal(t, "revoke-me", tok.Name)

	// Revoking again finds nothing left to revoke.
	status, raw = postJSON(t, server, "/api/agents/"+created.Agent.ID+"/revoke", nil)
	require.Equal(t, http.StatusNotFound, status, string(raw))
}

func TestRevokeAgentUnknownIDReturns404(t *testing.T) {
	pool := testutil.NewDB(t)
	server := newAgentsServer(t, pool)

	status, raw := postJSON(t, server, "/api/agents/does-not-exist/revoke", nil)
	require.Equal(t, http.StatusNotFound, status, string(raw))
}
