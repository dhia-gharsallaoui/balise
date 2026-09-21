package api

// This file is the Agents screen's write side: POST /api/agents (mint) and
// POST /api/agents/{id}/revoke. Both became possible to expose over HTTP only once commit
// b0e12a1 gave /api an owner session to require — see agents.go's own doc comment and
// server.go's `auth` field. Both routes are protected by default (server.go's apiRoutes
// table), so requireSession already turns a missing/invalid session cookie into a 401 before
// either handler here ever runs.
//
// The raw token minted by handleCreateAgent is returned exactly once, in this response, and
// never again: store.CreateToken's own doc comment is explicit that only sha256(raw) is ever
// persisted, so there is no later endpoint that could return it even if this one wanted to.
// agents_test.go's no-hash-leak test covers this response alongside GET /api/agents.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/dhia/balise/internal/store"
)

// createAgentRequest is POST /api/agents' body. Capabilities and ExpiresAt are both
// optional in the JSON sense (an absent "capabilities" array or "expires_at" string are both
// zero-value-and-valid) — store.CreateToken itself still enforces that every supplied
// capability is one of read/remember/propose.
type createAgentRequest struct {
	Name         string   `json:"name"`
	Scopes       []string `json:"scopes"`
	Capabilities []string `json:"capabilities"`
	// ExpiresAt, when non-nil and non-empty, must be RFC 3339 ("2026-12-31T00:00:00Z"). A nil
	// or empty value means no expiry, matching store.CreateToken's own *time.Time-is-optional
	// convention.
	ExpiresAt *string `json:"expires_at"`
}

type createAgentResponse struct {
	Agent agentSummary `json:"agent"`
	// Token is the raw, one-time secret — see this file's own doc comment. It appears in
	// exactly this one response and nowhere else this package ever serves.
	Token string `json:"token"`
}

// handleCreateAgent serves POST /api/agents. Every requested scope must already be one this
// server enforces (s.q().AllowedScopes(), the live declared ∪ discovered set) — granting a
// scope the vault has no concept of would look like it worked and then silently return
// nothing for it ever after, which is worse than refusing the request up front.
func (s *server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	var req createAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := validateSegmentName("agent name", req.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Scopes) == 0 {
		writeError(w, http.StatusBadRequest, "at least one space is required")
		return
	}

	allowed := s.q().AllowedScopes()
	for _, scope := range req.Scopes {
		if err := validateSegmentName("space name", scope); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !slices.Contains(allowed, scope) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown space %q", scope))
			return
		}
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "expires_at must be an RFC 3339 timestamp")
			return
		}
		expiresAt = &t
	}

	pool, err := s.pool()
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	ctx := r.Context()

	// store.CreateToken has no unique constraint of its own to lean on (agent_tokens.name
	// carries none — see migrations/00004_agent_tokens.sql), so a duplicate, still-live name
	// is checked here. A revoked token's name is free to reuse: its row stays for audit
	// history (RevokeToken never deletes), but a revoked agent is gone in every sense that
	// matters for "does this name already denote something live".
	existing, err := store.ListTokens(ctx, pool)
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	for _, t := range existing {
		if t.Name == req.Name && !t.Revoked() {
			writeError(w, http.StatusConflict, fmt.Sprintf("agent %q already exists", req.Name))
			return
		}
	}

	id, raw, err := store.CreateToken(ctx, pool, req.Name, req.Scopes, req.Capabilities, expiresAt)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCapability) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeServerError(w, r, err)
		return
	}

	created, err := store.GetToken(ctx, pool, id)
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	if created == nil {
		// CreateToken just returned this id with no error; a nil lookup immediately after
		// would mean the insert never actually committed — a server bug, not a bad request.
		writeServerError(w, r, fmt.Errorf("created agent %s not found immediately after creation", id))
		return
	}

	if auditErr := store.RecordAdminAudit(ctx, pool, "agent.create", id, map[string]any{
		"name":         req.Name,
		"scopes":       req.Scopes,
		"capabilities": req.Capabilities,
	}); auditErr != nil {
		// The agent now exists and is live; failing to log it is a courtesy miss, not a
		// reason to fail a request that already succeeded — matching TouchToken's own
		// "audit trail failure must not become a failed request" precedent (tokens.go).
		slog.Error("record admin audit failed", "action", "agent.create", "target", id, "error", auditErr)
	}

	writeJSON(w, http.StatusCreated, createAgentResponse{
		Agent: agentSummary{
			ID:           created.ID,
			Name:         created.Name,
			Scopes:       nonNilStrings(created.Scopes),
			Capabilities: nonNilStrings(created.Capabilities),
			State:        agentState(*created, time.Now()),
			CreatedAt:    created.CreatedAt,
			LastUsedAt:   created.LastUsedAt,
		},
		Token: raw,
	})
}

// handleRevokeAgent serves POST /api/agents/{id}/revoke. Revocation sets revoked_at rather
// than deleting the row (store.RevokeToken) — this handler does not change that: a revoked
// agent's audit history is worth more than a tidy table.
func (s *server) handleRevokeAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pool, err := s.pool()
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	ctx := r.Context()

	if err := store.RevokeToken(ctx, pool, id); err != nil {
		if errors.Is(err, store.ErrTokenNotFound) {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
		writeServerError(w, r, err)
		return
	}

	if auditErr := store.RecordAdminAudit(ctx, pool, "agent.revoke", id, nil); auditErr != nil {
		slog.Error("record admin audit failed", "action", "agent.revoke", "target", id, "error", auditErr)
	}

	revoked, err := store.GetToken(ctx, pool, id)
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	if revoked == nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"agent": agentSummary{
			ID:           revoked.ID,
			Name:         revoked.Name,
			Scopes:       nonNilStrings(revoked.Scopes),
			Capabilities: nonNilStrings(revoked.Capabilities),
			State:        agentState(*revoked, time.Now()),
			CreatedAt:    revoked.CreatedAt,
			LastUsedAt:   revoked.LastUsedAt,
		},
	})
}
