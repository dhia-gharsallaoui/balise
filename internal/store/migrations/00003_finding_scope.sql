-- A global lint finding (doc_uid is null) has no document row to inherit a scope from, but it
-- can still be about one: an unparseable page is reported with its path in `detail`, and that
-- path's first segment is the scope (04 section 14: scope = directory). Without a scope of its
-- own such a finding is readable by every caller, which would leak one tenant's scope name and
-- filename to a caller holding only another tenant's scope — through the one findings path
-- that has no document to check ownership against.
--
-- NULL means "not attributable to any one scope" and stays visible to every caller, which is
-- the right reading for the global findings that already exist.
alter table lint_findings add column if not exists scope text;
