-- Semantic search needs a vector per claim, but claims themselves are not a stable place
-- to hang one: ReplaceClaims (internal/store/queries.go) deletes and reinserts a page's
-- entire claim set on every reindex of a changed page, so any column added to claims
-- would be destroyed and recomputed for claims whose text never changed at all. Claims
-- also carry no identity that survives a delete+reinsert cycle -- there is no claim id to
-- key a companion table by.
--
-- claim_embeddings is instead keyed purely by (hash, model), where hash is the SHA-256 of
-- the claim's normalised text. It has no foreign key to claims and no scope column: it is
-- a content-addressed cache, identical in shape to the content-hash file cache pattern --
-- the same claim text recurs verbatim across pages and even across scopes (boilerplate,
-- shared runbook steps), and every one of those occurrences should reuse the same vector
-- rather than re-embed it. Scope enforcement for semantic search happens independently, in
-- Go, at query time: the set of (hash, claim) pairs ever looked up here is first filtered
-- to the caller's own scopes (ScopedClaimTexts), and the documents ultimately returned are
-- re-filtered by scope again (documentMetaByUID) -- this table only ever answers "what is
-- the vector for this exact text under this exact model", never "what scope is this in".
--
-- model is part of the key (not a separate dimension check) because switching embedding
-- models must not silently mix vectors from two different embedding spaces: a reindex
-- under a new model name simply misses cache for every claim and re-embeds all of them,
-- the same "unrecognised fingerprint forces exactly one re-evaluation pass" property as
-- type_fingerprint in 00006.
--
-- vector is bytea (little-endian float32, packed by internal/embed.EncodeVector) rather
-- than a pgvector column: pgvector is not installed in this environment, and at roughly
-- 1,100 vectors total, brute-force cosine similarity computed in Go is sub-millisecond and
-- exact, so there is no HNSW index to build or maintain here.
create table if not exists claim_embeddings (
    hash text not null,
    model text not null,
    dim integer not null,
    vector bytea not null,
    created_at timestamptz not null default now(),
    primary key (hash, model)
);
