package compile_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dhia/balise/internal/compile"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/stretchr/testify/require"
)

const defaultTypes = "../../defaults/types"

func writePage(t *testing.T, pages store.PageStore, path, content string) {
	t.Helper()
	_, err := pages.Write(path, []byte(content), "")
	require.NoError(t, err)
}

func gotchaPage(title, body string) string {
	return "---\n" +
		"uid: 01JGOTCHA0000000000000001\n" +
		"slug: sample-gotcha\n" +
		"type: gotcha\n" +
		"scope: work\n" +
		"title: " + title + "\n" +
		"claims:\n" +
		"  - {text: \"AzAPI PUT on an ER gateway deletes all expressRouteConnections\", status: active, id: c1}\n" +
		"---\n" + body + "\n"
}

func newTestRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	reg, err := registry.Load(defaultTypes)
	require.NoError(t, err)
	return reg
}

func validClaimsChangeJSON() string {
	return `{"keep":["c1"],"reword":[],"retire":[],"add":[]}`
}

func TestRunDryRunRendersPromptsWithoutCallingTheModelOrWriting(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writePage(t, pages, "work/gotcha/sample-gotcha.md",
		gotchaPage("A gateway gotcha", "Any PUT on the gateway replaces the resource and drops connections."))

	client := &fakeLLMClient{}
	report, err := compile.Run(context.Background(), pages, newTestRegistry(t), client, compile.Options{DryRun: true})
	require.NoError(t, err)

	require.Len(t, report.Pages, 1)
	require.NotEmpty(t, report.Pages[0].Prompt)
	require.Contains(t, report.Pages[0].Prompt, "gateway")
	require.Empty(t, report.CommitSHAs)
	require.Zero(t, report.TotalInputTokens)
	require.Len(t, client.calls, 0, "dry run must never call the model")

	entries, err := pages.List("review/")
	require.NoError(t, err)
	require.Empty(t, entries, "dry run must never write a proposal")
}

func TestRunWritesOneProposalPerPageAndBatchesIntoOneCommit(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writePage(t, pages, "work/gotcha/sample-gotcha.md",
		gotchaPage("A gateway gotcha", "Any PUT on the gateway replaces the resource and drops connections."))

	client := &fakeLLMClient{responses: []fakeResponse{
		{input: validClaimsChangeJSON(), inputTokens: 120, outputTokens: 30},
	}}
	report, err := compile.Run(context.Background(), pages, newTestRegistry(t), client, compile.Options{})
	require.NoError(t, err)

	require.Len(t, report.Pages, 1)
	require.Len(t, report.CommitSHAs, 1, "one page well under the batch size lands in a single commit")
	require.Equal(t, 120, report.TotalInputTokens)
	require.Equal(t, 30, report.TotalOutputTokens)

	entries, err := pages.List("review/")
	require.NoError(t, err)
	require.Len(t, entries, 1)

	proposalBytes, _, err := pages.Read(entries[0])
	require.NoError(t, err)
	require.Contains(t, string(proposalBytes), "kind: claims")
	require.Contains(t, string(proposalBytes), "target: sample-gotcha")

	stateBytes, _, err := pages.Read(".balise/compile/extract-claims-state.json")
	require.NoError(t, err)
	require.Contains(t, string(stateBytes), "work/gotcha/sample-gotcha.md")
}

func TestRunSkipsAPageWhoseBodyHashHasNotChanged(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writePage(t, pages, "work/gotcha/sample-gotcha.md",
		gotchaPage("A gateway gotcha", "Any PUT on the gateway replaces the resource and drops connections."))

	client := &fakeLLMClient{responses: []fakeResponse{
		{input: validClaimsChangeJSON(), inputTokens: 100, outputTokens: 20},
	}}
	_, err = compile.Run(context.Background(), pages, newTestRegistry(t), client, compile.Options{})
	require.NoError(t, err)
	require.Len(t, client.calls, 1)

	// second run against the identical body must skip, not call the model again
	report, err := compile.Run(context.Background(), pages, newTestRegistry(t), client, compile.Options{})
	require.NoError(t, err)
	require.Len(t, client.calls, 1, "an unchanged page must not trigger another model call")
	require.Len(t, report.Pages, 1)
	require.True(t, report.Pages[0].Skipped)
	require.Empty(t, report.CommitSHAs, "a run with nothing to propose must not create an empty commit")
}

func TestRunReextractsAPageWhoseBodyChanged(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writePage(t, pages, "work/gotcha/sample-gotcha.md",
		gotchaPage("A gateway gotcha", "Any PUT on the gateway replaces the resource and drops connections."))

	client := &fakeLLMClient{responses: []fakeResponse{
		{input: validClaimsChangeJSON(), inputTokens: 100, outputTokens: 20},
		{input: validClaimsChangeJSON(), inputTokens: 110, outputTokens: 22},
	}}
	_, err = compile.Run(context.Background(), pages, newTestRegistry(t), client, compile.Options{})
	require.NoError(t, err)

	writePage(t, pages, "work/gotcha/sample-gotcha.md",
		gotchaPage("A gateway gotcha", "Any PUT on the gateway replaces the resource, drops connections, and requires a manual re-create."))

	report, err := compile.Run(context.Background(), pages, newTestRegistry(t), client, compile.Options{})
	require.NoError(t, err)
	require.Len(t, client.calls, 2)
	require.False(t, report.Pages[0].Skipped)
}

func TestRunAppliesLimitOnlyToAttemptedPages(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writePage(t, pages, "work/gotcha/a.md", strings.Replace(gotchaPage("A", "First gateway body about gateways."), "sample-gotcha", "page-a", -1))
	writePage(t, pages, "work/gotcha/b.md", strings.Replace(gotchaPage("B", "Second gateway body about gateways."), "sample-gotcha", "page-b", -1))

	client := &fakeLLMClient{}
	report, err := compile.Run(context.Background(), pages, newTestRegistry(t), client, compile.Options{DryRun: true, Limit: 1})
	require.NoError(t, err)
	require.Len(t, report.Pages, 1, "limit must cap the number of pages actually rendered")
}

func TestRunIgnoresNonIndexedAndNonMarkdownPaths(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writePage(t, pages, "work/gotcha/sample-gotcha.md",
		gotchaPage("A gateway gotcha", "Any PUT on the gateway replaces the resource and drops connections."))
	writePage(t, pages, "work/raw/meeting-notes.md", "no frontmatter here, just prose")
	writePage(t, pages, "index.md", "---\ntype: gotcha\n---\nshould never be a candidate")
	writePage(t, pages, "review/existing-proposal.md", "---\nkind: claims\n---\n")

	client := &fakeLLMClient{}
	report, err := compile.Run(context.Background(), pages, newTestRegistry(t), client, compile.Options{DryRun: true})
	require.NoError(t, err)
	require.Len(t, report.Pages, 1, "only the one real gotcha page should be a candidate")
	require.Equal(t, "work/gotcha/sample-gotcha.md", report.Pages[0].Path)
}

func TestRunRestrictsToTheRequestedScope(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writePage(t, pages, "work/gotcha/a.md", strings.Replace(
		strings.Replace(gotchaPage("A", "First gateway body about gateways."), "sample-gotcha", "page-a", -1),
		"scope: work", "scope: work", -1))
	writePage(t, pages, "personal/gotcha/b.md", strings.Replace(
		strings.Replace(gotchaPage("B", "Second gateway body about gateways."), "sample-gotcha", "page-b", -1),
		"scope: work", "scope: personal", -1))

	client := &fakeLLMClient{}
	report, err := compile.Run(context.Background(), pages, newTestRegistry(t), client, compile.Options{DryRun: true, Scope: "work"})
	require.NoError(t, err)
	require.Len(t, report.Pages, 1)
	require.Equal(t, "work/gotcha/a.md", report.Pages[0].Path)
}

func TestRunPassesTheRequestedModelThrough(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writePage(t, pages, "work/gotcha/sample-gotcha.md",
		gotchaPage("A gateway gotcha", "Any PUT on the gateway replaces the resource and drops connections."))

	client := &fakeLLMClient{responses: []fakeResponse{
		{input: validClaimsChangeJSON(), inputTokens: 10, outputTokens: 2},
	}}
	_, err = compile.Run(context.Background(), pages, newTestRegistry(t), client, compile.Options{Model: "claude-haiku-test"})
	require.NoError(t, err)
	require.Len(t, client.calls, 1)
	require.Equal(t, "claude-haiku-test", client.calls[0].Model)
}

// Fix 2: one page's hard failure must not abort the run — the other page still
// gets extracted, proposed, and committed, and Run returns no error.
func TestRunRecordsAFailedPageAndContinuesWithoutAborting(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writePage(t, pages, "work/gotcha/a-fails.md", strings.Replace(gotchaPage("A", "First gateway body about gateways."), "sample-gotcha", "page-a", -1))
	writePage(t, pages, "work/gotcha/b-succeeds.md", strings.Replace(gotchaPage("B", "Second gateway body about gateways."), "sample-gotcha", "page-b", -1))

	invalid := `{"keep":[],"reword":[],"retire":[],"add":[]}`
	client := &fakeLLMClient{responses: []fakeResponse{
		{input: invalid, inputTokens: 10, outputTokens: 2},                 // page a, attempt 1: hard invalid
		{input: invalid, inputTokens: 12, outputTokens: 3},                 // page a, attempt 2: still hard invalid
		{input: validClaimsChangeJSON(), inputTokens: 50, outputTokens: 8}, // page b: valid first try
	}}
	report, err := compile.Run(context.Background(), pages, newTestRegistry(t), client, compile.Options{})
	require.NoError(t, err, "a per-page hard failure must not abort the run")

	require.Len(t, report.Pages, 2)
	var failed, succeeded *compile.PageReport
	for i := range report.Pages {
		p := &report.Pages[i]
		switch p.Path {
		case "work/gotcha/a-fails.md":
			failed = p
		case "work/gotcha/b-succeeds.md":
			succeeded = p
		}
	}
	require.NotNil(t, failed)
	require.NotNil(t, succeeded)
	require.True(t, failed.Failed)
	require.Contains(t, failed.FailReason, "failed validation")
	require.False(t, succeeded.Failed)
	require.NotEmpty(t, succeeded.ProposalPath)

	succeededCount, _, failedCount := report.Counts()
	require.Equal(t, 1, succeededCount)
	require.Equal(t, 1, failedCount)
	require.False(t, report.AllAttemptedPagesFailed(), "one success among the attempted pages must not exit non-zero")

	entries, err := pages.List("review/")
	require.NoError(t, err)
	require.Len(t, entries, 1, "only the succeeding page gets a proposal committed")

	require.Equal(t, 72, report.TotalInputTokens, "the failed page's spend (10+12) plus the succeeded page's (50) are both counted")
	require.Equal(t, 13, report.TotalOutputTokens)

	stateBytes, _, err := pages.Read(".balise/compile/extract-claims-state.json")
	require.NoError(t, err)
	require.Contains(t, string(stateBytes), "b-succeeds.md", "only the succeeded page is recorded, so the failed one is retried next run")
	require.NotContains(t, string(stateBytes), "a-fails")
}

// Fix 2: a run where every attempted page fails must still not error out of
// Run (the caller decides the exit code), but must be visible via
// AllAttemptedPagesFailed so the CLI can exit non-zero.
func TestRunReportsAllAttemptedPagesFailedWhenNothingSucceeds(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writePage(t, pages, "work/gotcha/sample-gotcha.md",
		gotchaPage("A gateway gotcha", "Any PUT on the gateway replaces the resource and drops connections."))

	invalid := `{"keep":[],"reword":[],"retire":[],"add":[]}`
	client := &fakeLLMClient{responses: []fakeResponse{
		{input: invalid, inputTokens: 10, outputTokens: 2},
		{input: invalid, inputTokens: 12, outputTokens: 3},
	}}
	report, err := compile.Run(context.Background(), pages, newTestRegistry(t), client, compile.Options{})
	require.NoError(t, err)
	require.True(t, report.AllAttemptedPagesFailed())
	require.Empty(t, report.CommitSHAs, "nothing succeeded, so nothing should be committed")

	entries, err := pages.List("review/")
	require.NoError(t, err)
	require.Empty(t, entries)
}

// Fix 3: proposals are committed in batches rather than one commit at the very
// end — CommitBatchSize+1 successful pages must land in two commits.
func TestRunCommitsInBatchesRatherThanAllAtOnce(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)

	pageCount := compile.CommitBatchSize + 1
	responses := make([]fakeResponse, 0, pageCount)
	for i := 0; i < pageCount; i++ {
		slug := fmt.Sprintf("page-%03d", i)
		content := strings.Replace(gotchaPage(slug, fmt.Sprintf("Gateway body number %d about gateways.", i)), "sample-gotcha", slug, -1)
		writePage(t, pages, fmt.Sprintf("work/gotcha/%s.md", slug), content)
		responses = append(responses, fakeResponse{input: validClaimsChangeJSON(), inputTokens: 10, outputTokens: 2})
	}

	client := &fakeLLMClient{responses: responses}
	report, err := compile.Run(context.Background(), pages, newTestRegistry(t), client, compile.Options{})
	require.NoError(t, err)
	require.Len(t, report.Pages, pageCount)
	require.Len(t, report.CommitSHAs, 2, "one full batch plus a final partial batch")

	entries, err := pages.List("review/")
	require.NoError(t, err)
	require.Len(t, entries, pageCount)
}

// Fix 1, part A: a pending proposal already sitting in review/ for a target that is about to
// get a fresh extraction must be superseded — moved to review/.rejected/ with a reason — never
// left alongside the new one. This is exactly the sequence (interrupted run, then a resumed
// run) that produced 15 duplicated targets in the live vault before this fix.
func TestRunSupersedesAnExistingPendingProposalForTheSameTarget(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writePage(t, pages, "work/gotcha/sample-gotcha.md",
		gotchaPage("A gateway gotcha", "Any PUT on the gateway replaces the resource and drops connections."))

	old := compile.Proposal{
		ID:         "01OLDPROPOSALEXISTS0000001",
		Kind:       "claims",
		Scope:      "work",
		Target:     "sample-gotcha",
		Confidence: 0.6,
		CreatedBy:  "compile:2026-01-01T00:00:00Z",
		Change:     compile.ProposalChange{Claims: compile.ProposalClaims{Keep: []string{"c1"}}},
		Status:     "pending",
	}
	rendered, err := old.Render("stale rationale from an interrupted run")
	require.NoError(t, err)
	writePage(t, pages, "review/"+old.ID+".md", rendered)

	client := &fakeLLMClient{responses: []fakeResponse{
		{input: validClaimsChangeJSON(), inputTokens: 10, outputTokens: 2},
	}}
	report, err := compile.Run(context.Background(), pages, newTestRegistry(t), client, compile.Options{})
	require.NoError(t, err)

	require.Len(t, report.Pages, 1)
	require.Equal(t, old.ID, report.Pages[0].SupersededProposalID)
	require.Equal(t, 1, report.SupersededCount())

	entries, err := pages.List("review/")
	require.NoError(t, err)
	var pending []string
	for _, e := range entries {
		if !strings.HasPrefix(e, "review/.rejected/") {
			pending = append(pending, e)
		}
	}
	require.Len(t, pending, 1, "the stale pending proposal must be superseded, not left alongside the fresh one")
	require.NotEqual(t, "review/"+old.ID+".md", pending[0], "the old path must no longer exist under review/")

	rejectedBytes, _, err := pages.Read("review/.rejected/" + old.ID + ".md")
	require.NoError(t, err)
	require.Contains(t, string(rejectedBytes), "status: rejected")
	require.Contains(t, string(rejectedBytes), "superseded")
	require.Contains(t, string(rejectedBytes), "stale rationale from an interrupted run",
		"the old rationale is preserved verbatim, same as a human-driven reject")
}

func TestRunAbortsWithoutCommittingWhenAPageHardFails(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writePage(t, pages, "work/gotcha/sample-gotcha.md",
		gotchaPage("A gateway gotcha", "Any PUT on the gateway replaces the resource and drops connections."))

	invalid := `{"keep":[],"reword":[],"retire":[],"add":[]}`
	client := &fakeLLMClient{responses: []fakeResponse{
		{input: invalid, inputTokens: 10, outputTokens: 2},
		{input: invalid, inputTokens: 12, outputTokens: 3},
	}}
	// Fix 2 changed this behavior: a single failing page no longer aborts the
	// run or the function's error return — see
	// TestRunReportsAllAttemptedPagesFailedWhenNothingSucceeds and
	// TestRunRecordsAFailedPageAndContinuesWithoutAborting above for the
	// current, intended behavior. This test only confirms the surviving half of
	// the old guarantee: no partial/invalid proposal or state entry is ever
	// written for the page that failed.
	_, err = compile.Run(context.Background(), pages, newTestRegistry(t), client, compile.Options{})
	require.NoError(t, err)

	entries, err := pages.List("review/")
	require.NoError(t, err)
	require.Empty(t, entries, "a hard failure must never leave a partial proposal committed")

	_, _, err = pages.Read(filepath.Join(".balise", "compile", "extract-claims-state.json"))
	require.Error(t, err, "state must not record a page that never produced a valid change")
}
