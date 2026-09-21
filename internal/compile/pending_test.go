package compile_test

import (
	"strings"
	"testing"

	"github.com/dhia/balise/internal/compile"
	"github.com/dhia/balise/internal/store"
	"github.com/stretchr/testify/require"
)

func writeProposal(t *testing.T, pages store.PageStore, p compile.Proposal, rationale string) {
	t.Helper()
	rendered, err := p.Render(rationale)
	require.NoError(t, err)
	writePage(t, pages, "review/"+p.ID+".md", rendered)
}

func pendingReviewPaths(t *testing.T, pages store.PageStore) []string {
	t.Helper()
	entries, err := pages.List("review/")
	require.NoError(t, err)
	var out []string
	for _, e := range entries {
		if !strings.HasPrefix(e, "review/.rejected/") {
			out = append(out, e)
		}
	}
	return out
}

// TestDedupePendingSupersedesEveryDuplicateExceptTheNewest is the one-time repair (fix 1,
// required cleanup) for duplicates that piled up in review/ before Run started preventing new
// ones. Three proposals target the same page: DedupePending must keep only the newest (by
// CreatedBy) and supersede the other two, each with an audit trail preserved under
// review/.rejected/.
func TestDedupePendingSupersedesEveryDuplicateExceptTheNewest(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)

	oldest := compile.Proposal{
		ID: "01AAAAAAAAAAAAAAAAAAAAAAAA", Kind: "claims", Scope: "work", Target: "dup-target",
		CreatedBy: "compile:2026-01-01T00:00:00Z",
		Change:    compile.ProposalChange{Claims: compile.ProposalClaims{Keep: []string{"c1"}}},
		Status:    "pending",
	}
	middle := compile.Proposal{
		ID: "01BBBBBBBBBBBBBBBBBBBBBBBB", Kind: "claims", Scope: "work", Target: "dup-target",
		CreatedBy: "compile:2026-01-02T00:00:00Z",
		Change:    compile.ProposalChange{Claims: compile.ProposalClaims{Keep: []string{"c1"}}},
		Status:    "pending",
	}
	newest := compile.Proposal{
		ID: "01CCCCCCCCCCCCCCCCCCCCCCCC", Kind: "claims", Scope: "work", Target: "dup-target",
		CreatedBy: "compile:2026-01-03T00:00:00Z",
		Change:    compile.ProposalChange{Claims: compile.ProposalClaims{Keep: []string{"c1"}}},
		Status:    "pending",
	}
	// An unrelated single (non-duplicated) target must be left untouched.
	lonely := compile.Proposal{
		ID: "01DDDDDDDDDDDDDDDDDDDDDDDD", Kind: "claims", Scope: "work", Target: "lonely-target",
		CreatedBy: "compile:2026-01-01T00:00:00Z",
		Change:    compile.ProposalChange{Claims: compile.ProposalClaims{Keep: []string{"c1"}}},
		Status:    "pending",
	}
	writeProposal(t, pages, oldest, "oldest rationale")
	writeProposal(t, pages, middle, "middle rationale")
	writeProposal(t, pages, newest, "newest rationale")
	writeProposal(t, pages, lonely, "lonely rationale")

	count, err := compile.DedupePending(pages)
	require.NoError(t, err)
	require.Equal(t, 2, count, "only the two older dup-target proposals must be superseded")

	pending := pendingReviewPaths(t, pages)
	require.ElementsMatch(t, []string{"review/" + newest.ID + ".md", "review/" + lonely.ID + ".md"}, pending,
		"only the newest dup-target proposal and the untouched lonely one remain pending")

	for _, id := range []string{oldest.ID, middle.ID} {
		raw, _, err := pages.Read("review/.rejected/" + id + ".md")
		require.NoError(t, err)
		require.Contains(t, string(raw), "status: rejected")
		require.Contains(t, string(raw), "superseded")
	}
	oldRaw, _, err := pages.Read("review/.rejected/" + oldest.ID + ".md")
	require.NoError(t, err)
	require.Contains(t, string(oldRaw), "oldest rationale", "rationale preserved verbatim, like a human reject")
}

// TestDedupePendingIsANoOpWithoutDuplicates confirms the cleanup does not touch, or commit
// anything for, a review/ directory with no duplicated targets — an empty run must be silent
// and cheap to call repeatedly.
func TestDedupePendingIsANoOpWithoutDuplicates(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	solo := compile.Proposal{
		ID: "01EEEEEEEEEEEEEEEEEEEEEEEE", Kind: "claims", Scope: "work", Target: "solo-target",
		CreatedBy: "compile:2026-01-01T00:00:00Z",
		Change:    compile.ProposalChange{Claims: compile.ProposalClaims{Keep: []string{"c1"}}},
		Status:    "pending",
	}
	writeProposal(t, pages, solo, "solo rationale")

	count, err := compile.DedupePending(pages)
	require.NoError(t, err)
	require.Zero(t, count)

	pending := pendingReviewPaths(t, pages)
	require.Equal(t, []string{"review/" + solo.ID + ".md"}, pending)

	entries, err := pages.List("review/.rejected/")
	require.NoError(t, err)
	require.Empty(t, entries)
}
