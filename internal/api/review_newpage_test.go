package api_test

import (
	"net/http"
	"testing"

	"github.com/dhia/balise/internal/compile"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/stretchr/testify/require"
)

func newPageProposal(id, slug string) compile.Proposal {
	return compile.Proposal{
		ID: id, Kind: compile.KindNewPage, Scope: "work", Target: slug, Confidence: 0.9,
		CreatedBy: "compile:2026-10-01T08:00:00Z", Status: "pending",
		Evidence: []compile.ProposalEvidence{{Source: "work/memory/agent/2026-10-01.md", Text: "spines are Dell, not NVIDIA"}},
		Change: compile.ProposalChange{Claims: compile.ProposalClaims{Add: []compile.ProposalAdd{
			{Text: "Miamisburg spines are Dell Z9864F-ON switches", Status: "active"},
			{Text: "The BOM wrongly lists NVIDIA SN5610 spines", Status: "active"},
		}}},
		Page: &compile.ProposalPage{Type: "state", Title: "Miamisburg spines are Dell, not NVIDIA",
			Body: "The site's north/south spines differ from the BOM."},
	}
}

// The detail view shows a new page as what accepting will create: its type, title, path and
// body, with every claim marked added and nothing before it.
func TestReviewDetailDescribesANewPage(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writeClaimsProposal(t, pages, newPageProposal("p-new-detail", "miamisburg-fabric"))
	server := newReviewServer(t, store.NewQueries(testutil.NewDB(t), store.Scopes{"work"}), pages)

	var detail struct {
		Kind        string `json:"kind"`
		TargetTitle string `json:"target_title"`
		Before      []any  `json:"before"`
		After       []struct{ Text, Mark string }
		NewPage     struct{ Type, Title, Path, Body string } `json:"new_page"`
	}
	require.Equal(t, http.StatusOK, getJSON(t, server, "/api/review/p-new-detail", &detail))
	require.Equal(t, "new_page", detail.Kind)
	require.Equal(t, "Miamisburg spines are Dell, not NVIDIA", detail.TargetTitle, "a new page is titled by its proposed title, not its slug")
	require.Empty(t, detail.Before)
	require.Len(t, detail.After, 2)
	require.Equal(t, "added", detail.After[0].Mark)
	require.Equal(t, "state", detail.NewPage.Type)
	require.Equal(t, "work/state/miamisburg-fabric.md", detail.NewPage.Path)
	require.Equal(t, "The site's north/south spines differ from the BOM.", detail.NewPage.Body)
}

// Accepting a new_page proposal creates the page, in its type's folder, with a uid, the
// proposal's claims and body, and removes the proposal, all in one commit.
func TestReviewAcceptCreatesTheNewPage(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writeClaimsProposal(t, pages, newPageProposal("p-new-accept", "miamisburg-fabric"))
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	server := newReviewServer(t, q, pages)

	resp, body := doPost(t, server, "/api/review/p-new-accept/accept", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, body)

	raw, _, err := pages.Read("work/state/miamisburg-fabric.md")
	require.NoError(t, err)
	var fm struct {
		UID, Slug, Type, Scope, Title string
		Claims                        []compile.ClaimRecord
	}
	page := mustParsePage(t, string(raw))
	require.NoError(t, page.Decode(&fm))
	require.NotEmpty(t, fm.UID)
	require.Equal(t, "miamisburg-fabric", fm.Slug)
	require.Equal(t, "state", fm.Type)
	require.Equal(t, "work", fm.Scope)
	require.Equal(t, "Miamisburg spines are Dell, not NVIDIA", fm.Title)
	require.Equal(t, []compile.ClaimRecord{
		{ID: "c1", Text: "Miamisburg spines are Dell Z9864F-ON switches", Status: "active"},
		{ID: "c2", Text: "The BOM wrongly lists NVIDIA SN5610 spines", Status: "active"},
	}, fm.Claims)
	require.Equal(t, "The site's north/south spines differ from the BOM.\n", page.Body)

	_, _, err = pages.Read("review/p-new-accept.md")
	require.Error(t, err, "the proposal file goes in the same commit as the page")

	indexed, err := q.GetPage(t.Context(), "work", "miamisburg-fabric")
	require.NoError(t, err)
	require.NotNil(t, indexed, "the accepted page is indexed straight away")
}

// A slug taken since the proposal was built, by a page in any folder, must never be
// overwritten or duplicated: accept refuses, and the detail view still opens so the reviewer
// can reject it.
func TestReviewAcceptRefusesANewPageWhoseSlugIsTaken(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writeReviewTargetPage(t, pages) // work/foo.md, slug foo, in no type folder
	writeClaimsProposal(t, pages, newPageProposal("p-new-taken", "foo"))
	server := newReviewServer(t, store.NewQueries(testutil.NewDB(t), store.Scopes{"work"}), pages)

	resp, body := doPost(t, server, "/api/review/p-new-taken/accept", nil)
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	require.Contains(t, body["detail"], `slug "foo" already exists`)

	raw, _, err := pages.Read("work/foo.md")
	require.NoError(t, err)
	require.Equal(t, reviewTargetPage, string(raw), "the existing page is untouched")

	require.Equal(t, http.StatusOK, getJSON(t, server, "/api/review/p-new-taken", nil))
}

// Edit-then-accept works on a new page's claims exactly as on an added claim of an existing
// page: the reviewer's wording is what lands.
func TestReviewEditAcceptRewordsANewPagesClaim(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writeClaimsProposal(t, pages, newPageProposal("p-new-edit", "miamisburg-fabric"))
	server := newReviewServer(t, store.NewQueries(testutil.NewDB(t), store.Scopes{"work"}), pages)

	resp, body := doPost(t, server, "/api/review/p-new-edit/edit-accept", map[string]any{
		"edits": []map[string]string{{"id": "c2", "text": "The BOM lists NVIDIA SN5610 spines, which were never installed"}},
	})
	require.Equal(t, http.StatusOK, resp.StatusCode, body)

	raw, _, err := pages.Read("work/state/miamisburg-fabric.md")
	require.NoError(t, err)
	require.Contains(t, string(raw), "which were never installed")
}

// A new page may only take an indexed type that exists: anything else is refused before a
// file is written.
func TestReviewAcceptRefusesANewPageOfAnUnknownType(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	p := newPageProposal("p-new-type", "some-page")
	p.Page.Type = "no-such-type"
	writeClaimsProposal(t, pages, p)
	server := newReviewServer(t, store.NewQueries(testutil.NewDB(t), store.Scopes{"work"}), pages)

	resp, _ := doPost(t, server, "/api/review/p-new-type/accept", nil)
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}
