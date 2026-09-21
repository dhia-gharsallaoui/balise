package api

// This file is the write side of Settings' Scopes tab (02-ui-design-v1.md section 5.6's "add
// a space" — the screen says space, this file's identifiers and JSON keep saying scope,
// matching every other backend/API file in this package; see 02 section 8's copy rule and
// this repo's dispatch-rules.md for that split). It is deliberately the only place besides
// registry.AppendDeclaredScope's own call site that ever adds to defaults/scopes.yaml.
//
// Scope creation is add-only: there is no rename or delete handler here, and none should be
// added. Renaming would orphan every page already filed under the old name and silently
// change what an existing agent token's scope list refers to (agent_tokens.scopes stores the
// name itself, not a stable id); deleting is destructive in the same way. See
// registry.declared_scopes.go's own doc comment, which this file's behavior must not
// contradict.
//
// A newly declared scope grants no existing agent any access to it: agent tokens carry their
// own explicit scopes list (store.AgentToken.Scopes, checked by AllowsScope), which this
// handler never touches. See scopes_test.go's
// TestCreatingAScopeGrantsNoExistingAgentAccess for a test that proves this rather than
// assumes it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"

	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/vault"
)

// createScopeRequest is POST /api/scopes' body: just the new scope's name. There is nothing
// else to configure at creation — a scope has no other settable properties anywhere in this
// codebase (defaults/spaces.yaml's display tree and defaults/tenants.yaml's client marker are
// both independent, owner-edited-on-disk concerns, not part of what this endpoint creates).
type createScopeRequest struct {
	Name string `json:"name"`
}

// createScopeResponse echoes the created scope's name alongside the full, current scope set
// this server enforces after the create — so the caller (Settings' Scopes tab) can repaint
// its whole list from one response instead of issuing a second GET /api/settings.
type createScopeResponse struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

// handleCreateScope serves POST /api/scopes. The whole read-validate-append-rebuild sequence
// runs under s.scopesMu (see the field's own doc comment on server): two concurrent creates
// must not both read defaults/scopes.yaml's "before" content and each write back a two-item
// list missing the other's addition.
func (s *server) handleCreateScope(w http.ResponseWriter, r *http.Request) {
	var req createScopeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := validateSegmentName("space name", req.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.scopesMu.Lock()
	defer s.scopesMu.Unlock()

	// A name already in the live, effective scope set — declared or discovered, page-empty
	// or not — is a duplicate, not a silent success: the caller asked to add a new one.
	if slices.Contains(s.q().AllowedScopes(), req.Name) {
		writeError(w, http.StatusConflict, fmt.Sprintf("space %q already exists", req.Name))
		return
	}

	scopesPath := filepath.Join(s.defaultsDir, "scopes.yaml")
	declared, err := registry.AppendDeclaredScope(scopesPath, req.Name)
	if err != nil {
		switch {
		case errors.Is(err, registry.ErrScopeAlreadyDeclared):
			writeError(w, http.StatusConflict, fmt.Sprintf("space %q already exists", req.Name))
		case errors.Is(err, vault.ErrInvalidSegment):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeServerError(w, r, err)
		}
		return
	}

	ctx := r.Context()
	if err := s.rebuildQueries(ctx, declared.Names()); err != nil {
		writeServerError(w, r, err)
		return
	}

	if pool, err := s.pool(); err == nil {
		if auditErr := store.RecordAdminAudit(ctx, pool, "scope.create", req.Name, nil); auditErr != nil {
			// The scope now exists and is live; failing to log it is a courtesy miss, not a
			// reason to tell the caller their create failed. Log server-side and move on —
			// matching TouchToken's own "audit trail failure must not become a failed
			// request" precedent (tokens.go).
			slog.Error("record admin audit failed", "action", "scope.create", "target", req.Name, "error", auditErr)
		}
	}

	writeJSON(w, http.StatusCreated, createScopeResponse{
		Name:   req.Name,
		Scopes: []string(s.q().AllowedScopes()),
	})
}

// rebuildQueries recomputes the effective scope set (database ∪ vault ∪ declared, via
// store.EffectiveScopes) from declared and swaps a brand new *store.Queries bound to it into
// s.queriesPtr. See the queriesPtr field's own doc comment on server: store.Queries binds its
// Scopes at construction and internal/guard's AST guards rest on that binding staying
// immutable, so widening what this server enforces means building a whole new Queries and
// swapping the pointer, never mutating the one already in flight. Must be called with
// s.scopesMu held — it is not itself safe to call concurrently with another rebuild, since two
// concurrent callers computing EffectiveScopes from two different "current" pools would each
// overwrite the other's swap with a snapshot that no longer includes it.
func (s *server) rebuildQueries(ctx context.Context, declared []string) error {
	pool, err := s.pool()
	if err != nil {
		return fmt.Errorf("rebuild queries: %w", err)
	}
	scopes, err := store.EffectiveScopes(ctx, pool, s.pages, declared)
	if err != nil {
		return fmt.Errorf("rebuild queries: %w", err)
	}
	s.queriesPtr.Store(store.NewQueries(pool, scopes))
	return nil
}
