package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dhia/balise/internal/api"
	"github.com/dhia/balise/internal/compile"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/dhia/balise/internal/vault"
	"github.com/stretchr/testify/require"
)

func mustParsePage(t *testing.T, raw string) vault.Page {
	t.Helper()
	page, err := vault.Parse(raw)
	require.NoError(t, err)
	return page
}

// newReviewServer is newHomeServer plus a real defaultsDir, since the accept endpoint
// reindexes the vault synchronously (review.go's reindexAfterAccept) and this package's own
// defaults/ directory (types + facets) is what every other test in this file already points
// registry.Load-style helpers at.
func newReviewServer(t *testing.T, q *store.Queries, pages store.PageStore) *httptest.Server {
	t.Helper()
	spaces, err := registry.LoadSpaces("../../defaults/spaces.yaml")
	require.NoError(t, err)
	order, err := registry.LoadOrder("../../defaults/order.yaml")
	require.NoError(t, err)
	server := httptest.NewServer(api.New(q, pages, spaces, order, "../../defaults"))
	t.Cleanup(server.Close)
	return server
}

const reviewTargetPage = `---
uid: 01J8Z3K9V6Q2M4N7P8R9S0T1U3
slug: foo
type: gotcha
scope: work
title: Foo gotcha
owner: dhia
claims:
  - text: Claim one stays as-is
    status: active
    id: c1
  - text: Claim two needs rewording
    status: active
    id: c2
  - text: Claim three is out of date
    status: active
    id: c3
---
Some body text about foo.
`

func writeReviewTargetPage(t *testing.T, pages store.PageStore) {
	t.Helper()
	_, err := pages.Write("work/foo.md", []byte(reviewTargetPage), "")
	require.NoError(t, err)
}

// writeClaimsProposal writes review/<id>.md carrying a real claims change (unlike this
// file's sibling writeProposal in home_test.go, which only needs id/kind/scope/status for
// Home's queue-grouping tests and leaves Change empty).
func writeClaimsProposal(t *testing.T, pages store.PageStore, p compile.Proposal) {
	t.Helper()
	rendered, err := p.Render("Because the source page changed.")
	require.NoError(t, err)
	_, err = pages.Write("review/"+p.ID+".md", []byte(rendered), "")
	require.NoError(t, err)
}

func baseReviewProposal(id string) compile.Proposal {
	return compile.Proposal{
		ID: id, Kind: "claims", Scope: "work", Target: "foo", Confidence: 0.92,
		CreatedBy: "compile:2026-09-18T08:00:00Z", Status: "pending",
		Change: compile.ProposalChange{Claims: compile.ProposalClaims{
			Keep:   []string{"c1"},
			Reword: []compile.ProposalReword{{ID: "c2", Text: "Claim two, reworded"}},
			Retire: []compile.ProposalRetire{{ID: "c3", AsOf: "2026-09-18"}},
			Add:    []compile.ProposalAdd{{Text: "A brand new claim", Status: "active"}},
		}},
	}
}

func doPost(t *testing.T, server *httptest.Server, path string, body any) (*http.Response, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, json.NewEncoder(&buf).Encode(body))
	resp, err := http.Post(server.URL+path, "application/json", &buf)
	require.NoError(t, err)
	defer resp.Body.Close()
	var decoded map[string]any
	// A 204/empty body is never expected from these handlers, but a failed decode should
	// not itself fail the test before the caller gets to see the status code.
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	return resp, decoded
}

// TestReviewAcceptAppliesKeepRewordRetireAdd is the core F-50 acceptance test: every one of
// keep/reword/retire/add must land on the target page exactly as 01-design-spec-v0.4.md
// section 6.6 describes, in a single commit, authored agent:<pipeline>.
func TestReviewAcceptAppliesKeepRewordRetireAdd(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writeReviewTargetPage(t, pages)
	writeClaimsProposal(t, pages, baseReviewProposal("p-accept-1"))

	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	server := newReviewServer(t, q, pages)

	resp, body := doPost(t, server, "/api/review/p-accept-1/accept", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	commitSHA, _ := body["commit"].(string)
	require.NotEmpty(t, commitSHA, "accept must report the commit it made")
	require.False(t, body["index_stale"].(bool), "the real defaults dir must let the inline reindex succeed")

	// The proposal file is gone: readProposal-based lookups (and a second accept) must 404.
	_, _, err = pages.Read("review/p-accept-1.md")
	require.Error(t, err, "the proposal file must be deleted as part of accepting it")

	raw, _, err := pages.Read("work/foo.md")
	require.NoError(t, err)
	var fm struct {
		Claims []compile.ClaimRecord `yaml:"claims"`
	}
	page := mustParsePage(t, string(raw))
	require.NoError(t, page.Decode(&fm))
	byID := map[string]compile.ClaimRecord{}
	for _, c := range fm.Claims {
		byID[c.ID] = c
	}
	require.Len(t, fm.Claims, 4, "c1, c2, c3 plus one freshly added claim")

	require.Equal(t, "Claim one stays as-is", byID["c1"].Text, "kept claim must be untouched")
	require.Equal(t, "active", byID["c1"].Status)

	require.Equal(t, "Claim two, reworded", byID["c2"].Text, "reworded claim must carry the new text")
	require.Equal(t, "active", byID["c2"].Status, "reword must not change status")

	require.Equal(t, "superseded", byID["c3"].Status, "retire must mark the claim superseded, never delete it")
	require.Equal(t, "2026-09-18", byID["c3"].AsOf)
	require.Equal(t, "Claim three is out of date", byID["c3"].Text, "retire must not touch the claim's text")

	added, ok := byID["c4"]
	require.True(t, ok, "add must mint a fresh id continuing the page's own c<N> convention")
	require.Equal(t, "A brand new claim", added.Text)
	require.Equal(t, "active", added.Status)

	// One commit, author agent:<pipeline> derived from created_by's "compile:..." prefix
	// (01 section 6.6), touching both the page and the deleted proposal.
	history, err := pages.History("work/foo.md", 1)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, commitSHA, history[0].SHA)
	require.Contains(t, history[0].Author, "agent:compile",
		"accept commits must be authored agent:<pipeline>, per 01 section 6.6")

	proposalHistory, err := pages.History("review/p-accept-1.md", 1)
	require.NoError(t, err)
	require.Len(t, proposalHistory, 1)
	require.Equal(t, commitSHA, proposalHistory[0].SHA,
		"the page edit and the proposal deletion must be the exact same commit")
}

// TestReviewAcceptRefusesOutOfScopeProposal is the scope-safety test: a proposal naming a
// scope the caller's Queries were not constructed with must be refused exactly like an
// absent proposal (404), reusing scopeAllowed — the same boundary a previous review found a
// hole in — rather than a second, bespoke check.
func TestReviewAcceptRefusesOutOfScopeProposal(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writeReviewTargetPage(t, pages)
	p := baseReviewProposal("p-out-of-scope")
	p.Scope = "personal" // caller below is only constructed with "work"
	writeClaimsProposal(t, pages, p)

	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	server := newReviewServer(t, q, pages)

	resp, _ := doPost(t, server, "/api/review/p-out-of-scope/accept", nil)
	require.Equal(t, http.StatusNotFound, resp.StatusCode,
		"an out-of-scope proposal must 404, never reveal it exists via a 403")

	// And it must not have been touched.
	_, _, err = pages.Read("review/p-out-of-scope.md")
	require.NoError(t, err, "a refused accept must not delete the proposal")
}

// TestReviewReacceptFailsCleanly is the double-apply regression test: accepting the same
// proposal twice must not double-apply the change. Because accept's one commit deletes the
// proposal file, a second sequential accept finds nothing to read and fails with 404/409
// rather than silently reapplying reword/retire/add on top of their own already-applied
// result.
func TestReviewReacceptFailsCleanly(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writeReviewTargetPage(t, pages)
	writeClaimsProposal(t, pages, baseReviewProposal("p-reaccept"))

	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	server := newReviewServer(t, q, pages)

	first, _ := doPost(t, server, "/api/review/p-reaccept/accept", nil)
	require.Equal(t, http.StatusOK, first.StatusCode)

	second, body := doPost(t, server, "/api/review/p-reaccept/accept", nil)
	require.NotEqual(t, http.StatusOK, second.StatusCode,
		"re-accepting an already-accepted proposal must fail, not double-apply")
	require.Equal(t, http.StatusNotFound, second.StatusCode, "the proposal file is gone by now")
	_ = body

	raw, _, err := pages.Read("work/foo.md")
	require.NoError(t, err)
	page := mustParsePage(t, string(raw))
	var fm struct {
		Claims []compile.ClaimRecord `yaml:"claims"`
	}
	require.NoError(t, page.Decode(&fm))
	require.Len(t, fm.Claims, 4, "the change must have been applied exactly once, not twice")
}

// TestReviewAcceptRefusesAStaleProposal is fix 1, part B's safety net: a proposal whose
// recorded target_version no longer matches the target page must be refused (409) rather than
// applied. This is the "known gap" the review-loop implementer flagged — accept had no
// optimistic-locking token — and it matters even though fix 1 part A now stops a second pending
// proposal from coexisting for one target: a proposal can still go stale if the target page is
// edited (by a human, or by another accept) while it sits pending.
func TestReviewAcceptRefusesAStaleProposal(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writeReviewTargetPage(t, pages)

	_, staleVersion, err := pages.Read("work/foo.md")
	require.NoError(t, err)

	// The target page changes after the proposal's baseline was recorded — e.g. a human edit,
	// or another accept, landed while this proposal sat pending.
	_, err = pages.Write("work/foo.md", []byte(strings.Replace(reviewTargetPage,
		"Some body text about foo.", "Some body text about foo, now edited.", 1)), "")
	require.NoError(t, err)

	p := baseReviewProposal("p-stale-1").WithTargetVersion(staleVersion)
	writeClaimsProposal(t, pages, p)

	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	server := newReviewServer(t, q, pages)

	resp, body := doPost(t, server, "/api/review/p-stale-1/accept", nil)
	require.Equal(t, http.StatusConflict, resp.StatusCode,
		"a proposal whose target changed since it was built must be refused, not applied against stale claim ids")
	require.Contains(t, body["detail"], "stale")

	// Untouched: the proposal is still pending, and the page carries only the edit, not the
	// stale proposal's keep/reword/retire/add.
	_, _, err = pages.Read("review/p-stale-1.md")
	require.NoError(t, err, "a refused accept must not delete or move the proposal")

	raw, _, err := pages.Read("work/foo.md")
	require.NoError(t, err)
	require.Contains(t, string(raw), "now edited")
	require.NotContains(t, string(raw), "id: c4", "the stale proposal's add must never have been applied")
}

// TestReviewEditAcceptAppliesEditedTextAndKeepsOthers is edit-accept's core F-50-adjacent
// acceptance test (04 section 13): a reviewer-supplied edit to a reworded claim's text must
// land on the page in place of the proposal's own text, while every other claim (kept,
// retired, added-and-unedited) is applied exactly as a plain accept would apply it. The commit
// must still be a single commit, authored agent:<pipeline>, but its message must name the
// edited claim so the edit is never silently invisible in history.
func TestReviewEditAcceptAppliesEditedTextAndKeepsOthers(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writeReviewTargetPage(t, pages)
	writeClaimsProposal(t, pages, baseReviewProposal("p-edit-1"))

	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	server := newReviewServer(t, q, pages)

	resp, body := doPost(t, server, "/api/review/p-edit-1/edit-accept", map[string]any{
		"edits": []map[string]string{
			{"id": "c2", "text": "Claim two, reworded by the reviewer this time"},
		},
	})
	require.Equal(t, http.StatusOK, resp.StatusCode)

	commitSHA, _ := body["commit"].(string)
	require.NotEmpty(t, commitSHA, "edit-accept must report the commit it made")
	require.False(t, body["index_stale"].(bool))

	_, _, err = pages.Read("review/p-edit-1.md")
	require.Error(t, err, "the proposal file must be deleted as part of edit-accepting it")

	raw, _, err := pages.Read("work/foo.md")
	require.NoError(t, err)
	var fm struct {
		Claims []compile.ClaimRecord `yaml:"claims"`
	}
	page := mustParsePage(t, string(raw))
	require.NoError(t, page.Decode(&fm))
	byID := map[string]compile.ClaimRecord{}
	for _, c := range fm.Claims {
		byID[c.ID] = c
	}
	require.Len(t, fm.Claims, 4)

	require.Equal(t, "Claim one stays as-is", byID["c1"].Text, "an untouched keep must be unaffected by the edit")
	require.Equal(t, "Claim two, reworded by the reviewer this time", byID["c2"].Text,
		"the edited text must replace the proposal's own reworded text")
	require.Equal(t, "active", byID["c2"].Status, "editing text must not change status")
	require.Equal(t, "superseded", byID["c3"].Status, "an untouched retire must still apply")
	require.Equal(t, "A brand new claim", byID["c4"].Text, "an unedited add must keep the proposal's own text")

	history, err := pages.History("work/foo.md", 1)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, commitSHA, history[0].SHA)
	require.Contains(t, history[0].Author, "agent:compile",
		"an edited accept must still be authored agent:<pipeline> — the claim's starting point really was that proposal")
	require.Contains(t, history[0].Message, "edited before accept: c2",
		"the commit message must disclose which claim's text was human-edited, never silently crediting the machine")

	proposalHistory, err := pages.History("review/p-edit-1.md", 1)
	require.NoError(t, err)
	require.Len(t, proposalHistory, 1)
	require.Equal(t, commitSHA, proposalHistory[0].SHA,
		"the page edit and the proposal deletion must be the exact same commit")
}

// TestReviewEditAcceptRefusesAStaleProposal is edit-accept's equivalent of
// TestReviewAcceptRefusesAStaleProposal: editing a proposal's text must not be a way to bypass
// the same TargetVersion staleness check a plain accept enforces.
func TestReviewEditAcceptRefusesAStaleProposal(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writeReviewTargetPage(t, pages)

	_, staleVersion, err := pages.Read("work/foo.md")
	require.NoError(t, err)

	_, err = pages.Write("work/foo.md", []byte(strings.Replace(reviewTargetPage,
		"Some body text about foo.", "Some body text about foo, now edited.", 1)), "")
	require.NoError(t, err)

	p := baseReviewProposal("p-edit-stale").WithTargetVersion(staleVersion)
	writeClaimsProposal(t, pages, p)

	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	server := newReviewServer(t, q, pages)

	resp, body := doPost(t, server, "/api/review/p-edit-stale/edit-accept", map[string]any{
		"edits": []map[string]string{{"id": "c2", "text": "Some perfectly fine edited text"}},
	})
	require.Equal(t, http.StatusConflict, resp.StatusCode,
		"a stale proposal must be refused before any edit is even considered")
	require.Contains(t, body["detail"], "stale")

	_, _, err = pages.Read("review/p-edit-stale.md")
	require.NoError(t, err, "a refused edit-accept must not delete or move the proposal")
	raw, _, err := pages.Read("work/foo.md")
	require.NoError(t, err)
	require.NotContains(t, string(raw), "reworded", "the stale proposal's edit must never have been applied")
}

// TestReviewEditAcceptRefusesEditingAnUnchangedClaim pins the editable surface: only a claim
// ApplyClaims marked added or reworded is "the change" a reviewer may edit. A kept claim (c1)
// is the page's own untouched history, not something this proposal is introducing, so editing
// it here must be refused rather than silently allowed.
func TestReviewEditAcceptRefusesEditingAnUnchangedClaim(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writeReviewTargetPage(t, pages)
	writeClaimsProposal(t, pages, baseReviewProposal("p-edit-unchanged"))

	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	server := newReviewServer(t, q, pages)

	resp, _ := doPost(t, server, "/api/review/p-edit-unchanged/edit-accept", map[string]any{
		"edits": []map[string]string{{"id": "c1", "text": "Trying to edit a kept claim"}},
	})
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

	_, _, err = pages.Read("review/p-edit-unchanged.md")
	require.NoError(t, err, "a refused edit-accept must not touch the proposal")
	raw, _, err := pages.Read("work/foo.md")
	require.NoError(t, err)
	require.Equal(t, reviewTargetPage, string(raw), "a refused edit-accept must not touch the page")
}

// TestReviewEditAcceptRefusesTooShortEditedText pins the floor: an edited claim is held to
// exactly the same minClaimWords hard rule a freshly proposed claim is (compile.ValidateClaimWord)
// — editing must never be a looser gate than proposing.
func TestReviewEditAcceptRefusesTooShortEditedText(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writeReviewTargetPage(t, pages)
	writeClaimsProposal(t, pages, baseReviewProposal("p-edit-short"))

	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	server := newReviewServer(t, q, pages)

	resp, _ := doPost(t, server, "/api/review/p-edit-short/edit-accept", map[string]any{
		"edits": []map[string]string{{"id": "c2", "text": "Nope"}},
	})
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

	_, _, err = pages.Read("review/p-edit-short.md")
	require.NoError(t, err, "a refused edit-accept must not touch the proposal")
}

// TestReviewEditAcceptWarnsButAppliesOverLengthEditedText pins the ceiling: an edited claim
// over maxClaimWords must warn, not block — exactly the same treatment validateClaimText gives
// a freshly proposed over-length claim (fix 1) — so a stricter gate is never accidentally
// introduced for the edit path.
func TestReviewEditAcceptWarnsButAppliesOverLengthEditedText(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writeReviewTargetPage(t, pages)
	writeClaimsProposal(t, pages, baseReviewProposal("p-edit-long"))

	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	server := newReviewServer(t, q, pages)

	longText := "This edited claim text deliberately runs on for more than fifteen words so it must warn but still be accepted"
	require.Greater(t, len(strings.Fields(longText)), 15)

	resp, body := doPost(t, server, "/api/review/p-edit-long/edit-accept", map[string]any{
		"edits": []map[string]string{{"id": "c2", "text": longText}},
	})
	require.Equal(t, http.StatusOK, resp.StatusCode, "an over-length edited claim must warn, never block")

	warnings, _ := body["warnings"].([]any)
	require.Len(t, warnings, 1)
	warning, _ := warnings[0].(map[string]any)
	require.Equal(t, "c2", warning["id"])
	require.Equal(t, "over_word_limit", warning["code"])

	raw, _, err := pages.Read("work/foo.md")
	require.NoError(t, err)
	require.Contains(t, string(raw), longText, "the over-length edit must still be applied to the page")
}

// TestReviewEditAcceptRefusesUnknownClaimID: an edit naming an id ApplyClaims never produced at
// all must be refused, never silently ignored.
func TestReviewEditAcceptRefusesUnknownClaimID(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writeReviewTargetPage(t, pages)
	writeClaimsProposal(t, pages, baseReviewProposal("p-edit-unknown"))

	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	server := newReviewServer(t, q, pages)

	resp, _ := doPost(t, server, "/api/review/p-edit-unknown/edit-accept", map[string]any{
		"edits": []map[string]string{{"id": "c99", "text": "This claim id does not exist anywhere"}},
	})
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

	_, _, err = pages.Read("review/p-edit-unknown.md")
	require.NoError(t, err, "a refused edit-accept must not touch the proposal")
}

// TestReviewEditAcceptWithNoEditsBehavesLikePlainAccept: an empty edits list is a degenerate
// but valid case (the UI never sends it — Accept is the button for that — but the API must not
// crash or misbehave), and must produce a plain, unannotated accept commit message.
func TestReviewEditAcceptWithNoEditsBehavesLikePlainAccept(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writeReviewTargetPage(t, pages)
	writeClaimsProposal(t, pages, baseReviewProposal("p-edit-none"))

	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	server := newReviewServer(t, q, pages)

	resp, _ := doPost(t, server, "/api/review/p-edit-none/edit-accept", map[string]any{
		"edits": []map[string]string{},
	})
	require.Equal(t, http.StatusOK, resp.StatusCode)

	history, err := pages.History("work/foo.md", 1)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.NotContains(t, history[0].Message, "edited before accept",
		"no edits were made, so the commit message must not falsely claim any were")
}

// TestReviewRejectMovesFileAndRecordsReason covers 04 section 13's reject endpoint: the
// proposal file must move to review/.rejected/ with the one-line reason recorded, in one
// commit, leaving the target page untouched.
func TestReviewRejectMovesFileAndRecordsReason(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writeReviewTargetPage(t, pages)
	writeClaimsProposal(t, pages, baseReviewProposal("p-reject-1"))

	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	server := newReviewServer(t, q, pages)

	resp, body := doPost(t, server, "/api/review/p-reject-1/reject",
		map[string]string{"reason": "Claim 2's rewording changes its meaning"})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "review/.rejected/p-reject-1.md", body["path"])

	_, _, err = pages.Read("review/p-reject-1.md")
	require.Error(t, err, "the original proposal path must be gone")

	raw, _, err := pages.Read("review/.rejected/p-reject-1.md")
	require.NoError(t, err, "the rejected copy must exist at review/.rejected/<id>.md")

	var rejected compile.Proposal
	page := mustParsePage(t, string(raw))
	require.NoError(t, page.Decode(&rejected))
	require.Equal(t, "rejected", rejected.Status)
	require.NotNil(t, rejected.Rejected)
	require.Equal(t, "Claim 2's rewording changes its meaning", rejected.Rejected.Reason)
	require.Contains(t, string(raw), "Because the source page changed.",
		"the original rationale body text must be preserved, not dropped")

	// The target page must be untouched by a reject.
	pageRaw, _, err := pages.Read("work/foo.md")
	require.NoError(t, err)
	require.Equal(t, reviewTargetPage, string(pageRaw))
}

// TestReviewRejectRequiresOneLineReason pins the input-validation boundary: an empty reason,
// or one containing a newline, must be refused rather than silently accepted or truncated.
func TestReviewRejectRequiresOneLineReason(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writeReviewTargetPage(t, pages)
	writeClaimsProposal(t, pages, baseReviewProposal("p-reject-2"))

	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	server := newReviewServer(t, q, pages)

	resp, _ := doPost(t, server, "/api/review/p-reject-2/reject", map[string]string{"reason": ""})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, "an empty reason must be refused")

	resp2, _ := doPost(t, server, "/api/review/p-reject-2/reject",
		map[string]string{"reason": "line one\nline two"})
	require.Equal(t, http.StatusBadRequest, resp2.StatusCode, "a multi-line reason must be refused")

	// Still pending: neither rejected attempt should have touched the file.
	_, _, err = pages.Read("review/p-reject-2.md")
	require.NoError(t, err, "a refused reject must not move the proposal file")
}
