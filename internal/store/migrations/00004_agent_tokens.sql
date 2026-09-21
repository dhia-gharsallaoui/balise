-- Agent tokens and audit log — 04 section 6, 04 section 14, 04 section 12.
--
-- agent_tokens and audit_log are deliberately NOT in internal/guard's derivedTables list:
-- they are not scope-derived knowledge (documents/claims/chunks/edges/lint_findings), they
-- are the access-control and audit surface that sits in front of that knowledge. Code that
-- reads or writes them lives in internal/store/tokens.go, not behind store.Queries.
create table if not exists agent_tokens (
  id             text primary key,
  name           text not null,
  token_hash     text not null unique,
  scopes         text[] not null,
  capabilities   text[] not null,      -- read|remember|propose
  default_space  text,
  default_budget int default 6000,
  expires_at     timestamptz,
  created_at     timestamptz default now(),
  last_used_at   timestamptz,
  revoked_at     timestamptz
);

create table if not exists audit_log (
  id           bigserial primary key,
  ts           timestamptz default now(),
  token_id     text,
  tool         text,
  scopes       text[],
  query        text,
  params       jsonb,
  returned_uids text[],
  tokens_out   int,
  coverage     text,
  latency_ms   int
);
-- Every MCP call writes exactly one row here (04 section 14); this index is what makes "show
-- me this token's history" a cheap query instead of a sequential scan once the log grows.
create index if not exists audit_log_token_id_idx on audit_log (token_id);
