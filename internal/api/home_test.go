package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dhia/balise/internal/api"
	"github.com/dhia/balise/internal/compile"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/stretchr/testify/require"
)

// newHomeServer is newServer plus an explicit pages argument, since every /api/home test
// needs to seed the vault itself before the server starts.
func newHomeServer(t *testing.T, q *store.Queries, pages store.PageStore) *httptest.Server {
	t.Helper()
	spaces, err := registry.LoadSpaces("../../defaults/spaces.yaml")
	require.NoError(t, err)
	order, err := registry.LoadOrder("../../defaults/order.yaml")
	require.NoError(t, err)
	server := httptest.NewServer(api.New(q, pages, spaces, order, ""))
	t.Cleanup(server.Close)
	return server
}

func writeProposal(t *testing.T, pages store.PageStore, id, kind, scope, target, status string) {
	t.Helper()
	p := compile.Proposal{
		ID: id, Kind: kind, Scope: scope, Target: target, Confidence: 0.9,
		CreatedBy: "compile:2026-01-01T00:00:00Z", Status: status,
	}
	rendered, err := p.Render("")
	require.NoError(t, err)
	_, err = pages.Write("review/"+id+".md", []byte(rendered), "")
	require.NoError(t, err)
}

type homeBody struct {
	Waiting struct {
		Total  int `json:"total"`
		ByKind []struct {
			Kind  string `json:"kind"`
			Count int    `json:"count"`
		} `json:"by_kind"`
		Proposals []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"proposals"`
	} `json:"waiting"`
	Attention []struct {
		Rule     string `json:"rule"`
		Sentence string `json:"sentence"`
		Count    int    `json:"count"`
		Pages    []struct {
			Scope string `json:"scope"`
			Slug  string `json:"slug"`
			Title string `json:"title"`
		} `json:"pages"`
	} `json:"attention"`
	Changes []struct {
		SHA        string `json:"sha"`
		Path       string `json:"path"`
		Message    string `json:"message"`
		AuthorWord string `json:"author_word"`
		When       string `json:"when"`
		Scope      string `json:"scope"`
		Slug       string `json:"slug"`
		Title      string `json:"title"`
	} `json:"changes"`
	Owner string `json:"owner"`
}

// TestHomeWaitingGroupsProposalsByKind pins "Waiting for you": review/*.md proposals grouped
// by kind, restricted to this caller's scopes and to still-pending proposals — a proposal
// already accepted or rejected is no longer "waiting" for anyone, and another tenant's
// proposal must never leak through a filesystem read that (unlike every SQL query in this
// codebase) carries no scope filter of its own.
func TestHomeWaitingGroupsProposalsByKind(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	writeProposal(t, pages, "p-new-1", "new_page", "work", "work/foo.md", "pending")
	writeProposal(t, pages, "p-new-2", "new_page", "work", "work/bar.md", "pending")
	writeProposal(t, pages, "p-update", "update", "work", "work/baz.md", "pending")
	writeProposal(t, pages, "p-other-scope", "new_page", "personal", "personal/qux.md", "pending")
	writeProposal(t, pages, "p-decided", "merge", "work", "work/done.md", "accepted")

	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	server := newHomeServer(t, q, pages)

	var body homeBody
	require.Equal(t, http.StatusOK, getJSON(t, server, "/api/home", &body))

	require.Equal(t, 3, body.Waiting.Total,
		"another scope's proposal and an already-decided proposal must not count")
	byKind := map[string]int{}
	for _, k := range body.Waiting.ByKind {
		byKind[k.Kind] = k.Count
	}
	require.Equal(t, 2, byKind["new_page"])
	require.Equal(t, 1, byKind["update"])
	require.NotContains(t, byKind, "merge", "a decided proposal must not be counted as waiting")
}

// TestHomeAttentionRendersSentences pins "Needs attention": lint findings aggregated by
// rule and rendered as the exact plain-English sentences 02-ui-design-v1.md section 5.1
// spells out, with correct singular/plural grammar at the actual count — not a number next
// to a rule name.
func TestHomeAttentionRendersSentences(t *testing.T) {
	pool := testutil.NewDB(t)
	q := store.NewQueries(pool, store.Scopes{"work"})
	ctx := t.Context()

	seed := func(uid, slug string) {
		require.NoError(t, q.UpsertDocument(ctx, store.Document{
			UID: uid, Slug: slug, Scope: "work", Type: "gotcha",
			Path: "work/" + slug + ".md", Title: "Title " + slug,
			Status: "active", Owner: "dhia", BodyMD: "body", BodyHash: "h-" + uid,
			GitVersion: "abc",
		}))
	}
	seed("d-oversize", "oversize-page")
	seed("d-dangling-1", "dangling-1")
	seed("d-dangling-2", "dangling-2")
	seed("d-headline", "headline-page")
	seed("d-mc-1", "mc-1")
	seed("d-mc-2", "mc-2")
	seed("d-mc-3", "mc-3")

	require.NoError(t, q.UpsertFinding(ctx, "d-oversize",
		store.Finding{Rule: "oversize", Severity: "warn", Detail: "oversize-page is 9001 tokens"}))
	require.NoError(t, q.UpsertFinding(ctx, "d-dangling-1",
		store.Finding{Rule: "dangling_ref", Severity: "warn", Detail: "dangling-1 -> missing-a"}))
	require.NoError(t, q.UpsertFinding(ctx, "d-dangling-2",
		store.Finding{Rule: "dangling_ref", Severity: "warn", Detail: "dangling-2 -> missing-b"}))
	require.NoError(t, q.UpsertFinding(ctx, "d-headline",
		store.Finding{Rule: "long_headline", Severity: "warn", Detail: "headline too long"}))
	for _, uid := range []string{"d-mc-1", "d-mc-2", "d-mc-3"} {
		require.NoError(t, q.UpsertFinding(ctx, uid,
			store.Finding{Rule: "missing_claims", Severity: "warn", Detail: uid + " has no claims"}))
	}

	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	server := newHomeServer(t, q, pages)

	var body homeBody
	require.Equal(t, http.StatusOK, getJSON(t, server, "/api/home", &body))

	byRule := map[string]string{}
	countByRule := map[string]int{}
	for _, a := range body.Attention {
		byRule[a.Rule] = a.Sentence
		countByRule[a.Rule] = a.Count
	}
	require.Equal(t, "1 page is too long to fit an agent's context", byRule["oversize"])
	require.Equal(t, "2 links point at pages that do not exist", byRule["dangling_ref"])
	require.Equal(t, "1 headline is longer than 15 words", byRule["long_headline"])
	require.Equal(t, "3 pages have no claims yet", byRule["missing_claims"])
	require.Equal(t, 2, countByRule["dangling_ref"])
}

// TestHomeAttentionExcludesSuggestSeverity is bug 3's first regression test: resolved_ref is
// the only rule written at suggest severity — bookkeeping for Task 15's link-recovery metric,
// filed for every link that already resolved cleanly — and it must never reach "Needs
// attention". A person is never meant to act on a link that already worked; showing one here
// would be exactly the kind of false signal this bug is about.
func TestHomeAttentionExcludesSuggestSeverity(t *testing.T) {
	pool := testutil.NewDB(t)
	q := store.NewQueries(pool, store.Scopes{"work"})
	ctx := t.Context()

	require.NoError(t, q.UpsertDocument(ctx, store.Document{
		UID: "d-ok", Slug: "ok-page", Scope: "work", Type: "gotcha",
		Path: "work/ok-page.md", Title: "OK page", Status: "active", Owner: "dhia",
		BodyMD: "body", BodyHash: "h-ok", GitVersion: "abc",
	}))
	require.NoError(t, q.UpsertFinding(ctx, "d-ok",
		store.Finding{Rule: "resolved_ref", Severity: "suggest", Detail: "some-target via alias"}))

	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	server := newHomeServer(t, q, pages)

	var body homeBody
	require.Equal(t, http.StatusOK, getJSON(t, server, "/api/home", &body))
	require.Empty(t, body.Attention,
		"a purely suggest-severity finding must produce no attention item at all")
}

// TestHomeAttentionExcludesCrossScopeDanglingRefs is bug 3's second regression test.
// vault.Resolve refuses to cross a scope boundary by design (04 section 14): a dangling_ref
// whose target exists under a different scope is that boundary working correctly, not a
// broken link the owner can fix by editing this vault, so it must not appear in "Needs
// attention" — only a genuinely missing target should.
func TestHomeAttentionExcludesCrossScopeDanglingRefs(t *testing.T) {
	pool := testutil.NewDB(t)
	q := store.NewQueries(pool, store.Scopes{"work", "personal"})
	ctx := t.Context()

	require.NoError(t, q.UpsertDocument(ctx, store.Document{
		UID: "d-src", Slug: "source-page", Scope: "work", Type: "gotcha",
		Path: "work/source-page.md", Title: "Source page", Status: "active", Owner: "dhia",
		BodyMD: "body", BodyHash: "h-src", GitVersion: "abc",
	}))
	// The cross-scope target: the ref names it, and it exists — just under "personal", not
	// the referring page's own "work" scope.
	require.NoError(t, q.UpsertDocument(ctx, store.Document{
		UID: "d-target", Slug: "elsewhere", Scope: "personal", Type: "gotcha",
		Path: "personal/elsewhere.md", Title: "Elsewhere", Status: "active", Owner: "dhia",
		BodyMD: "body", BodyHash: "h-target", GitVersion: "abc",
	}))
	// A second source page whose ref names no document anywhere: genuinely missing.
	require.NoError(t, q.UpsertDocument(ctx, store.Document{
		UID: "d-src2", Slug: "source-page-2", Scope: "work", Type: "gotcha",
		Path: "work/source-page-2.md", Title: "Source page 2", Status: "active", Owner: "dhia",
		BodyMD: "body", BodyHash: "h-src2", GitVersion: "abc",
	}))

	require.NoError(t, q.UpsertFinding(ctx, "d-src",
		store.Finding{Rule: "dangling_ref", Severity: "warn", Detail: "elsewhere"}))
	require.NoError(t, q.UpsertFinding(ctx, "d-src2",
		store.Finding{Rule: "dangling_ref", Severity: "warn", Detail: "truly-missing"}))

	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	server := newHomeServer(t, q, pages)

	var body homeBody
	require.Equal(t, http.StatusOK, getJSON(t, server, "/api/home", &body))

	found := false
	for _, a := range body.Attention {
		if a.Rule != "dangling_ref" {
			continue
		}
		found = true
		require.Equal(t, 1, a.Count,
			"the cross-scope ref must be excluded, leaving only the genuinely missing one")
		require.Equal(t, "1 link points at a page that does not exist", a.Sentence)
	}
	require.True(t, found, "the genuinely-missing dangling_ref must still surface")
}

// TestHomeAttentionIncludesUnparseablePages is bug 3's third regression test. A page that
// fails to parse never gets a document row, so FindingsByRule — which joins against
// documents — can never see it; before this fix such a page was invisible everywhere Home
// looks, however badly it failed. homeAttention merges it in from GlobalFindings instead.
func TestHomeAttentionIncludesUnparseablePages(t *testing.T) {
	pool := testutil.NewDB(t)
	q := store.NewQueries(pool, store.Scopes{"work"})
	ctx := t.Context()

	require.NoError(t, q.UpsertFinding(ctx, "", store.Finding{
		Rule: "unparseable", Severity: "error",
		Detail: "work/broken.md: yaml: line 3: mapping values are not allowed", Scope: "work",
	}))

	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	server := newHomeServer(t, q, pages)

	var body homeBody
	require.Equal(t, http.StatusOK, getJSON(t, server, "/api/home", &body))

	found := false
	for _, a := range body.Attention {
		if a.Rule != "unparseable" {
			continue
		}
		found = true
		require.Equal(t, 1, a.Count)
		require.Equal(t, "1 page could not be parsed", a.Sentence)
	}
	require.True(t, found, "an unparseable page must surface in Needs attention, not vanish silently")
}

// TestHomeChangesMapsAuthorWordAndFiltersScope pins "Changed recently": the merged, deduped
// commit list with author resolved to "you" / "a connector" / "an agent" by commit-message
// prefix, since compile and the importer both commit as the identical git author
// "balise <balise@localhost>" and are only distinguishable by their message.
func TestHomeChangesMapsAuthorWordAndFiltersScope(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)

	_, err = pages.Write("work/a.md", []byte("body a"), "")
	require.NoError(t, err)
	_, err = pages.Commit([]store.Change{{Path: "work/b.md", Data: []byte("body b")}},
		"balise <balise@localhost>", "compile: extract_claims")
	require.NoError(t, err)
	_, err = pages.Commit([]store.Change{{Path: "work/c.md", Data: []byte("body c")}},
		"balise <balise@localhost>", "balise: import claude-code memory")
	require.NoError(t, err)
	_, err = pages.Write("personal/d.md", []byte("body d"), "")
	require.NoError(t, err)

	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work"})
	server := newHomeServer(t, q, pages)

	var body homeBody
	require.Equal(t, http.StatusOK, getJSON(t, server, "/api/home", &body))

	byPath := map[string]string{}
	for _, c := range body.Changes {
		byPath[c.Path] = c.AuthorWord
	}
	require.Equal(t, "you", byPath["work/a.md"])
	require.Equal(t, "an agent", byPath["work/b.md"])
	require.Equal(t, "a connector", byPath["work/c.md"])
	require.NotContains(t, byPath, "personal/d.md", "another scope's commit must not appear")
}

// TestHomeChangesResolvesNavigableRef pins the "Changed recently" navigation gap: a change
// whose path resolves to an indexed document gets a scope/slug/title so the frontend can open
// it, while a change with no matching document (a real tracked file the indexer never turned
// into a page, exactly like the live vault's memory-import commits) gets none of the three,
// rather than a scope/slug pair that would 404.
func TestHomeChangesResolvesNavigableRef(t *testing.T) {
	pool := testutil.NewDB(t)
	q := store.NewQueries(pool, store.Scopes{"work"})
	ctx := t.Context()

	require.NoError(t, q.UpsertDocument(ctx, store.Document{
		UID: "d-a", Slug: "a", Scope: "work", Type: "gotcha",
		Path: "work/a.md", Title: "Page A", Status: "active", Owner: "dhia",
		BodyMD: "body", BodyHash: "h-a", GitVersion: "abc",
	}))

	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	_, err = pages.Write("work/a.md", []byte("body a"), "")
	require.NoError(t, err)
	// Not indexed as a document — the memory-import case: a real tracked file with no row in
	// `documents`.
	_, err = pages.Write("work/memory/claude-code/2026-09-18.md", []byte("remember: something"), "")
	require.NoError(t, err)

	server := newHomeServer(t, q, pages)

	var body homeBody
	require.Equal(t, http.StatusOK, getJSON(t, server, "/api/home", &body))

	byPath := map[string]struct {
		Scope, Slug, Title string
	}{}
	for _, c := range body.Changes {
		byPath[c.Path] = struct{ Scope, Slug, Title string }{c.Scope, c.Slug, c.Title}
	}
	require.Equal(t, "work", byPath["work/a.md"].Scope)
	require.Equal(t, "a", byPath["work/a.md"].Slug)
	require.Equal(t, "Page A", byPath["work/a.md"].Title)

	unindexed := byPath["work/memory/claude-code/2026-09-18.md"]
	require.Empty(t, unindexed.Scope, "an unindexed path must not get a scope that leads nowhere")
	require.Empty(t, unindexed.Slug, "an unindexed path must not get a slug that leads nowhere")
	require.Empty(t, unindexed.Title)
}

// TestHomeOwnerIsModeOfPageOwners pins the greeting name as computed data, not a hardcoded
// string: it is the most common non-empty Owner across every indexed page, so a vault with a
// different owner (or a mix, with ties broken alphabetically) gets an honest answer.
func TestHomeOwnerIsModeOfPageOwners(t *testing.T) {
	pool := testutil.NewDB(t)
	q := store.NewQueries(pool, store.Scopes{"work"})
	ctx := t.Context()

	seed := func(uid, owner string) {
		require.NoError(t, q.UpsertDocument(ctx, store.Document{
			UID: uid, Slug: uid, Scope: "work", Type: "gotcha",
			Path: "work/" + uid + ".md", Title: "Title " + uid,
			Status: "active", Owner: owner, BodyMD: "body", BodyHash: "h-" + uid,
			GitVersion: "abc",
		}))
	}
	seed("d-1", "dhia")
	seed("d-2", "dhia")
	seed("d-3", "sam")

	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	server := newHomeServer(t, q, pages)

	var body homeBody
	require.Equal(t, http.StatusOK, getJSON(t, server, "/api/home", &body))
	require.Equal(t, "dhia", body.Owner)
}
