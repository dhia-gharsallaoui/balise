package compile_test

import (
	"context"
	"strings"
	"testing"

	"github.com/dhia/balise/internal/compile"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/vault"
	"github.com/stretchr/testify/require"
)

// fakeFinder returns fixed candidates per scope and records every scope it was asked about,
// so a test can prove a batch only ever searched its own scope.
type fakeFinder struct {
	byScope map[string][]compile.CandidatePage
	asked   []string
}

func (f *fakeFinder) Candidates(_ context.Context, scope, _ string, _ int) ([]compile.CandidatePage, error) {
	f.asked = append(f.asked, scope)
	return f.byScope[scope], nil
}

const ergwPage = "---\nuid: 01J8Z3K9V6Q2M4N7P8R9S0T1E1\nslug: ergw\ntype: gotcha\nscope: work\n" +
	"title: ER gateway update deletes connections\nclaims:\n  - id: c1\n    text: Updating the ER gateway deletes its connections\n    status: active\n---\nbody\n"

func memoryAt(scope, day string, entries ...string) store.Change {
	var b strings.Builder
	b.WriteString("---\nuid: 01J8Z3K9V6Q2M4N7P8R9S0" + strings.ToUpper(scope[:2]) + day[8:] + "\nslug: \"" + day + "\"\ntype: memory\nscope: " + scope +
		"\ntitle: agent -- " + day + "\nagent: agent\ndate: \"" + day + "\"\n---\n")
	for i, e := range entries {
		b.WriteString("- **0" + string(rune('1'+i)) + ":00:00 UTC** " + e + "\n")
	}
	return store.Change{Path: scope + "/memory/agent/" + day + ".md", Data: []byte(b.String())}
}

func proposeVault(t *testing.T, changes ...store.Change) *store.GitPageStore {
	t.Helper()
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	_, err = pages.Commit(changes, "t <t@x>", "seed")
	require.NoError(t, err)
	return pages
}

func shippedRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	reg, err := registry.Load("../../defaults/types")
	require.NoError(t, err)
	return reg
}

func readProposals(t *testing.T, pages store.PageStore) []compile.Proposal {
	t.Helper()
	paths, err := pages.List("review/")
	require.NoError(t, err)
	var out []compile.Proposal
	for _, p := range paths {
		if strings.HasPrefix(p, "review/.rejected/") {
			continue
		}
		raw, _, err := pages.Read(p)
		require.NoError(t, err)
		page, err := vault.Parse(string(raw))
		require.NoError(t, err)
		var proposal compile.Proposal
		require.NoError(t, page.Decode(&proposal))
		out = append(out, proposal)
	}
	return out
}

const placementAnswer = `{
  "decisions": [
    {"note": 1, "action": "attach", "target": "ergw", "claim": "azapi update patches the ER gateway in place and keeps connections", "reason": "same gateway"},
    {"note": 2, "action": "new_page", "page": "miamisburg-fabric", "claim": "Miamisburg spines are Dell Z9864F-ON switches, not NVIDIA SN5610", "reason": "no page on the site fabric"},
    {"note": 3, "action": "skip", "reason": "progress chatter"}
  ],
  "new_pages": [
    {"type": "state", "slug": "miamisburg-fabric", "title": "Miamisburg spines are Dell, not NVIDIA", "body": "The site's north/south spines differ from the BOM."}
  ]
}`

// The whole promise: notes become reviewable proposals, a claims proposal on the page a note
// belongs to and a new_page proposal for notes that belong nowhere, and a note is never
// proposed twice.
func TestProposePagesTurnsNotesIntoProposalsOnce(t *testing.T) {
	pages := proposeVault(t,
		store.Change{Path: "work/gotchas/ergw.md", Data: []byte(ergwPage)},
		memoryAt("work", "2026-10-01",
			"azapi update patches the gateway in place, connections survive",
			"Miamisburg spines are Dell Z9864F-ON, not the NVIDIA SN5610 in the BOM",
			"started looking at the ticket"))
	finder := &fakeFinder{byScope: map[string][]compile.CandidatePage{
		"work": {{Path: "work/gotchas/ergw.md", Slug: "ergw", Title: "ER gateway update deletes connections", Type: "gotcha",
			Claims: []string{"Updating the ER gateway deletes its connections"}}},
	}}
	client := &fakeLLMClient{responses: []fakeResponse{{input: placementAnswer}}}

	report, err := compile.RunProposePages(context.Background(), pages, shippedRegistry(t), finder, client, compile.ProposeOptions{})
	require.NoError(t, err)
	require.Equal(t, 3, report.NotesPending)
	require.Len(t, report.CommitSHAs, 1)

	proposals := readProposals(t, pages)
	require.Len(t, proposals, 2)
	byKind := map[string]compile.Proposal{}
	for _, p := range proposals {
		byKind[p.Kind] = p
	}
	claims := byKind[compile.KindClaims]
	require.Equal(t, "ergw", claims.Target)
	require.NotEmpty(t, claims.TargetVersion, "an attach proposal pins the page version it was built against")
	require.Equal(t, "azapi update patches the ER gateway in place and keeps connections", claims.Change.Claims.Add[0].Text)
	require.Empty(t, claims.Change.Claims.Retire, "placing a note never retires anything")
	require.Equal(t, "work/memory/agent/2026-10-01.md", claims.Evidence[0].Source)

	newPage := byKind[compile.KindNewPage]
	require.Equal(t, "miamisburg-fabric", newPage.Target)
	require.Equal(t, "work", newPage.Scope)
	require.Equal(t, &compile.ProposalPage{Type: "state", Title: "Miamisburg spines are Dell, not NVIDIA",
		Body: "The site's north/south spines differ from the BOM."}, newPage.Page)
	require.Len(t, newPage.Change.Claims.Add, 1)

	again, err := compile.RunProposePages(context.Background(), pages, shippedRegistry(t), finder, client, compile.ProposeOptions{})
	require.NoError(t, err)
	require.Zero(t, again.NotesPending, "every note was decided, including the skipped one")
	require.Len(t, client.calls, 1, "a second run makes no model call")
}

// A client scope's notes must only ever be matched against, and proposed onto, that same
// scope: each scope is its own batch, its own candidate search, its own prompt.
func TestProposePagesNeverMixesScopes(t *testing.T) {
	pages := proposeVault(t,
		memoryAt("work", "2026-10-01", "work-only observation about the shared pipeline"),
		memoryAt("client-globex", "2026-10-01", "globex-only observation about their gateway"))
	finder := &fakeFinder{byScope: map[string][]compile.CandidatePage{}}
	skipOne := `{"decisions":[{"note":1,"action":"skip","reason":"not durable"}],"new_pages":[]}`
	client := &fakeLLMClient{responses: []fakeResponse{{input: skipOne}, {input: skipOne}}}

	_, err := compile.RunProposePages(context.Background(), pages, shippedRegistry(t), finder, client, compile.ProposeOptions{})
	require.NoError(t, err)
	require.Len(t, client.calls, 2)
	require.ElementsMatch(t, []string{"work", "client-globex"}, finder.asked)
	for _, call := range client.calls {
		globex := strings.Contains(call.User, "globex-only")
		work := strings.Contains(call.User, "work-only")
		require.True(t, globex != work, "each prompt carries exactly one scope's notes")
	}
}

// An answer naming a page that was not a candidate is a hard problem: the model gets one
// retry with the problem spelled out, and a second bad answer fails the batch without
// recording its notes, so they are simply retried on the next run.
func TestProposePagesRetriesThenGivesUpOnAnInventedTarget(t *testing.T) {
	pages := proposeVault(t, memoryAt("work", "2026-10-01", "a durable observation about something real"))
	finder := &fakeFinder{byScope: map[string][]compile.CandidatePage{}}
	invented := `{"decisions":[{"note":1,"action":"attach","target":"no-such-page","claim":"the observation restated as a claim","reason":"x"}],"new_pages":[]}`
	client := &fakeLLMClient{responses: []fakeResponse{{input: invented}, {input: invented}}}

	report, err := compile.RunProposePages(context.Background(), pages, shippedRegistry(t), finder, client, compile.ProposeOptions{})
	require.NoError(t, err)
	require.Len(t, client.calls, 2)
	require.Contains(t, client.calls[1].User, `"no-such-page", which is not a listed candidate slug`)
	require.True(t, report.Batches[0].Failed)
	require.Empty(t, report.CommitSHAs)
	require.Empty(t, readProposals(t, pages))
}

// Two pending proposals against one page is the hazard pending.go exists for, so a note that
// would attach to a page already awaiting review is deferred, not proposed, while the rest of
// the batch still lands.
func TestProposePagesDefersAnAttachToAPageAlreadyInReview(t *testing.T) {
	existing := compile.Proposal{ID: "01J8Z3K9V6Q2M4N7P8R9S0T1P1", Kind: compile.KindClaims, Scope: "work",
		Target: "ergw", Confidence: 0.9, CreatedBy: "compile:x", Status: "pending"}
	rendered, err := existing.Render("")
	require.NoError(t, err)
	pages := proposeVault(t,
		store.Change{Path: "work/gotchas/ergw.md", Data: []byte(ergwPage)},
		store.Change{Path: "review/" + existing.ID + ".md", Data: []byte(rendered)},
		memoryAt("work", "2026-10-01",
			"azapi update patches the gateway in place, connections survive",
			"Miamisburg spines are Dell Z9864F-ON, not the NVIDIA SN5610 in the BOM",
			"started looking at the ticket"))
	finder := &fakeFinder{byScope: map[string][]compile.CandidatePage{
		"work": {{Path: "work/gotchas/ergw.md", Slug: "ergw", Title: "ER gateway", Type: "gotcha"}},
	}}
	client := &fakeLLMClient{responses: []fakeResponse{{input: placementAnswer}}}

	report, err := compile.RunProposePages(context.Background(), pages, shippedRegistry(t), finder, client, compile.ProposeOptions{})
	require.NoError(t, err)
	require.Equal(t, 1, report.Batches[0].Deferred)
	require.Equal(t, []string{"miamisburg-fabric"}, report.Batches[0].Proposals)

	again, err := compile.RunProposePages(context.Background(), pages, shippedRegistry(t), finder,
		&fakeLLMClient{}, compile.ProposeOptions{DryRun: true})
	require.NoError(t, err)
	require.Equal(t, 1, again.NotesPending, "only the deferred note is still waiting")
}

// A new page may not take a slug a page or a pending proposal already uses.
func TestProposePagesRejectsATakenSlugForANewPage(t *testing.T) {
	pages := proposeVault(t,
		store.Change{Path: "work/gotchas/ergw.md", Data: []byte(ergwPage)},
		memoryAt("work", "2026-10-01", "a durable observation that deserves a page"))
	answer := `{"decisions":[{"note":1,"action":"new_page","page":"ergw","claim":"the durable observation restated as a claim","reason":"x"}],
	  "new_pages":[{"type":"state","slug":"ergw","title":"A title","body":"Body."}]}`
	client := &fakeLLMClient{responses: []fakeResponse{{input: answer}, {input: answer}}}

	report, err := compile.RunProposePages(context.Background(), pages, shippedRegistry(t),
		&fakeFinder{byScope: map[string][]compile.CandidatePage{}}, client, compile.ProposeOptions{})
	require.NoError(t, err)
	require.True(t, report.Batches[0].Failed)
	require.Contains(t, report.Batches[0].FailReason, `slug "ergw" is already taken`)
}

// Agents' notes are dense: one note can hold two facts that belong in different places, so a
// note may be placed more than once (found live, where the model split 10 of 18 notes). What
// it may not be is skipped and placed at once.
func TestProposePagesPlacesOneNoteInTwoPlacesButNeverSkipsAndPlacesIt(t *testing.T) {
	seed := func() *store.GitPageStore {
		return proposeVault(t,
			store.Change{Path: "work/gotchas/ergw.md", Data: []byte(ergwPage)},
			memoryAt("work", "2026-10-01", "azapi patches the gateway in place; the Miamisburg spines are Dell"))
	}
	finder := &fakeFinder{byScope: map[string][]compile.CandidatePage{
		"work": {{Path: "work/gotchas/ergw.md", Slug: "ergw", Title: "ER gateway", Type: "gotcha"}},
	}}
	split := `{"decisions":[
	  {"note":1,"action":"attach","target":"ergw","claim":"azapi update patches the gateway in place","reason":"gateway fact"},
	  {"note":1,"action":"new_page","page":"miamisburg-spines","claim":"Miamisburg spines are Dell switches","reason":"site fact"}],
	  "new_pages":[{"type":"state","slug":"miamisburg-spines","title":"Miamisburg spines are Dell","body":"Body."}]}`
	pages := seed()
	report, err := compile.RunProposePages(context.Background(), pages, shippedRegistry(t), finder,
		&fakeLLMClient{responses: []fakeResponse{{input: split}}}, compile.ProposeOptions{})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"ergw", "miamisburg-spines"}, report.Batches[0].Proposals)

	both := `{"decisions":[
	  {"note":1,"action":"skip","reason":"x"},
	  {"note":1,"action":"attach","target":"ergw","claim":"azapi update patches the gateway in place","reason":"y"}],
	  "new_pages":[]}`
	client := &fakeLLMClient{responses: []fakeResponse{{input: both}, {input: both}}}
	report, err = compile.RunProposePages(context.Background(), seed(), shippedRegistry(t), finder, client, compile.ProposeOptions{})
	require.NoError(t, err)
	require.Contains(t, report.Batches[0].FailReason, "skipped and also placed")
}

// A new_page decision naming an existing candidate (and none of the answer's own new pages)
// can only mean attach; found live, where one such slip failed a whole 18-note batch.
func TestProposePagesReadsANewPageNamingACandidateAsAnAttach(t *testing.T) {
	pages := proposeVault(t,
		store.Change{Path: "work/gotchas/ergw.md", Data: []byte(ergwPage)},
		memoryAt("work", "2026-10-01", "azapi patches the gateway in place, connections survive"))
	finder := &fakeFinder{byScope: map[string][]compile.CandidatePage{
		"work": {{Path: "work/gotchas/ergw.md", Slug: "ergw", Title: "ER gateway", Type: "gotcha"}},
	}}
	slip := `{"decisions":[{"note":1,"action":"new_page","page":"ergw","claim":"azapi update patches the gateway in place","reason":"x"}],"new_pages":[]}`
	client := &fakeLLMClient{responses: []fakeResponse{{input: slip}}}

	report, err := compile.RunProposePages(context.Background(), pages, shippedRegistry(t), finder, client, compile.ProposeOptions{})
	require.NoError(t, err)
	require.Len(t, client.calls, 1, "no retry needed")
	require.Equal(t, []string{"ergw"}, report.Batches[0].Proposals)
	require.Equal(t, compile.KindClaims, readProposals(t, pages)[0].Kind)
}
