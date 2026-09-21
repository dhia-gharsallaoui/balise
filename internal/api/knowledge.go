package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
)

type claimJSON struct {
	Text   string  `json:"text"`
	Status string  `json:"status"`
	AsOf   *string `json:"as_of"`
	// Span is [start,end] byte offsets into body_md, or nil when the claim carries none.
	// spec §7 lists it only on the /api/pages/{slug} claims, but the same claimJSON backs
	// both responses (handlePage merges toPageJSON's output wholesale) — omitempty keeps it
	// out of the /api/pages listing's wire shape whenever it is absent.
	Span *[2]int `json:"span,omitempty"`
}

type pageJSON struct {
	Slug   string `json:"slug"`
	UID    string `json:"uid"`
	Title  string `json:"title"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Scope  string `json:"scope"`
	// Space is the deepest declared space (defaults/spaces.yaml) whose scope and tag
	// filters match this page, or "" when none do. 04 §13's tree/pages views group by
	// space as well as by type — spec §7 lists it on both the list and detail rows.
	Space       string      `json:"space"`
	Historical  bool        `json:"historical"`
	ClaimsCount int         `json:"claims_count"`
	Tokens      int         `json:"tokens"`
	Owner       string      `json:"owner"`
	Tags        []string    `json:"tags"`
	Claims      []claimJSON `json:"claims"`
}

func (s *server) handleTree(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q().ListPages(r.Context(), store.PageFilter{IncludeHistorical: true})
	if err != nil {
		writeServerError(w, r, err)
		return
	}

	counts := map[string]int{}
	byScope := map[string]int{}
	historical := 0
	for _, row := range rows {
		counts[row.Type]++
		byScope[row.Scope]++
		if row.Historical {
			historical++
		}
	}

	types := make([]map[string]any, 0, len(counts))
	for name := range counts {
		types = append(types, map[string]any{"name": name, "count": counts[name]})
	}
	sort.Slice(types, func(i, j int) bool {
		return s.rank(types[i]["name"].(string)) < s.rank(types[j]["name"].(string))
	})

	tree := s.spaces.Tree()
	tagged := taggedRows(rows)
	spaces := make([]map[string]any, 0, len(tree))
	for _, space := range tree {
		spaces = append(spaces, spaceJSON(space, tagged))
	}
	// A scope with pages but no declared space in defaults/spaces.yaml would otherwise be
	// unreachable by browsing — that is exactly how the 13 colo datacenter pages went
	// invisible before spaces.yaml grew a client-globex entry but nothing for other scopes.
	// 04 section 3.4's auto_hierarchy is the spec's eventual, richer answer; a flat
	// fallback entry per orphaned scope is enough to guarantee a scope can never again
	// silently disappear from the tree.
	for _, scope := range sortedUncoveredScopes(tree, byScope) {
		spaces = append(spaces, map[string]any{
			"name": scope, "scope": scope, "count": byScope[scope], "children": []map[string]any{},
		})
	}

	// Global findings name no document, so /api/pages/{scope}/{slug} can never show them —
	// an unparseable page has no uid to look one up by. Without a home on the tree they were
	// written and never read: the page simply did not appear, and nothing said why.
	globals, err := s.q().GlobalFindings(r.Context())
	if err != nil {
		writeServerError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"spaces": spaces, "types": types, "historical_count": historical,
		"global_lint": findingsJSON(globals),
	})
}

// sortedUncoveredScopes returns, sorted, every scope in byScope that no space in tree (at
// any depth) declares — the scopes handleTree's fallback entries must synthesize so they
// are never silently unreachable by browsing.
func sortedUncoveredScopes(tree []registry.Space, byScope map[string]int) []string {
	covered := map[string]bool{}
	var walk func([]registry.Space)
	walk = func(nodes []registry.Space) {
		for _, node := range nodes {
			covered[node.Scope] = true
			walk(node.Children)
		}
	}
	walk(tree)

	var uncovered []string
	for scope := range byScope {
		if !covered[scope] {
			uncovered = append(uncovered, scope)
		}
	}
	sort.Strings(uncovered)
	return uncovered
}

// spaceJSON renders space and its children, each with its own filter-matched count.
// The count is computed directly against this node's Scope/TagFilters (countMatching)
// rather than through any map keyed by space name: defaults/spaces.yaml declares two
// distinct spaces named "Azure" (Work>Azure, scope=work; Globex CNS>Azure, scope=client-globex),
// so a running-count map keyed by bare Name would conflate them, and a deeply nested
// space (e.g. Work/Azure/ExpressRoute) would report a wrong, less-specific total. Prior
// versions of this function had exactly that bug in two different forms: first
// reporting byScope[space.Scope] (the whole scope's total) for every space in it, and
// then — even after switching to a per-node Matches check — still accumulating into a
// map[string]int keyed by node.Name, which the two same-named Azure branches collided
// on. Neither map exists now; each node's count is a fresh scan of rows.
func spaceJSON(space registry.Space, rows []taggedRow) map[string]any {
	children := make([]map[string]any, 0, len(space.Children))
	for _, child := range space.Children {
		children = append(children, spaceJSON(child, rows))
	}
	return map[string]any{
		"name": space.Name, "scope": space.Scope,
		"count": countMatching(space, rows), "children": children,
	}
}

// countMatching counts the rows whose scope matches space and whose tags satisfy every
// one of space's own filters — registry.Space.Matches already implements that filter
// test and is tested. See spaceJSON's comment for why this scans rows fresh per node
// instead of accumulating into any name-keyed map.
func countMatching(space registry.Space, rows []taggedRow) int {
	count := 0
	for _, row := range rows {
		if row.scope == space.Scope && space.Matches(row.tags) {
			count++
		}
	}
	return count
}

// taggedRow is a page row's scope and tags, with tags already converted from Postgres's
// ltree dot form to the slash form defaults/spaces.yaml's filters and the UI both use —
// precomputed once via taggedRows so spaceJSON's recursive per-node scan (countMatching)
// doesn't reconvert the same row's tags once per space in the tree.
type taggedRow struct {
	scope string
	tags  []string
}

func taggedRows(rows []store.PageRow) []taggedRow {
	out := make([]taggedRow, len(rows))
	for i, row := range rows {
		out[i] = taggedRow{scope: row.Scope, tags: ltreeTagsToSlash(row.Tags)}
	}
	return out
}

// ltreeTagsToSlash converts tags from Postgres's ltree form (dots) to the slash form
// defaults/spaces.yaml's filters and the UI both use.
func ltreeTagsToSlash(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		out = append(out, strings.ReplaceAll(tag, ".", "/"))
	}
	return out
}

func (s *server) handlePages(w http.ResponseWriter, r *http.Request) {
	filter := store.PageFilter{
		IncludeHistorical: r.URL.Query().Get("historical") == "true",
	}
	if typeName := r.URL.Query().Get("type"); typeName != "" {
		filter.Types = []string{typeName}
	}
	if tags := r.URL.Query().Get("tags"); tags != "" {
		filter.Tags = strings.Split(strings.ReplaceAll(tags, "/", "."), ",")
	}

	rows, err := s.q().ListPages(r.Context(), filter)
	if err != nil {
		writeServerError(w, r, err)
		return
	}

	grouped := map[string][]pageJSON{}
	for _, row := range rows {
		grouped[row.Type] = append(grouped[row.Type], toPageJSON(row, s.spaces))
	}
	names := make([]string, 0, len(grouped))
	for name := range grouped {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return s.rank(names[i]) < s.rank(names[j]) })

	groups := make([]map[string]any, 0, len(names))
	for _, name := range names {
		groups = append(groups, map[string]any{
			"type": name, "count": len(grouped[name]), "pages": grouped[name],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups, "total": len(rows)})
}

func (s *server) handlePage(w http.ResponseWriter, r *http.Request) {
	page, err := s.q().GetPage(r.Context(), r.PathValue("scope"), r.PathValue("slug"))
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	if page == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	relations, backlinks, err := s.q().GetRelations(r.Context(), page.UID)
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	findings, err := s.q().GetFindings(r.Context(), page.UID)
	if err != nil {
		writeServerError(w, r, err)
		return
	}

	body := map[string]any{
		"body_md": page.BodyMD, "tokens": page.Tokens,
		"relations": relationsJSON(relations), "backlinks": relatedListJSON(backlinks),
		"sources": []string{}, "lint": findingsJSON(findings),
		"last_verified": formatDate(page.LastVerified),
	}
	for key, value := range structToMap(toPageJSON(*page, s.spaces)) {
		body[key] = value
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *server) handleGraph(w http.ResponseWriter, r *http.Request) {
	var tags []string
	if raw := r.URL.Query().Get("tags"); raw != "" {
		tags = strings.Split(strings.ReplaceAll(raw, "/", "."), ",")
	}
	nodes, edges, err := s.q().Graph(r.Context(), tags, 200)
	if err != nil {
		writeServerError(w, r, err)
		return
	}

	nodeJSON := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		nodeJSON = append(nodeJSON, map[string]any{
			"uid": node.UID, "slug": node.Slug, "title": node.Title,
			"type": node.Type, "scope": node.Scope,
		})
	}
	edgeJSON := make([]map[string]any, 0, len(edges))
	for _, edge := range edges {
		edgeJSON = append(edgeJSON, map[string]any{
			"from_uid": edge.FromUID, "to_uid": edge.ToUID, "kind": edge.Kind,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodeJSON, "edges": edgeJSON})
}

// rank delegates to the loaded order so the API names no type either.
func (s *server) rank(typeName string) int { return s.order.Rank(typeName) }

// toPageJSON converts a stored row into the shape the UI renders. Tags come back from
// Postgres in ltree form (dots); the UI shows slashes.
func toPageJSON(row store.PageRow, spaces *registry.Spaces) pageJSON {
	tags := ltreeTagsToSlash(row.Tags)
	claims := make([]claimJSON, 0, len(row.Claims))
	for _, claim := range row.Claims {
		claims = append(claims, claimJSON{
			Text: claim.Text, Status: claim.Status, AsOf: formatDate(claim.AsOf),
			Span: spanJSON(claim),
		})
	}
	return pageJSON{
		Slug: row.Slug, UID: row.UID, Title: row.Title, Type: row.Type,
		Status: row.Status, Scope: row.Scope, Space: deepestSpace(spaces, row.Scope, tags),
		Historical: row.Historical, ClaimsCount: row.ClaimsCount, Tokens: row.Tokens,
		Owner: row.Owner, Tags: tags, Claims: claims,
	}
}

// spanJSON reports a claim's [start,end] offsets, or nil when either is unset.
func spanJSON(claim store.Claim) *[2]int {
	if claim.SpanStart == nil || claim.SpanEnd == nil {
		return nil
	}
	return &[2]int{*claim.SpanStart, *claim.SpanEnd}
}

// deepestSpace walks the declared space tree (defaults/spaces.yaml) and returns the name of
// the most specific space that shares the page's scope and whose tag filters the page's tags
// satisfy, or "" when none match. A child's filters always include its parent's (LoadSpaces
// prepends inherited filters), so a match can only get more specific walking down — there is
// no need to keep searching a branch once a node fails to match.
//
// Matching goes through each node's own Matches(tags), not registry.Spaces.Matches(name,
// tags): two different branches can declare a space with the same name (Work>Azure and
// Globex CNS>Azure both in defaults/spaces.yaml), and a name-keyed lookup cannot tell them apart.
func deepestSpace(spaces *registry.Spaces, scope string, tags []string) string {
	best := ""
	var walk func(nodes []registry.Space)
	walk = func(nodes []registry.Space) {
		for _, node := range nodes {
			if node.Scope != scope || !node.Matches(tags) {
				continue
			}
			best = node.Name
			walk(node.Children)
		}
	}
	walk(spaces.Tree())
	return best
}

// formatDate renders a nullable date as YYYY-MM-DD, or null.
func formatDate(when *time.Time) *string {
	if when == nil {
		return nil
	}
	formatted := when.Format("2006-01-02")
	return &formatted
}

// structToMap round-trips a value through encoding/json so the detail response can carry
// every pageJSON field alongside the extras the list response does not have.
func structToMap(value any) map[string]any {
	body, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return map[string]any{}
	}
	return out
}

// relatedJSON and findingJSON give store.Related and store.Finding lowercase wire keys.
// Neither store type carries json tags (they were never meant to be marshalled directly),
// so encoding them as-is would emit "Slug"/"Title"/"Status"/"Rule"/... — spec §7 promises
// lowercase keys throughout, and the frontend binds to them exactly.
type relatedJSON struct {
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type findingJSON struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Detail   string `json:"detail"`
	Action   string `json:"action"`
	// Scope is set only on a global finding (one attached to no document), which carries
	// its own scope because it has no document row to inherit one from. omitempty keeps
	// the per-page lint wire shape exactly as it was.
	Scope string `json:"scope,omitempty"`
}

// relationsJSON converts every role's neighbour list, and reports [] not null for the map
// itself and each of its lists — the UI maps over both directly.
func relationsJSON(in map[string][]store.Related) map[string][]relatedJSON {
	out := map[string][]relatedJSON{}
	for role, related := range in {
		out[role] = relatedListJSON(related)
	}
	return out
}

func relatedListJSON(in []store.Related) []relatedJSON {
	out := make([]relatedJSON, 0, len(in))
	for _, related := range in {
		out = append(out, relatedJSON{Slug: related.Slug, Title: related.Title, Status: related.Status})
	}
	return out
}

func findingsJSON(in []store.Finding) []findingJSON {
	out := make([]findingJSON, 0, len(in))
	for _, finding := range in {
		out = append(out, findingJSON{
			Rule: finding.Rule, Severity: finding.Severity,
			Detail: finding.Detail, Action: finding.Action, Scope: finding.Scope,
		})
	}
	return out
}
