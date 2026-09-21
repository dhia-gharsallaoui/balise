-- lint_findings' uniqueness constraint is (rule, doc_uid, detail), but Postgres treats NULLs
-- as distinct by default, so it never matches a row whose doc_uid is NULL. A "global" finding
-- (one that names no specific document) would therefore insert a fresh duplicate row on every
-- lint run instead of upserting in place. Give the NULL case its own partial unique index so
-- ON CONFLICT has an arbiter to target.
--
-- A database that has been running lint before this migration existed may already hold
-- duplicate global findings for the same (rule, detail) — exactly the rows this index exists
-- to prevent going forward. Creating the index without first deduping them would fail outright
-- (Postgres refuses a unique index over rows that violate it), so collapse each duplicate group
-- down to one row first. Keep the one with the most recent last_seen — it reflects the freshest
-- observation and may carry an updated severity/action/resolved_at that an older duplicate
-- doesn't — tie-broken by the highest id when last_seen ties, so the choice is deterministic.
-- This delete is itself idempotent: once no group has more than one row, it matches nothing.
delete from lint_findings f
using lint_findings newer
where f.doc_uid is null
  and newer.doc_uid is null
  and f.rule = newer.rule
  and f.detail = newer.detail
  and (newer.last_seen, newer.id) > (f.last_seen, f.id);

create unique index if not exists lint_findings_global_idx
  on lint_findings (rule, detail) where doc_uid is null;
