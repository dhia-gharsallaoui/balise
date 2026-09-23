// Scope enforcement lives here and nowhere else.
//
// Scopes are bound into Queries at construction, so there is no way to spell a query
// that lacks a scope filter — the compiler prevents it. Every statement below applies
// `scope = any($n)` inside the SQL rather than filtering afterwards, per 04 section 10.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dhia/balise/internal/embed"
	"github.com/dhia/balise/internal/vault"
)

// ErrScopeDenied means a write named a scope the caller may not touch.
var ErrScopeDenied = errors.New("scope denied")

// Scopes is the set of scopes a caller may read and write.
type Scopes []string

func (s Scopes) allows(scope string) bool {
	for _, allowed := range s {
		if allowed == scope {
			return true
		}
	}
	return false
}

// Document is one indexed page as stored.
type Document struct {
	UID, Slug, Scope, Type, Path, Title string
	Aliases, Tags                       []string
	Status, Owner                       string
	Fields                              map[string]any
	BodyMD, BodyHash, GitVersion        string
	// TypeFingerprint is registry.Registry.Fingerprint(Type) as of this row's last real
	// indexing pass — see that method's doc comment for exactly what it covers. The indexer
	// compares it against the current registry's fingerprint alongside BodyHash and
	// GitVersion to decide whether a page needs re-evaluation, so a type's limits changing
	// in defaults/types/*.yaml is not missed just because the page itself did not change.
	TypeFingerprint      string
	Tokens, ClaimsCount  int
	Historical           bool
	CreatedAt, UpdatedAt *time.Time
	LastVerified         *time.Time
}

// Claim is one indexed claim.
type Claim struct {
	ClaimID   string     `json:"claim_id"`
	Text      string     `json:"text"`
	Status    string     `json:"status"`
	Ord       int        `json:"ord"`
	AsOf      *time.Time `json:"as_of"`
	SpanStart *int       `json:"span_start"`
	SpanEnd   *int       `json:"span_end"`
}

// claimJSON mirrors Claim for json_agg decoding. Postgres serializes the `date` column
// as a plain "YYYY-MM-DD" string, which is not what encoding/json's time.Time parser
// accepts (it wants RFC 3339 with a time component) — so decode as *string first and
// parse it explicitly.
type claimJSON struct {
	ClaimID   string  `json:"claim_id"`
	Text      string  `json:"text"`
	Status    string  `json:"status"`
	Ord       int     `json:"ord"`
	AsOf      *string `json:"as_of"`
	SpanStart *int    `json:"span_start"`
	SpanEnd   *int    `json:"span_end"`
}

func (c claimJSON) toClaim() (Claim, error) {
	claim := Claim{
		ClaimID: c.ClaimID, Text: c.Text, Status: c.Status, Ord: c.Ord,
		SpanStart: c.SpanStart, SpanEnd: c.SpanEnd,
	}
	if c.AsOf != nil {
		asOf, err := time.Parse("2006-01-02", *c.AsOf)
		if err != nil {
			return Claim{}, fmt.Errorf("parse as_of %q: %w", *c.AsOf, err)
		}
		claim.AsOf = &asOf
	}
	return claim, nil
}

// Chunk is one body chunk.
type Chunk struct {
	Ord                     int
	HeadingPath, Text, Hash string
	Tokens                  int
}

// PageRow is a page as the API renders it.
type PageRow struct {
	Document
	Claims []Claim
}

// Hit is one search result.
type Hit struct {
	Slug, Title, Type, Status, Scope string
	MatchedClaims                    []string
	Why                              string
	Score                            float64
}

// Finding is one lint finding.
//
// Scope is only meaningful on a global finding — one written with an empty docUID, which
// therefore has no document row to inherit a scope from. A finding about a page that failed
// to parse is exactly that case: there is no uid to attach it to, but its path still names a
// customer's scope, so the scope travels on the finding itself and GlobalFindings filters on
// it. On a finding attached to a document it is ignored; that finding's scope is the
// document's own.
type Finding struct{ Rule, Severity, Detail, Action, Scope string }

// Related is one neighbour page.
type Related struct{ Slug, Title, Status string }

// Edge is one relation between two pages.
type Edge struct{ FromUID, ToUID, Kind string }

// PageFilter narrows ListPages.
type PageFilter struct {
	Types             []string
	Tags              []string
	Status            []string
	IncludeHistorical bool
	// Limit caps the number of rows returned. Zero or negative means
	// unlimited, matching nullableSlice's "empty means no filter"
	// convention for the other fields on this struct.
	Limit int
}

// RelatedNeighbor is one page reached by following an outbound edge, tagged
// with the kind of edge that reached it. It is GetOutboundNeighbors's
// result shape, distinct from Related (GetRelations's shape) because a
// caller doing multi-hop traversal (internal/mcp's related tool) needs the
// neighbour's own UID to keep expanding from it, which Related does not
// carry.
type RelatedNeighbor struct {
	Kind, UID, Slug, Title, Status string
}

// Queries is the only type that sees SQL. Construct it with NewQueries.
type Queries struct {
	pool   *pgxpool.Pool
	scopes Scopes
}

// NewQueries binds a scope set to a pool. This is the only constructor: no call site
// can reach the derived tables without declaring which scopes it may see.
//
// scopes is cloned rather than retained by reference: if the caller kept the slice and later
// appended to it, an append that reuses the backing array would silently widen an already
// constructed Queries — the clone makes that impossible.
func NewQueries(pool *pgxpool.Pool, scopes Scopes) *Queries {
	return &Queries{pool: pool, scopes: slices.Clone(scopes)}
}

// AllowedScopes returns the scopes this Queries was constructed with. It exists so a
// caller outside this package — internal/api's Home handler, in particular — can filter
// data it reads from outside the database (review/*.md proposal files, which carry no
// SQL-enforced scope filter of their own) down to the same scope boundary every query in
// this file already enforces on the derived tables. Without it, a scoped caller could read
// every proposal in the vault regardless of tenant, defeating the point of scoping at all.
//
// The slice is cloned, matching NewQueries's own doc comment: a caller that kept the
// returned slice and appended to it must not be able to widen a live Queries by mutating
// shared backing storage.
func (q *Queries) AllowedScopes() Scopes {
	return slices.Clone(q.scopes)
}

// DiscoverScopes returns every distinct scope already present in the documents table, in
// ascending order. It exists so a caller (cmd/balise's openQueries, in particular) can build the
// Scopes a Queries is constructed with by asking the database what scopes exist, rather than
// naming the documents table itself outside this file — the only file in the repository allowed
// to mention derived tables in a SQL-shaped string literal (internal/guard's
// TestOnlyQueriesFileNamesDerivedTables). It takes the raw pool rather than a *Queries because,
// before the first import, there is no scope set yet to construct a Queries with — this is the
// bootstrap step that produces one.
//
// On a brand-new database this returns an empty, non-nil slice and no error: an empty result is
// not itself an error, it is the true state of an empty vault, and callers that need a
// non-empty scope set to proceed (so writes are not immediately refused by ErrScopeDenied) must
// fall back to a known default themselves.
func DiscoverScopes(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `select distinct scope from documents order by scope`)
	if err != nil {
		return nil, fmt.Errorf("discover scopes: %w", err)
	}
	defer rows.Close()

	scopes := []string{}
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			return nil, fmt.Errorf("discover scopes: scan: %w", err)
		}
		scopes = append(scopes, scope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("discover scopes: %w", err)
	}
	return scopes, nil
}

// UpsertDocument writes one page. Refuses a scope the caller does not hold — both the scope
// named in doc.Scope, checked up front, and, when a row with this uid already exists, the
// scope that row already carries. Without the second check a caller holding only one scope
// could supply a uid that belongs to a document in another scope and rewrite that row's scope
// out from under its owner, exposing its claims through GetPage — a write becoming a
// cross-scope read. That check is enforced inside the statement itself, via
// `where documents.scope = any($n)` on the conflict branch, rather than as a Go-side
// pre-check: a pre-check leaves a time-of-check/time-of-use window between the check and the
// write. When the existing row is not in scope, the ON CONFLICT DO UPDATE ... WHERE clause
// discards the update and the command reports zero rows affected; we surface that as
// ErrScopeDenied instead of silently doing nothing, because a no-op that returns success would
// make the indexer believe it wrote a document it did not touch.
func (q *Queries) UpsertDocument(ctx context.Context, doc Document) error {
	if !q.scopes.allows(doc.Scope) {
		return fmt.Errorf("%s: %w", doc.Scope, ErrScopeDenied)
	}
	fields, err := json.Marshal(orEmptyMap(doc.Fields))
	if err != nil {
		return fmt.Errorf("marshal fields: %w", err)
	}
	tag, err := q.pool.Exec(ctx, `
		insert into documents (uid, slug, scope, type, path, title, aliases, tags, status,
		                       owner, last_verified, fields, body_md, body_hash, tokens,
		                       claims_count, historical, git_version, type_fingerprint,
		                       created_at, updated_at)
		values ($1,$2,$3,$4,$5,$6,$7,$8::text[]::ltree[],$9,$10,$11,$12::jsonb,$13,$14,$15,$16,$17,$18,$19,$20,$21)
		on conflict (uid) do update set
		  slug=excluded.slug, scope=excluded.scope, type=excluded.type, path=excluded.path,
		  title=excluded.title, aliases=excluded.aliases, tags=excluded.tags,
		  status=excluded.status, owner=excluded.owner, last_verified=excluded.last_verified,
		  fields=excluded.fields, body_md=excluded.body_md, body_hash=excluded.body_hash,
		  tokens=excluded.tokens, claims_count=excluded.claims_count,
		  historical=excluded.historical, git_version=excluded.git_version,
		  type_fingerprint=excluded.type_fingerprint,
		  updated_at=excluded.updated_at, indexed_at=now()
		where documents.scope = any($22::text[])`,
		doc.UID, doc.Slug, doc.Scope, doc.Type, doc.Path, doc.Title,
		orEmpty(doc.Aliases), orEmpty(doc.Tags), nullable(doc.Status), nullable(doc.Owner),
		doc.LastVerified, fields, doc.BodyMD, doc.BodyHash, doc.Tokens,
		doc.ClaimsCount, doc.Historical, doc.GitVersion, doc.TypeFingerprint, doc.CreatedAt, doc.UpdatedAt,
		[]string(q.scopes))
	if err != nil {
		return fmt.Errorf("upsert %s: %w", doc.Slug, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%s: %w", doc.UID, ErrScopeDenied)
	}
	return nil
}

// ReplaceClaims swaps a page's entire claim set.
func (q *Queries) ReplaceClaims(ctx context.Context, docUID string, claims []Claim) error {
	if err := q.assertOwned(ctx, docUID); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	batch.Queue(`delete from claims where doc_uid = $1`, docUID)
	for _, claim := range claims {
		batch.Queue(`insert into claims (doc_uid, claim_id, ord, text, status, as_of, span_start, span_end)
		             values ($1,$2,$3,$4,$5,$6,$7,$8)`,
			docUID, claim.ClaimID, claim.Ord, claim.Text, orDefaultStr(claim.Status, "active"),
			claim.AsOf, claim.SpanStart, claim.SpanEnd)
	}
	if err := q.pool.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("replace claims for %s: %w", docUID, err)
	}
	return nil
}

// ReplaceChunks swaps a page's entire chunk set.
func (q *Queries) ReplaceChunks(ctx context.Context, docUID string, chunks []Chunk) error {
	if err := q.assertOwned(ctx, docUID); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	batch.Queue(`delete from chunks where doc_uid = $1`, docUID)
	for _, chunk := range chunks {
		batch.Queue(`insert into chunks (doc_uid, ord, heading_path, text, text_hash, tokens)
		             values ($1,$2,$3,$4,$5,$6)`,
			docUID, chunk.Ord, chunk.HeadingPath, chunk.Text, chunk.Hash, chunk.Tokens)
	}
	if err := q.pool.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("replace chunks for %s: %w", docUID, err)
	}
	return nil
}

// UpsertEdge records one relation. toUID is empty for a dangling reference.
func (q *Queries) UpsertEdge(ctx context.Context, fromUID, toUID, toRef, kind, field string) error {
	if err := q.assertOwned(ctx, fromUID); err != nil {
		return err
	}
	_, err := q.pool.Exec(ctx, `
		insert into edges (from_uid, to_uid, to_ref, kind, field) values ($1,$2,$3,$4,$5)
		on conflict (from_uid, to_ref, kind, field) do update set to_uid = excluded.to_uid`,
		fromUID, nullable(toUID), toRef, kind, field)
	if err != nil {
		return fmt.Errorf("upsert edge %s→%s: %w", fromUID, toRef, err)
	}
	return nil
}

// DeleteDocumentsNotIn removes every document in the allowed scopes whose path is not in
// keep, and reports how many it removed. It is how a reindex prunes: nothing else in this
// package ever deletes a document, so without it a page that moved — the corpus reclassified,
// a tenant corrected in defaults/tenants.yaml — left its old row behind forever, serving one
// customer's body and claims under the wrong scope through /api/pages, /api/search and
// /api/graph with no way to ever remove it.
//
// Scope is honoured exactly as everywhere else: a document in a scope this Queries does not
// hold is not a candidate for deletion, so a narrowly scoped caller can never prune another
// tenant's pages by handing over a short keep list.
//
// claims and chunks are removed by the foreign keys' ON DELETE CASCADE. edges and
// lint_findings are not: neither declares a foreign key to documents (see
// migrations/00001_initial.sql), so both are deleted explicitly here. Inbound edges are
// deleted too, not just outbound ones — an edge whose to_uid names a page that no longer
// exists is a row no query can ever join through, and the next reindex of the referring page
// re-records the reference as dangling anyway.
//
// All of it runs in one transaction: a prune that deleted a document's edges and then failed
// before the document itself would leave a page with its relations silently stripped.
func (q *Queries) DeleteDocumentsNotIn(ctx context.Context, keep []string) (int, error) {
	tx, err := q.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("prune: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		select uid from documents
		where scope = any($1::text[]) and not (path = any($2::text[]))`,
		[]string(q.scopes), orEmpty(keep))
	if err != nil {
		return 0, fmt.Errorf("prune: select: %w", err)
	}
	var dead []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			rows.Close()
			return 0, fmt.Errorf("prune: scan: %w", err)
		}
		dead = append(dead, uid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("prune: select: %w", err)
	}
	if len(dead) == 0 {
		return 0, nil
	}

	if _, err := tx.Exec(ctx, `delete from lint_findings where doc_uid = any($1::text[])`, dead); err != nil {
		return 0, fmt.Errorf("prune: findings: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`delete from edges where from_uid = any($1::text[]) or to_uid = any($1::text[])`, dead); err != nil {
		return 0, fmt.Errorf("prune: edges: %w", err)
	}
	tag, err := tx.Exec(ctx, `delete from documents where uid = any($1::text[])`, dead)
	if err != nil {
		return 0, fmt.Errorf("prune: documents: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("prune: commit: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// ListPages returns pages in the allowed scopes, ordered by type then title.
// A non-positive filter.Limit means unlimited, per PageFilter's own doc
// comment -- `limit $5` with a null argument imposes no cap in Postgres, so
// nullableLimit maps that case to nil rather than to 0 (which would return
// zero rows, the opposite of "unlimited").
func (q *Queries) ListPages(ctx context.Context, filter PageFilter) ([]PageRow, error) {
	rows, err := q.pool.Query(ctx, `
		select d.uid, d.slug, d.scope, d.type, d.path, d.title, d.aliases,
		       d.tags::text[], coalesce(d.status,''), coalesce(d.owner,''), d.last_verified,
		       d.body_md, d.body_hash, d.tokens, d.claims_count, d.historical, d.git_version,
		       d.type_fingerprint,
		       coalesce((select json_agg(json_build_object(
		           'claim_id', c.claim_id, 'ord', c.ord, 'text', c.text,
		           'status', c.status, 'as_of', c.as_of,
		           'span_start', c.span_start, 'span_end', c.span_end) order by c.ord)
		         from claims c where c.doc_uid = d.uid), '[]'::json)
		from documents d
		where d.scope = any($1::text[])
		  and ($2::text[] is null or d.type = any($2::text[]))
		  and ($3::text[] is null or d.tags && $3::text[]::ltree[])
		  and ($4::text[] is null or d.status = any($4::text[]))
		  and ($5 or not d.historical)
		order by d.type, d.title
		limit $6`,
		[]string(q.scopes), nullableSlice(filter.Types), nullableSlice(filter.Tags),
		nullableSlice(filter.Status), filter.IncludeHistorical, nullableLimit(filter.Limit))
	if err != nil {
		return nil, fmt.Errorf("list pages: %w", err)
	}
	defer rows.Close()
	return scanPageRows(rows)
}

// ModalOwner returns the most common non-empty Owner across pages in the allowed scopes — the
// vault's greeting recipient (internal/api's homeOwner). It exists so that computation is a
// single `group by owner` aggregate instead of ListPages's full per-page query, which also
// json-aggregates every page's claims through a correlated subquery: correct for building a
// page list, wasteful for reducing to one name. Empty owners are excluded in SQL, not in Go,
// so a row with no owner never has a chance to outweigh a real one. Ties break alphabetically
// (`order by count(*) desc, owner asc`), the same deterministic tie-break homeOwner's Go-side
// sort used, so the result never depends on scan or map iteration order. Returns "", nil — not
// an error — when no page in scope has an owner, matching ListPages's own "absent is not an
// error" convention.
func (q *Queries) ModalOwner(ctx context.Context, includeHistorical bool) (string, error) {
	rows, err := q.pool.Query(ctx, `
		select owner
		from documents
		where scope = any($1::text[])
		  and owner is not null and owner <> ''
		  and ($2 or not historical)
		group by owner
		order by count(*) desc, owner asc
		limit 1`,
		[]string(q.scopes), includeHistorical)
	if err != nil {
		return "", fmt.Errorf("modal owner: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return "", rows.Err()
	}
	var owner string
	if err := rows.Scan(&owner); err != nil {
		return "", fmt.Errorf("modal owner: scan: %w", err)
	}
	return owner, nil
}

// GetPage returns the page at (scope, slug), or nil when it is absent or out of scope.
// It never distinguishes the two — 04 section 12 forbids leaking "exists elsewhere".
//
// scope is a parameter, not a tie-break, because (scope, slug) is a page's identity in the
// schema: documents is uniquely keyed on (scope, slug), so two scopes may each hold their own
// page at the same slug and those two pages belong to two different customers. An earlier
// version of this method keyed on slug alone and disambiguated with `order by d.scope`. That
// was deterministic and wrong: for a slug present in two allowed scopes it served one
// tenant's body, claims, relations and lint under the other tenant's row, and nothing in the
// response contradicted it. Determinism is not the property this needs — correctness is, and
// the only correct answer is the row the caller actually named. The scope filter is applied
// twice on purpose: `d.scope = $2` selects the named scope, `d.scope = any($3)` still refuses
// a scope this Queries does not hold, so naming a scope in the URL can never widen access.
//
// The unique (scope, slug) index makes at most one row matchable here, so there is no
// remaining ambiguity for this method to resolve. The place where an ambiguous slug can still
// be manufactured is the importer, which turns two source filenames into one slug — see
// ImportCorpus's collision guard, which now rejects that outright instead of letting two rows
// exist for one slug.
func (q *Queries) GetPage(ctx context.Context, scope, slug string) (*PageRow, error) {
	rows, err := q.pool.Query(ctx, `
		select d.uid, d.slug, d.scope, d.type, d.path, d.title, d.aliases,
		       d.tags::text[], coalesce(d.status,''), coalesce(d.owner,''), d.last_verified,
		       d.body_md, d.body_hash, d.tokens, d.claims_count, d.historical, d.git_version,
		       d.type_fingerprint,
		       coalesce((select json_agg(json_build_object(
		           'claim_id', c.claim_id, 'ord', c.ord, 'text', c.text,
		           'status', c.status, 'as_of', c.as_of,
		           'span_start', c.span_start, 'span_end', c.span_end) order by c.ord)
		         from claims c where c.doc_uid = d.uid), '[]'::json)
		from documents d
		where d.slug = $1 and d.scope = $2 and d.scope = any($3::text[])`,
		slug, scope, []string(q.scopes))
	if err != nil {
		return nil, fmt.Errorf("get page %s/%s: %w", scope, slug, err)
	}
	defer rows.Close()

	found, err := scanPageRows(rows)
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return &found[0], nil
}

// SemanticQuery is an optional third input to SearchClaims: a query vector, under a named
// embedding model, to rank claims by cosine similarity alongside the existing lexical and
// trigram signals. A nil SemanticQuery disables the semantic arm entirely and SearchClaims
// behaves exactly as it did before this arm existed -- the behaviour every call site gets
// automatically when no embedding model is configured (internal/embed.TryLoad returning a
// nil *embed.Embedder), so search keeps working, lexical-only, with no special-casing
// needed at the call site.
type SemanticQuery struct {
	Model  string
	Vector []float32
}

// claimRRFRow is one per-claim ranked row from a single retrieval arm (lexical, trigram, or
// semantic), before fusion across arms. why identifies which arm produced it; rrf is that
// arm's own reciprocal-rank score (1/(60+rank), rank being 1-based within the arm).
type claimRRFRow struct {
	docUID string
	claim  string
	why    string
	rrf    float64
}

// rankedDoc is one document after fusing every claimRRFRow that named it, immediately
// before the final documentMetaByUID lookup fills in its displayable fields.
type rankedDoc struct {
	docUID string
	why    string
	score  float64
	claims []string
}

// SearchClaims fuses a lexical claim search and a trigram title search -- and, when sem is
// non-nil, a semantic claim search -- using RRF (k=60).
func (q *Queries) SearchClaims(ctx context.Context, query string, includeHistorical bool, limit int, sem *SemanticQuery) ([]Hit, error) {
	rows, err := q.lexTrigramRows(ctx, query, includeHistorical)
	if err != nil {
		return nil, err
	}
	if sem != nil {
		semRows, err := q.semanticRows(ctx, sem, includeHistorical)
		if err != nil {
			return nil, err
		}
		rows = append(rows, semRows...)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	ranked := fuseRRF(rows, limit)
	uids := make([]string, len(ranked))
	for i, r := range ranked {
		uids[i] = r.docUID
	}
	meta, err := q.documentMetaByUID(ctx, uids)
	if err != nil {
		return nil, err
	}

	hits := make([]Hit, 0, len(ranked))
	for _, r := range ranked {
		m, ok := meta[r.docUID]
		if !ok {
			// Scope re-check found this doc_uid no longer (or never) in the allowed
			// scopes -- defence in depth, since every arm above already applied its own
			// scope filter. Silently dropping it, not erroring, matches every other
			// scope-blind read in this file (GetPage, GetPagesByUIDs, ...).
			continue
		}
		hits = append(hits, Hit{
			Slug: m.slug, Title: m.title, Type: m.docType, Status: m.status, Scope: m.scope,
			MatchedClaims: r.claims, Why: r.why, Score: r.score,
		})
	}
	return hits, nil
}

// lexTrigramRows is 04's original two-signal RRF search (websearch tsquery over claims,
// pg_trgm similarity over titles), returned as flat per-claim rows rather than already
// joined and grouped by document -- splitting the grouping step out (into fuseRRF) is what
// lets semanticRows contribute a third ranked list without duplicating this SQL.
func (q *Queries) lexTrigramRows(ctx context.Context, query string, includeHistorical bool) ([]claimRRFRow, error) {
	rows, err := q.pool.Query(ctx, `
		with lex as (
		  select c.doc_uid, c.text as claim, 'lexical' as why,
		         ts_rank_cd(c.tsv, websearch_to_tsquery('simple', immutable_unaccent($1))) as score
		  from claims c join documents d on d.uid = c.doc_uid
		  where d.scope = any($2::text[]) and ($3 or not d.historical)
		    and c.tsv @@ websearch_to_tsquery('simple', immutable_unaccent($1))
		),
		trg as (
		  select d.uid as doc_uid, d.title as claim, 'trigram' as why,
		         similarity(d.title, $1) as score
		  from documents d
		  where d.scope = any($2::text[]) and ($3 or not d.historical)
		    and similarity(d.title, $1) > 0.25
		)
		select doc_uid, claim, why,
		       1.0 / (60 + rank() over (partition by why order by score desc)) as rrf
		from (select * from lex union all select * from trg) u`,
		query, []string(q.scopes), includeHistorical)
	if err != nil {
		return nil, fmt.Errorf("search %q: %w", query, err)
	}
	defer rows.Close()

	var out []claimRRFRow
	for rows.Next() {
		var row claimRRFRow
		if err := rows.Scan(&row.docUID, &row.claim, &row.why, &row.rrf); err != nil {
			return nil, fmt.Errorf("scan claim row: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// semanticRows scores every scoped, already-embedded claim by cosine similarity against
// sem.Vector and returns the ranking as RRF rows with why="semantic". This is the one arm
// of SearchClaims that runs almost entirely in Go rather than SQL: at this vault's scale
// (~1,100 vectors), brute-force cosine over every candidate is sub-millisecond and exact,
// so there is no approximate index (pgvector+HNSW, per 04 section 21) to build or
// maintain -- and pgvector is not installed in this environment regardless.
//
// Only claims with a cached vector under sem.Model participate. A claim that has not yet
// been embedded (added since the last reindex's embedding pass, or simply because no
// embedder is configured at all) is absent from this arm, never an error -- the same
// graceful-degradation contract one layer down from "no embedder configured skips the arm
// entirely", extended to "a specific claim not yet embedded skips just that claim".
//
// Scope is enforced here by construction, not by an extra filter: ScopedClaimTexts already
// restricts its candidates to q.scopes, so a claim from a scope this Queries does not hold
// is never even a candidate to look up a vector for, let alone score and return.
func (q *Queries) semanticRows(ctx context.Context, sem *SemanticQuery, includeHistorical bool) ([]claimRRFRow, error) {
	candidates, err := q.ScopedClaimTexts(ctx, includeHistorical)
	if err != nil {
		return nil, fmt.Errorf("semantic search: %w", err)
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	hashes := make([]string, len(candidates))
	for i, c := range candidates {
		hashes[i] = embed.Hash(c.Claim)
	}
	cached, err := q.CachedClaimEmbeddings(ctx, sem.Model, hashes)
	if err != nil {
		return nil, fmt.Errorf("semantic search: %w", err)
	}
	if len(cached) == 0 {
		return nil, nil
	}

	results := make([]scoredClaim, 0, len(cached))
	for i, c := range candidates {
		raw, ok := cached[hashes[i]]
		if !ok {
			continue
		}
		vec, err := embed.DecodeVector(raw)
		if err != nil {
			return nil, fmt.Errorf("semantic search: decode vector for %s: %w", c.DocUID, err)
		}
		results = append(results, scoredClaim{c.DocUID, c.Claim, embed.Cosine(sem.Vector, vec)})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].score > results[j].score })

	// A dense embedder always returns its full candidate set ranked by similarity, even
	// when nothing in the vault is actually relevant -- unlike the lexical/trigram arms,
	// which return zero rows for a query that shares no terms with anything indexed.
	// Without this check, a query about a topic entirely absent from the vault (e.g. "best
	// recipe for sourdough bread" against an infrastructure-knowledge vault) still gets 30
	// semantic rows back, and every one of those rows shares *some* incidental term with the
	// query somewhere in its claim text -- so the coverage heuristic in
	// internal/search.AssessCoverage (any shared term across ~450 words of claim text is
	// near-certain over 30 hits) reports "ok" for a query the vault has nothing on.
	//
	// So: treat a weak-evidence semantic arm as if it found nothing at all, the same
	// (nil, nil) signal already used above for "no candidates" / "no cached embeddings".
	// This suppresses the whole arm's rows rather than just flagging them, which both fixes
	// coverage at its source (every coverage computation in the codebase keys off hit
	// count or shared terms, so an empty arm is automatically read as "low" everywhere,
	// with no need to touch AssessCoverage or either MCP coverage computation) and is more
	// honest to an agent than handing back 30 rows it would otherwise have to read in full
	// only to find the best of them is weak.
	if !semanticStrongEnough(results) {
		return nil, nil
	}

	out := make([]claimRRFRow, len(results))
	for rank, r := range results {
		out[rank] = claimRRFRow{
			docUID: r.docUID, claim: r.claim, why: "semantic",
			rrf: 1.0 / float64(60+rank+1),
		}
	}
	return out, nil
}

// scoredClaim is one candidate claim's raw cosine similarity against a query vector, before
// it is converted to an RRF rank score in semanticRows.
type scoredClaim struct {
	docUID, claim string
	score         float64
}

// semanticFloor, semanticFloorDocs and semanticTop1Floor implement the coverage-quality
// floor for the semantic search arm: a query is "strong enough" for semantic evidence if
// EITHER (a) the mean of its best-matching-claim cosine similarity across the top
// semanticFloorDocs *distinct documents* clears semanticFloor, OR (b) its single best match
// alone clears the much higher semanticTop1Floor.
//
// Rule (a) alone -- the original mechanism -- has a real failure mode: a corpus where a
// topic is covered by exactly one excellent document and a handful of only loosely-related
// ones has its one genuine match diluted by averaging in those neighbours to reach
// semanticFloorDocs. A bigger corpus is more likely to have several genuinely-relevant
// documents on any topic it covers well, so the same mean is more likely to stay high there
// -- meaning rule (a) alone is *biased toward large corpora* even though nothing in its
// formula names corpus size directly. Rule (b) exists specifically to catch the case rule
// (a) is biased against: a single standout match, undiluted.
//
// Both rules, and the choice not to use a top-1-relative or z-score contrast metric instead,
// are calibrated against measurements on two real corpora (not guessed), captured here so
// the next person does not have to re-derive them:
//
//   - Real vault at /tmp/balise-live (139 pages / 611 claims): 3 queries it has good
//     material for ("why did the apply fail", "how do I avoid breaking telemetry", "what
//     happens when a gateway is updated") and 3 it has nothing on ("quarterly revenue
//     forecast for the sales team", "how do I train a neural network", "best recipe for
//     sourdough bread").
//   - Demo vault (40 pages / 95 claims, freshly indexed by `make demo`): 2 queries it has
//     good material for ("charged twice", "cannot log in after group change") and 2 it has
//     nothing on ("best pizza toppings for a party", "recommend a hiking trail for
//     beginners").
//
// Measured outcomes, all with sentence-transformers/all-MiniLM-L6-v2:
//
//   - Raw top-1 cosine similarity alone does NOT separate good/bad on the real vault: the
//     weakest good-topic query scored 0.3243, but the strongest absent-topic query scored
//     higher, 0.3371. A flat top-1 floor low enough to admit 0.3243 would also admit 0.3371.
//   - A relative/contrast signal -- z-score of top-1 against the mean and stdev of a wide
//     (n=20) tail of distinct-document scores, and separately the gap between rank-1 and
//     rank-2 -- was tried and measured, per this task's "measure before committing"
//     requirement, and REJECTED: both invert the desired signal on this corpus. An
//     off-topic query in a topically-coherent vault produces a uniformly low AND tight
//     noise floor (nothing in an infra vault relates to "neural network" or "sourdough
//     bread"), so even an incidental, low-absolute top-1 score looks like a huge contrast
//     against that quiet baseline. An on-topic query's tail is itself noisier and higher
//     (many documents share the vault's own domain vocabulary), which suppresses its
//     contrast despite genuinely higher absolute relevance. Measured: "how do I train a
//     neural network" (bad) scored a HIGHER z-score (3.53) than "how do I avoid breaking
//     telemetry" (good, 2.89) -- the opposite of the needed ordering.
//   - The mean cosine similarity of the best-matching claim across the top
//     semanticFloorDocs (5) distinct documents DOES separate both corpora cleanly:
//     real vault good means ranged 0.2668-0.3818, bad means ranged 0.1469-0.2326 (margin
//     ~0.034); demo vault good means were 0.2944 and 0.3164, bad means were 0.0802 and
//     0.0907 (margin ~0.20, wider than the real vault's own margin -- this specific "small
//     corpus" did not in fact suffer the dilution rule (b) guards against, but the demo
//     vault's "charged twice" mean (0.2944) sits close enough to semanticFloor that a
//     smaller demo vault, or a corpus with more filler documents on adjacent topics, could
//     plausibly dip below it; the existing test fixture already showed the mean rule
//     works down to 5 total documents with a wider hand-picked margin, but did not
//     reproduce the dilution failure itself. See TestSemanticFewClaimsSingleStrongMatch for
//     a fixture built specifically to reproduce it.)
//
// semanticFloor (0.25) sits at the midpoint of the real vault's gap (0.2668 and 0.2326).
// semanticTop1Floor (0.45) sits comfortably above the highest incidental top-1 score
// measured on either corpus's bad-topic queries (0.3371 real, 0.1314 demo), with margin to
// spare, while sitting below every good-topic top-1 measured that needed it (demo good
// top-1s were 0.5153 and 0.5315). Neither constant claims to be a universal cutoff for every
// possible vault -- an absolute cosine similarity is fundamentally corpus- and
// model-dependent -- but both are the product of the mechanism the "calibrate, don't guess"
// requirement asked for (per-document dedup, not per-claim, plus this OR), each backed by
// measurement on two corpora three orders of magnitude apart in size, not one.
const (
	semanticFloor     = 0.25
	semanticFloorDocs = 5
	semanticTop1Floor = 0.45
)

// semanticStrongEnough reports whether results (already sorted by descending score) carries
// enough genuine evidence to be worth returning at all. See semanticFloor for the
// calibration this implements.
func semanticStrongEnough(results []scoredClaim) bool {
	if len(results) == 0 {
		return false
	}
	if results[0].score >= semanticTop1Floor {
		return true
	}

	// results is sorted descending, so a document's first appearance in it is already that
	// document's best-matching claim -- no need to scan for the max explicitly.
	seen := make(map[string]bool, semanticFloorDocs)
	var sum float64
	var n int
	for _, r := range results {
		if n >= semanticFloorDocs {
			break
		}
		if seen[r.docUID] {
			continue
		}
		seen[r.docUID] = true
		sum += r.score
		n++
	}
	if n == 0 {
		return false
	}
	return sum/float64(n) >= semanticFloor
}

// fuseRRF sums each document's RRF score across every arm that produced a row for it
// (lexical, trigram, semantic — regardless of how many arms fired for that document), and
// picks one Why per document by fixed precedence: lexical, then trigram, then semantic. A
// claim matched on exact identifier text ("DCGM_FI_", "/etc/alloy", "erconn-003-01") or a
// near-exact title match is always reported under its own, more precise reason, even when
// the same document also happens to score on semantic similarity -- semantic search adds
// recall, it never gets credit for a hit a sharper signal already explains. Results are
// sorted by summed score, descending, then truncated to limit.
func fuseRRF(rows []claimRRFRow, limit int) []rankedDoc {
	whyPriority := map[string]int{"lexical": 0, "trigram": 1, "semantic": 2}

	byDoc := map[string]*rankedDoc{}
	var order []string
	claimSeen := map[string]map[string]bool{}
	for _, row := range rows {
		doc, ok := byDoc[row.docUID]
		if !ok {
			doc = &rankedDoc{docUID: row.docUID, why: row.why}
			byDoc[row.docUID] = doc
			claimSeen[row.docUID] = map[string]bool{}
			order = append(order, row.docUID)
		}
		doc.score += row.rrf
		if !claimSeen[row.docUID][row.claim] {
			claimSeen[row.docUID][row.claim] = true
			doc.claims = append(doc.claims, row.claim)
		}
		if whyPriority[row.why] < whyPriority[doc.why] {
			doc.why = row.why
		}
	}

	docs := make([]rankedDoc, len(order))
	for i, uid := range order {
		docs[i] = *byDoc[uid]
	}
	sort.SliceStable(docs, func(i, j int) bool { return docs[i].score > docs[j].score })
	if limit > 0 && len(docs) > limit {
		docs = docs[:limit]
	}
	return docs
}

// docMeta is the small, scope-checked slice of a document's fields SearchClaims needs to
// build a Hit, once fuseRRF has already picked the winning doc_uids.
type docMeta struct {
	slug, title, docType, status, scope string
}

// documentMetaByUID fetches display fields for exactly the given uids, re-applying the
// scope filter one more time. Every uid reaching this point already passed a scope filter
// in whichever arm produced it (lexTrigramRows's own SQL, or semanticRows via
// ScopedClaimTexts) -- this is deliberate defence in depth, not the only check, so that a
// bug in one arm's filter can never by itself leak a document across scopes.
func (q *Queries) documentMetaByUID(ctx context.Context, uids []string) (map[string]docMeta, error) {
	if len(uids) == 0 {
		return map[string]docMeta{}, nil
	}
	rows, err := q.pool.Query(ctx, `
		select uid, slug, title, type, coalesce(status,''), scope
		from documents
		where uid = any($1::text[]) and scope = any($2::text[])`,
		uids, []string(q.scopes))
	if err != nil {
		return nil, fmt.Errorf("document meta: %w", err)
	}
	defer rows.Close()

	out := map[string]docMeta{}
	for rows.Next() {
		var uid string
		var m docMeta
		if err := rows.Scan(&uid, &m.slug, &m.title, &m.docType, &m.status, &m.scope); err != nil {
			return nil, fmt.Errorf("scan document meta: %w", err)
		}
		out[uid] = m
	}
	return out, rows.Err()
}

// EmbeddingCandidate is one claim eligible for embedding: its text, plus the document it
// belongs to (needed to reconstruct a Hit once a semantic match is found).
type EmbeddingCandidate struct {
	DocUID string
	Claim  string
}

// ScopedClaimTexts returns every claim's text, with its owning document, across the allowed
// scopes. It is the single shared candidate universe for both the bulk embedding pass
// (internal/cli's reindex step) and query-time semantic search (semanticRows) -- using the
// same method in both places means one scope filter guards both call sites, rather than a
// second one being written and risking drift from the first.
func (q *Queries) ScopedClaimTexts(ctx context.Context, includeHistorical bool) ([]EmbeddingCandidate, error) {
	rows, err := q.pool.Query(ctx, `
		select c.doc_uid, c.text
		from claims c join documents d on d.uid = c.doc_uid
		where d.scope = any($1::text[]) and ($2 or not d.historical)`,
		[]string(q.scopes), includeHistorical)
	if err != nil {
		return nil, fmt.Errorf("scoped claim texts: %w", err)
	}
	defer rows.Close()

	var out []EmbeddingCandidate
	for rows.Next() {
		var c EmbeddingCandidate
		if err := rows.Scan(&c.DocUID, &c.Claim); err != nil {
			return nil, fmt.Errorf("scan scoped claim text: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ClaimEmbeddingWrite is one vector ready to be cached, keyed by its claim text's content
// hash (internal/embed.Hash) -- not by claim id or doc_uid, since claim_embeddings has
// neither column (see migrations/00007_claim_embeddings.sql).
type ClaimEmbeddingWrite struct {
	Hash   string
	Vector []byte
}

// UpsertClaimEmbeddings writes newly computed vectors into the cache, keyed by (hash,
// model). "do nothing" on conflict, not "do update": claim_embeddings is a pure
// content-addressed cache and a given (hash, model) pair always maps to the same vector (an
// embedding model is deterministic), so a conflict only happens when two distinct claims
// share identical normalised text and both get embedded within the same pass -- the second
// write is redundant, never a correction, so there is nothing to overwrite it with.
func (q *Queries) UpsertClaimEmbeddings(ctx context.Context, model string, dim int, items []ClaimEmbeddingWrite) error {
	if len(items) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, item := range items {
		batch.Queue(`
			insert into claim_embeddings (hash, model, dim, vector) values ($1,$2,$3,$4)
			on conflict (hash, model) do nothing`,
			item.Hash, model, dim, item.Vector)
	}
	if err := q.pool.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("upsert claim embeddings: %w", err)
	}
	return nil
}

// CachedClaimEmbeddings returns the cached vector bytes for every hash in hashes that is
// already embedded under model, keyed by hash. A hash absent from the result was never
// embedded, or was only ever embedded under a different model -- the caller must treat that
// as a cache miss to re-embed later, never as an error.
func (q *Queries) CachedClaimEmbeddings(ctx context.Context, model string, hashes []string) (map[string][]byte, error) {
	if len(hashes) == 0 {
		return map[string][]byte{}, nil
	}
	rows, err := q.pool.Query(ctx, `
		select hash, vector from claim_embeddings where model = $1 and hash = any($2::text[])`,
		model, hashes)
	if err != nil {
		return nil, fmt.Errorf("cached claim embeddings: %w", err)
	}
	defer rows.Close()

	out := map[string][]byte{}
	for rows.Next() {
		var hash string
		var vector []byte
		if err := rows.Scan(&hash, &vector); err != nil {
			return nil, fmt.Errorf("scan cached claim embedding: %w", err)
		}
		out[hash] = vector
	}
	return out, rows.Err()
}

// Graph returns nodes and the edges between them, all within the allowed scopes.
func (q *Queries) Graph(ctx context.Context, tags []string, limit int) ([]Document, []Edge, error) {
	nodeRows, err := q.pool.Query(ctx, `
		select uid, slug, title, type, scope from documents
		where scope = any($1::text[]) and ($2::text[] is null or tags && $2::text[]::ltree[])
		order by title limit $3`,
		[]string(q.scopes), nullableSlice(tags), limit)
	if err != nil {
		return nil, nil, fmt.Errorf("graph nodes: %w", err)
	}
	defer nodeRows.Close()

	var nodes []Document
	var uids []string
	for nodeRows.Next() {
		var doc Document
		if err := nodeRows.Scan(&doc.UID, &doc.Slug, &doc.Title, &doc.Type, &doc.Scope); err != nil {
			return nil, nil, fmt.Errorf("scan node: %w", err)
		}
		nodes = append(nodes, doc)
		uids = append(uids, doc.UID)
	}
	if err := nodeRows.Err(); err != nil {
		return nil, nil, err
	}

	edgeRows, err := q.pool.Query(ctx, `
		select from_uid, coalesce(to_uid,''), kind from edges
		where from_uid = any($1::text[]) and to_uid = any($1::text[])`, uids)
	if err != nil {
		return nil, nil, fmt.Errorf("graph edges: %w", err)
	}
	defer edgeRows.Close()

	var edges []Edge
	for edgeRows.Next() {
		var edge Edge
		if err := edgeRows.Scan(&edge.FromUID, &edge.ToUID, &edge.Kind); err != nil {
			return nil, nil, fmt.Errorf("scan edge: %w", err)
		}
		edges = append(edges, edge)
	}
	return nodes, edges, edgeRows.Err()
}

// GetRelations returns outbound edges grouped by role plus inbound backlinks.
func (q *Queries) GetRelations(ctx context.Context, docUID string) (map[string][]Related, []Related, error) {
	outbound := map[string][]Related{}
	rows, err := q.pool.Query(ctx, `
		select e.kind, d.slug, d.title, coalesce(d.status,'')
		from edges e join documents d on d.uid = e.to_uid
		where e.from_uid = $1 and d.scope = any($2::text[])
		order by e.kind, d.title`, docUID, []string(q.scopes))
	if err != nil {
		return nil, nil, fmt.Errorf("relations for %s: %w", docUID, err)
	}
	for rows.Next() {
		var kind string
		var rel Related
		if err := rows.Scan(&kind, &rel.Slug, &rel.Title, &rel.Status); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("scan relation: %w", err)
		}
		outbound[kind] = append(outbound[kind], rel)
	}
	rows.Close()

	backRows, err := q.pool.Query(ctx, `
		select d.slug, d.title, coalesce(d.status,'')
		from edges e join documents d on d.uid = e.from_uid
		where e.to_uid = $1 and d.scope = any($2::text[])
		order by d.title`, docUID, []string(q.scopes))
	if err != nil {
		return nil, nil, fmt.Errorf("backlinks for %s: %w", docUID, err)
	}
	defer backRows.Close()

	var backlinks []Related
	for backRows.Next() {
		var rel Related
		if err := backRows.Scan(&rel.Slug, &rel.Title, &rel.Status); err != nil {
			return nil, nil, fmt.Errorf("scan backlink: %w", err)
		}
		backlinks = append(backlinks, rel)
	}
	return outbound, backlinks, backRows.Err()
}

// GetOutboundNeighbors returns every page reached by one outbound edge from
// any of fromUIDs, restricted to the allowed scopes and, when kinds is
// non-empty, to edges of one of those kinds. It exists for internal/mcp's
// related tool to expand a frontier one hop at a time (depth > 1): unlike
// GetRelations, which returns everything grouped by role for a single
// starting page, this takes a whole frontier at once so a BFS caller issues
// one query per hop rather than one per node.
//
// The inner join to documents means a dangling edge (to_uid null, or
// naming a document outside the allowed scopes) is silently excluded from
// the result, never surfaced as an error and never crashing the caller --
// the same behaviour GetRelations already documents and that a hostile or
// merely stale graph (edges pointing at deleted or cross-scope pages) must
// not be able to disrupt.
func (q *Queries) GetOutboundNeighbors(ctx context.Context, fromUIDs []string, kinds []string) ([]RelatedNeighbor, error) {
	if len(fromUIDs) == 0 {
		return nil, nil
	}
	rows, err := q.pool.Query(ctx, `
		select e.kind, d.uid, d.slug, d.title, coalesce(d.status,'')
		from edges e join documents d on d.uid = e.to_uid
		where e.from_uid = any($1::text[]) and d.scope = any($2::text[])
		  and ($3::text[] is null or e.kind = any($3::text[]))
		order by e.kind, d.title`,
		fromUIDs, []string(q.scopes), nullableSlice(kinds))
	if err != nil {
		return nil, fmt.Errorf("outbound neighbors: %w", err)
	}
	defer rows.Close()

	var out []RelatedNeighbor
	for rows.Next() {
		var n RelatedNeighbor
		if err := rows.Scan(&n.Kind, &n.UID, &n.Slug, &n.Title, &n.Status); err != nil {
			return nil, fmt.Errorf("scan outbound neighbor: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// GetPagesByUIDs batch-fetches full page rows (including claims) for every
// uid in uids that falls within the allowed scopes, in no particular order.
// It exists so internal/mcp's context tool can fetch full page data for a
// whole candidate/expansion set in one round trip rather than one GetPage
// call per uid. A uid that is unknown or outside the allowed scopes is
// simply absent from the result -- never an error and never distinguished
// from "does not exist", matching GetPage's own scope-blindness contract.
func (q *Queries) GetPagesByUIDs(ctx context.Context, uids []string) ([]PageRow, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	rows, err := q.pool.Query(ctx, `
		select d.uid, d.slug, d.scope, d.type, d.path, d.title, d.aliases,
		       d.tags::text[], coalesce(d.status,''), coalesce(d.owner,''), d.last_verified,
		       d.body_md, d.body_hash, d.tokens, d.claims_count, d.historical, d.git_version,
		       d.type_fingerprint,
		       coalesce((select json_agg(json_build_object(
		           'claim_id', c.claim_id, 'ord', c.ord, 'text', c.text,
		           'status', c.status, 'as_of', c.as_of,
		           'span_start', c.span_start, 'span_end', c.span_end) order by c.ord)
		         from claims c where c.doc_uid = d.uid), '[]'::json)
		from documents d
		where d.uid = any($1::text[]) and d.scope = any($2::text[])`,
		uids, []string(q.scopes))
	if err != nil {
		return nil, fmt.Errorf("get pages by uid: %w", err)
	}
	defer rows.Close()
	return scanPageRows(rows)
}

// GetFindings returns unresolved lint findings for a page in the allowed scopes.
func (q *Queries) GetFindings(ctx context.Context, docUID string) ([]Finding, error) {
	if err := q.assertOwned(ctx, docUID); err != nil {
		if errors.Is(err, ErrScopeDenied) {
			return nil, nil // out of scope reads as "no findings", never as an error
		}
		return nil, err // an infrastructure failure must not be mistaken for "no findings"
	}
	rows, err := q.pool.Query(ctx, `
		select rule, severity, detail, coalesce(action,'') from lint_findings
		where doc_uid = $1 and resolved_at is null order by severity, rule`, docUID)
	if err != nil {
		return nil, fmt.Errorf("findings for %s: %w", docUID, err)
	}
	defer rows.Close()

	var findings []Finding
	for rows.Next() {
		var finding Finding
		if err := rows.Scan(&finding.Rule, &finding.Severity, &finding.Detail, &finding.Action); err != nil {
			return nil, fmt.Errorf("scan finding: %w", err)
		}
		findings = append(findings, finding)
	}
	return findings, rows.Err()
}

// GlobalFindings returns every unresolved finding that names no document — the ones
// UpsertFinding writes with an empty docUID. They were previously unreadable through any
// query in this file: GetFindings filters on `doc_uid = $1`, which never matches a NULL, so
// an unparseable page (the one finding that by definition has no uid to attach to) was
// counted in a reindex's stdout and then lost. /api/tree surfaces these.
//
// A global finding carries its own scope column rather than inheriting one from a document
// row it does not have, and rows whose scope this Queries does not hold are filtered out
// here. Without that, a work-only caller would read back an unparseable finding whose detail
// names a path like client-globex/notes/x.md — leaking both a tenant's scope name and one of its
// filenames through the one findings path that has no document to check against. A NULL
// scope means "not attributable to any one scope" and is visible to every caller.
func (q *Queries) GlobalFindings(ctx context.Context) ([]Finding, error) {
	rows, err := q.pool.Query(ctx, `
		select rule, severity, detail, coalesce(action,''), coalesce(scope,'')
		from lint_findings
		where doc_uid is null and resolved_at is null
		  and (scope is null or scope = any($1::text[]))
		order by severity, rule, detail`, []string(q.scopes))
	if err != nil {
		return nil, fmt.Errorf("global findings: %w", err)
	}
	defer rows.Close()

	findings := make([]Finding, 0)
	for rows.Next() {
		var finding Finding
		if err := rows.Scan(&finding.Rule, &finding.Severity, &finding.Detail,
			&finding.Action, &finding.Scope); err != nil {
			return nil, fmt.Errorf("scan global finding: %w", err)
		}
		findings = append(findings, finding)
	}
	return findings, rows.Err()
}

// RuleFinding is one unresolved, document-attached lint finding, for aggregating counts
// grouped by rule (Home's "Needs attention" block, spec 02 section 5.1). Only
// document-attached findings are returned; a global finding (no document, e.g.
// unparseable) means something different and is surfaced through GlobalFindings instead.
// Severity and Detail are carried alongside Rule/Scope/Slug/Title so a caller can (a) drop
// suggest-severity bookkeeping findings (resolved_ref) before they ever reach a sentence,
// and (b) classify a dangling_ref's Detail (the unresolved ref text) as cross-scope or
// genuinely missing via a vault.ScopeIndex built from SlugScopeIndex — without either
// requiring a second query or duplicating that classification's inputs.
type RuleFinding struct {
	Rule     string
	Severity string
	Detail   string
	Scope    string
	Slug     string
	Title    string
}

// FindingsByRule returns every unresolved, document-attached lint finding in the allowed
// scopes, one row per finding, ordered by rule then scope/slug so a caller grouping by
// rule gets deterministic output. It deliberately returns one row per finding rather than
// one per affected page: a rule such as dangling_ref can fire more than once on the same
// page (one per broken link), and Home's sentences ("5 links point at pages that do not
// exist") count findings, not pages.
func (q *Queries) FindingsByRule(ctx context.Context) ([]RuleFinding, error) {
	rows, err := q.pool.Query(ctx, `
		select f.rule, f.severity, f.detail, d.scope, d.slug, d.title
		from lint_findings f
		join documents d on d.uid = f.doc_uid
		where f.resolved_at is null and d.scope = any($1::text[])
		order by f.rule, d.scope, d.slug`, []string(q.scopes))
	if err != nil {
		return nil, fmt.Errorf("findings by rule: %w", err)
	}
	defer rows.Close()

	out := make([]RuleFinding, 0)
	for rows.Next() {
		var rf RuleFinding
		if err := rows.Scan(&rf.Rule, &rf.Severity, &rf.Detail, &rf.Scope, &rf.Slug, &rf.Title); err != nil {
			return nil, fmt.Errorf("scan rule finding: %w", err)
		}
		out = append(out, rf)
	}
	return out, rows.Err()
}

// SlugScopeIndex builds a vault.ScopeIndex from every currently indexed document's slug
// and declared aliases, across the allowed scopes. It is the storage-backed counterpart to
// the identity index cli.Reindex builds by walking git during a live reindex: same shape,
// same classification semantics (see vault.ScopeIndex), but sourced from what is actually
// persisted right now rather than from one run's git walk — so a caller with no git access
// (a later `balise reindex` invocation's own printed report, or Home's "Needs attention"
// block) can still tell a cross-scope dangling ref from a genuinely missing one, using
// data that stays correct regardless of how partial the most recent reindex was.
func (q *Queries) SlugScopeIndex(ctx context.Context) (vault.ScopeIndex, error) {
	rows, err := q.pool.Query(ctx, `
		select scope, slug, aliases from documents where scope = any($1::text[])`,
		[]string(q.scopes))
	if err != nil {
		return nil, fmt.Errorf("slug scope index: %w", err)
	}
	defer rows.Close()

	index := vault.ScopeIndex{}
	for rows.Next() {
		var scope, slug string
		var aliases []string
		if err := rows.Scan(&scope, &slug, &aliases); err != nil {
			return nil, fmt.Errorf("scan slug scope row: %w", err)
		}
		index.Record(slug, scope)
		for _, alias := range aliases {
			index.Record(alias, scope)
		}
	}
	return index, rows.Err()
}

// ReplaceFindings makes docUID's set of active lint findings exactly the given set: every
// finding in findings is upserted (same refresh-in-place semantics as UpsertFinding — first_seen
// is untouched, last_seen and resolved_at are refreshed), and every finding still active on
// docUID whose (rule, detail) pair is absent from findings is soft-resolved (resolved_at =
// now()). It intentionally diverges from ReplaceClaims/ReplaceChunks' delete-then-reinsert
// pattern: a finding's row is its own history (first_seen, and now resolved_at), so clearing it
// on every reindex the way claims and chunks are cleared would destroy exactly the timestamps a
// reviewer needs ("how long has this been broken", "when did this get fixed").
//
// This is the fix for findings that never cleared once their cause was gone (see 04's reindex
// section): before this method existed, the only write to lint_findings was UpsertFinding's
// `on conflict do update ... resolved_at = null`, which can reactivate a finding but never
// resolve one, and the only delete was DeleteDocumentsNotIn's whole-document prune. A page
// whose long_headline-triggering title was shortened kept the finding active forever because
// nothing ever ran the other half of the lifecycle.
//
// Every comparison here is scoped to this one docUID — deliberately, not incidentally. A
// naive fix would instead resolve "every active finding not re-raised by this run" across the
// whole vault, which is correct on a full reindex and silently wrong on anything less than
// one: a single-page reindex (there is no such caller today, but ReplaceFindings must still be
// safe if one is ever added) would then clear every other page's findings too, since none of
// them were "re-raised" by a run that never looked at them. Scoping to one docUID makes that
// failure mode structurally impossible rather than relying on a caller to remember a rule —
// the same reasoning DeleteDocumentsNotIn's own doc comment gives for scoping the prune to
// "only the paths this run actually walked."
func (q *Queries) ReplaceFindings(ctx context.Context, docUID string, findings []Finding) error {
	if err := q.assertOwned(ctx, docUID); err != nil {
		return err
	}
	tx, err := q.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("replace findings for %s: begin: %w", docUID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, finding := range findings {
		if _, err := tx.Exec(ctx, `
			insert into lint_findings (rule, severity, doc_uid, detail, action) values ($1,$2,$3,$4,$5)
			on conflict (rule, doc_uid, detail) do update set
			  last_seen = now(), resolved_at = null,
			  severity = excluded.severity, action = excluded.action`,
			finding.Rule, finding.Severity, docUID, finding.Detail, nullable(finding.Action)); err != nil {
			return fmt.Errorf("replace findings for %s: upsert %s: %w", docUID, finding.Rule, err)
		}
	}

	rules := make([]string, len(findings))
	details := make([]string, len(findings))
	for i, finding := range findings {
		rules[i] = finding.Rule
		details[i] = finding.Detail
	}
	// unnest zips the two arrays into (rule, detail) pairs — the "keep set" for this
	// document. An empty findings slice zips to zero pairs, so `not exists` is true for
	// every currently active row and this resolves all of them, which is correct: a page
	// that now raises no findings at all should end up with none active.
	if _, err := tx.Exec(ctx, `
		update lint_findings
		set resolved_at = now()
		where doc_uid = $1 and resolved_at is null
		  and not exists (
		    select 1 from unnest($2::text[], $3::text[]) as keep(rule, detail)
		    where keep.rule = lint_findings.rule and keep.detail = lint_findings.detail
		  )`,
		docUID, rules, details); err != nil {
		return fmt.Errorf("replace findings for %s: resolve stale: %w", docUID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("replace findings for %s: commit: %w", docUID, err)
	}
	return nil
}

// UpsertFinding records or refreshes one lint finding. A finding with no docUID is a global
// finding — one that names no specific document. lint_findings' own uniqueness constraint is
// (rule, doc_uid, detail), but Postgres treats NULLs as distinct by default, so it never
// matches an existing global finding; ON CONFLICT would then have nothing to match and every
// call would insert a fresh duplicate. Global findings therefore target a separate partial
// unique index instead (migrations/00002_global_findings.sql, on (rule, detail) where doc_uid
// is null) — a different ON CONFLICT arbiter, since Postgres allows only one per statement.
func (q *Queries) UpsertFinding(ctx context.Context, docUID string, finding Finding) error {
	if docUID == "" {
		return q.upsertGlobalFinding(ctx, finding)
	}
	if err := q.assertOwned(ctx, docUID); err != nil {
		return err
	}
	_, err := q.pool.Exec(ctx, `
		insert into lint_findings (rule, severity, doc_uid, detail, action) values ($1,$2,$3,$4,$5)
		on conflict (rule, doc_uid, detail) do update set last_seen = now(), resolved_at = null`,
		finding.Rule, finding.Severity, docUID, finding.Detail, nullable(finding.Action))
	if err != nil {
		return fmt.Errorf("upsert finding %s: %w", finding.Rule, err)
	}
	return nil
}

// upsertGlobalFinding writes a finding that names no document. A non-empty finding.Scope is
// refused unless this Queries holds it — a global finding is the one write in this file with
// no document row to check ownership against, so the check has to be made against the value
// the caller supplied, or a caller could file a finding (and a path in its detail) under a
// tenant it does not hold.
func (q *Queries) upsertGlobalFinding(ctx context.Context, finding Finding) error {
	if finding.Scope != "" && !q.scopes.allows(finding.Scope) {
		return fmt.Errorf("%s: %w", finding.Scope, ErrScopeDenied)
	}
	_, err := q.pool.Exec(ctx, `
		insert into lint_findings (rule, severity, doc_uid, detail, action, scope)
		values ($1,$2,null,$3,$4,$5)
		on conflict (rule, detail) where doc_uid is null
		do update set last_seen = now(), resolved_at = null,
		              severity = excluded.severity, action = excluded.action,
		              scope = excluded.scope`,
		finding.Rule, finding.Severity, finding.Detail, nullable(finding.Action),
		nullable(finding.Scope))
	if err != nil {
		return fmt.Errorf("upsert finding %s: %w", finding.Rule, err)
	}
	return nil
}

// ResolveGlobalFindingsForPath soft-resolves any currently active global finding (doc_uid is
// null) of the given rule whose Detail names path. Every global finding this codebase writes
// today ("unparseable", the one case a page fails to parse at all before it ever gets a uid —
// see pipeline.go's parsePage) is written with Detail = "<path>: <message>", so the path is
// the only stable identity such a finding has; starts_with matches that exact prefix rather
// than a LIKE pattern, so a path containing '_' or '%' (both valid in a filename) cannot be
// misread as a wildcard.
//
// It is called once for every path IndexPage successfully parses this run, whether or not
// that parse ends up a content noop, which is what makes a page that recovers from
// "unparseable" clear its finding the same run it starts parsing again. Scoping to one exact
// path — instead of, say, "every unparseable finding not re-raised this run" — is the same
// partial-run safety argument ReplaceFindings makes for document-attached findings: a global
// finding has no document row to check ownership against, so scope is checked directly
// against what this Queries holds, exactly as upsertGlobalFinding checks it on write — a
// caller must not be able to resolve, and by resolving silence, a finding filed under a scope
// it does not hold.
func (q *Queries) ResolveGlobalFindingsForPath(ctx context.Context, rule, scope, path string) error {
	if scope != "" && !q.scopes.allows(scope) {
		return fmt.Errorf("%s: %w", scope, ErrScopeDenied)
	}
	_, err := q.pool.Exec(ctx, `
		update lint_findings
		set resolved_at = now()
		where doc_uid is null and resolved_at is null and rule = $1
		  and starts_with(detail, $2 || ': ')
		  and (scope is null or scope = any($3::text[]))`,
		rule, path, []string(q.scopes))
	if err != nil {
		return fmt.Errorf("resolve global finding %s for %s: %w", rule, path, err)
	}
	return nil
}

func (q *Queries) assertOwned(ctx context.Context, docUID string) error {
	var one int
	err := q.pool.QueryRow(ctx,
		`select 1 from documents where uid = $1 and scope = any($2::text[])`,
		docUID, []string(q.scopes)).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", docUID, ErrScopeDenied)
	}
	if err != nil {
		return fmt.Errorf("check scope for %s: %w", docUID, err)
	}
	return nil
}

// ScopePageCounts returns the number of indexed pages per scope, across every scope this
// Queries was constructed with. It exists for the Settings screen's Scopes section
// (02-ui-design-v1.md section 5.6), which needs a real per-scope count next to each scope's
// name rather than the placeholder figures knowledge-v3.html's mockup shows.
//
// SQL's `group by` only returns scopes that own at least one row, so a configured scope with
// zero indexed pages simply does not appear in the returned map — callers that need every
// configured scope regardless of whether anything has been indexed into it yet must treat a
// missing key as zero themselves, the same convention ModalOwner's caller already applies to
// its "no owner" case.
func (q *Queries) ScopePageCounts(ctx context.Context) (map[string]int, error) {
	rows, err := q.pool.Query(ctx, `
		select scope, count(*)
		from documents
		where scope = any($1::text[])
		group by scope`,
		[]string(q.scopes))
	if err != nil {
		return nil, fmt.Errorf("scope page counts: %w", err)
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var scope string
		var n int
		if err := rows.Scan(&scope, &n); err != nil {
			return nil, fmt.Errorf("scan scope page count: %w", err)
		}
		counts[scope] = n
	}
	return counts, rows.Err()
}

// TagCount is one row of Queries.TagCounts: a tag actually present on at least one page in
// scope (in its stored, dot-separated ltree form, e.g. "customer.globex"), and how many pages
// carry it.
type TagCount struct {
	Tag   string
	Count int
}

// TagCounts returns every distinct tag in use across pages in the allowed scopes, most-used
// first, tag name ascending to break ties deterministically. It exists for the Settings
// screen's Tags section (02-ui-design-v1.md section 5.6), which needs real per-tag counts
// rather than knowledge-v3.html's placeholder figures.
//
// Tag values come back in the same stored dot-separated form ListPages/GetPage's own
// `d.tags::text[]` cast already exposes — converting to the on-screen slash form
// (internal/api's ltreeTagsToSlash) stays the API layer's job, so there is exactly one place
// in the codebase that ever does that conversion.
func (q *Queries) TagCounts(ctx context.Context) ([]TagCount, error) {
	rows, err := q.pool.Query(ctx, `
		select t.tag::text, count(*)
		from documents d, unnest(d.tags) as t(tag)
		where d.scope = any($1::text[])
		group by t.tag
		order by count(*) desc, t.tag::text asc`,
		[]string(q.scopes))
	if err != nil {
		return nil, fmt.Errorf("tag counts: %w", err)
	}
	defer rows.Close()

	var out []TagCount
	for rows.Next() {
		var tc TagCount
		if err := rows.Scan(&tc.Tag, &tc.Count); err != nil {
			return nil, fmt.Errorf("scan tag count: %w", err)
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

// EdgeKindCount is one row of Queries.EdgeKindCounts: a relation kind actually used by at
// least one edge whose source page is in scope, and how many edges use it.
type EdgeKindCount struct {
	Kind  string
	Count int
}

// EdgeKindCounts returns every distinct edge kind in use, most-used first, kind name
// ascending to break ties. It exists for the Settings screen's Relations section
// (02-ui-design-v1.md section 5.6).
//
// Joined to documents rather than read off edges alone, so scope enforcement applies here
// exactly as it does everywhere else in this file: an edge whose source page sits outside
// every scope this Queries was constructed with must not be counted, let alone let its kind
// name leak onto a screen this caller cannot otherwise see into.
func (q *Queries) EdgeKindCounts(ctx context.Context) ([]EdgeKindCount, error) {
	rows, err := q.pool.Query(ctx, `
		select e.kind, count(*)
		from edges e join documents d on d.uid = e.from_uid
		where d.scope = any($1::text[])
		group by e.kind
		order by count(*) desc, e.kind asc`,
		[]string(q.scopes))
	if err != nil {
		return nil, fmt.Errorf("edge kind counts: %w", err)
	}
	defer rows.Close()

	var out []EdgeKindCount
	for rows.Next() {
		var kc EdgeKindCount
		if err := rows.Scan(&kc.Kind, &kc.Count); err != nil {
			return nil, fmt.Errorf("scan edge kind count: %w", err)
		}
		out = append(out, kc)
	}
	return out, rows.Err()
}

func scanPageRows(rows pgx.Rows) ([]PageRow, error) {
	var out []PageRow
	for rows.Next() {
		var row PageRow
		var claimsRaw []byte
		if err := rows.Scan(&row.UID, &row.Slug, &row.Scope, &row.Type, &row.Path, &row.Title,
			&row.Aliases, &row.Tags, &row.Status, &row.Owner, &row.LastVerified,
			&row.BodyMD, &row.BodyHash, &row.Tokens, &row.ClaimsCount, &row.Historical,
			&row.GitVersion, &row.TypeFingerprint, &claimsRaw); err != nil {
			return nil, fmt.Errorf("scan page: %w", err)
		}
		claims, err := decodeClaims(claimsRaw)
		if err != nil {
			return nil, fmt.Errorf("decode claims for %s: %w", row.Slug, err)
		}
		row.Claims = claims
		out = append(out, row)
	}
	return out, rows.Err()
}

func decodeClaims(raw []byte) ([]Claim, error) {
	var wire []claimJSON
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	claims := make([]Claim, 0, len(wire))
	for _, item := range wire {
		claim, err := item.toClaim()
		if err != nil {
			return nil, err
		}
		claims = append(claims, claim)
	}
	return claims, nil
}

func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func orEmptyMap(fields map[string]any) map[string]any {
	if fields == nil {
		return map[string]any{}
	}
	return fields
}

func nullableSlice(values []string) any {
	if len(values) == 0 {
		return nil
	}
	return values
}

// nullableLimit maps a non-positive limit to nil (no `limit` clause applies),
// matching this file's existing "empty/zero means unfiltered" convention
// (see nullableSlice) rather than the SQL-level footgun of `limit 0`, which
// would return zero rows instead of an uncapped result.
func nullableLimit(limit int) any {
	if limit <= 0 {
		return nil
	}
	return limit
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func orDefaultStr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
