package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func seedDoc(t *testing.T, q *store.Queries, overrides func(*store.Document)) store.Document {
	t.Helper()
	doc := store.Document{
		UID: "u1", Slug: "ergw", Scope: "work", Type: "gotcha", Path: "work/gotchas/ergw.md",
		Title:   "AzAPI ER gateway update DELETES its connections",
		Aliases: []string{"ergw-deletion"}, Tags: []string{"vendor.azure.ergw"},
		Status: "active", Owner: "dhia", BodyMD: "body", BodyHash: "h1",
		Tokens: 400, ClaimsCount: 1, GitVersion: "abc",
	}
	if overrides != nil {
		overrides(&doc)
	}
	require.NoError(t, q.UpsertDocument(context.Background(), doc))
	return doc
}

func TestUpsertThenGet(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	seedDoc(t, q, nil)

	page, err := q.GetPage(context.Background(), "work", "ergw")
	require.NoError(t, err)
	require.NotNil(t, page)
	require.Contains(t, page.Title, "AzAPI")
}

func TestGetOutsideScopeReturnsNil(t *testing.T) {
	pool := testutil.NewDB(t)
	seedDoc(t, store.NewQueries(pool, store.Scopes{"work"}), nil)

	page, err := store.NewQueries(pool, store.Scopes{"personal"}).GetPage(context.Background(), "work", "ergw")
	require.NoError(t, err)
	require.Nil(t, page, "a page outside the allowed scopes must be invisible")
}

func TestListPagesFiltersByScope(t *testing.T) {
	pool := testutil.NewDB(t)
	all := store.NewQueries(pool, store.Scopes{"work", "personal"})
	seedDoc(t, all, nil)
	seedDoc(t, all, func(d *store.Document) {
		d.UID, d.Slug, d.Scope, d.Path = "u2", "secret", "personal", "personal/notes/secret.md"
	})

	rows, err := store.NewQueries(pool, store.Scopes{"work"}).ListPages(context.Background(), store.PageFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "ergw", rows[0].Slug)
}

func TestListPagesExcludesHistoricalByDefault(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	seedDoc(t, q, nil)
	seedDoc(t, q, func(d *store.Document) {
		d.UID, d.Slug, d.Path, d.Historical = "u2", "old", "work/gotchas/old.md", true
	})

	rows, err := q.ListPages(context.Background(), store.PageFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 1)

	rows, err = q.ListPages(context.Background(), store.PageFilter{IncludeHistorical: true})
	require.NoError(t, err)
	require.Len(t, rows, 2)
}

func TestUpsertIsIdempotent(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	seedDoc(t, q, nil)
	seedDoc(t, q, nil)

	rows, err := q.ListPages(context.Background(), store.PageFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestEmptyScopesReturnNothing(t *testing.T) {
	pool := testutil.NewDB(t)
	seedDoc(t, store.NewQueries(pool, store.Scopes{"work"}), nil)

	rows, err := store.NewQueries(pool, store.Scopes{}).ListPages(context.Background(), store.PageFilter{})
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestUpsertOutsideScopeIsRefused(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	err := q.UpsertDocument(context.Background(), store.Document{
		UID: "x", Slug: "x", Scope: "personal", Type: "note", Path: "personal/x.md",
		Title: "x", BodyMD: "b", BodyHash: "h", GitVersion: "g",
	})
	require.ErrorIs(t, err, store.ErrScopeDenied)
}

func TestReplaceClaimsSwapsTheWholeSet(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	seedDoc(t, q, nil)
	ctx := context.Background()

	require.NoError(t, q.ReplaceClaims(ctx, "u1", []store.Claim{
		{ClaimID: "c1", Ord: 0, Text: "first", Status: "active"},
	}))
	require.NoError(t, q.ReplaceClaims(ctx, "u1", []store.Claim{
		{ClaimID: "c2", Ord: 0, Text: "second", Status: "active"},
	}))

	page, err := q.GetPage(ctx, "work", "ergw")
	require.NoError(t, err)
	require.Len(t, page.Claims, 1)
	require.Equal(t, "second", page.Claims[0].Text)
}

func TestSearchFindsAClaimAndNamesWhy(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	ctx := context.Background()
	seedDoc(t, q, nil)
	require.NoError(t, q.ReplaceClaims(ctx, "u1", []store.Claim{
		{ClaimID: "c1", Ord: 0, Text: "Updating the gateway removes every connection", Status: "active"},
	}))

	hits, err := q.SearchClaims(ctx, "gateway", false, 10)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.Equal(t, "ergw", hits[0].Slug)
	require.NotEmpty(t, hits[0].MatchedClaims)
	require.Equal(t, "lexical", hits[0].Why)
}

func TestSearchRespectsScope(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()
	work := store.NewQueries(pool, store.Scopes{"work"})
	seedDoc(t, work, nil)
	require.NoError(t, work.ReplaceClaims(ctx, "u1", []store.Claim{
		{ClaimID: "c1", Ord: 0, Text: "Updating the gateway removes every connection", Status: "active"},
	}))

	hits, err := store.NewQueries(pool, store.Scopes{"personal"}).SearchClaims(ctx, "gateway", false, 10)
	require.NoError(t, err)
	require.Empty(t, hits)
}

// seedVictimDoc plants a "personal"-scoped document with a claim only "personal" can see,
// standing in for a page an attacker without that scope should never be able to touch.
func seedVictimDoc(t *testing.T, personal *store.Queries) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, personal.UpsertDocument(ctx, store.Document{
		UID: "victim", Slug: "vpn", Scope: "personal", Type: "gotcha",
		Path: "personal/gotchas/vpn.md", Title: "VPN secret setup",
		BodyMD: "original body", BodyHash: "h1", GitVersion: "g1",
	}))
	require.NoError(t, personal.ReplaceClaims(ctx, "victim", []store.Claim{
		{ClaimID: "c1", Ord: 0, Text: "secret personal claim", Status: "active"},
	}))
}

func TestUpsertCannotHijackAnotherScopesRow(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()
	personal := store.NewQueries(pool, store.Scopes{"personal"})
	seedVictimDoc(t, personal)

	work := store.NewQueries(pool, store.Scopes{"work"})
	err := work.UpsertDocument(ctx, store.Document{
		UID: "victim", Slug: "hijacked", Scope: "work", Type: "gotcha",
		Path: "work/gotchas/hijacked.md", Title: "Hijacked",
		BodyMD: "attacker body", BodyHash: "h2", GitVersion: "g2",
	})
	require.ErrorIs(t, err, store.ErrScopeDenied)

	page, err := personal.GetPage(ctx, "personal", "vpn")
	require.NoError(t, err)
	require.NotNil(t, page, "the victim row must still exist under its own scope")
	require.Equal(t, "personal", page.Scope)
	require.Equal(t, "original body", page.BodyMD)
}

func TestUpsertHijackAttemptLeavesClaimsUnreachable(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()
	personal := store.NewQueries(pool, store.Scopes{"personal"})
	seedVictimDoc(t, personal)

	work := store.NewQueries(pool, store.Scopes{"work"})
	err := work.UpsertDocument(ctx, store.Document{
		UID: "victim", Slug: "hijacked", Scope: "work", Type: "gotcha",
		Path: "work/gotchas/hijacked.md", Title: "Hijacked",
		BodyMD: "attacker body", BodyHash: "h2", GitVersion: "g2",
	})
	require.ErrorIs(t, err, store.ErrScopeDenied)

	hits, err := work.SearchClaims(ctx, "secret", false, 10)
	require.NoError(t, err)
	require.Empty(t, hits, "the victim's claims must stay unreachable from work")

	hijacked, err := work.GetPage(ctx, "work", "hijacked")
	require.NoError(t, err)
	require.Nil(t, hijacked, "the hijack attempt must not have created anything visible to work")
}

func TestGetFindingsOutOfScopeIsEmptyNotError(t *testing.T) {
	pool := testutil.NewDB(t)
	personal := store.NewQueries(pool, store.Scopes{"personal"})
	seedDoc(t, personal, func(d *store.Document) {
		d.UID, d.Slug, d.Scope, d.Path = "p1", "priv", "personal", "personal/priv.md"
	})

	work := store.NewQueries(pool, store.Scopes{"work"})
	findings, err := work.GetFindings(context.Background(), "p1")
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestGetFindingsPropagatesNonScopeErrors(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	findings, err := q.GetFindings(ctx, "whatever")
	require.Error(t, err)
	require.False(t, errors.Is(err, store.ErrScopeDenied),
		"a cancelled context is an infrastructure failure, not a scope decision")
	require.Nil(t, findings)
}

func countLintFindings(t *testing.T, pool *pgxpool.Pool, rule string) int {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(),
		`select count(*) from lint_findings where rule = $1`, rule).Scan(&count)
	require.NoError(t, err)
	return count
}

func TestUpsertFindingGlobalIsIdempotent(t *testing.T) {
	pool := testutil.NewDB(t)
	q := store.NewQueries(pool, store.Scopes{"work"})
	ctx := context.Background()

	finding := store.Finding{Rule: "no-orphan-claims", Severity: "warn", Detail: "global check"}
	require.NoError(t, q.UpsertFinding(ctx, "", finding))
	require.NoError(t, q.UpsertFinding(ctx, "", finding))

	require.Equal(t, 1, countLintFindings(t, pool, "no-orphan-claims"))
}

// findingTimestamps reads first_seen/resolved_at for one document-attached finding, so tests
// can assert on the lifecycle ReplaceFindings is responsible for: first_seen must never move
// once a finding exists, and resolved_at is the soft-resolve signal every read query filters
// on.
func findingTimestamps(t *testing.T, pool *pgxpool.Pool, docUID, rule, detail string) (firstSeen time.Time, resolvedAt *time.Time) {
	t.Helper()
	err := pool.QueryRow(context.Background(),
		`select first_seen, resolved_at from lint_findings where doc_uid = $1 and rule = $2 and detail = $3`,
		docUID, rule, detail).Scan(&firstSeen, &resolvedAt)
	require.NoError(t, err)
	return firstSeen, resolvedAt
}

// globalFindingTimestamps is findingTimestamps for a global (doc_uid is null) finding.
func globalFindingTimestamps(t *testing.T, pool *pgxpool.Pool, rule, detail string) (firstSeen time.Time, resolvedAt *time.Time) {
	t.Helper()
	err := pool.QueryRow(context.Background(),
		`select first_seen, resolved_at from lint_findings where doc_uid is null and rule = $1 and detail = $2`,
		rule, detail).Scan(&firstSeen, &resolvedAt)
	require.NoError(t, err)
	return firstSeen, resolvedAt
}

// TestReplaceFindingsResolvesAFindingWhoseCauseIsGone is the store-level reproduction of the
// bug this method fixes: a page indexed once with a long_headline finding, then reindexed
// with the finding's cause gone, used to leave it active forever because nothing ever wrote a
// non-null resolved_at outside of pruning the whole document. ReplaceFindings with the
// now-empty finding set must resolve it.
func TestReplaceFindingsResolvesAFindingWhoseCauseIsGone(t *testing.T) {
	pool := testutil.NewDB(t)
	q := store.NewQueries(pool, store.Scopes{"work"})
	ctx := context.Background()
	seedDoc(t, q, nil)

	require.NoError(t, q.ReplaceFindings(ctx, "u1", []store.Finding{
		{Rule: "long_headline", Severity: "warn", Detail: "title is 20 words"},
	}))
	findings, err := q.GetFindings(ctx, "u1")
	require.NoError(t, err)
	require.Len(t, findings, 1)

	// The title was shortened: this reindex raises no findings for u1 at all.
	require.NoError(t, q.ReplaceFindings(ctx, "u1", nil))

	findings, err = q.GetFindings(ctx, "u1")
	require.NoError(t, err)
	require.Empty(t, findings, "a finding whose cause is gone must clear, not stay active forever")

	_, resolvedAt := findingTimestamps(t, pool, "u1", "long_headline", "title is 20 words")
	require.NotNil(t, resolvedAt, "a resolved finding is soft-resolved (kept, timestamped), not deleted")
}

// TestReplaceFindingsKeepsAnActiveFindingStableAcrossReindexes covers the other half of the
// spec: a finding whose cause is still present must stay active, and its identity — first_seen
// — must not reset just because the page was reindexed again. A naive "delete then reinsert"
// implementation (the pattern ReplaceClaims/ReplaceChunks use) would fail this, which is why
// ReplaceFindings upserts in place instead.
func TestReplaceFindingsKeepsAnActiveFindingStableAcrossReindexes(t *testing.T) {
	pool := testutil.NewDB(t)
	q := store.NewQueries(pool, store.Scopes{"work"})
	ctx := context.Background()
	seedDoc(t, q, nil)

	finding := store.Finding{Rule: "long_headline", Severity: "warn", Detail: "still too long"}
	require.NoError(t, q.ReplaceFindings(ctx, "u1", []store.Finding{finding}))
	firstSeen1, resolvedAt1 := findingTimestamps(t, pool, "u1", "long_headline", "still too long")
	require.Nil(t, resolvedAt1)

	// Reindexed again; the title is still too long, so the same finding is raised again.
	require.NoError(t, q.ReplaceFindings(ctx, "u1", []store.Finding{finding}))
	findings, err := q.GetFindings(ctx, "u1")
	require.NoError(t, err)
	require.Len(t, findings, 1, "a finding whose cause remains must stay active")

	firstSeen2, resolvedAt2 := findingTimestamps(t, pool, "u1", "long_headline", "still too long")
	require.Nil(t, resolvedAt2)
	require.Equal(t, firstSeen1, firstSeen2,
		"first_seen must not churn just because the page was reindexed again")
}

// TestReplaceFindingsNeverTouchesAnotherDocument is the partial-run safety guarantee: nothing
// about calling ReplaceFindings for one document may ever resolve a finding that belongs to a
// different one. There is no --limit/single-page reindex caller today, but this is exactly the
// trap a vault-wide "resolve everything not re-raised this run" sweep would fall into, so the
// guarantee is proven at the method's own boundary rather than relying on every future caller
// to pass a complete list.
func TestReplaceFindingsNeverTouchesAnotherDocument(t *testing.T) {
	pool := testutil.NewDB(t)
	q := store.NewQueries(pool, store.Scopes{"work"})
	ctx := context.Background()
	seedDoc(t, q, nil)
	seedDoc(t, q, func(d *store.Document) {
		d.UID, d.Slug, d.Path = "u2", "stale", "work/gotchas/stale.md"
	})
	require.NoError(t, q.ReplaceFindings(ctx, "u1", []store.Finding{
		{Rule: "long_headline", Severity: "warn", Detail: "u1 is long"},
	}))
	require.NoError(t, q.ReplaceFindings(ctx, "u2", []store.Finding{
		{Rule: "long_headline", Severity: "warn", Detail: "u2 is long"},
	}))

	// Only u1 gets reindexed this run, with its finding's cause now gone.
	require.NoError(t, q.ReplaceFindings(ctx, "u1", nil))

	u1Findings, err := q.GetFindings(ctx, "u1")
	require.NoError(t, err)
	require.Empty(t, u1Findings)

	u2Findings, err := q.GetFindings(ctx, "u2")
	require.NoError(t, err)
	require.Len(t, u2Findings, 1,
		"a document never passed to ReplaceFindings this run must keep its findings untouched")
}

// TestResolveGlobalFindingsForPathResolvesWhenPageParses covers the one real global finding
// in the codebase today: "unparseable". A page that used to fail to parse and now parses
// again must have its global finding resolved, keyed by the path in Detail since an
// unparseable page never gets a uid to attach a finding to.
func TestResolveGlobalFindingsForPathResolvesWhenPageParses(t *testing.T) {
	pool := testutil.NewDB(t)
	q := store.NewQueries(pool, store.Scopes{"work"})
	ctx := context.Background()

	require.NoError(t, q.UpsertFinding(ctx, "", store.Finding{
		Rule: "unparseable", Severity: "error", Detail: "work/bad.md: boom", Scope: "work",
	}))

	require.NoError(t, q.ResolveGlobalFindingsForPath(ctx, "unparseable", "work", "work/bad.md"))

	found, err := q.GlobalFindings(ctx)
	require.NoError(t, err)
	for _, f := range found {
		require.NotEqual(t, "work/bad.md: boom", f.Detail, "a page that now parses must clear its unparseable finding")
	}
	_, resolvedAt := globalFindingTimestamps(t, pool, "unparseable", "work/bad.md: boom")
	require.NotNil(t, resolvedAt)
}

// TestResolveGlobalFindingsForPathOnlyTouchesThatPath is the same partial-run safety guarantee
// as TestReplaceFindingsNeverTouchesAnotherDocument, for the global side: resolving one path's
// unparseable finding must never resolve another path's, however similar their names (e.g.
// sharing an underscore, which would be a LIKE wildcard if this used LIKE instead of
// starts_with).
func TestResolveGlobalFindingsForPathOnlyTouchesThatPath(t *testing.T) {
	pool := testutil.NewDB(t)
	q := store.NewQueries(pool, store.Scopes{"work"})
	ctx := context.Background()

	require.NoError(t, q.UpsertFinding(ctx, "", store.Finding{
		Rule: "unparseable", Severity: "error", Detail: "work/a_b.md: boom", Scope: "work"}))
	require.NoError(t, q.UpsertFinding(ctx, "", store.Finding{
		Rule: "unparseable", Severity: "error", Detail: "work/axb.md: boom", Scope: "work"}))

	require.NoError(t, q.ResolveGlobalFindingsForPath(ctx, "unparseable", "work", "work/a_b.md"))

	found, err := q.GlobalFindings(ctx)
	require.NoError(t, err)
	details := []string{}
	for _, f := range found {
		details = append(details, f.Detail)
	}
	require.NotContains(t, details, "work/a_b.md: boom")
	require.Contains(t, details, "work/axb.md: boom",
		"an unrelated path must survive even if its name would match as a LIKE wildcard")
}

// TestResolveGlobalFindingsForPathOutsideScopeIsRefused mirrors
// TestGlobalFindingOutsideScopeIsRefused: a global finding has no document row to check
// ownership against, so resolving one must be refused exactly like writing one is.
func TestResolveGlobalFindingsForPathOutsideScopeIsRefused(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	err := q.ResolveGlobalFindingsForPath(context.Background(), "unparseable", "personal", "personal/x.md")
	require.ErrorIs(t, err, store.ErrScopeDenied)
}

func TestNewQueriesClonesScopes(t *testing.T) {
	pool := testutil.NewDB(t)
	backing := make(store.Scopes, 1, 4) // headroom so append reuses the same backing array
	backing[0] = "work"
	q := store.NewQueries(pool, backing)

	backing = append(backing, "personal") // would silently widen q if scopes were shared

	seedDoc(t, store.NewQueries(pool, store.Scopes{"personal"}), func(d *store.Document) {
		d.UID, d.Slug, d.Scope, d.Path = "u3", "should-stay-hidden", "personal", "personal/hidden.md"
	})

	ctx := context.Background()
	page, err := q.GetPage(ctx, "personal", "should-stay-hidden")
	require.NoError(t, err)
	require.Nil(t, page, "appending to the caller's slice must not widen an existing Queries")

	err = q.UpsertDocument(ctx, store.Document{
		UID: "u4", Slug: "u4", Scope: "personal", Type: "note", Path: "personal/u4.md",
		Title: "u4", BodyMD: "b", BodyHash: "h", GitVersion: "g",
	})
	require.ErrorIs(t, err, store.ErrScopeDenied)
}

func TestReplaceClaimsRoundTripsAsOfDate(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	seedDoc(t, q, nil)
	ctx := context.Background()

	asOf := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	require.NoError(t, q.ReplaceClaims(ctx, "u1", []store.Claim{
		{ClaimID: "c1", Ord: 0, Text: "dated claim", Status: "active", AsOf: &asOf},
	}))

	page, err := q.GetPage(ctx, "work", "ergw")
	require.NoError(t, err)
	require.Len(t, page.Claims, 1)
	require.NotNil(t, page.Claims[0].AsOf)
	require.True(t, asOf.Equal(*page.Claims[0].AsOf))
}

// TestGetPageIsScopeQualified pins the fix for a slug held in two scopes. documents is
// unique (scope, slug), so "vpn" can name a different customer's page in each scope. GetPage
// used to key on slug alone and tie-break with `order by d.scope`, which handed the
// alphabetically-first scope's body, claims, relations and lint to a caller who had opened the
// other one — deterministic, and wrong. Each scope must get its own row, and naming a scope
// the caller does not hold must read as absent rather than widen what it can see.
func TestGetPageIsScopeQualified(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()
	work := store.NewQueries(pool, store.Scopes{"work"})
	personal := store.NewQueries(pool, store.Scopes{"personal"})
	seedDoc(t, work, func(d *store.Document) {
		d.UID, d.Slug, d.Scope, d.Path = "wa", "vpn", "work", "work/vpn.md"
		d.BodyMD = "work body"
	})
	seedDoc(t, personal, func(d *store.Document) {
		d.UID, d.Slug, d.Scope, d.Path = "pa", "vpn", "personal", "personal/vpn.md"
		d.BodyMD = "personal body"
	})

	both := store.NewQueries(pool, store.Scopes{"work", "personal"})
	fromWork, err := both.GetPage(ctx, "work", "vpn")
	require.NoError(t, err)
	require.NotNil(t, fromWork)
	require.Equal(t, "work", fromWork.Scope)
	require.Equal(t, "work body", fromWork.BodyMD)

	fromPersonal, err := both.GetPage(ctx, "personal", "vpn")
	require.NoError(t, err)
	require.NotNil(t, fromPersonal)
	require.Equal(t, "personal", fromPersonal.Scope)
	require.Equal(t, "personal body", fromPersonal.BodyMD)

	hidden, err := work.GetPage(ctx, "personal", "vpn")
	require.NoError(t, err)
	require.Nil(t, hidden, "a scope the caller does not hold must read as absent")
}

// TestDeleteDocumentsNotInPrunesOnlyWhatIsGone is the store half of the fix for a correction
// duplicating a customer's data: nothing in this package deleted a document at all, so a page
// that moved scope left its old row behind forever.
func TestDeleteDocumentsNotInPrunesOnlyWhatIsGone(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	ctx := context.Background()
	seedDoc(t, q, nil)
	seedDoc(t, q, func(d *store.Document) {
		d.UID, d.Slug, d.Path = "u2", "stale", "work/gotchas/stale.md"
	})

	removed, err := q.DeleteDocumentsNotIn(ctx, []string{"work/gotchas/ergw.md"})
	require.NoError(t, err)
	require.Equal(t, 1, removed)

	rows, err := q.ListPages(ctx, store.PageFilter{IncludeHistorical: true})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "ergw", rows[0].Slug)

	again, err := q.DeleteDocumentsNotIn(ctx, []string{"work/gotchas/ergw.md"})
	require.NoError(t, err)
	require.Zero(t, again, "pruning twice must remove nothing the second time")
}

// TestDeleteDocumentsNotInNeverTouchesAnotherScope pins the prune to the same scope boundary
// every other method honours: a narrowly scoped caller handing over a short keep list must not
// be able to delete another tenant's pages with it.
func TestDeleteDocumentsNotInNeverTouchesAnotherScope(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()
	personal := store.NewQueries(pool, store.Scopes{"personal"})
	seedVictimDoc(t, personal)

	work := store.NewQueries(pool, store.Scopes{"work"})
	removed, err := work.DeleteDocumentsNotIn(ctx, nil)
	require.NoError(t, err)
	require.Zero(t, removed)

	page, err := personal.GetPage(ctx, "personal", "vpn")
	require.NoError(t, err)
	require.NotNil(t, page, "another scope's page must survive a prune it was never in")
}

// TestDeleteDocumentsNotInRemovesEdgesAndFindings covers the two tables that have no foreign
// key to documents (see migrations/00001_initial.sql): claims and chunks cascade, edges and
// lint_findings do not, so both have to be deleted explicitly or a pruned page leaves rows
// behind that no query can ever join through.
func TestDeleteDocumentsNotInRemovesEdgesAndFindings(t *testing.T) {
	pool := testutil.NewDB(t)
	q := store.NewQueries(pool, store.Scopes{"work"})
	ctx := context.Background()
	seedDoc(t, q, nil)
	seedDoc(t, q, func(d *store.Document) {
		d.UID, d.Slug, d.Path = "u2", "stale", "work/gotchas/stale.md"
	})
	require.NoError(t, q.UpsertEdge(ctx, "u2", "u1", "ergw", "link", ""))
	require.NoError(t, q.UpsertEdge(ctx, "u1", "u2", "stale", "link", ""))
	require.NoError(t, q.UpsertFinding(ctx, "u2", store.Finding{
		Rule: "oversize", Severity: "warn", Detail: "too big"}))

	_, err := q.DeleteDocumentsNotIn(ctx, []string{"work/gotchas/ergw.md"})
	require.NoError(t, err)

	require.Zero(t, countLintFindings(t, pool, "oversize"))
	var edges int
	require.NoError(t, pool.QueryRow(ctx, `select count(*) from edges`).Scan(&edges))
	require.Zero(t, edges, "both the pruned page's own edges and edges pointing at it must go")
}

// TestGlobalFindingsAreReadableAndScoped covers the findings that name no document — the only
// kind an unparseable page can produce, since it has no uid. GetFindings filters on
// `doc_uid = $1`, which never matches a NULL, so before GlobalFindings existed these were
// written and never readable through anything. They must also stay behind the scope boundary:
// an unparseable page's detail carries its path, which names a tenant and one of its files.
func TestGlobalFindingsAreReadableAndScoped(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()
	work := store.NewQueries(pool, store.Scopes{"work"})
	personal := store.NewQueries(pool, store.Scopes{"personal"})

	require.NoError(t, work.UpsertFinding(ctx, "", store.Finding{
		Rule: "unparseable", Severity: "error", Detail: "work/bad.md: boom", Scope: "work"}))
	require.NoError(t, personal.UpsertFinding(ctx, "", store.Finding{
		Rule: "unparseable", Severity: "error", Detail: "personal/secret.md: boom", Scope: "personal"}))
	require.NoError(t, work.UpsertFinding(ctx, "", store.Finding{
		Rule: "no-orphan-claims", Severity: "warn", Detail: "whole-vault check"}))

	found, err := work.GlobalFindings(ctx)
	require.NoError(t, err)
	details := []string{}
	for _, finding := range found {
		details = append(details, finding.Detail)
	}
	require.Contains(t, details, "work/bad.md: boom")
	require.Contains(t, details, "whole-vault check", "an unscoped global finding is visible to all")
	require.NotContains(t, details, "personal/secret.md: boom",
		"another scope's path must never leak through the findings with no document to check")
}

// TestGlobalFindingOutsideScopeIsRefused: a global finding is the one write with no document
// row to check ownership against, so the scope it names has to be checked directly.
func TestGlobalFindingOutsideScopeIsRefused(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	err := q.UpsertFinding(context.Background(), "", store.Finding{
		Rule: "unparseable", Severity: "error", Detail: "personal/x.md: boom", Scope: "personal"})
	require.ErrorIs(t, err, store.ErrScopeDenied)
}

// TestAllowedScopesReturnsAClone guards the same hazard TestNewQueriesClonesScopes checks at
// construction time: AllowedScopes hands scope data to a caller outside this package (Home's
// handler, filtering file-based proposals), and mutating the returned slice must never reach
// back into the live Queries.
func TestAllowedScopesReturnsAClone(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work", "personal"})

	got := q.AllowedScopes()
	require.ElementsMatch(t, []string{"work", "personal"}, []string(got))

	got[0] = "tampered"
	require.ElementsMatch(t, []string{"work", "personal"}, []string(q.AllowedScopes()),
		"mutating the returned slice must not affect the Queries it came from")
}

// insertLintFinding writes a document-attached finding directly, bypassing UpsertFinding's
// scope check, so tests can seed findings for two different scopes against the same pool.
func insertLintFinding(t *testing.T, pool *pgxpool.Pool, docUID, rule, detail string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`insert into lint_findings (doc_uid, rule, severity, detail) values ($1,$2,'warn',$3)`,
		docUID, rule, detail)
	require.NoError(t, err)
}

func TestFindingsByRuleGroupsAcrossPagesAndCountsEachRow(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()
	q := store.NewQueries(pool, store.Scopes{"work"})

	seedDoc(t, q, func(d *store.Document) {
		d.UID, d.Slug, d.Path, d.Title = "d1", "one", "work/one.md", "Page One"
	})
	seedDoc(t, q, func(d *store.Document) {
		d.UID, d.Slug, d.Path, d.Title = "d2", "two", "work/two.md", "Page Two"
	})

	// dangling_ref fires twice on the same page — the count must be of findings, not pages.
	insertLintFinding(t, pool, "d1", "dangling_ref", "one.md -> missing-a")
	insertLintFinding(t, pool, "d1", "dangling_ref", "one.md -> missing-b")
	insertLintFinding(t, pool, "d2", "oversize", "two.md is 9000 tokens")

	found, err := q.FindingsByRule(ctx)
	require.NoError(t, err)

	byRule := map[string][]store.RuleFinding{}
	for _, rf := range found {
		byRule[rf.Rule] = append(byRule[rf.Rule], rf)
	}
	require.Len(t, byRule["dangling_ref"], 2, "one row per finding, not one per page")
	require.Len(t, byRule["oversize"], 1)
	require.Equal(t, "two", byRule["oversize"][0].Slug)
}

func TestFindingsByRuleRespectsScope(t *testing.T) {
	pool := testutil.NewDB(t)
	work := store.NewQueries(pool, store.Scopes{"work"})
	personal := store.NewQueries(pool, store.Scopes{"personal"})

	seedDoc(t, work, func(d *store.Document) {
		d.UID, d.Slug, d.Scope, d.Path = "w1", "wpage", "work", "work/wpage.md"
	})
	seedDoc(t, personal, func(d *store.Document) {
		d.UID, d.Slug, d.Scope, d.Path = "p1", "ppage", "personal", "personal/ppage.md"
	})
	insertLintFinding(t, pool, "w1", "missing_claims", "wpage has no claims")
	insertLintFinding(t, pool, "p1", "missing_claims", "ppage has no claims")

	found, err := work.FindingsByRule(context.Background())
	require.NoError(t, err)
	require.Len(t, found, 1)
	require.Equal(t, "wpage", found[0].Slug, "another scope's finding must not leak through")
}

// TestScopePageCountsCountsPerScopeAndOmitsEmptyScopes pins Settings' Scopes-section
// dependency: a page count per scope, and a scope with zero pages simply absent from the
// map (the caller — internal/api/settings.go's settingsScopes — must treat a missing key
// as zero, not as an error).
func TestScopePageCountsCountsPerScopeAndOmitsEmptyScopes(t *testing.T) {
	pool := testutil.NewDB(t)
	q := store.NewQueries(pool, store.Scopes{"work", "client-globex", "client-empty"})
	seedDoc(t, q, func(d *store.Document) {
		d.UID, d.Slug, d.Scope, d.Path = "w1", "wpage", "work", "work/wpage.md"
	})
	seedDoc(t, q, func(d *store.Document) {
		d.UID, d.Slug, d.Scope, d.Path = "w2", "wpage2", "work", "work/wpage2.md"
	})
	seedDoc(t, q, func(d *store.Document) {
		d.UID, d.Slug, d.Scope, d.Path = "l1", "lpage", "client-globex", "client-globex/lpage.md"
	})

	counts, err := q.ScopePageCounts(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, counts["work"])
	require.Equal(t, 1, counts["client-globex"])
	_, present := counts["client-empty"]
	require.False(t, present, "a scope with zero pages must be absent, not zero-valued")
}

// TestScopePageCountsRespectsScope confirms an out-of-scope page's count never leaks into
// a Queries bound to a narrower scope set — the same boundary every other Queries method
// enforces.
func TestScopePageCountsRespectsScope(t *testing.T) {
	pool := testutil.NewDB(t)
	all := store.NewQueries(pool, store.Scopes{"work", "personal"})
	seedDoc(t, all, func(d *store.Document) {
		d.UID, d.Slug, d.Scope, d.Path = "w1", "wpage", "work", "work/wpage.md"
	})
	seedDoc(t, all, func(d *store.Document) {
		d.UID, d.Slug, d.Scope, d.Path = "p1", "ppage", "personal", "personal/ppage.md"
	})

	workOnly := store.NewQueries(pool, store.Scopes{"work"})
	counts, err := workOnly.ScopePageCounts(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, counts["work"])
	_, present := counts["personal"]
	require.False(t, present)
}

// TestTagCountsOrdersByCountThenTagAndRespectsScope pins Settings' Tags list: most-used
// tag first, ties broken alphabetically, in the stored dot-separated ltree form (slash
// conversion is internal/api's job, per TagCounts' own doc comment) — and a tag that only
// exists on an out-of-scope page must not appear at all.
func TestTagCountsOrdersByCountThenTagAndRespectsScope(t *testing.T) {
	pool := testutil.NewDB(t)
	all := store.NewQueries(pool, store.Scopes{"work", "personal"})
	seedDoc(t, all, func(d *store.Document) {
		d.UID, d.Slug, d.Scope, d.Path = "w1", "wpage1", "work", "work/wpage1.md"
		d.Tags = []string{"customer.acme", "layer.api"}
	})
	seedDoc(t, all, func(d *store.Document) {
		d.UID, d.Slug, d.Scope, d.Path = "w2", "wpage2", "work", "work/wpage2.md"
		d.Tags = []string{"customer.acme"}
	})
	seedDoc(t, all, func(d *store.Document) {
		d.UID, d.Slug, d.Scope, d.Path = "p1", "ppage", "personal", "personal/ppage.md"
		// ltree labels only allow alphanumerics and underscores (no hyphens), hence
		// "only_personal" rather than "only-personal" here.
		d.Tags = []string{"customer.only_personal"}
	})

	workOnly := store.NewQueries(pool, store.Scopes{"work"})
	tags, err := workOnly.TagCounts(context.Background())
	require.NoError(t, err)
	require.Len(t, tags, 2)
	require.Equal(t, "customer.acme", tags[0].Tag)
	require.Equal(t, 2, tags[0].Count)
	require.Equal(t, "layer.api", tags[1].Tag)
	require.Equal(t, 1, tags[1].Count)
	for _, tag := range tags {
		require.NotEqual(t, "customer.only_personal", tag.Tag)
	}
}

// TestEdgeKindCountsRespectsScopeOfFromDocument pins the join EdgeKindCounts' doc comment
// promises: an edge is scoped by its *from* document, so an edge whose source page is
// outside the allowed scopes must never contribute its kind to the count, even if its
// target is in-scope.
func TestEdgeKindCountsRespectsScopeOfFromDocument(t *testing.T) {
	pool := testutil.NewDB(t)
	all := store.NewQueries(pool, store.Scopes{"work", "personal"})
	seedDoc(t, all, func(d *store.Document) {
		d.UID, d.Slug, d.Scope, d.Path = "w1", "wpage1", "work", "work/wpage1.md"
	})
	seedDoc(t, all, func(d *store.Document) {
		d.UID, d.Slug, d.Scope, d.Path = "w2", "wpage2", "work", "work/wpage2.md"
	})
	seedDoc(t, all, func(d *store.Document) {
		d.UID, d.Slug, d.Scope, d.Path = "p1", "ppage", "personal", "personal/ppage.md"
	})
	require.NoError(t, all.UpsertEdge(context.Background(), "w1", "w2", "wpage2", "about", "related"))
	require.NoError(t, all.UpsertEdge(context.Background(), "p1", "w2", "wpage2", "supersedes", "related"))

	workOnly := store.NewQueries(pool, store.Scopes{"work"})
	kinds, err := workOnly.EdgeKindCounts(context.Background())
	require.NoError(t, err)
	require.Len(t, kinds, 1)
	require.Equal(t, "about", kinds[0].Kind)
	require.Equal(t, 1, kinds[0].Count)
}
