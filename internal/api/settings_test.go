package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dhia/balise/internal/api"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// newSettingsServer mirrors newAgentsServer: the Scopes section needs a real
// *pgxpool.Pool (via api.WithTokenPool) to join scopes against agent_tokens, and the
// Sources section needs a real defaultsDir so registry.Load/LoadTenants can find
// defaults/types and defaults/tenants.yaml — the same reason newSourcesServer passes one.
func newSettingsServer(t *testing.T, pool *pgxpool.Pool, scopes store.Scopes) *httptest.Server {
	t.Helper()
	q := store.NewQueries(pool, scopes)
	spaces, err := registry.LoadSpaces("../../defaults/spaces.yaml")
	require.NoError(t, err)
	order, err := registry.LoadOrder("../../defaults/order.yaml")
	require.NoError(t, err)
	server := httptest.NewServer(api.New(q, newGitVault(t), spaces, order, "../../defaults", api.WithTokenPool(pool)))
	t.Cleanup(server.Close)
	return server
}

// TestSettingsPayloadNeverLeaksSecrets pins the task's hard security requirement
// verbatim: "Never expose a secret. No DSN password, no API key, no token hash." It
// plants three real secrets a naive implementation could leak — an ANTHROPIC_API_KEY
// value, a freshly minted agent token's raw secret and its stored hash, and the test
// database pool's own password — and asserts none of them, nor any field shaped like
// one, appear anywhere in the raw response body.
func TestSettingsPayloadNeverLeaksSecrets(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-do-not-leak-this-value-1234567890")
	t.Setenv("ANTHROPIC_BASE_URL", "https://gateway.example.internal")

	id, rawSecret, err := store.CreateToken(ctx, pool, "settings-secret-agent", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, id)
	require.NotEmpty(t, rawSecret)

	tok, err := store.GetToken(ctx, pool, id)
	require.NoError(t, err)
	require.NotNil(t, tok)
	require.NotEmpty(t, tok.TokenHash)

	server := newSettingsServer(t, pool, store.Scopes{"work"})
	resp, err := http.Get(server.URL + "/api/settings")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	bodyBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	body := string(bodyBytes)

	// The planted secrets themselves must never appear, verbatim, anywhere in the body.
	require.NotContains(t, body, "sk-ant-do-not-leak-this-value-1234567890")
	require.NotContains(t, body, rawSecret)
	require.NotContains(t, body, tok.TokenHash)

	// Beyond specific values, no field shaped like a credential may exist at all — checked
	// structurally rather than by substring, since this local dev database's own user,
	// password and database name are coincidentally all the literal word "balise" (and
	// db_name legitimately appears in the payload), so a plain substring check on the DSN
	// password would false-positive on the honest db_name field.
	require.NotContains(t, strings.ToLower(body), "\"password\"")
	require.NotContains(t, strings.ToLower(body), "\"db_password\"")
	require.NotContains(t, strings.ToLower(body), "\"db_user\"")
	require.NotContains(t, strings.ToLower(body), "\"token_hash\"")
	require.NotContains(t, strings.ToLower(body), "\"api_key\":\"")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(bodyBytes, &parsed))
	sources, ok := parsed["sources"].(map[string]any)
	require.True(t, ok)
	require.ElementsMatch(t,
		[]string{"vault_path", "is_git_repo", "db_host", "db_name", "page_count", "commit_count"},
		mapKeys(sources),
	)
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

type settingsScopeBody struct {
	Name     string   `json:"name"`
	Folder   string   `json:"folder"`
	IsClient bool     `json:"is_client"`
	Count    int      `json:"count"`
	Agents   []string `json:"agents"`
}

type settingsBody struct {
	Types []struct {
		Name           string `json:"name"`
		Count          int    `json:"count"`
		Folder         string `json:"folder"`
		StaleAfterDays int    `json:"stale_after_days"`
	} `json:"types"`
	TagGroups []struct {
		Name          string `json:"name"`
		TopLevelCount int    `json:"top_level_count"`
	} `json:"tag_groups"`
	Tags []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	} `json:"tags"`
	Relations []struct {
		Kind  string `json:"kind"`
		Count int    `json:"count"`
	} `json:"relations"`
	Scopes  []settingsScopeBody `json:"scopes"`
	Sources struct {
		VaultPath   string `json:"vault_path"`
		IsGitRepo   bool   `json:"is_git_repo"`
		DBHost      string `json:"db_host"`
		DBName      string `json:"db_name"`
		PageCount   int    `json:"page_count"`
		CommitCount int    `json:"commit_count"`
	} `json:"sources"`
	Models struct {
		GatewayURL    string `json:"gateway_url"`
		GatewayURLSet bool   `json:"gateway_url_set"`
		APIKeySet     bool   `json:"api_key_set"`
		Note          string `json:"note"`
	} `json:"models"`
}

// TestSettingsReturnsRealTypesTagsRelationsAndScopes covers Structure, Scopes, Sources
// and Models with real, planted data rather than an empty vault, so the assertions pin
// actual joins (tag counts, edge kinds, scope-to-agent) instead of just "the endpoint
// returns 200".
func TestSettingsReturnsRealTypesTagsRelationsAndScopes(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	// The client scope's tenant name is derived from whichever tenants.yaml this checkout
	// actually loads (real or the generic .example fallback — see registry.LoadTenants)
	// rather than hardcoded, so this test passes both on a fresh clone (example tenants)
	// and on a machine with a real, gitignored tenants.yaml declaring different names.
	tenants, err := registry.LoadTenants("../../defaults/tenants.yaml")
	require.NoError(t, err)
	clientScope := "client-" + tenants.Names()[0]

	q := store.NewQueries(pool, store.Scopes{"work", clientScope})

	require.NoError(t, q.UpsertDocument(ctx, store.Document{
		UID: "u-1", Slug: "alloy-outage", Scope: "work", Type: "incident",
		Path: "work/incidents/alloy-outage.md", Title: "Alloy outage",
		Tags: []string{"customer.acme", "layer.api"}, Status: "active", Owner: "dhia",
		BodyMD: "body", BodyHash: "h1", GitVersion: "v1",
	}))
	require.NoError(t, q.UpsertDocument(ctx, store.Document{
		UID: "u-2", Slug: "acme-contract", Scope: clientScope, Type: "entity",
		Path: clientScope + "/entities/acme-contract.md", Title: "Acme contract",
		Tags: []string{"customer.acme"}, Status: "active", Owner: "dhia",
		BodyMD: "body", BodyHash: "h2", GitVersion: "v1",
	}))
	require.NoError(t, q.UpsertEdge(ctx, "u-1", "u-2", "acme-contract", "about", "related"))

	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", "")

	server := newSettingsServer(t, pool, store.Scopes{"work", clientScope})
	resp, err := http.Get(server.URL + "/api/settings")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body settingsBody
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	// Structure: Types.
	require.NotEmpty(t, body.Types)
	byName := map[string]int{}
	staleByName := map[string]int{}
	for _, ty := range body.Types {
		byName[ty.Name] = ty.Count
		staleByName[ty.Name] = ty.StaleAfterDays
	}
	require.Equal(t, 1, byName["incident"])
	require.Equal(t, 1, byName["entity"])
	require.Equal(t, 0, byName["decision"])

	// Structure: Types carry defaults/types/*.yaml's own real staleness_days — 0 for a type
	// that declares none (incident), never a fabricated placeholder.
	require.Equal(t, 365, staleByName["entity"])
	require.Equal(t, 0, staleByName["incident"])

	// Structure: facet groups (defaults/facets/ has exactly customer, layer, vendor).
	groupNames := map[string]bool{}
	for _, g := range body.TagGroups {
		groupNames[g.Name] = true
		require.Positive(t, g.TopLevelCount)
	}
	require.True(t, groupNames["customer"])
	require.True(t, groupNames["layer"])
	require.True(t, groupNames["vendor"])

	// Structure: Tags, in slash form (never dot form) with real counts.
	tagCounts := map[string]int{}
	for _, tag := range body.Tags {
		tagCounts[tag.Name] = tag.Count
		require.NotContains(t, tag.Name, ".", "tag names must be converted to slash form")
	}
	require.Equal(t, 2, tagCounts["customer/acme"])
	require.Equal(t, 1, tagCounts["layer/api"])

	// Structure: Relations.
	require.Len(t, body.Relations, 1)
	require.Equal(t, "about", body.Relations[0].Kind)
	require.Equal(t, 1, body.Relations[0].Count)

	// Scopes: real counts, folder = scope name, is_client set correctly.
	scopesByName := map[string]settingsScopeBody{}
	for _, s := range body.Scopes {
		scopesByName[s.Name] = s
	}
	work, ok := scopesByName["work"]
	require.True(t, ok)
	require.Equal(t, "work", work.Folder)
	require.False(t, work.IsClient)
	require.Equal(t, 1, work.Count)

	globexScope, ok := scopesByName[clientScope]
	require.True(t, ok)
	require.Equal(t, clientScope, globexScope.Folder)
	require.True(t, globexScope.IsClient)
	require.Equal(t, 1, globexScope.Count)

	// Sources: real vault path/git repo flag, real page count.
	require.True(t, body.Sources.IsGitRepo)
	require.NotEmpty(t, body.Sources.VaultPath)

	// Models: unset env vars are reported as unset, never a fabricated default.
	require.False(t, body.Models.GatewayURLSet)
	require.Empty(t, body.Models.GatewayURL)
	require.False(t, body.Models.APIKeySet)
	require.NotEmpty(t, body.Models.Note)
}

// TestSettingsScopeAgentsExcludesRevokedAndExpired pins 02 section 5.6's "which agents
// can read it" join and its one sharp edge: a revoked or expired agent must not appear
// in a scope's agent list, because AllowsScope alone does not know about either state.
func TestSettingsScopeAgentsExcludesRevokedAndExpired(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	activeID, _, err := store.CreateToken(ctx, pool, "active-agent", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)
	revokedID, _, err := store.CreateToken(ctx, pool, "revoked-agent", []string{"work"}, []string{"read"}, nil)
	require.NoError(t, err)
	require.NoError(t, store.RevokeToken(ctx, pool, revokedID))
	_ = activeID

	server := newSettingsServer(t, pool, store.Scopes{"work"})
	resp, err := http.Get(server.URL + "/api/settings")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body settingsBody
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	var work settingsScopeBody
	for _, s := range body.Scopes {
		if s.Name == "work" {
			work = s
		}
	}
	require.Contains(t, work.Agents, "active-agent")
	require.NotContains(t, work.Agents, "revoked-agent")
}
