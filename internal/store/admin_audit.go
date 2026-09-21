package store

// admin_audit_log — the owner-action audit trail. See migrations/00005_admin_audit_log.sql's
// own comment for why this is a separate table from audit_log (tokens.go, above): audit_log
// records one MCP tool call by one agent token; this records one write action the owner took
// through the browser (create/revoke an agent, declare a scope). Like agent_tokens and
// audit_log, this file takes *pgxpool.Pool directly rather than *Queries, for the same reason
// tokens.go's own package doc gives: this is the access-control/audit surface in front of
// scope-derived knowledge, not part of it, so it must stay outside internal/guard's reach.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AdminAuditEntry is one row of admin_audit_log.
type AdminAuditEntry struct {
	ID     int64
	TS     time.Time
	Actor  string
	Action string
	Target string
	Detail map[string]any
}

// defaultAdminAuditLimit is the page size ListAdminAudit falls back to when limit <= 0.
const defaultAdminAuditLimit = 200

// RecordAdminAudit inserts one admin_audit_log row for a completed owner write action. actor
// is always "owner" today (internal/api/auth.go's session model has exactly one identity —
// see its own doc comment), passed here as a literal rather than threaded through every
// caller so a future multi-identity session model has exactly one place to change. detail may
// be nil; a nil map marshals to a JSON null, not an error.
func RecordAdminAudit(ctx context.Context, pool *pgxpool.Pool, action, target string, detail map[string]any) error {
	var raw any
	if detail != nil {
		b, err := json.Marshal(detail)
		if err != nil {
			return fmt.Errorf("record admin audit: marshal detail: %w", err)
		}
		raw = b
	}
	if _, err := pool.Exec(ctx,
		`insert into admin_audit_log (actor, action, target, detail) values ('owner', $1, $2, $3)`,
		action, target, raw,
	); err != nil {
		return fmt.Errorf("record admin audit: %w", err)
	}
	return nil
}

// ListAdminAudit returns admin_audit_log rows newest-first, capped at limit (defaulting to
// defaultAdminAuditLimit when limit <= 0). Nothing in this build's UI reads this yet — it
// exists so the audit trail this task's brief requires ("who, what, when") is queryable, not
// just written — but it is exercised directly by tests.
func ListAdminAudit(ctx context.Context, pool *pgxpool.Pool, limit int) ([]AdminAuditEntry, error) {
	if limit <= 0 {
		limit = defaultAdminAuditLimit
	}
	rows, err := pool.Query(ctx,
		`select id, ts, actor, action, target, detail from admin_audit_log order by id desc limit $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list admin audit: %w", err)
	}
	defer rows.Close()

	var entries []AdminAuditEntry
	for rows.Next() {
		var e AdminAuditEntry
		var raw []byte
		if err := rows.Scan(&e.ID, &e.TS, &e.Actor, &e.Action, &e.Target, &raw); err != nil {
			return nil, fmt.Errorf("list admin audit: scan: %w", err)
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &e.Detail); err != nil {
				return nil, fmt.Errorf("list admin audit: unmarshal detail: %w", err)
			}
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list admin audit: %w", err)
	}
	return entries, nil
}
