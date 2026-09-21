package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dhia/balise/internal/cli"
	"github.com/dhia/balise/internal/importer"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/stretchr/testify/require"
)

// corpusPaths locates the real, owner-provided seed corpus this acceptance suite runs
// against. There is no generic substitute for "a real corpus" — these criteria measure
// import behavior against genuine content — so the paths are never hardcoded into source;
// the owner points at their own local corpus with BALISE_ACCEPTANCE_CORPUS and
// BALISE_ACCEPTANCE_ARTIFACTS (e.g. in their own shell profile, outside this repo). Every
// test in this file skips gracefully when either is unset or the path it names doesn't
// exist, which is exactly what happens on a fresh clone or in CI.
func corpusPaths(t *testing.T) (string, string) {
	t.Helper()
	corpus := os.Getenv("BALISE_ACCEPTANCE_CORPUS")
	artifacts := os.Getenv("BALISE_ACCEPTANCE_ARTIFACTS")
	if corpus == "" || artifacts == "" {
		t.Skip("BALISE_ACCEPTANCE_CORPUS / BALISE_ACCEPTANCE_ARTIFACTS not set")
	}
	if _, err := os.Stat(corpus); err != nil {
		t.Skip("seed corpus not present")
	}
	return corpus, artifacts
}

// acceptanceScopes is every scope the real corpus can land pages in after the 2026-09-17
// domain correction: work, plus a client-<name> scope for each tenant declared in
// defaults/tenants.yaml. It is derived from that file rather than hardcoded so the same
// test works against either the generic example tenants or the owner's real list. A
// datacenter code or one of the owner's own internal categories (e.g. platform, intranet)
// is deliberately excluded from tenants.yaml — those aren't tenants — so a page naming
// only a datacenter and/or an internal category (e.g. datacenter-egress-zones) lands in
// work like any other page with no declared tenant.
func acceptanceScopes(t *testing.T, tenants *registry.Tenants) store.Scopes {
	t.Helper()
	scopes := store.Scopes{"work"}
	for _, name := range tenants.Names() {
		scopes = append(scopes, "client-"+name)
	}
	return scopes
}

func importedVault(t *testing.T) (*store.GitPageStore, *store.Queries) {
	t.Helper()
	corpus, artifacts := corpusPaths(t)
	pages, err := store.InitGit(filepath.Join(t.TempDir(), "vault"))
	require.NoError(t, err)
	tenants, err := registry.LoadTenants("../../defaults/tenants.yaml")
	require.NoError(t, err)
	_, err = importer.ImportCorpus(corpus, artifacts, pages, tenants)
	require.NoError(t, err)
	q := store.NewQueries(testutil.NewDB(t), acceptanceScopes(t, tenants))
	return pages, q
}

// wantImportedPageCount measures the live corpus at test time and returns the number of pages
// an import must produce: every *.md file except the owner's hand-maintained MEMORY.md, which
// importer.ImportCorpus explicitly skips. A hardcoded literal here goes stale the moment the
// owner edits the corpus (internal/importer/corpus_test.go hit exactly this); deriving it from
// the corpus each run keeps the assertion true as the corpus grows or shrinks.
func wantImportedPageCount(t *testing.T, corpus string) int {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(corpus, "*.md"))
	require.NoError(t, err)
	return len(entries) - 1
}

func TestCriterion1ImportProducesACommittedVaultWithUIDs(t *testing.T) {
	corpus, _ := corpusPaths(t)
	pages, _ := importedVault(t)
	paths, err := pages.List("")
	require.NoError(t, err)
	require.Len(t, paths, wantImportedPageCount(t, corpus))

	data, _, err := pages.Read(paths[0])
	require.NoError(t, err)
	require.Contains(t, string(data), "uid:")
}

func TestCriterion2ReindexCompletesUnderTwoMinutes(t *testing.T) {
	pages, q := importedVault(t)
	started := time.Now()
	_, err := cli.Reindex(context.Background(), q, pages, "../../defaults")
	require.NoError(t, err)
	require.Less(t, time.Since(started), 2*time.Minute)
}

func TestCriterion3LinkRecoveryIsAtLeast95Percent(t *testing.T) {
	pages, q := importedVault(t)
	report, err := cli.Reindex(context.Background(), q, pages, "../../defaults")
	require.NoError(t, err)
	// The criterion excludes cross-scope dangling refs: a target that exists in another
	// scope is blocked by the scope-isolation security boundary by design, not a
	// resolution failure. All four raw counts are printed on failure — asserting only the
	// passing metric (referential integrity) is how a criterion becomes a lie.
	require.GreaterOrEqual(t, report.WithinScopeRecovery(), 0.95,
		"within-scope recovery %.4f: total=%d resolved=%d cross-scope=%d missing=%d (referential integrity %.4f)",
		report.WithinScopeRecovery(), report.LinksTotal, report.LinksResolved,
		report.LinksCrossScope, report.LinksMissing, report.ReferentialIntegrity())
	require.Zero(t, report.AliasCollisions)
}

func TestCriterion4DropAndReindexReproducesTheDatabase(t *testing.T) {
	pages, q := importedVault(t)
	ctx := context.Background()
	_, err := cli.Reindex(ctx, q, pages, "../../defaults")
	require.NoError(t, err)

	before, err := q.ListPages(ctx, store.PageFilter{IncludeHistorical: true})
	require.NoError(t, err)

	tenants, err := registry.LoadTenants("../../defaults/tenants.yaml")
	require.NoError(t, err)
	fresh := store.NewQueries(testutil.NewDB(t), acceptanceScopes(t, tenants))
	_, err = cli.Reindex(ctx, fresh, pages, "../../defaults")
	require.NoError(t, err)

	after, err := fresh.ListPages(ctx, store.PageFilter{IncludeHistorical: true})
	require.NoError(t, err)
	require.Len(t, after, len(before))
	for i := range before {
		require.Equal(t, before[i].Slug, after[i].Slug)
		require.Equal(t, before[i].BodyHash, after[i].BodyHash)
		require.Equal(t, before[i].ClaimsCount, after[i].ClaimsCount)
	}
}

func TestCriterion7SearchReturnsClaimLevelHits(t *testing.T) {
	pages, q := importedVault(t)
	ctx := context.Background()
	_, err := cli.Reindex(ctx, q, pages, "../../defaults")
	require.NoError(t, err)

	hits, err := q.SearchClaims(ctx, "gateway", false, 10)
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	require.NotEmpty(t, hits[0].MatchedClaims)
}

func TestReindexIsIdempotent(t *testing.T) {
	pages, q := importedVault(t)
	ctx := context.Background()
	_, err := cli.Reindex(ctx, q, pages, "../../defaults")
	require.NoError(t, err)

	second, err := cli.Reindex(ctx, q, pages, "../../defaults")
	require.NoError(t, err)
	require.Zero(t, second.Changed, "re-indexing unchanged files must be a no-op")
}
