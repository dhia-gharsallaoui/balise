create extension if not exists ltree;
create extension if not exists pg_trgm;
create extension if not exists unaccent;

-- unaccent() (both the 1-arg and the (regdictionary, text) forms) is marked
-- STABLE, not IMMUTABLE, because Postgres cannot prove the "unaccent"
-- dictionary never changes. A generated column requires an IMMUTABLE
-- expression, so we pin the dictionary explicitly and wrap it in a function
-- we declare IMMUTABLE ourselves. This is the standard, widely documented
-- workaround for using unaccent() in generated columns and indexes.
--
-- Caution: "immutable" is asserted, not true — it only pins the "unaccent"
-- dictionary by name. If that dictionary is ever altered, stored tsv values
-- go silently stale (generated columns aren't recomputed); recovery means
-- dropping and recomputing claims.tsv and chunks.tsv (a full reindex).
create or replace function immutable_unaccent(text)
returns text
language sql
immutable
parallel safe
as $$
  select unaccent('unaccent', $1)
$$;

create table if not exists documents (
  uid          text primary key,
  slug         text not null,
  scope        text not null,
  type         text not null,
  path         text not null,
  title        text not null,
  aliases      text[] not null default '{}',
  tags         ltree[] not null default '{}',
  status       text,
  as_of        date, valid_from date, valid_to date,
  owner        text, last_verified date,
  fields       jsonb not null default '{}',
  body_md      text not null,
  body_hash    text not null,
  tokens       int not null,
  claims_count int not null default 0,
  historical   bool not null default false,
  git_version  text not null,
  created_at   timestamptz, updated_at timestamptz,
  indexed_at   timestamptz not null default now(),
  unique (scope, slug)
);
create index if not exists documents_tags_idx on documents using gin (tags);
create index if not exists documents_scope_type_idx on documents (scope, type, historical);
create index if not exists documents_aliases_idx on documents using gin (aliases);
create index if not exists documents_title_trgm_idx on documents using gin (title gin_trgm_ops);

create table if not exists claims (
  id         bigserial primary key,
  doc_uid    text not null references documents(uid) on delete cascade,
  claim_id   text not null,
  ord        int not null,
  text       text not null,
  status     text not null default 'active',
  as_of      date,
  span_start int, span_end int,
  tsv        tsvector generated always as (to_tsvector('simple', immutable_unaccent(text))) stored,
  unique (doc_uid, claim_id)
);
create index if not exists claims_tsv_idx on claims using gin (tsv);

create table if not exists chunks (
  id bigserial primary key,
  doc_uid text not null references documents(uid) on delete cascade,
  ord int not null, heading_path text, text text not null,
  text_hash text not null, tokens int not null,
  tsv tsvector generated always as (to_tsvector('simple', immutable_unaccent(text))) stored,
  unique (doc_uid, ord)
);
create index if not exists chunks_tsv_idx on chunks using gin (tsv);

create table if not exists edges (
  from_uid text not null,
  to_uid   text,
  to_ref   text not null,
  kind     text not null,
  field    text not null default '',
  weight   real not null default 1.0,
  primary key (from_uid, to_ref, kind, field)
);
create index if not exists edges_to_idx on edges (to_uid, kind);

create table if not exists lint_findings (
  id bigserial primary key,
  rule text not null, severity text not null,
  doc_uid text, detail text not null default '', action text,
  first_seen timestamptz default now(),
  last_seen timestamptz default now(),
  resolved_at timestamptz,
  unique (rule, doc_uid, detail)
);
