package indexer_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dhia/balise/internal/indexer"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/dhia/balise/internal/vault"
	"github.com/stretchr/testify/require"
)

const gotchaPage = `---
uid: 01J8Z3K9V6Q2M4N7P8R9S0T1U2
slug: ergw
type: gotcha
scope: work
title: AzAPI ER gateway update DELETES its connections
tags: [vendor/azure/expressroute]
claims:
  - text: Updating the gateway through AzAPI removes every connection
    status: active
    id: c1
  - text: Old workaround was a manual re-create
    status: superseded
    as_of: 2026-06-10
    id: c2
---
Body text mentioning [[other-page]].
`

func pipelineContext(t *testing.T) indexer.Context {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gotcha.yaml"),
		[]byte("name: gotcha\ntraits: [indexed, cited]\nmax_tokens: 1200\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vendor.yaml"),
		[]byte("name: vendor\nvalues:\n  azure:\n    children:\n      expressroute: {}\n"), 0o644))

	reg, err := registry.Load(dir)
	require.NoError(t, err)
	facets, err := registry.LoadFacets(dir)
	require.NoError(t, err)
	return indexer.Context{Registry: reg, Facets: facets,
		Slugs: map[vault.ScopedSlug]string{}, Aliases: vault.AliasRegistry{}}
}

func index(t *testing.T, q *store.Queries, raw string) indexer.Result {
	t.Helper()
	result, err := indexer.IndexPage(context.Background(), q,
		"work/gotchas/ergw.md", raw, "abc", pipelineContext(t))
	require.NoError(t, err)
	return result
}

func hasFinding(result indexer.Result, rule string) bool {
	for _, finding := range result.Findings {
		if finding.Rule == rule {
			return true
		}
	}
	return false
}

func TestIndexesAPageWithItsClaims(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	index(t, q, gotchaPage)

	page, err := q.GetPage(context.Background(), "work", "ergw")
	require.NoError(t, err)
	require.Equal(t, 2, page.ClaimsCount)
	require.Equal(t, "active", page.Claims[0].Status)
	require.Equal(t, "superseded", page.Claims[1].Status)
}

func TestTagsAreStoredAsLtree(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	index(t, q, gotchaPage)

	rows, err := q.ListPages(context.Background(),
		store.PageFilter{Tags: []string{"vendor.azure.expressroute"}})
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestReindexingUnchangedContentIsANoop(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	require.True(t, index(t, q, gotchaPage).Changed)
	require.False(t, index(t, q, gotchaPage).Changed)
}

func TestUnparseableYAMLIsReportedNotFatal(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	result, err := indexer.IndexPage(context.Background(), q,
		"work/bad.md", "---\n\tbad: [unclosed\n---\nbody\n", "abc", pipelineContext(t))
	require.NoError(t, err, "one bad file must never fail a reindex")
	require.False(t, result.Indexed)
	require.True(t, hasFinding(result, "unparseable"))

	// The finding used to be returned to the caller and nothing else: counted in the
	// reindex's stdout total and then discarded, so the page was simply absent from the UI
	// with nothing anywhere explaining why. It has no uid to attach to — that is the whole
	// problem with an unparseable page — so it is stored as a global finding carrying the
	// path in its detail, and the path's scope so it stays behind the same boundary.
	globals, err := q.GlobalFindings(context.Background())
	require.NoError(t, err)
	require.Len(t, globals, 1)
	require.Equal(t, "unparseable", globals[0].Rule)
	require.Contains(t, globals[0].Detail, "work/bad.md")
	require.Equal(t, "work", globals[0].Scope)
}

// TestUnparseablePageOutsideScopeStillDoesNotFailTheReindex: an unparseable page has no
// frontmatter, so its scope can only come from its path — and that path may name a scope this
// Queries does not hold, in which case the finding cannot be filed. 04 section 7 is the
// governing rule of this pipeline: one bad file must never fail a reindex of many, and a file
// that is both unparseable and out of scope is exactly the case it exists for.
func TestUnparseablePageOutsideScopeStillDoesNotFailTheReindex(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	result, err := indexer.IndexPage(context.Background(), q,
		"personal/bad.md", "---\n\tbad: [unclosed\n---\nbody\n", "abc", pipelineContext(t))
	require.NoError(t, err, "one bad file must never fail a reindex, in scope or out")
	require.False(t, result.Indexed)
	require.True(t, hasFinding(result, "unparseable"), "the finding still reaches the caller")

	globals, err := q.GlobalFindings(context.Background())
	require.NoError(t, err)
	require.Empty(t, globals, "but it is never filed under a scope the caller does not hold")
}

func TestMissingUIDIsAssignedAndFlaggedForWriteback(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	result := index(t, q, "---\nslug: x\ntype: gotcha\nscope: work\ntitle: T\n---\nbody\n")
	require.NotEmpty(t, result.AssignedUID)
	require.True(t, result.NeedsWriteback)
}

// TestUIDLessPageReusesItsRecordedUIDAcrossReindexes pins bug 1: vault.NewUID mints a fresh
// ULID on every call, and resolveIdentity (the code this replaced) called it unconditionally
// whenever frontmatter lacked a uid — so a uid-less page got a different uid every reindex.
// The first run's row stayed in the documents table under the old uid, and the second run's
// insert of a brand new uid for the same (scope, slug) collided with documents_scope_slug_key
// — exactly the "duplicate key value violates unique constraint" failure hit reindexing a real
// vault. Reusing the uid already recorded for (scope, slug) — via resolveUID reading the row
// GetPage already fetched — is the fix; this pins both that the uid stays stable and that
// reindexing twice never errors.
func TestUIDLessPageReusesItsRecordedUIDAcrossReindexes(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	raw := "---\nslug: x\ntype: gotcha\nscope: work\ntitle: T\n---\nbody\n"

	first := index(t, q, raw)
	require.NotEmpty(t, first.UID)
	require.True(t, first.NeedsWriteback, "the frontmatter still has no uid on disk")

	second := index(t, q, raw)
	require.Equal(t, first.UID, second.UID,
		"a uid-less page must reuse the uid already recorded for its (scope, slug), not mint a new one")
	// The second pass is a true no-op (identical content, git version and registry), and a
	// no-op Result never reports NeedsWriteback/AssignedUID regardless of uid — that is this
	// pipeline's pre-existing, unrelated contract (see the early "if isNoop" return in
	// IndexPage), not something bug 1's fix changes.
	require.False(t, second.Changed, "unchanged content, git version and registry must still be a no-op")

	rows, err := q.ListPages(context.Background(), store.PageFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 1, "the second reindex must update the existing row, not insert a colliding one")
}

func TestOversizePageIsIndexedWholeWithAFinding(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	big := strings.Replace(gotchaPage, "Body text mentioning [[other-page]].",
		strings.Repeat("word ", 4000), 1)
	require.True(t, hasFinding(index(t, q, big), "oversize"))

	page, err := q.GetPage(context.Background(), "work", "ergw")
	require.NoError(t, err)
	require.Contains(t, page.BodyMD, "word word", "the body must never be truncated")
}

func TestUnresolvedLinkProducesADanglingFinding(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	require.True(t, hasFinding(index(t, q, gotchaPage), "dangling_ref"))
}

func TestResolvedLinkProducesASuggestFinding(t *testing.T) {
	// Task 15 computes link recovery as dangling over total, so a resolved
	// reference must be counted too.
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	ic := pipelineContext(t)
	ic.Slugs[vault.ScopedSlug{Scope: "work", Slug: "other-page"}] = "u-other"

	result, err := indexer.IndexPage(context.Background(), q,
		"work/gotchas/ergw.md", gotchaPage, "abc", ic)
	require.NoError(t, err)
	require.True(t, hasFinding(result, "resolved_ref"))
	require.False(t, hasFinding(result, "dangling_ref"))
}

func TestSupersededStatusMarksThePageHistorical(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	index(t, q, strings.Replace(gotchaPage, "type: gotcha", "type: gotcha\nstatus: superseded", 1))

	live, err := q.ListPages(context.Background(), store.PageFilter{})
	require.NoError(t, err)
	require.Empty(t, live)

	all, err := q.ListPages(context.Background(), store.PageFilter{IncludeHistorical: true})
	require.NoError(t, err)
	require.Len(t, all, 1)
}

// TestNeedsSplitProducesAMultiCustomerFinding pins the needs_split -> multi_customer
// pattern, mirroring needs_classification -> missing_classification exactly: a page the
// importer flagged as naming more than one customer must surface a warn-severity,
// split-action finding a reviewer can act on.
func TestNeedsSplitProducesAMultiCustomerFinding(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	flagged := strings.Replace(gotchaPage, "type: gotcha\n", "type: gotcha\nneeds_split: true\n", 1)
	result := index(t, q, flagged)
	require.True(t, hasFinding(result, "multi_customer"))
}

// TestLongTitleProducesALongHeadlineFinding pins the title word-count check added alongside
// the importer's title resolution ladder: balise-docs/01-design-spec-v0.4.md section 3 caps
// a headline at 15 words, and a title that exceeds it must be flagged rather than silently
// accepted — it is never truncated, since truncating a claim mid-sentence destroys its
// meaning.
func TestLongTitleProducesALongHeadlineFinding(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	longTitle := strings.TrimSpace(strings.Repeat("word ", 16))
	long := strings.Replace(gotchaPage,
		"title: AzAPI ER gateway update DELETES its connections", "title: "+longTitle, 1)
	require.True(t, hasFinding(index(t, q, long), "long_headline"))
}

func TestShortTitleDoesNotProduceALongHeadlineFinding(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	require.False(t, hasFinding(index(t, q, gotchaPage), "long_headline"))
}

func hasStoreFinding(findings []store.Finding, rule string) bool {
	for _, finding := range findings {
		if finding.Rule == rule {
			return true
		}
	}
	return false
}

// otherGotchaPage is a second, independent page — different uid, slug and path — used to
// prove a reindex of one page never disturbs another page's findings.
const otherGotchaPage = `---
uid: 01J8Z3K9V6Q2M4N7P8R9S0T1U3
slug: other
type: gotcha
scope: work
title: A second unrelated page with its own long_headline problem word word word word word word word word word word word
tags: [vendor/azure/expressroute]
---
Body text for the other page.
`

// TestReindexResolvesALongHeadlineFindingOnceTheTitleIsShortened reproduces, at the indexer
// integration level, the bug the user found live: shorten globex-onboarding.md's over-15-word
// title, commit, run `balise reindex` — the long_headline finding stayed active forever. Two
// IndexPage calls with different gitVersion values stand in for the two git commits (the
// version a real reindex passes is the file's git blob SHA, which changes the moment the
// title is edited, even though the body text below the frontmatter does not) — using the same
// gitVersion for both calls, as this file's `index` helper does, would make the second call a
// noop and never re-run finding computation at all, which would not exercise the fix.
func TestReindexResolvesALongHeadlineFindingOnceTheTitleIsShortened(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	ctx := context.Background()
	longTitle := strings.TrimSpace(strings.Repeat("word ", 16))
	long := strings.Replace(gotchaPage,
		"title: AzAPI ER gateway update DELETES its connections", "title: "+longTitle, 1)

	first, err := indexer.IndexPage(ctx, q, "work/gotchas/ergw.md", long, "v1", pipelineContext(t))
	require.NoError(t, err)
	require.True(t, hasFinding(first, "long_headline"))
	findings, err := q.GetFindings(ctx, first.UID)
	require.NoError(t, err)
	require.True(t, hasStoreFinding(findings, "long_headline"))

	// The title is shortened and the page committed again — a new git blob version, exactly
	// like the reproduction this fix is for.
	second, err := indexer.IndexPage(ctx, q, "work/gotchas/ergw.md", gotchaPage, "v2", pipelineContext(t))
	require.NoError(t, err)
	require.True(t, second.Changed, "a real content change must never be treated as a noop")
	require.False(t, hasFinding(second, "long_headline"))

	findings, err = q.GetFindings(ctx, second.UID)
	require.NoError(t, err)
	require.False(t, hasStoreFinding(findings, "long_headline"),
		"once the title is short again the finding must clear, not stay active forever")
}

// gotchaRegistryContext builds a pipeline Context whose sole "gotcha" type has the given
// max_tokens, everything else identical to pipelineContext's. It lets a test hold the page's
// own content and git version fixed while varying only what registry.Registry.Fingerprint
// covers, to isolate that as the thing driving re-evaluation.
func gotchaRegistryContext(t *testing.T, maxTokens int) indexer.Context {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gotcha.yaml"),
		[]byte(fmt.Sprintf("name: gotcha\ntraits: [indexed, cited]\nmax_tokens: %d\n", maxTokens)), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vendor.yaml"),
		[]byte("name: vendor\nvalues:\n  azure:\n    children:\n      expressroute: {}\n"), 0o644))

	reg, err := registry.Load(dir)
	require.NoError(t, err)
	facets, err := registry.LoadFacets(dir)
	require.NoError(t, err)
	return indexer.Context{Registry: reg, Facets: facets,
		Slugs: map[vault.ScopedSlug]string{}, Aliases: vault.AliasRegistry{}}
}

// TestReindexReevaluatesFindingsWhenTheRegistryChangesEvenIfThePageDidNot pins bug 2: the
// indexer's skip-unchanged optimisation used to compare only body_hash and git_version, never
// the registry, even though validationFindings depends on reg.Limits/reg.Traits too — so
// editing a type's max_tokens in defaults/types/*.yaml had no effect on an already-indexed,
// otherwise-unchanged page; its stale findings stuck forever. gitVersion is deliberately held
// at "abc" for every call below — unlike
// TestReindexResolvesALongHeadlineFindingOnceTheTitleIsShortened's two different versions —
// specifically to prove the re-evaluation is driven by the registry fingerprint changing, not
// by content or git version, and that an unchanged registry is still a true no-op.
func TestReindexReevaluatesFindingsWhenTheRegistryChangesEvenIfThePageDidNot(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	ctx := context.Background()

	first, err := indexer.IndexPage(ctx, q, "work/gotchas/ergw.md", gotchaPage, "abc",
		gotchaRegistryContext(t, 1200))
	require.NoError(t, err)
	require.True(t, first.Changed)
	require.False(t, hasFinding(first, "oversize"))

	// Same content, same git version, same registry limits: an unchanged registry must still
	// be a true no-op, per the fix's own requirement not to force re-evaluation on every run.
	same, err := indexer.IndexPage(ctx, q, "work/gotchas/ergw.md", gotchaPage, "abc",
		gotchaRegistryContext(t, 1200))
	require.NoError(t, err)
	require.False(t, same.Changed, "unchanged vault and unchanged registry must still be a no-op")

	// The type's max_tokens is lowered below the page's own token count: the registry moved,
	// the page did not.
	tightened, err := indexer.IndexPage(ctx, q, "work/gotchas/ergw.md", gotchaPage, "abc",
		gotchaRegistryContext(t, 1))
	require.NoError(t, err)
	require.True(t, tightened.Changed,
		"a registry change must force re-evaluation even though content and git version did not move")
	require.True(t, hasFinding(tightened, "oversize"))

	findings, err := q.GetFindings(ctx, tightened.UID)
	require.NoError(t, err)
	require.True(t, hasStoreFinding(findings, "oversize"))

	// Reverting the registry must revert the finding, not leave it stuck from the tightened pass.
	reverted, err := indexer.IndexPage(ctx, q, "work/gotchas/ergw.md", gotchaPage, "abc",
		gotchaRegistryContext(t, 1200))
	require.NoError(t, err)
	require.True(t, reverted.Changed)
	require.False(t, hasFinding(reverted, "oversize"))

	findings, err = q.GetFindings(ctx, reverted.UID)
	require.NoError(t, err)
	require.False(t, hasStoreFinding(findings, "oversize"),
		"reverting the registry must clear the finding, not leave it stuck")
}

// TestReindexingOnePageNeverResolvesAnothersFindings is the partial-run trap named directly
// in the fix's requirements: a naive "resolve everything not re-raised this run" would clear
// the whole vault's findings on a run that only touched one page. Reindexing ergw.md (fixed)
// must never affect the unrelated, still-broken "other" page's own long_headline finding.
func TestReindexingOnePageNeverResolvesAnothersFindings(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	ctx := context.Background()
	longTitle := strings.TrimSpace(strings.Repeat("word ", 16))
	long := strings.Replace(gotchaPage,
		"title: AzAPI ER gateway update DELETES its connections", "title: "+longTitle, 1)

	_, err := indexer.IndexPage(ctx, q, "work/gotchas/ergw.md", long, "v1", pipelineContext(t))
	require.NoError(t, err)
	other, err := indexer.IndexPage(ctx, q, "work/gotchas/other.md", otherGotchaPage, "v1", pipelineContext(t))
	require.NoError(t, err)
	require.True(t, hasFinding(other, "long_headline"))

	// Only ergw.md gets reindexed this run, title fixed.
	fixed, err := indexer.IndexPage(ctx, q, "work/gotchas/ergw.md", gotchaPage, "v2", pipelineContext(t))
	require.NoError(t, err)
	require.False(t, hasFinding(fixed, "long_headline"))

	otherFindings, err := q.GetFindings(ctx, other.UID)
	require.NoError(t, err)
	require.True(t, hasStoreFinding(otherFindings, "long_headline"),
		"a page never touched by this run must keep its own finding, not have it swept away")
}

// TestReindexResolvesAnUnparseableFindingOnceThePageParses covers the one true global finding
// in the codebase (doc_uid is null): a page that failed to parse, then is fixed and reindexed
// again at the same path, must have its global "unparseable" finding resolved.
func TestReindexResolvesAnUnparseableFindingOnceThePageParses(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	ctx := context.Background()

	result, err := indexer.IndexPage(ctx, q,
		"work/bad.md", "---\n\tbad: [unclosed\n---\nbody\n", "v1", pipelineContext(t))
	require.NoError(t, err)
	require.False(t, result.Indexed)
	globals, err := q.GlobalFindings(ctx)
	require.NoError(t, err)
	require.Len(t, globals, 1)

	// The frontmatter is fixed and the page reindexed again at the same path.
	result, err = indexer.IndexPage(ctx, q, "work/bad.md", gotchaPage, "v2", pipelineContext(t))
	require.NoError(t, err)
	require.True(t, result.Indexed)

	globals, err = q.GlobalFindings(ctx)
	require.NoError(t, err)
	require.Empty(t, globals, "a page that now parses must clear its unparseable finding")
}

func TestOutOfVocabularyTagIsReported(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	bad := strings.Replace(gotchaPage, "vendor/azure/expressroute", "Vendor/Azure!", 1)
	require.True(t, hasFinding(index(t, q, bad), "out_of_vocabulary"))
}

func TestInvalidClaimDateIsReportedNotDropped(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	bad := strings.Replace(gotchaPage, "as_of: 2026-06-10", "as_of: June 2026", 1)
	result := index(t, q, bad)
	require.True(t, hasFinding(result, "invalid_date"))

	page, err := q.GetPage(context.Background(), "work", "ergw")
	require.NoError(t, err)
	require.Nil(t, page.Claims[1].AsOf, "an unparseable date must be dropped, not guessed at")
}

func TestDuplicateReferenceToSameTargetCountsOnceForLinkRecovery(t *testing.T) {
	// gotchaPage's body already links [[other-page]]; adding a frontmatter ref field to
	// the same target produces two edges (different Kind) for one reference. Task 15
	// counts resolved_ref/dangling_ref as references recovered, not edges indexed.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gotcha.yaml"),
		[]byte("name: gotcha\ntraits: [indexed, cited]\nmax_tokens: 1200\n"+
			"fields:\n  related:\n    type: ref\n    role: related\n"), 0o644))
	reg, err := registry.Load(dir)
	require.NoError(t, err)
	facets, err := registry.LoadFacets(dir)
	require.NoError(t, err)
	ic := indexer.Context{Registry: reg, Facets: facets,
		Slugs: map[vault.ScopedSlug]string{}, Aliases: vault.AliasRegistry{}}
	ic.Slugs[vault.ScopedSlug{Scope: "work", Slug: "other-page"}] = "u-other"

	raw := strings.Replace(gotchaPage,
		"title: AzAPI ER gateway update DELETES its connections",
		"title: AzAPI ER gateway update DELETES its connections\nrelated: other-page", 1)

	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	result, err := indexer.IndexPage(context.Background(), q, "work/gotchas/ergw.md", raw, "abc", ic)
	require.NoError(t, err)

	count := 0
	for _, finding := range result.Findings {
		if finding.Rule == "resolved_ref" {
			count++
		}
	}
	require.Equal(t, 1, count, "the same target named twice must count once for link recovery")
}
