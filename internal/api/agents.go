package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/dhia/balise/internal/store"
)

// This file is the Agents screen's read side (02-ui-design-v1.md section 5.5 / 04 section
// 13's `GET /agents`, `GET /agents/{id}/activity`). Minting and revoking a token now have a
// real, session-authenticated HTTP path too — see agents_write.go — now that commit b0e12a1
// gave /api an owner session to gate that button on; this file stays read-only on purpose,
// so the two concerns (list/inspect vs. create/revoke) stay in separate, smaller files. See
// server.go's tokenPool doc comment for why these handlers need a *pgxpool.Pool that Queries
// cannot give them.
//
// Every JSON field name and value here is chosen to survive 02's copy rule: never say "uid",
// "hook", "pack", "index", "token", "budget" or "trait" on screen. That rule is about the
// rendered page, not the wire format, but agentSummary and activityRow are named as if it
// applied to them too — an "agent" has "scopes" and was "last used", not a "token" with a
// "budget" — so the frontend never has to translate a store-shaped name into a screen-shaped
// one, and can render a field's name directly as a label if it ever needs to.

// agentSummary is one row of GET /api/agents. It deliberately excludes store.AgentToken's
// TokenHash, DefaultSpace and DefaultBudget: TokenHash is the one field this entire package
// must never let onto the wire (a hash is not the raw secret, but it is still the credential's
// fingerprint — 04 section 14 is unambiguous that only the agent that was shown the raw value
// once should ever be able to prove it holds this token), and DefaultSpace/DefaultBudget are
// mint-time knobs this display-only screen has no use for.
type agentSummary struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Scopes       []string   `json:"scopes"`
	Capabilities []string   `json:"capabilities"`
	State        string     `json:"state"`
	CreatedAt    time.Time  `json:"created_at"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
}

// agentState renders an AgentToken's lifecycle as exactly one of three words the Agents
// screen needs to make unmissable: "active", "revoked" or "expired". Revoked takes precedence
// over expired when (in principle) both could be true, since a deliberately revoked agent is
// the stronger, human-initiated fact.
func agentState(t store.AgentToken, now time.Time) string {
	if t.Revoked() {
		return "revoked"
	}
	if t.Expired(now) {
		return "expired"
	}
	return "active"
}

// nonNilStrings turns a nil slice into an empty one so agents/activity JSON always renders
// "scopes": [] rather than "scopes": null — a reader (or a frontend .map call) should never
// have to special-case the absence of scopes as a different shape from zero scopes.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// handleAgents serves GET /api/agents: every agent token, newest first, including revoked
// ones — mirroring store.ListTokens's own "show a token's true state rather than silently
// hide it" rule, now for a human reading the screen instead of `balise token list`.
func (s *server) handleAgents(w http.ResponseWriter, r *http.Request) {
	pool, err := s.pool()
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	tokens, err := store.ListTokens(r.Context(), pool)
	if err != nil {
		writeServerError(w, r, err)
		return
	}

	now := time.Now()
	agents := make([]agentSummary, 0, len(tokens))
	for _, t := range tokens {
		agents = append(agents, agentSummary{
			ID:           t.ID,
			Name:         t.Name,
			Scopes:       nonNilStrings(t.Scopes),
			Capabilities: nonNilStrings(t.Capabilities),
			State:        agentState(t, now),
			CreatedAt:    t.CreatedAt,
			LastUsedAt:   t.LastUsedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
}

// activityRow is one row of GET /api/agents/{id}/activity's "activity" list: one audit_log
// entry, already shaped for the Activity tab (v3's agentTabActivity: when, query, how much
// came back) rather than as the raw ActivityEntry the store layer returns.
type activityRow struct {
	ID          int64     `json:"id"`
	When        time.Time `json:"when"`
	Tool        string    `json:"tool"`
	Scopes      []string  `json:"scopes"`
	Query       string    `json:"query"`
	ResultCount int       `json:"result_count"`
	LatencyMS   int       `json:"latency_ms"`
}

// defaultAgentActivityPage and maxAgentActivityPage bound the "limit" query parameter
// GET /api/agents/{id}/activity accepts. They are named distinctly from
// store.defaultActivityLimit (a different package's constant of a similar name, for a
// different purpose: this one is an HTTP input clamp, that one is ListActivity's own
// zero-value fallback) purely for readability at each call site.
const (
	defaultAgentActivityPage = 50
	maxAgentActivityPage     = 200
)

// parseAgentActivityLimit turns the "limit" query parameter into a page size, clamped to
// (0, maxAgentActivityPage]. Anything absent, unparsable or non-positive falls back to
// defaultAgentActivityPage rather than erroring — a malformed limit should degrade to a
// sensible default, not break the whole screen.
func parseAgentActivityLimit(raw string) int {
	if raw == "" {
		return defaultAgentActivityPage
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultAgentActivityPage
	}
	if n > maxAgentActivityPage {
		return maxAgentActivityPage
	}
	return n
}

// parseAgentActivityBefore turns the "before" query parameter (an audit_log id cursor) into
// ListActivity's beforeID. Anything absent, unparsable or negative becomes 0 — "no cursor,
// start from the newest row" — the same fallback ListActivity itself applies to a
// non-positive limit.
func parseAgentActivityBefore(raw string) int64 {
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// handleAgentActivity serves GET /api/agents/{id}/activity: one agent's recent audit_log
// rows, newest first, for the Agents screen's right-hand Activity tab. A 404 covers both "no
// such agent" and (deliberately, matching loadPendingProposal's convention elsewhere in this
// package) any request for an id nothing has ever recorded activity under — the caller cannot
// distinguish those from the outside, and does not need to.
func (s *server) handleAgentActivity(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pool, err := s.pool()
	if err != nil {
		writeServerError(w, r, err)
		return
	}

	token, err := store.GetToken(r.Context(), pool, id)
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	if token == nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	limit := parseAgentActivityLimit(r.URL.Query().Get("limit"))
	before := parseAgentActivityBefore(r.URL.Query().Get("before"))
	entries, err := store.ListActivity(r.Context(), pool, id, limit, before)
	if err != nil {
		writeServerError(w, r, err)
		return
	}

	rows := make([]activityRow, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, activityRow{
			ID:          e.ID,
			When:        e.TS,
			Tool:        e.Tool,
			Scopes:      nonNilStrings(e.Scopes),
			Query:       e.Query,
			ResultCount: e.ResultCount,
			LatencyMS:   e.LatencyMS,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_name": token.Name,
		"activity":   rows,
	})
}
