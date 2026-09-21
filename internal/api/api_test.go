package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dhia/balise/internal/api"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/stretchr/testify/require"
)

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work", "client-globex"})
	spaces, err := registry.LoadSpaces("../../defaults/spaces.yaml")
	require.NoError(t, err)
	order, err := registry.LoadOrder("../../defaults/order.yaml")
	require.NoError(t, err)
	server := httptest.NewServer(api.New(q, newGitVault(t), spaces, order, ""))
	t.Cleanup(server.Close)
	return server
}

// newGitVault gives api.New a real, empty git-backed PageStore rooted in a scratch
// directory — every test in this file other than home_test.go's cares only about Queries,
// but api.New now needs a PageStore too since Home reads proposals and commit history
// straight off the vault (02-ui-design-v1.md section 5.1).
func newGitVault(t *testing.T) store.PageStore {
	t.Helper()
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	return pages
}

func getJSON(t *testing.T, server *httptest.Server, path string, into any) int {
	t.Helper()
	resp, err := http.Get(server.URL + path)
	require.NoError(t, err)
	defer resp.Body.Close()
	if into != nil && resp.StatusCode == http.StatusOK {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(into))
	}
	return resp.StatusCode
}

func TestTreeReturnsSpacesAndTypeCounts(t *testing.T) {
	var body struct {
		Spaces []struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		} `json:"spaces"`
		Types           []map[string]any `json:"types"`
		HistoricalCount int              `json:"historical_count"`
	}
	require.Equal(t, http.StatusOK, getJSON(t, newServer(t), "/api/tree", &body))
	require.NotNil(t, body.Spaces)
}

// TestTreeSurfacesAScopeWithNoDeclaredSpace pins the fix for how the 13 colo datacenter
// pages went invisible: defaults/spaces.yaml only declares work and client-globex (see
// newServer above), so a scope holding pages but named in no declared space would
// otherwise never appear in the tree a UI browses. /api/tree must synthesize a fallback
// entry for any such orphaned scope rather than silently dropping it.
func TestTreeSurfacesAScopeWithNoDeclaredSpace(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work", "client-umbrella"})
	require.NoError(t, q.UpsertDocument(t.Context(), store.Document{
		UID: "u-orphan", Slug: "orphan-scope-page", Scope: "client-umbrella",
		Type: "gotcha", Path: "client-umbrella/gotchas/orphan-scope-page.md",
		Title: "Orphaned scope page", Status: "active", Owner: "dhia",
		BodyMD: "body", BodyHash: "h1", GitVersion: "abc",
	}))

	spaces, err := registry.LoadSpaces("../../defaults/spaces.yaml")
	require.NoError(t, err)
	order, err := registry.LoadOrder("../../defaults/order.yaml")
	require.NoError(t, err)
	server := httptest.NewServer(api.New(q, newGitVault(t), spaces, order, ""))
	t.Cleanup(server.Close)

	var body struct {
		Spaces []struct {
			Name  string `json:"name"`
			Scope string `json:"scope"`
			Count int    `json:"count"`
		} `json:"spaces"`
	}
	require.Equal(t, http.StatusOK, getJSON(t, server, "/api/tree", &body))

	var found bool
	for _, space := range body.Spaces {
		if space.Scope == "client-umbrella" {
			found = true
			require.Equal(t, 1, space.Count)
		}
	}
	require.True(t, found, "a scope with pages but no declared space must still appear in the tree")
}

func TestPagesAreGroupedByTypeInAgentOrder(t *testing.T) {
	var body struct {
		Groups []struct {
			Type string `json:"type"`
		} `json:"groups"`
	}
	require.Equal(t, http.StatusOK, getJSON(t, newServer(t), "/api/pages", &body))
	// With no seed data the list is empty; ordering is asserted in the acceptance run.
	require.NotNil(t, body.Groups)
}

func TestUnknownPageIs404(t *testing.T) {
	require.Equal(t, http.StatusNotFound, getJSON(t, newServer(t), "/api/pages/work/nope", nil))
}

func TestOutOfScopePageIs404NotForbidden(t *testing.T) {
	// 04 section 12: never distinguish "exists elsewhere" from "does not exist".
	require.Equal(t, http.StatusNotFound,
		getJSON(t, newServer(t), "/api/pages/personal/personal-note", nil))
}

// TestPageIsServedUnderItsOwnScope pins the route's half of the scope-qualified page lookup.
// documents is unique (scope, slug), so one slug can name a different customer's page in each
// scope; GET /api/pages/{slug} keyed on slug alone and served whichever scope sorted first, so
// a user opening the client-globex row got the work page's body, claims, relations and lint — and
// the panel binds `space`, not `scope`, so nothing on screen contradicted it.
func TestPageIsServedUnderItsOwnScope(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work", "client-globex"})
	insert := func(scope, body string) {
		require.NoError(t, q.UpsertDocument(t.Context(), store.Document{
			UID: "u-" + scope, Slug: "expressroute", Scope: scope, Type: "gotcha",
			Path: scope + "/gotchas/expressroute.md", Title: scope + " ExpressRoute",
			Status: "active", Owner: "dhia", BodyMD: body, BodyHash: "h-" + scope,
			GitVersion: "abc",
		}))
	}
	insert("work", "the work body")
	insert("client-globex", "Globex's confidential body")

	spaces, err := registry.LoadSpaces("../../defaults/spaces.yaml")
	require.NoError(t, err)
	order, err := registry.LoadOrder("../../defaults/order.yaml")
	require.NoError(t, err)
	server := httptest.NewServer(api.New(q, newGitVault(t), spaces, order, ""))
	t.Cleanup(server.Close)

	var body struct {
		Scope  string `json:"scope"`
		BodyMD string `json:"body_md"`
	}
	require.Equal(t, http.StatusOK,
		getJSON(t, server, "/api/pages/client-globex/expressroute", &body))
	require.Equal(t, "client-globex", body.Scope)
	require.Equal(t, "Globex's confidential body", body.BodyMD)

	require.Equal(t, http.StatusOK, getJSON(t, server, "/api/pages/work/expressroute", &body))
	require.Equal(t, "work", body.Scope)
	require.Equal(t, "the work body", body.BodyMD)
}

// TestTreeSurfacesGlobalFindings pins the read path for a finding that names no document. An
// unparseable page has no uid, so /api/pages/{scope}/{slug} can never carry its finding and
// GetFindings (which filters on doc_uid) can never return it: without a home on the tree the
// page was simply invisible with nothing saying why.
func TestTreeSurfacesGlobalFindings(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work", "client-globex"})
	require.NoError(t, q.UpsertFinding(t.Context(), "", store.Finding{
		Rule: "unparseable", Severity: "error",
		Detail: "work/gotchas/broken.md: yaml: line 2: found a tab character", Scope: "work"}))

	spaces, err := registry.LoadSpaces("../../defaults/spaces.yaml")
	require.NoError(t, err)
	order, err := registry.LoadOrder("../../defaults/order.yaml")
	require.NoError(t, err)
	server := httptest.NewServer(api.New(q, newGitVault(t), spaces, order, ""))
	t.Cleanup(server.Close)

	var body struct {
		GlobalLint []struct {
			Rule   string `json:"rule"`
			Detail string `json:"detail"`
			Scope  string `json:"scope"`
		} `json:"global_lint"`
	}
	require.Equal(t, http.StatusOK, getJSON(t, server, "/api/tree", &body))
	require.Len(t, body.GlobalLint, 1)
	require.Equal(t, "unparseable", body.GlobalLint[0].Rule)
	require.Contains(t, body.GlobalLint[0].Detail, "work/gotchas/broken.md")
	require.Equal(t, "work", body.GlobalLint[0].Scope)
}

func TestSearchReportsCoverage(t *testing.T) {
	var body struct {
		Hits     []any  `json:"hits"`
		Coverage string `json:"coverage"`
	}
	require.Equal(t, http.StatusOK, getJSON(t, newServer(t), "/api/search?q=gateway", &body))
	require.Contains(t, []string{"ok", "low"}, body.Coverage)
}

func TestSearchWithNoMatchIsLowCoverage(t *testing.T) {
	var body struct {
		Hits     []any  `json:"hits"`
		Coverage string `json:"coverage"`
	}
	require.Equal(t, http.StatusOK,
		getJSON(t, newServer(t), "/api/search?q=quarterly+revenue+forecast", &body))
	require.Equal(t, "low", body.Coverage)
	require.Empty(t, body.Hits)
}

func TestSearchWithoutAQueryIs400(t *testing.T) {
	require.Equal(t, http.StatusBadRequest, getJSON(t, newServer(t), "/api/search", nil))
}

func TestGraphReturnsNodesAndEdges(t *testing.T) {
	var body struct {
		Nodes []any `json:"nodes"`
		Edges []any `json:"edges"`
	}
	require.Equal(t, http.StatusOK, getJSON(t, newServer(t), "/api/graph", &body))
	require.NotNil(t, body.Nodes)
	require.NotNil(t, body.Edges)
}

// spaceNode mirrors the tree's recursive wire shape so nested space nodes can be found by
// name in a test.
type spaceNode struct {
	Name     string      `json:"name"`
	Scope    string      `json:"scope"`
	Count    int         `json:"count"`
	Children []spaceNode `json:"children"`
}

func findSpace(nodes []spaceNode, name string) *spaceNode {
	for i := range nodes {
		if nodes[i].Name == name {
			return &nodes[i]
		}
	}
	return nil
}

// loadSpacesFromYAML writes the given spaces.yaml content to a temp file and loads it. The two
// nested-count regression tests below pin the tree-counting algorithm's behavior (each space
// counts pages matching its own, possibly inherited, filter — never a shared-by-name or
// whole-scope total) against a fixed, minimal tree shape of their own, rather than against
// defaults/spaces.yaml. That file's real content is expected to evolve for product reasons —
// e.g. its top-level spaces were renamed from Work/Globex CNS to Platform/Globex (see its own comment) —
// and such a rename must not silently invalidate a counting-algorithm regression test.
func loadSpacesFromYAML(t *testing.T, content string) *registry.Spaces {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spaces.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	spaces, err := registry.LoadSpaces(path)
	require.NoError(t, err)
	return spaces
}

// TestTreeNestedSpaceCountReflectsItsOwnFilter pins the fix for spaceJSON reporting every
// space's count as byScope[space.Scope] — the whole scope's total — instead of the count of
// pages actually matching that space's own (possibly inherited) tag filters. The fixture tree
// declares Work (scope work, no filter) > Azure (vendor/azure/**) > ExpressRoute
// (vendor/azure/expressroute), so seeding pages with progressively narrower tags must produce
// strictly decreasing counts walking down the tree, not three copies of the same scope total.
func TestTreeNestedSpaceCountReflectsItsOwnFilter(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work", "client-globex"})
	insert := func(slug string, tags []string) {
		require.NoError(t, q.UpsertDocument(t.Context(), store.Document{
			UID: "u-" + slug, Slug: slug, Scope: "work", Type: "gotcha",
			Path: "work/gotchas/" + slug + ".md", Title: slug, Status: "active",
			Owner: "dhia", Tags: tags, BodyMD: "body", BodyHash: "h-" + slug, GitVersion: "abc",
		}))
	}
	// Matches only Work: no vendor/azure tag at all.
	insert("plain-1", []string{"layer.network"})
	insert("plain-2", []string{"layer.network"})
	insert("plain-3", []string{"layer.observability"})
	// Matches Work and Azure, but not the narrower ExpressRoute filter.
	insert("azure-1", []string{"vendor.azure.loadbalancer"})
	insert("azure-2", []string{"vendor.azure.vnet"})
	// Matches Work, Azure, and ExpressRoute.
	insert("er-1", []string{"vendor.azure.expressroute"})

	spaces := loadSpacesFromYAML(t, `
- name: Work
  scope: work
  filter: {tags: []}
  children:
    - name: Azure
      filter: {tags: ["vendor/azure/**"]}
      children:
        - name: ExpressRoute
          filter: {tags: ["vendor/azure/expressroute"]}
`)
	order, err := registry.LoadOrder("../../defaults/order.yaml")
	require.NoError(t, err)
	server := httptest.NewServer(api.New(q, newGitVault(t), spaces, order, ""))
	t.Cleanup(server.Close)

	var body struct {
		Spaces []spaceNode `json:"spaces"`
	}
	require.Equal(t, http.StatusOK, getJSON(t, server, "/api/tree", &body))

	work := findSpace(body.Spaces, "Work")
	require.NotNil(t, work, "Work space must be present")
	azure := findSpace(work.Children, "Azure")
	require.NotNil(t, azure, "Azure must nest under Work")
	expressRoute := findSpace(azure.Children, "ExpressRoute")
	require.NotNil(t, expressRoute, "ExpressRoute must nest under Azure")

	require.Equal(t, 6, work.Count, "Work counts every page in the work scope")
	require.Equal(t, 3, azure.Count, "Azure counts only pages tagged vendor/azure/**")
	require.Equal(t, 1, expressRoute.Count, "ExpressRoute counts only its own narrower tag")

	require.Less(t, expressRoute.Count, azure.Count,
		"a narrower nested space must count strictly fewer pages than its parent")
	require.Less(t, azure.Count, work.Count,
		"a narrower nested space must count strictly fewer pages than its parent")
}

// TestTreeSameNamedSpaceInDifferentScopesCountsIndependently pins a second, distinct bug
// that a first fix for TestTreeNestedSpaceCountReflectsItsOwnFilter introduced: an
// intermediate counts map keyed only by bare space Name. The fixture tree declares two
// different spaces both named "Azure" — Work > Azure (scope work) and Globex CNS > Azure
// (scope client-globex) — so a running-count map keyed by Name alone conflates them: every
// row matching either Azure node incremented the same counts["Azure"] entry, and both
// nodes then read back that single, inflated total. That bug only surfaces when rows in
// *both* scopes exist in the same request, which is why this test seeds both work and
// client-globex pages, unlike TestTreeNestedSpaceCountReflectsItsOwnFilter above (work only).
func TestTreeSameNamedSpaceInDifferentScopesCountsIndependently(t *testing.T) {
	q := store.NewQueries(testutil.NewDB(t), store.Scopes{"work", "client-globex"})
	insertWork := func(slug string, tags []string) {
		require.NoError(t, q.UpsertDocument(t.Context(), store.Document{
			UID: "u-" + slug, Slug: slug, Scope: "work", Type: "gotcha",
			Path: "work/gotchas/" + slug + ".md", Title: slug, Status: "active",
			Owner: "dhia", Tags: tags, BodyMD: "body", BodyHash: "h-" + slug, GitVersion: "abc",
		}))
	}
	insertGlobex := func(slug string, tags []string) {
		require.NoError(t, q.UpsertDocument(t.Context(), store.Document{
			UID: "u-" + slug, Slug: slug, Scope: "client-globex", Type: "gotcha",
			Path: "client-globex/gotchas/" + slug + ".md", Title: slug, Status: "active",
			Owner: "dhia", Tags: tags, BodyMD: "body", BodyHash: "h-" + slug, GitVersion: "abc",
		}))
	}
	// Work scope: 3 pages total, 2 under Work>Azure.
	insertWork("w-plain", []string{"layer.network"})
	insertWork("w-azure-1", []string{"vendor.azure.vnet"})
	insertWork("w-azure-2", []string{"vendor.azure.loadbalancer"})
	// client-globex scope: 4 pages total (all tagged customer/globex so Globex CNS matches them),
	// only 1 under Globex CNS>Azure — deliberately a different count from Work>Azure's 2,
	// so a name-keyed collision (which would make both nodes read the same combined
	// total of 3) is distinguishable from the correct, independent counts of 2 and 1.
	insertGlobex("globex-plain-1", []string{"customer.globex.billing"})
	insertGlobex("globex-plain-2", []string{"customer.globex.billing"})
	insertGlobex("globex-plain-3", []string{"customer.globex.onboarding"})
	insertGlobex("globex-azure", []string{"customer.globex.infra", "vendor.azure.vnet"})

	spaces := loadSpacesFromYAML(t, `
- name: Work
  scope: work
  filter: {tags: []}
  children:
    - name: Azure
      filter: {tags: ["vendor/azure/**"]}

- name: Globex CNS
  scope: client-globex
  filter: {tags: ["customer/globex/**"]}
  children:
    - name: Azure
      filter: {tags: ["vendor/azure/**"]}
`)
	order, err := registry.LoadOrder("../../defaults/order.yaml")
	require.NoError(t, err)
	server := httptest.NewServer(api.New(q, newGitVault(t), spaces, order, ""))
	t.Cleanup(server.Close)

	var body struct {
		Spaces []spaceNode `json:"spaces"`
	}
	require.Equal(t, http.StatusOK, getJSON(t, server, "/api/tree", &body))

	work := findSpace(body.Spaces, "Work")
	require.NotNil(t, work, "Work space must be present")
	workAzure := findSpace(work.Children, "Azure")
	require.NotNil(t, workAzure, "Azure must nest under Work")

	globexCNS := findSpace(body.Spaces, "Globex CNS")
	require.NotNil(t, globexCNS, "Globex CNS space must be present")
	globexAzure := findSpace(globexCNS.Children, "Azure")
	require.NotNil(t, globexAzure, "Azure must nest under Globex CNS")

	require.Equal(t, 3, work.Count, "Work counts every page in the work scope")
	require.Equal(t, 2, workAzure.Count, "Work>Azure counts only its own scope's azure pages")
	require.Equal(t, 4, globexCNS.Count, "Globex CNS counts every page tagged customer/globex/**")
	require.Equal(t, 1, globexAzure.Count, "Globex CNS>Azure counts only its own scope's azure page")

	require.LessOrEqual(t, globexAzure.Count, globexCNS.Count,
		"a child space can never count more pages than its own parent")
	require.LessOrEqual(t, workAzure.Count, work.Count,
		"a child space can never count more pages than its own parent")
	require.NotEqual(t, workAzure.Count, globexAzure.Count,
		"the two same-named Azure nodes must not share a combined, conflated count")
}
