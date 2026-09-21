package cli_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dhia/balise/internal/cli"
	"github.com/dhia/balise/internal/compile"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestReindexIgnoresProposals is bug 1's regression test: review/<id>.md proposal files
// must never be indexed as pages. Before the fix, cli.Reindex walked every .md file
// pages.List("") returned with no exclusion for review/ (or .balise/), so a proposal was
// indexed as a `note` page titled with its raw ULID, and vault.ScopeOf handed it the
// phantom scope "review" — a scope that names no tenant and appears in no defaults/ file.
func TestReindexIgnoresProposals(t *testing.T) {
	pages, err := store.InitGit(filepath.Join(t.TempDir(), "vault"))
	require.NoError(t, err)

	const pageBody = `---
uid: 01JXPROPTESTPAGE0000000001
slug: reindex-scope-fixture
type: note
scope: work
title: Reindex scope fixture page
status: active
owner: test
---
Body text for the fixture page.
`
	_, err = pages.Write("work/notes/reindex-scope-fixture.md", []byte(pageBody), "")
	require.NoError(t, err)

	proposal := compile.BuildProposal(
		"work/notes/reindex-scope-fixture.md", "work", "reindex-scope-fixture",
		compile.ClaimsChange{}, 0.9, "Body text for the fixture page.", time.Now())
	rendered, err := proposal.Render("")
	require.NoError(t, err)
	_, err = pages.Write("review/"+proposal.ID+".md", []byte(rendered), "")
	require.NoError(t, err)

	// "review" is included in the allowed scopes so that, if the fix regresses and the
	// proposal is indexed as a page again, its row is visible to this test instead of being
	// silently dropped by scope enforcement first — that would hide the very bug this test
	// exists to catch.
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work", "review"})
	report, err := cli.Reindex(context.Background(), q, pages, "../../defaults")
	require.NoError(t, err)
	require.Equal(t, 1, report.Pages, "the proposal must not be counted as a walked page")

	rows, err := q.ListPages(context.Background(), store.PageFilter{IncludeHistorical: true})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "work", rows[0].Scope)
	require.Equal(t, "reindex-scope-fixture", rows[0].Slug)
	for _, row := range rows {
		require.NotEqual(t, "review", row.Scope)
		require.False(t, strings.HasPrefix(row.Path, "review/"),
			"row path %q must not be under review/", row.Path)
	}
}
