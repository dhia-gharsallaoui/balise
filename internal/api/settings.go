package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"gopkg.in/yaml.v3"

	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
)

// This file is the Settings screen's read side (02-ui-design-v1.md section 5.6; layout from
// /tmp/balise-design/knowledge-v3.html lines 480-610). Every number here is real: it comes
// from the same registry loaders and store queries the rest of the app already trusts, never
// a mockup placeholder. Two sections the mockup shows do not exist in this build and are not
// approximated: Export (no redaction engine anywhere in this codebase — a preview that does
// not really redact would be the most dangerous thing on this screen) and every per-row
// "Add"/"Edit" control (mutating the type registry or scope config over HTTP has no auth story
// here). The frontend renders both as honest, visibly disabled controls with an explanatory
// title, the same precedent AgentsScreen's Connect/Edit tabs already set — nothing here needs
// to serve data for them.
//
// Copy rule (02 section 8): never say "uid", "hook", "pack", "index", "token", "budget" or
// "trait" on screen. Every JSON field below is named the way agents.go's agentSummary is, so
// the frontend never has to translate a store-shaped name into a screen-shaped one.
//
// Security-critical: this handler must never let a secret reach the wire. No DSN password, no
// API key value, no token hash — settings_test.go asserts the whole payload for exactly that.

type settingsTypeJSON struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Folder      string   `json:"folder"`
	Count       int      `json:"count"`
	Fields      []string `json:"fields"`
	// StaleAfterDays is defaults/types/*.yaml's own staleness_days, real per type (0 for a
	// type that declares none — incident, issue and note ship with no staleness rule at
	// all — rather than a made-up number standing in for "unknown").
	StaleAfterDays int `json:"stale_after_days"`
}

// settingsTagGroupJSON is one facet group's vocabulary size (defaults/facets/*.yaml), shown
// next to the real, live per-tag counts in settingsTagJSON — the vocabulary a group defines
// and how much of it is actually in use are two different, both-real facts.
type settingsTagGroupJSON struct {
	Name          string `json:"name"`
	TopLevelCount int    `json:"top_level_count"`
}

type settingsTagJSON struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type settingsRelationJSON struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// settingsScopeJSON is one scope, joined with the Agents screen's own data — 02 section 5.6
// calls "which agents can read it" the most valuable thing on this screen. Agents is never
// []string(nil) here: a revoked or expired agent is filtered out before this list is built,
// because a name left in it would misstate a boundary that no longer holds.
type settingsScopeJSON struct {
	Name     string   `json:"name"`
	Folder   string   `json:"folder"`
	IsClient bool     `json:"is_client"`
	Count    int      `json:"count"`
	Agents   []string `json:"agents"`
}

// settingsSourcesJSON never carries DBUser or a password — only host and database, which
// identify where the vault lives without handing over anything that could authenticate as it.
type settingsSourcesJSON struct {
	VaultPath   string `json:"vault_path"`
	IsGitRepo   bool   `json:"is_git_repo"`
	DBHost      string `json:"db_host"`
	DBName      string `json:"db_name"`
	PageCount   int    `json:"page_count"`
	CommitCount int    `json:"commit_count"`
}

// settingsModelsJSON reports presence, never the value, of ANTHROPIC_API_KEY — and says
// plainly when ANTHROPIC_BASE_URL is unset rather than printing a vendor default this project
// never calls (internal/compile/client.go's own doc comment: "never call vendor APIs direct").
type settingsModelsJSON struct {
	GatewayURL    string `json:"gateway_url"`
	GatewayURLSet bool   `json:"gateway_url_set"`
	APIKeySet     bool   `json:"api_key_set"`
	Note          string `json:"note"`
}

type settingsResponseJSON struct {
	Types     []settingsTypeJSON     `json:"types"`
	TagGroups []settingsTagGroupJSON `json:"tag_groups"`
	Tags      []settingsTagJSON      `json:"tags"`
	Relations []settingsRelationJSON `json:"relations"`
	Scopes    []settingsScopeJSON    `json:"scopes"`
	Sources   settingsSourcesJSON    `json:"sources"`
	Models    settingsModelsJSON     `json:"models"`
}

// handleSettings serves GET /api/settings: Structure (types, tags, relations), Scopes, Sources
// and storage, and Models — every real, verifiable fact 02 section 5.6 asks this screen to
// show. Export has no handler at all; there is nothing real to serve for it.
func (s *server) handleSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	types, err := s.settingsTypes(ctx)
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	tagGroups, tags, err := s.settingsTags(ctx)
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	relations, err := s.settingsRelations(ctx)
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	scopes, err := s.settingsScopes(ctx)
	if err != nil {
		writeServerError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, settingsResponseJSON{
		Types:     types,
		TagGroups: tagGroups,
		Tags:      tags,
		Relations: relations,
		Scopes:    scopes,
		Sources:   s.settingsSources(),
		Models:    settingsModelsInfo(),
	})
}

// settingsTypes lists every registered page type (defaults/types/*.yaml) with its real,
// live count of indexed pages — reusing handleTree's own "count by walking ListPages" pattern
// rather than a new SQL aggregate, since ListPages is already the one query that knows how to
// apply this Queries' scope boundary. Ordered by defaults/order.yaml's agent_order, the same
// ranking Knowledge's ResultList already uses, via the *registry.Order already loaded onto s.
func (s *server) settingsTypes(ctx context.Context) ([]settingsTypeJSON, error) {
	reg, err := registry.Load(filepath.Join(s.defaultsDir, "types"))
	if err != nil {
		return nil, fmt.Errorf("load types: %w", err)
	}

	rows, err := s.q().ListPages(ctx, store.PageFilter{IncludeHistorical: true})
	if err != nil {
		return nil, fmt.Errorf("list pages for type counts: %w", err)
	}
	counts := map[string]int{}
	for _, row := range rows {
		counts[row.Type]++
	}

	names := reg.Names()
	sort.Slice(names, func(i, j int) bool { return s.order.Rank(names[i]) < s.order.Rank(names[j]) })

	out := make([]settingsTypeJSON, 0, len(names))
	for _, name := range names {
		def, ok := reg.Get(name)
		if !ok {
			continue
		}
		fields := make([]string, 0, len(def.Fields))
		for field := range def.Fields {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		out = append(out, settingsTypeJSON{
			Name:           def.Name,
			Description:    def.Description,
			Folder:         def.Folder,
			Count:          counts[name],
			Fields:         fields,
			StaleAfterDays: def.StalenessDays,
		})
	}
	return out, nil
}

// rawFacetGroupYAML reads just enough of one defaults/facets/*.yaml file (see
// internal/registry/facets.go's own rawFacet for the full shape this mirrors a slice of) to
// report a group's name and how many top-level values it declares. A second, narrower parse
// rather than a call through registry.Facets because Facets deliberately exposes no way to
// enumerate what it knows — only IsKnown/ResolveAlias/ToLtree for a single tag at a time — and
// adding one would mean editing a file outside this build's owned or shared territory.
type rawFacetGroupYAML struct {
	Name   string         `yaml:"name"`
	Values map[string]any `yaml:"values"`
}

// settingsFacetGroups lists the real facet groups defaults/facets/ declares, one per file,
// alphabetically by file name.
func settingsFacetGroups(dir string) ([]settingsTagGroupJSON, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("glob %s: %w", dir, err)
	}
	sort.Strings(matches)

	out := make([]settingsTagGroupJSON, 0, len(matches))
	for _, path := range matches {
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		var raw rawFacetGroupYAML
		if err := yaml.Unmarshal(body, &raw); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		out = append(out, settingsTagGroupJSON{Name: raw.Name, TopLevelCount: len(raw.Values)})
	}
	return out, nil
}

// settingsTags returns the known facet groups alongside every real tag in use, most-used
// first — Queries.TagCounts' own ordering, unchanged. Tag names are converted to the on-screen
// slash form with ltreeTagsToSlash, the one place in the codebase (per its own doc comment in
// knowledge.go) that ever does that conversion.
func (s *server) settingsTags(ctx context.Context) ([]settingsTagGroupJSON, []settingsTagJSON, error) {
	groups, err := settingsFacetGroups(filepath.Join(s.defaultsDir, "facets"))
	if err != nil {
		return nil, nil, fmt.Errorf("load facet groups: %w", err)
	}

	rows, err := s.q().TagCounts(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("tag counts: %w", err)
	}
	tags := make([]settingsTagJSON, 0, len(rows))
	for _, row := range rows {
		slash := ltreeTagsToSlash([]string{row.Tag})
		tags = append(tags, settingsTagJSON{Name: slash[0], Count: row.Count})
	}
	return groups, tags, nil
}

// settingsRelations lists every edge kind actually in use, most-used first —
// Queries.EdgeKindCounts already enforces this Queries' scope boundary.
func (s *server) settingsRelations(ctx context.Context) ([]settingsRelationJSON, error) {
	rows, err := s.q().EdgeKindCounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("edge kind counts: %w", err)
	}
	out := make([]settingsRelationJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, settingsRelationJSON{Kind: row.Kind, Count: row.Count})
	}
	return out, nil
}

// settingsScopes lists every scope this server currently enforces, each with its real page
// count, whether it is a client scope, and which agents can currently read it. Folder is the
// scope name itself: defaults/spaces.yaml declares no separate folder field, and the live
// vault's top-level directories (work/, client-globex/, ...) match scope names exactly, so the
// scope name already is the answer 02 section 5.6 asks for.
//
// The name list comes from s.q().AllowedScopes() — the live Queries' own scope set — rather
// than solely from s.spaces.Tree()'s roots. spaces.yaml is only ever the display tree (names,
// tag sub-groupings); it says nothing about which scopes exist. A scope declared via POST
// /api/scopes with zero pages under it has no root in s.spaces.Tree() at all (there is nothing
// to derive a tree node from), but it must still appear here the moment it is created — that
// is the entire point of being able to declare one ahead of time. AllowedScopes is exactly the
// declared ∪ discovered set this process enforces right now (see scopes.go's rebuildQueries),
// so every scope — page-empty or not — is covered by reading it instead.
//
// An agent only appears in Agents once it is active: AllowsScope reports what a token's
// scopes field says even after revocation or expiry, but a revoked or expired agent can no
// longer read anything (see AgentsScreen's boundarySentence), so listing it here would assert
// an access grant that is no longer true.
func (s *server) settingsScopes(ctx context.Context) ([]settingsScopeJSON, error) {
	counts, err := s.q().ScopePageCounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("scope page counts: %w", err)
	}
	tenants, err := registry.LoadTenants(filepath.Join(s.defaultsDir, "tenants.yaml"))
	if err != nil {
		return nil, fmt.Errorf("load tenants: %w", err)
	}
	pool, err := s.pool()
	if err != nil {
		return nil, err
	}
	tokens, err := store.ListTokens(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}

	scopeNames := append([]string(nil), s.q().AllowedScopes()...)
	sort.Strings(scopeNames)

	now := time.Now()
	out := make([]settingsScopeJSON, 0, len(scopeNames))
	for _, scope := range scopeNames {
		var agents []string
		for _, t := range tokens {
			if t.Revoked() || t.Expired(now) || !t.AllowsScope(scope) {
				continue
			}
			agents = append(agents, t.Name)
		}
		sort.Strings(agents)
		out = append(out, settingsScopeJSON{
			Name:     scope,
			Folder:   scope,
			IsClient: tenants.Is(strings.TrimPrefix(scope, "client-")),
			Count:    counts[scope],
			Agents:   nonNilStrings(agents),
		})
	}
	return out, nil
}

// countCommits opens root as a git repository read-only and walks its full history from HEAD,
// counting every commit. Not exposed by store.PageStore or store.GitPageStore (History takes
// a single path and returns a bounded page, not a whole-repository total, and both are outside
// this build's owned or shared territory to extend), so this reopens the same on-disk
// repository directly with go-git — the same library git.go itself uses, read-only, no writes.
func countCommits(root string) (int, error) {
	repo, err := git.PlainOpen(root)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", root, err)
	}
	head, err := repo.Head()
	if err != nil {
		return 0, fmt.Errorf("resolve HEAD: %w", err)
	}
	iter, err := repo.Log(&git.LogOptions{From: head.Hash()})
	if err != nil {
		return 0, fmt.Errorf("log: %w", err)
	}
	defer iter.Close()

	count := 0
	if err := iter.ForEach(func(*object.Commit) error {
		count++
		return nil
	}); err != nil {
		return 0, fmt.Errorf("walk log: %w", err)
	}
	return count, nil
}

// settingsSources reports the vault's real location and the real database it is paired with —
// host and database name only, per this file's package doc: no password ever crosses this
// boundary. Every field degrades to its zero value rather than erroring the whole screen when
// a piece is unavailable (s.pages is nil in some tests; the token pool can fail to open) —
// Settings should still render everything it can when one dependency is missing.
func (s *server) settingsSources() settingsSourcesJSON {
	var out settingsSourcesJSON

	if s.pages != nil {
		if paths, err := s.pages.List(""); err == nil {
			count := 0
			for _, p := range paths {
				if isRealPage(p) {
					count++
				}
			}
			out.PageCount = count
		}
		if git, ok := s.pages.(*store.GitPageStore); ok {
			out.VaultPath = git.Root()
			out.IsGitRepo = true
			if n, err := countCommits(out.VaultPath); err == nil {
				out.CommitCount = n
			}
		}
	}

	if pool, err := s.pool(); err == nil {
		if cfg := pool.Config(); cfg != nil && cfg.ConnConfig != nil {
			out.DBHost = cfg.ConnConfig.Host
			out.DBName = cfg.ConnConfig.Database
		}
	}

	return out
}

// settingsModelsInfo reports what internal/compile/client.go's NewAnthropicClientFromEnv
// actually reads at run time: ANTHROPIC_BASE_URL's value (never a hardcoded vendor fallback —
// this project's stated policy is "never call vendor APIs direct") and whether
// ANTHROPIC_API_KEY is set, never its value. The model name itself is not environment
// configuration here: cmd/balise/main.go's extract-claims subcommand takes it as a --model
// flag per invocation, defaulting to "claude-sonnet-5", so the server has no pinned model to
// report — Note says so plainly rather than printing a value that would misdescribe how this
// build actually chooses a model.
func settingsModelsInfo() settingsModelsJSON {
	baseURL := os.Getenv("ANTHROPIC_BASE_URL")
	return settingsModelsJSON{
		GatewayURL:    baseURL,
		GatewayURLSet: baseURL != "",
		APIKeySet:     os.Getenv("ANTHROPIC_API_KEY") != "",
		Note:          "The model is chosen per run from the command line (balise extract-claims --model); the server does not pin one.",
	}
}
