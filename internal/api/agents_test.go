package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dhia/balise/internal/api"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// newAgentsServer is newServer plus api.WithTokenPool(pool): the Agents screen's two
// endpoints (agents.go) read agent_tokens/audit_log through a *pgxpool.Pool that Queries
// cannot hand out (see server.go's tokenPool doc comment), so tests exercising those two
// endpoints must inject the exact pool testutil.NewDB gave them — the one whose connections
// see this test's own schema — rather than let api.New's BALISE_DSN fallback reach for
// whatever Postgres happens to be at localhost:5432 outside the test's isolation.
func newAgentsServer(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()
	q := store.NewQueries(pool, store.Scopes{"work", "client-globex"})
	spaces, err := registry.LoadSpaces("../../defaults/spaces.yaml")
	require.NoError(t, err)
	order, err := registry.LoadOrder("../../defaults/order.yaml")
	require.NoError(t, err)
	server := httptest.NewServer(api.New(q, newGitVault(t), spaces, order, "", api.WithTokenPool(pool)))
	t.Cleanup(server.Close)
	return server
}

func getRaw(t *testing.T, server *httptest.Server, path string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(server.URL + path)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body
}

type agentJSON struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Scopes       []string   `json:"scopes"`
	Capabilities []string   `json:"capabilities"`
	State        string     `json:"state"`
	CreatedAt    time.Time  `json:"created_at"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
}

type agentsListJSON struct {
	Agents []agentJSON `json:"agents"`
}

// TestAgentsListNeverLeaksTokenHashOrSecret pins the hard rule from this task's brief: the
// agent list must never carry the token hash, the hex hash string itself, or the raw secret
// a token was minted with — any of those would let something with API access reconstruct or
// replay an agent's credential. This checks both the JSON shape (no "token_hash" key at all,
// by decoding into a struct that does not have one and then diffing against a raw map) and
// the raw response bytes (the hash and secret strings do not appear anywhere in the body,
// not just outside the known fields).
func TestAgentsListNeverLeaksTokenHashOrSecret(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()
	id, raw, err := store.CreateToken(ctx, pool, "good-agent", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, id)

	tok, err := store.GetToken(ctx, pool, id)
	require.NoError(t, err)
	require.NotNil(t, tok)
	require.NotEmpty(t, tok.TokenHash)

	server := newAgentsServer(t, pool)
	status, body := getRaw(t, server, "/api/agents")
	require.Equal(t, http.StatusOK, status)

	text := string(body)
	require.NotContains(t, text, "token_hash")
	require.NotContains(t, text, tok.TokenHash)
	require.NotContains(t, text, raw)

	// Belt and suspenders: decode into a generic map per row too, so a future rename of the
	// hash field (e.g. to "hash" or "secret") would still be caught by key name, not just by
	// this test's specific expectation of "token_hash".
	var generic struct {
		Agents []map[string]any `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(body, &generic))
	require.Len(t, generic.Agents, 1)
	for _, row := range generic.Agents {
		for key := range row {
			require.NotContains(t, strings.ToLower(key), "hash")
			require.NotContains(t, strings.ToLower(key), "secret")
		}
	}
}

// TestAgentsListReportsActiveRevokedAndExpiredStates covers all three lifecycle states
// agentState (agents.go) computes, so the Agents screen can tell them apart at a glance —
// the brief's "an agent whose token is revoked or expired must be visibly distinct" starts
// here at the API, which must hand the frontend an unambiguous word for each row.
func TestAgentsListReportsActiveRevokedAndExpiredStates(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	activeID, _, err := store.CreateToken(ctx, pool, "active-agent", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)

	revokedID, _, err := store.CreateToken(ctx, pool, "revoked-agent", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)
	require.NoError(t, store.RevokeToken(ctx, pool, revokedID))

	past := time.Now().Add(-time.Hour)
	expiredID, _, err := store.CreateToken(ctx, pool, "expired-agent", []string{"personal"}, []string{"read"}, &past)
	require.NoError(t, err)

	server := newAgentsServer(t, pool)
	var body agentsListJSON
	status, raw := getRaw(t, server, "/api/agents")
	require.Equal(t, http.StatusOK, status)
	require.NoError(t, json.Unmarshal(raw, &body))
	require.Len(t, body.Agents, 3)

	states := map[string]string{}
	scopesByID := map[string][]string{}
	for _, a := range body.Agents {
		states[a.ID] = a.State
		scopesByID[a.ID] = a.Scopes
	}
	require.Equal(t, "active", states[activeID])
	require.Equal(t, "revoked", states[revokedID])
	require.Equal(t, "expired", states[expiredID])

	// Scopes must show plainly and exactly — this is the product's security boundary, so
	// good-agent-equivalent rows must never gain or lose a scope in transit to JSON.
	require.Equal(t, []string{"work"}, scopesByID[activeID])
	require.Equal(t, []string{"personal"}, scopesByID[expiredID])
}

// TestAgentsListOrdersNewestFirst matches store.ListTokens's own "newest first" ordering —
// the Agents screen's list should not have to re-sort what the API already promises to hand
// back in a stable, predictable order.
func TestAgentsListOrdersNewestFirst(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	firstID, _, err := store.CreateToken(ctx, pool, "first-agent", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)
	time.Sleep(5 * time.Millisecond)
	secondID, _, err := store.CreateToken(ctx, pool, "second-agent", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)

	server := newAgentsServer(t, pool)
	var body agentsListJSON
	status, raw := getRaw(t, server, "/api/agents")
	require.Equal(t, http.StatusOK, status)
	require.NoError(t, json.Unmarshal(raw, &body))
	require.Len(t, body.Agents, 2)
	require.Equal(t, secondID, body.Agents[0].ID)
	require.Equal(t, firstID, body.Agents[1].ID)
}

type activityJSON struct {
	ID          int64     `json:"id"`
	When        time.Time `json:"when"`
	Tool        string    `json:"tool"`
	Scopes      []string  `json:"scopes"`
	Query       string    `json:"query"`
	ResultCount int       `json:"result_count"`
	LatencyMS   int       `json:"latency_ms"`
}

type activityListJSON struct {
	AgentName string         `json:"agent_name"`
	Activity  []activityJSON `json:"activity"`
}

// TestAgentActivityReturnsNewestFirstWithAgentName covers the join to the agent's name and
// the newest-first ordering GET /api/agents/{id}/activity promises, plus that a query's
// stored (already-truncated) text and its result count/latency all survive to JSON.
func TestAgentActivityReturnsNewestFirstWithAgentName(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	id, _, err := store.CreateToken(ctx, pool, "good-agent", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)

	require.NoError(t, store.RecordAudit(ctx, pool, store.AuditEntry{
		TokenID: id, Tool: "search", Scopes: []string{"work"}, Query: "gotcha alloy",
		ReturnedUIDs: []string{"u1", "u2"}, TokensOut: 120, Coverage: "full", LatencyMS: 45,
	}))
	require.NoError(t, store.RecordAudit(ctx, pool, store.AuditEntry{
		TokenID: id, Tool: "remember", Scopes: []string{"work"}, Query: "note about alloy",
		ReturnedUIDs: nil, TokensOut: 10, Coverage: "full", LatencyMS: 12,
	}))

	server := newAgentsServer(t, pool)
	var body activityListJSON
	status, raw := getRaw(t, server, "/api/agents/"+id+"/activity")
	require.Equal(t, http.StatusOK, status)
	require.NoError(t, json.Unmarshal(raw, &body))

	require.Equal(t, "good-agent", body.AgentName)
	require.Len(t, body.Activity, 2)
	require.Equal(t, "remember", body.Activity[0].Tool) // newest first
	require.Equal(t, "search", body.Activity[1].Tool)
	require.Equal(t, 2, body.Activity[1].ResultCount) // returned_uids array_length
	require.Equal(t, 0, body.Activity[0].ResultCount) // nil returned_uids -> 0
	require.Equal(t, 45, body.Activity[1].LatencyMS)
	require.Equal(t, []string{"work"}, body.Activity[1].Scopes)
}

// TestAgentActivityUnknownIDReturns404 matches loadPendingProposal's own convention
// elsewhere in this package: an id nothing recognizes is a 404, not a 200 with an empty list
// masquerading as "this agent exists but is quiet".
func TestAgentActivityUnknownIDReturns404(t *testing.T) {
	pool := testutil.NewDB(t)
	server := newAgentsServer(t, pool)
	status, _ := getRaw(t, server, "/api/agents/does-not-exist/activity")
	require.Equal(t, http.StatusNotFound, status)
}

// TestAgentActivityRespectsLimitAndBeforeCursor exercises the paging contract the brief
// asks for ("newest first... with a sensible limit and the ability to page or cap").
func TestAgentActivityRespectsLimitAndBeforeCursor(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()
	id, _, err := store.CreateToken(ctx, pool, "good-agent", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		require.NoError(t, store.RecordAudit(ctx, pool, store.AuditEntry{
			TokenID: id, Tool: "search", Scopes: []string{"work"}, Query: "q" + strconv.Itoa(i),
			ReturnedUIDs: []string{"u"}, TokensOut: 1, Coverage: "full", LatencyMS: 1,
		}))
	}

	server := newAgentsServer(t, pool)

	var page1 activityListJSON
	status, raw := getRaw(t, server, "/api/agents/"+id+"/activity?limit=2")
	require.Equal(t, http.StatusOK, status)
	require.NoError(t, json.Unmarshal(raw, &page1))
	require.Len(t, page1.Activity, 2)
	require.Equal(t, "q4", page1.Activity[0].Query)
	require.Equal(t, "q3", page1.Activity[1].Query)

	var page2 activityListJSON
	status, raw = getRaw(t, server, "/api/agents/"+id+"/activity?limit=2&before="+strconv.FormatInt(page1.Activity[1].ID, 10))
	require.Equal(t, http.StatusOK, status)
	require.NoError(t, json.Unmarshal(raw, &page2))
	require.Len(t, page2.Activity, 2)
	require.Equal(t, "q2", page2.Activity[0].Query)
	require.Equal(t, "q1", page2.Activity[1].Query)
}
