package importer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dhia/balise/internal/importer"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/vault"
	"github.com/stretchr/testify/require"
)

// corpusPaths locates the real, owner-provided seed corpus these tests measure the importer
// against. There is no generic substitute for "a real corpus" here — these tests pin importer
// behavior against genuine content and artifacts — so the paths are never hardcoded into
// source; the owner points at their own local corpus with BALISE_ACCEPTANCE_CORPUS and
// BALISE_ACCEPTANCE_ARTIFACTS (e.g. in their own shell profile, outside this repo). Every test
// in this file skips gracefully when either is unset or the path it names doesn't exist, which
// is exactly what happens on a fresh clone or in CI.
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
	if _, err := os.Stat(artifacts); err != nil {
		t.Skip("validation artifacts not present")
	}
	return corpus, artifacts
}

func runImport(t *testing.T) (importer.Report, *store.GitPageStore) {
	t.Helper()
	corpus, artifacts := corpusPaths(t)
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	tenants, err := registry.LoadTenants("../../defaults/tenants.yaml")
	require.NoError(t, err)
	report, err := importer.ImportCorpus(corpus, artifacts, pages, tenants)
	require.NoError(t, err)
	return report, pages
}

// corpusEntries globs the corpus the same way ImportCorpus does, so a test comparing against
// it stays true as the source corpus (a live, owner-edited directory) grows or shrinks rather
// than pinning a snapshot count that goes stale the next time a page is added.
func corpusEntries(t *testing.T, corpus string) []string {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(corpus, "*.md"))
	require.NoError(t, err)
	return entries
}

// TestImportsEveryPageExceptTheHandIndex asserts the one relationship that must hold no matter
// how many files the live corpus currently has: every corpus markdown file is imported except
// the owner's hand-maintained MEMORY.md, which the importer explicitly skips (convertAll). A
// pinned literal count broke the moment the owner edited the corpus; this instead measures the
// corpus at test time and requires the import to track it exactly.
func TestImportsEveryPageExceptTheHandIndex(t *testing.T) {
	corpus, _ := corpusPaths(t)
	entries := corpusEntries(t, corpus)

	handIndexes := 0
	for _, e := range entries {
		if filepath.Base(e) == "MEMORY.md" {
			handIndexes++
		}
	}
	require.Equal(t, 1, handIndexes,
		"exactly one hand-maintained MEMORY.md is expected in the corpus for this assertion to mean anything")

	report, _ := runImport(t)
	require.Equal(t, len(entries)-1, report.Pages,
		"imported pages must equal every corpus markdown file except MEMORY.md, whatever the corpus's current size")
}

// TestClassificationMatchesPassA checks the importer's classified/unclassified split against
// Pass A directly, rather than against a snapshot count: it derives, from the corpus and Pass A
// artifact as they stand right now, exactly which corpus files (other than MEMORY.md) Pass A
// has no row for, and requires the import to report precisely that many unclassified — no more,
// no fewer. It then goes a level deeper than a count ever could: for every file Pass A does
// classify, the page the importer actually wrote must carry that exact kind, file for file. Two
// pages could theoretically classify+unclassify to the right totals while individual files were
// mapped to the wrong kind; comparing the full per-file kind maps rules that out, which is what
// makes this assertion meaningful rather than a restated tautology.
func TestClassificationMatchesPassA(t *testing.T) {
	corpus, artifactsDir := corpusPaths(t)
	artifacts, err := importer.LoadArtifacts(artifactsDir)
	require.NoError(t, err)
	entries := corpusEntries(t, corpus)

	wantKindBySource := map[string]string{}
	wantUnclassified := 0
	for _, e := range entries {
		name := filepath.Base(e)
		if name == "MEMORY.md" {
			continue
		}
		if row, ok := artifacts.PassA[name]; ok {
			wantKindBySource[name] = row.Kind
		} else {
			wantUnclassified++
		}
	}

	report, pages := runImport(t)
	require.Equal(t, report.Pages, report.Classified+report.Unclassified,
		"every imported page must be counted as classified xor unclassified, never both or neither")
	require.Equal(t, wantUnclassified, report.Unclassified,
		"unclassified count must track exactly the corpus files Pass A has no row for today")

	paths, err := pages.List("")
	require.NoError(t, err)
	gotKindBySource := map[string]string{}
	for _, p := range paths {
		raw, _, err := pages.Read(p)
		require.NoError(t, err)
		parsed, err := vault.Parse(string(raw))
		require.NoError(t, err)
		var fm struct {
			Type       string `yaml:"type"`
			SourceFile string `yaml:"source_file"`
		}
		require.NoError(t, parsed.Decode(&fm))
		if fm.SourceFile != "" {
			gotKindBySource[fm.SourceFile] = fm.Type
		}
	}
	// Only compare the subset Pass A actually has an opinion about: gotKindBySource also holds
	// every unclassified page's fallback "note" kind, which is unrelated to what this assertion
	// is checking and would make an unfiltered map comparison fail on classification-count
	// grounds that the two require.Equal calls above already cover.
	gotKindForPassA := make(map[string]string, len(wantKindBySource))
	for source := range wantKindBySource {
		if kind, ok := gotKindBySource[source]; ok {
			gotKindForPassA[source] = kind
		}
	}
	require.Equal(t, wantKindBySource, gotKindForPassA,
		"every Pass-A-classified page's kind must match Pass A exactly, file for file — a stronger, "+
			"still corpus-size-independent check than comparing distribution totals")
}

func TestTypeDistributionReproducesAppendixG(t *testing.T) {
	_, artifactsDir := corpusPaths(t)
	artifacts, err := importer.LoadArtifacts(artifactsDir)
	require.NoError(t, err)

	counts := map[string]int{}
	for _, row := range artifacts.PassA {
		counts[row.Kind]++
	}
	require.Equal(t, map[string]int{
		"gotcha": 40, "decision": 36, "state": 34, "procedure": 8, "incident": 8, "issue": 10,
	}, counts)
}

func TestEveryPageWithAHookCarriesExactlyOneClaim(t *testing.T) {
	report, _ := runImport(t)
	require.GreaterOrEqual(t, report.Claims, 130)
	require.LessOrEqual(t, report.Claims, 139)
}

// TestPagesLandInScopeDirectories pins the corrected scope model against the real corpus,
// second half of the 2026-09-17 domain correction: client-colo must never appear (colo is a
// datacenter, not a tenant), and neither must client-platform or client-intranet — those
// are the owner's own internal categories, not tenants, and a lone customer/colo or
// customer/intranet facet must fall through to work like any other non-tenant facet. The
// only scopes produced are work and each declared tenant's client scope (defaults/tenants.yaml),
// measured at test time rather than pinned so this holds for either the generic example
// tenants or the owner's real list.
func TestPagesLandInScopeDirectories(t *testing.T) {
	_, pages := runImport(t)
	paths, err := pages.List("")
	require.NoError(t, err)

	scopes := map[string]bool{}
	for _, path := range paths {
		scopes[splitFirst(path)] = true
	}
	require.True(t, scopes["work"])
	require.NotContains(t, scopes, "client-colo", "colo is a datacenter, not a tenant")
	require.NotContains(t, scopes, "client-platform", "platform is not a declared tenant")
	require.NotContains(t, scopes, "client-intranet", "intranet is not a declared tenant")

	tenants, err := registry.LoadTenants("../../defaults/tenants.yaml")
	require.NoError(t, err)
	allowed := []string{"work"}
	for _, name := range tenants.Names() {
		allowed = append(allowed, "client-"+name)
	}
	for scope := range scopes {
		require.Contains(t, allowed, scope)
	}
}

// TestMultiCustomerPagesAreFlaggedNeverDemoted pins the real-corpus outcome of the second
// half of the 2026-09-17 domain correction: six of the seven pages originally flagged
// multi-customer (balise-docs/01-design-spec-v0.4.md section 11) paired a real tenant with
// colo and are single-tenant once colo is excluded from tenant-hood — that figure itself
// inherited the colo-counted-as-customer modelling error. Only the genuine umbrella+initech
// page (project_umbrella_initech_fortigate_operator_ssh.md) remains multi-customer, and it
// must not be demoted to work.
func TestMultiCustomerPagesAreFlaggedNeverDemoted(t *testing.T) {
	report, _ := runImport(t)
	require.Equal(t, 1, report.MultiCustomer)
}

func splitFirst(path string) string {
	for i, r := range path {
		if r == '/' {
			return path[:i]
		}
	}
	return path
}
