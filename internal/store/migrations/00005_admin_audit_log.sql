-- Admin audit log for owner-initiated, write-side actions taken through the browser (Agents
-- screen create/revoke, Settings screen add-a-space) — NOT audit_log above. audit_log's own
-- columns (token_id, tool, query, returned_uids, tokens_out, coverage, latency_ms) describe
-- exactly one shape of event: an MCP tool call made by an agent token. An owner clicking
-- "Create agent" in the browser is a different shape of event entirely: there is no agent
-- token making the call (the owner made it, authenticated by session cookie, not a bearer
-- token), no query, and no result set to report coverage or a token count for. Reusing
-- audit_log for this would mean forcing nulls into columns that are supposed to mean
-- something specific, or silently repurposing them — both worse than a second, smaller table.
--
-- Like agent_tokens and audit_log, admin_audit_log is deliberately NOT in internal/guard's
-- derivedTables list: it is not scope-derived knowledge, it is the access-control/audit
-- surface in front of it. actor is always "owner" today — internal/api/auth.go's session
-- model has exactly one identity (see auth.go's own doc comment) — but the column exists so a
-- future multi-identity owner session would not require a schema change to say who.
create table if not exists admin_audit_log (
  id      bigserial primary key,
  ts      timestamptz default now(),
  actor   text not null default 'owner',
  action  text not null,   -- e.g. "agent.create", "agent.revoke", "scope.create"
  target  text not null,   -- the agent id/name or scope name the action was about
  detail  jsonb            -- action-specific context (e.g. granted scopes/capabilities)
);
create index if not exists admin_audit_log_ts_idx on admin_audit_log (ts desc);
