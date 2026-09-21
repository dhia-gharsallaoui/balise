package importer_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dhia/balise/internal/importer"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/vault"
	"github.com/stretchr/testify/require"
)

// corpusWith writes one corpus file per classification, plus the artifacts LoadArtifacts
// needs, and returns both directories. It is deliberately not the real corpus: the correction
// workflow under test has to hold for any corpus, and the real one is only present on the
// owner's machine.
func corpusWith(t *testing.T, rows ...importer.Classification) (corpusDir, artifactsDir string) {
	t.Helper()
	corpusDir, artifactsDir = t.TempDir(), t.TempDir()

	passA := ""
	for _, row := range rows {
		line, err := json.Marshal(row)
		require.NoError(t, err)
		passA += string(line) + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(corpusDir, row.File),
			[]byte("---\nname: "+row.File+"\n---\nbody\n"), 0o644))
	}
	require.NoError(t, os.WriteFile(
		filepath.Join(artifactsDir, "02_pass_a.jsonl"), []byte(passA), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(artifactsDir, "01_alias_table.json"), []byte("{}"), 0o644))
	return corpusDir, artifactsDir
}

// syntheticCorpus is the two-page corpus most of these tests use: one page naming a tenant,
// one naming none.
func syntheticCorpus(t *testing.T, facets []string) (corpusDir, artifactsDir string) {
	t.Helper()
	return corpusWith(t,
		importer.Classification{
			File: "project_tenant_page.md", Kind: "gotcha", Status: "active", Facets: facets},
		importer.Classification{
			File: "project_shared_page.md", Kind: "gotcha", Status: "active",
			Facets: []string{"layer/network"}},
	)
}

// tenantsFile writes a tenants.yaml naming exactly names, so a test can perform the same
// correction a user would: edit the file, import again.
func tenantsFile(t *testing.T, names ...string) *registry.Tenants {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tenants.yaml")
	require.NoError(t, os.WriteFile(path,
		[]byte("tenants: ["+strings.Join(names, ", ")+"]\n"), 0o644))
	tenants, err := registry.LoadTenants(path)
	require.NoError(t, err)
	return tenants
}

func uidAt(t *testing.T, pages store.PageStore, path string) string {
	t.Helper()
	raw, _, err := pages.Read(path)
	require.NoError(t, err)
	page, err := vault.Parse(string(raw))
	require.NoError(t, err)
	var fm struct {
		UID string `yaml:"uid"`
	}
	require.NoError(t, page.Decode(&fm))
	return fm.UID
}

// TestCorrectingATenantMovesThePageInsteadOfDuplicatingIt is the vault half of the correction
// workflow. Editing defaults/tenants.yaml and re-importing is how the product says a customer
// misclassification is fixed. Before this, Convert minted a fresh ULID every run and Commit
// only ever added or overwrote the paths it was handed, so the corrected page was written to
// its new scope under a new identity while the previously-classified copy stayed on disk —
// one customer's content silently duplicated into another tenant's scope.
func TestCorrectingATenantMovesThePageInsteadOfDuplicatingIt(t *testing.T) {
	corpus, artifacts := syntheticCorpus(t, []string{"customer/umbrella", "layer/network"})
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)

	first, err := importer.ImportCorpus(corpus, artifacts, pages,
		tenantsFile(t, "globex", "umbrella"))
	require.NoError(t, err)
	require.Equal(t, 2, first.Pages)
	require.Zero(t, first.Removed)

	before, err := pages.List("")
	require.NoError(t, err)
	require.Equal(t,
		[]string{"client-umbrella/gotchas/tenant-page.md", "work/gotchas/shared-page.md"}, before)
	movedUID := uidAt(t, pages, "client-umbrella/gotchas/tenant-page.md")
	stableUID := uidAt(t, pages, "work/gotchas/shared-page.md")

	// The correction: umbrella is no longer a declared tenant.
	second, err := importer.ImportCorpus(corpus, artifacts, pages, tenantsFile(t, "globex"))
	require.NoError(t, err)
	require.Equal(t, 2, second.Pages)
	require.Equal(t, 1, second.Removed)

	after, err := pages.List("")
	require.NoError(t, err)
	require.Equal(t,
		[]string{"work/gotchas/shared-page.md", "work/gotchas/tenant-page.md"}, after,
		"the page must move, not exist in both scopes")
	require.Equal(t, movedUID, uidAt(t, pages, "work/gotchas/tenant-page.md"),
		"identity follows the source file, so git history stays continuous across the move")
	require.Equal(t, stableUID, uidAt(t, pages, "work/gotchas/shared-page.md"))
}

// TestReimportingAnUnchangedCorpusChangesNothing: uid preservation is what makes this true.
// While Convert minted a fresh ULID per run, every page's bytes differed on every import, so
// "nothing changed" was never observable.
func TestReimportingAnUnchangedCorpusChangesNothing(t *testing.T) {
	corpus, artifacts := syntheticCorpus(t, []string{"customer/globex"})
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	tenants := tenantsFile(t, "globex")

	first, err := importer.ImportCorpus(corpus, artifacts, pages, tenants)
	require.NoError(t, err)
	second, err := importer.ImportCorpus(corpus, artifacts, pages, tenants)
	require.NoError(t, err)

	require.Zero(t, second.Removed)
	require.Equal(t, first.Commit, second.Commit, "an unchanged re-import must add no commit")

	paths, err := pages.List("")
	require.NoError(t, err)
	require.Len(t, paths, 2)
	require.Equal(t, uidAt(t, pages, "client-globex/gotchas/tenant-page.md"),
		uidAt(t, pages, "client-globex/gotchas/tenant-page.md"))
}

// TestImportLeavesHandWrittenPagesAlone: import supersedes its own output, it does not sweep
// the vault. A page the owner wrote by hand with a slug no corpus file produces must survive.
func TestImportLeavesHandWrittenPagesAlone(t *testing.T) {
	corpus, artifacts := syntheticCorpus(t, []string{"customer/globex"})
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	_, err = pages.Commit([]store.Change{{
		Path: "work/notes/hand-written.md",
		Data: []byte("---\nuid: 01J8Z3K9V6Q2M4N7P8R9S0T1U2\nslug: hand-written\n---\nmine\n"),
	}}, "Dhia <d@x>", "hand-written page")
	require.NoError(t, err)

	_, err = importer.ImportCorpus(corpus, artifacts, pages, tenantsFile(t, "globex"))
	require.NoError(t, err)

	paths, err := pages.List("")
	require.NoError(t, err)
	require.Contains(t, paths, "work/notes/hand-written.md")
}

// TestImportRejectsASlugCollisionAcrossScopes covers the reachable path to two customers'
// pages sharing one slug. project_expressroute.md and feedback_expressroute.md both slug to
// "expressroute"; when their facets route them to different tenants the old guard, which keyed
// on the output path, never fired — the paths differed — and both rows were created.
func TestImportRejectsASlugCollisionAcrossScopes(t *testing.T) {
	corpus, artifacts := corpusWith(t,
		importer.Classification{File: "project_expressroute.md", Kind: "gotcha",
			Status: "active", Facets: []string{"customer/globex"}},
		importer.Classification{File: "feedback_expressroute.md", Kind: "gotcha",
			Status: "active", Facets: []string{"layer/network"}},
	)
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	_, err = importer.ImportCorpus(corpus, artifacts, pages, tenantsFile(t, "globex"))
	require.Error(t, err, "two source files claiming one slug must fail whatever scopes they land in")
	require.Contains(t, err.Error(), "project_expressroute.md")
	require.Contains(t, err.Error(), "feedback_expressroute.md")
	require.Contains(t, err.Error(), "expressroute")
}

// handWritten renders a vault page that no corpus file produced — no source_file field, the
// shape of both a hand-authored page and one written by an importer predating that field.
func handWritten(scope, folder, slug, uid, body string) store.Change {
	return store.Change{
		Path: scope + "/" + folder + "/" + slug + ".md",
		Data: []byte("---\nuid: " + uid + "\nslug: " + slug + "\ntype: note\nscope: " + scope +
			"\ntitle: " + slug + "\n---\n" + body + "\n"),
	}
}

// TestImportNeverAdoptsOrDeletesAnotherScopesPage is the test that would have caught the
// second-wave regression, in which the vault index was keyed on the slug alone.
//
// The vault holds Globex's own expressroute page. The corpus holds project_expressroute.md, which
// slugs to "expressroute" and names no tenant, so it belongs in work. Matching on the name
// alone, the import adopted Globex's uid for the work page and staged Globex's file for deletion —
// which the `on conflict (uid)` upsert then completed by relocating Globex's document row into
// work. One tenant's page deleted and its identity transplanted onto another's, reported as
// "1 superseded paths removed". Both pages must survive, in their own scopes, with their own
// uids, and nothing may be staged for removal.
func TestImportNeverAdoptsOrDeletesAnotherScopesPage(t *testing.T) {
	corpus, artifacts := corpusWith(t, importer.Classification{
		File: "project_expressroute.md", Kind: "gotcha", Status: "active",
		Facets: []string{"layer/network"},
	})
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	const globexUID = "01J8Z3K9V6Q2M4N7P8R9S0T1L1"
	_, err = pages.Commit(
		[]store.Change{handWritten("client-globex", "notes", "expressroute", globexUID, "Globex's confidential body")},
		"Dhia <d@x>", "Globex's own page")
	require.NoError(t, err)

	report, err := importer.ImportCorpus(corpus, artifacts, pages, tenantsFile(t, "globex"))
	require.NoError(t, err)
	require.Zero(t, report.Removed, "no page moved, so nothing may be staged for removal")

	paths, err := pages.List("")
	require.NoError(t, err)
	require.Equal(t,
		[]string{"client-globex/notes/expressroute.md", "work/gotchas/expressroute.md"}, paths,
		"both pages must survive, each in its own scope")

	require.Equal(t, globexUID, uidAt(t, pages, "client-globex/notes/expressroute.md"),
		"Globex's page must keep its own uid")
	require.NotEqual(t, globexUID, uidAt(t, pages, "work/gotchas/expressroute.md"),
		"the work page must get its own identity, never another tenant's")

	data, _, err := pages.Read("client-globex/notes/expressroute.md")
	require.NoError(t, err)
	require.Contains(t, string(data), "Globex's confidential body",
		"Globex's content must be untouched")
}

// TestImportAdoptsAPageInItsOwnScope pins the fallback the scope-qualified match still allows:
// a page predating the source_file field, landing back in the scope it is already in, is the
// same page by the schema's own definition of identity (documents is unique on (scope, slug)),
// so its uid is kept rather than a duplicate identity minted for it.
func TestImportAdoptsAPageInItsOwnScope(t *testing.T) {
	corpus, artifacts := corpusWith(t, importer.Classification{
		File: "project_expressroute.md", Kind: "gotcha", Status: "active",
		Facets: []string{"layer/network"},
	})
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	const legacyUID = "01J8Z3K9V6Q2M4N7P8R9S0T1W1"
	_, err = pages.Commit(
		[]store.Change{handWritten("work", "gotchas", "expressroute", legacyUID, "older body")},
		"Dhia <d@x>", "page written before source_file existed")
	require.NoError(t, err)

	report, err := importer.ImportCorpus(corpus, artifacts, pages, tenantsFile(t, "globex"))
	require.NoError(t, err)
	require.Zero(t, report.Removed)

	paths, err := pages.List("")
	require.NoError(t, err)
	require.Equal(t, []string{"work/gotchas/expressroute.md"}, paths)
	require.Equal(t, legacyUID, uidAt(t, pages, "work/gotchas/expressroute.md"))
}

// TestImportedPagesCarryTheirSourceFilename: source_file is what makes a move recognisable as
// a move rather than a coincidence of naming, so every written page has to carry it.
func TestImportedPagesCarryTheirSourceFilename(t *testing.T) {
	corpus, artifacts := syntheticCorpus(t, []string{"customer/globex"})
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	_, err = importer.ImportCorpus(corpus, artifacts, pages, tenantsFile(t, "globex"))
	require.NoError(t, err)

	data, _, err := pages.Read("client-globex/gotchas/tenant-page.md")
	require.NoError(t, err)
	require.Contains(t, string(data), "source_file: project_tenant_page.md")
}
