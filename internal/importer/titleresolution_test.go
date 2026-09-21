package importer_test

import (
	"testing"

	"github.com/dhia/balise/internal/importer"
	"github.com/stretchr/testify/require"
)

// titleArtifacts returns an Artifacts value with empty tables, ready for a test to populate
// just the Hooks entry it needs. Kept separate from the shared artifacts() fixture in
// claudememory_test.go because that fixture's PassA/Hooks entries are tuned for the scope
// tests and would make these title-ladder tests depend on unrelated fixture data.
func titleArtifacts() *importer.Artifacts {
	return &importer.Artifacts{
		PassA:      map[string]importer.Classification{},
		Hooks:      map[string]string{},
		AliasTable: map[string]string{},
	}
}

// TestTitleRungNameWinsOverHook pins rung 1 of the title resolution ladder: a corpus `name`
// that reads as a title (contains a space, and is not merely the filename dressed up as
// frontmatter) wins even when a hook is available for the same file.
func TestTitleRungNameWinsOverHook(t *testing.T) {
	raw := "---\nname: Operator access review runs quarterly\n---\nbody\n"
	a := titleArtifacts()
	a.Hooks["project_op_review.md"] = "Quarterly operator access review catches stale grants"

	got, err := importer.Convert("project_op_review.md", raw, a, testTenants(t))
	require.NoError(t, err)
	require.Equal(t, "Operator access review runs quarterly", got.Meta.Title)
	require.Equal(t, "name", got.Meta.TitleSource)
}

// TestTitleRungFilenameShapedNameLosesToHook pins rung 2: a `name` that is only the filename
// stem (the exact real-corpus shape — e.g. project_acme_no_terraform_state) is not a
// title, so the hook is used instead and recorded as the source.
func TestTitleRungFilenameShapedNameLosesToHook(t *testing.T) {
	raw := "---\nname: project_acme_no_terraform_state\n---\nbody\n"
	a := titleArtifacts()
	a.Hooks["project_acme_no_terraform_state.md"] =
		"Acme's Terraform state was never committed to the shared backend"

	got, err := importer.Convert("project_acme_no_terraform_state.md", raw, a, testTenants(t))
	require.NoError(t, err)
	require.Equal(t, "Acme's Terraform state was never committed to the shared backend",
		got.Meta.Title)
	require.Equal(t, "hook", got.Meta.TitleSource)
}

// TestTitleRungNoNameAtAllUsesHook covers name being entirely absent, not merely
// filename-shaped — the hook still wins.
func TestTitleRungNoNameAtAllUsesHook(t *testing.T) {
	raw := "---\ntype: project\n---\nbody\n"
	a := titleArtifacts()
	a.Hooks["project_absent_name.md"] = "A hook still becomes the title when name is entirely absent"

	got, err := importer.Convert("project_absent_name.md", raw, a, testTenants(t))
	require.NoError(t, err)
	require.Equal(t, "A hook still becomes the title when name is entirely absent", got.Meta.Title)
	require.Equal(t, "hook", got.Meta.TitleSource)
}

// TestTitleRungNoHookFallsBackToDescriptionFirstSentence pins rung 3: with a filename-shaped
// name and no hook, the title falls back to the first sentence of the description, trimmed.
func TestTitleRungNoHookFallsBackToDescriptionFirstSentence(t *testing.T) {
	raw := "---\nname: feedback_minimal_comments\n" +
		"description: Write at most one comment line. Rationale belongs in the PR.\n---\nbody\n"

	got, err := importer.Convert("feedback_minimal_comments.md", raw, titleArtifacts(), testTenants(t))
	require.NoError(t, err)
	require.Equal(t, "Write at most one comment line.", got.Meta.Title)
	require.Equal(t, "description", got.Meta.TitleSource)
}

// TestFirstSentenceStopsAtSentenceEndNotAtEveryPeriod pins the sentence-splitting rule that
// makes rung 3 safe on real prose: a period inside a token (dhia.user) must not truncate the
// extracted sentence early.
func TestFirstSentenceStopsAtSentenceEndNotAtEveryPeriod(t *testing.T) {
	raw := "---\nname: project_dotted_token\n" +
		"description: Grant dhia.user via forgecp-prod-dhia-users. Never platform-operators.\n" +
		"---\nbody\n"

	got, err := importer.Convert("project_dotted_token.md", raw, titleArtifacts(), testTenants(t))
	require.NoError(t, err)
	require.Equal(t, "Grant dhia.user via forgecp-prod-dhia-users.", got.Meta.Title)
	require.Equal(t, "description", got.Meta.TitleSource)
}

// TestTitleRungFallsBackToSlugWhenNothingElseIsUsable pins rung 4, the last resort: a
// filename-shaped name, no hook and no description at all leaves only the slug — and the
// task's measurement against the real corpus (0 of 138 pages) says this should be rare, not
// that it can't happen.
func TestTitleRungFallsBackToSlugWhenNothingElseIsUsable(t *testing.T) {
	raw := "---\nname: project_no_signal\n---\nbody\n"

	got, err := importer.Convert("project_no_signal.md", raw, titleArtifacts(), testTenants(t))
	require.NoError(t, err)
	require.Equal(t, "no-signal", got.Meta.Title)
	require.Equal(t, "slug", got.Meta.TitleSource)
}

// TestTitleSourceIsRecordedForTheSharedFixture pins title_source against the package's shared
// fixture (claudememory_test.go's `source` and artifacts()), where the name is a real title:
// TestTitleComesFromTheCorpusName already pins Title itself, this pins the provenance field
// alongside it.
func TestTitleSourceIsRecordedForTheSharedFixture(t *testing.T) {
	require.Equal(t, "name", convert(t, "project_aks_pod_cidr.md").Meta.TitleSource)
}
