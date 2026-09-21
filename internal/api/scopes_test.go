package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dhia/balise/internal/api"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// newScopesServer mounts a server exactly like newAgentsServer, except its defaultsDir is a
// fresh t.TempDir() holding only a throwaway scopes.yaml -- never the real
// "../../defaults" directory settings_test.go and sources_test.go point at. handleCreateScope
// calls registry.AppendDeclaredScope(filepath.Join(s.defaultsDir, "scopes.yaml"), ...), which
// rewrites that file on disk; pointing this at the real repo's defaults/scopes.yaml would
// permanently mutate a tracked file every time this test package runs.
//
// The temp scopes.yaml is seeded with `scopes` already declared, not left empty: rebuildQueries
// recomputes the effective set from database ∪ vault ∪ declared (store.EffectiveScopes), so a
// scope bound directly into the initial *store.Queries here (a plain store.NewQueries call, the
// same pattern newAgentsServer and newServer use, with no matching DB rows or vault pages)
// would otherwise silently fall out of the set the moment any create-scope call triggers a
// rebuild -- not because the handler is wrong, but because nothing (declared, DB or vault)
// would attest to that scope's existence anymore. Declaring it up front models what a real
// running server's startup actually does, and keeps this fixture's initial binding consistent
// with itself across a rebuild.
//
// defaults/spaces.yaml and order.yaml are still loaded from the real, read-only "../../defaults"
// path -- reading them never writes anything, so that part is safe to share with every other
// test file.
func newScopesServer(t *testing.T, pool *pgxpool.Pool, scopes store.Scopes) (*httptest.Server, string) {
	t.Helper()
	tempDefaults := t.TempDir()
	quoted := make([]string, len(scopes))
	for i, s := range scopes {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	seed := fmt.Sprintf("scopes: [%s]\n", strings.Join(quoted, ", "))
	require.NoError(t, os.WriteFile(filepath.Join(tempDefaults, "scopes.yaml"), []byte(seed), 0o644))

	// GET /api/settings also reads defaultsDir/tenants.yaml (registry.LoadTenants) -- copy
	// whichever tenants file this checkout actually has (the real one, or its tracked
	// .example fallback on a fresh clone that has no real one -- see
	// registry.LoadTenants/realOrExample) over so a test that happens to hit /api/settings
	// on this fixture (TestCreateScopeReachesRunningServerWithoutRestart does, as its "no
	// restart needed" proof) does not 500 on a missing file that this handler has nothing
	// to do with.
	realTenants, err := os.ReadFile("../../defaults/tenants.yaml")
	if os.IsNotExist(err) {
		realTenants, err = os.ReadFile("../../defaults/tenants.example.yaml")
	}
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(tempDefaults, "tenants.yaml"), realTenants, 0o644))

	q := store.NewQueries(pool, scopes)
	spaces, err := registry.LoadSpaces("../../defaults/spaces.yaml")
	require.NoError(t, err)
	order, err := registry.LoadOrder("../../defaults/order.yaml")
	require.NoError(t, err)
	server := httptest.NewServer(api.New(q, newGitVault(t), spaces, order, tempDefaults, api.WithTokenPool(pool)))
	t.Cleanup(server.Close)
	return server, tempDefaults
}

type createScopeBody struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

// TestCreateScopeHappyPathIsAddOnlyAndPersistsToDisk pins the two things the brief calls
// non-negotiable: an empty scope becomes a real, selectable thing (it shows up in the
// response's "scopes" list before any page is ever filed under it), and the on-disk
// declared-scopes file is the thing that actually changed -- not just an in-memory list
// that would vanish on the next restart.
func TestCreateScopeHappyPathIsAddOnlyAndPersistsToDisk(t *testing.T) {
	pool := testutil.NewDB(t)
	server, tempDefaults := newScopesServer(t, pool, store.Scopes{"work", "client-globex"})

	status, raw := postJSON(t, server, "/api/scopes", map[string]any{"name": "brand-new-space"})
	require.Equal(t, http.StatusCreated, status, string(raw))

	var body createScopeBody
	require.NoError(t, json.Unmarshal(raw, &body))
	require.Equal(t, "brand-new-space", body.Name)
	require.Contains(t, body.Scopes, "brand-new-space")
	require.Contains(t, body.Scopes, "work")
	require.Contains(t, body.Scopes, "client-globex")

	onDisk, err := os.ReadFile(filepath.Join(tempDefaults, "scopes.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(onDisk), "brand-new-space")

	declared, err := registry.LoadDeclaredScopes(filepath.Join(tempDefaults, "scopes.yaml"))
	require.NoError(t, err)
	require.True(t, declared.Has("brand-new-space"))
}

// TestCreateScopeReachesRunningServerWithoutRestart is the atomic-pointer rebuild's own
// test: two scopes created back-to-back, in the same still-running httptest.Server (no
// restart between them), must both show up in the second response's scope list --
// otherwise the server's live *store.Queries never actually picked up the first create.
func TestCreateScopeReachesRunningServerWithoutRestart(t *testing.T) {
	pool := testutil.NewDB(t)
	server, _ := newScopesServer(t, pool, store.Scopes{"work"})

	status, raw := postJSON(t, server, "/api/scopes", map[string]any{"name": "first-new-space"})
	require.Equal(t, http.StatusCreated, status, string(raw))

	status, raw = postJSON(t, server, "/api/scopes", map[string]any{"name": "second-new-space"})
	require.Equal(t, http.StatusCreated, status, string(raw))

	var second createScopeBody
	require.NoError(t, json.Unmarshal(raw, &second))
	require.Contains(t, second.Scopes, "first-new-space")
	require.Contains(t, second.Scopes, "second-new-space")
	require.Contains(t, second.Scopes, "work")

	// And a plain GET /api/settings, on the same running server, must see it too -- proof
	// this isn't just the create response echoing back what it was told to create.
	status, settingsRaw := getRaw(t, server, "/api/settings")
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, string(settingsRaw), "first-new-space")
	require.Contains(t, string(settingsRaw), "second-new-space")
}

// TestCreatingAScopeGrantsNoExistingAgentAccess is the test scopes.go's own doc comment
// forward-references: agent tokens carry an explicit scopes list (store.AgentToken.Scopes),
// set once at mint time, that handleCreateScope never touches -- so a brand-new scope must
// leave every existing agent's grant untouched, not implicitly widened.
func TestCreatingAScopeGrantsNoExistingAgentAccess(t *testing.T) {
	pool := testutil.NewDB(t)
	server, _ := newScopesServer(t, pool, store.Scopes{"work"})

	id, _, err := store.CreateToken(t.Context(), pool, "existing-agent", []string{"work"}, nil, nil)
	require.NoError(t, err)

	status, raw := postJSON(t, server, "/api/scopes", map[string]any{"name": "freshly-created-space"})
	require.Equal(t, http.StatusCreated, status, string(raw))

	tok, err := store.GetToken(t.Context(), pool, id)
	require.NoError(t, err)
	require.NotNil(t, tok)
	require.False(t, tok.AllowsScope("freshly-created-space"))
	require.Equal(t, []string{"work"}, tok.Scopes)
}

// TestCreateScopeRejectsPathTraversalName pins vault.ValidateSegment's rejection of "/" and
// ".." reaching registry.AppendDeclaredScope at all -- a scope name becomes a path
// component (<scope>/...) throughout the vault, so a traversal-shaped name must never be
// written to scopes.yaml.
func TestCreateScopeRejectsPathTraversalName(t *testing.T) {
	pool := testutil.NewDB(t)
	server, tempDefaults := newScopesServer(t, pool, store.Scopes{"work"})

	status, raw := postJSON(t, server, "/api/scopes", map[string]any{"name": "../etc"})
	require.Equal(t, http.StatusBadRequest, status, string(raw))

	status, raw = postJSON(t, server, "/api/scopes", map[string]any{"name": ".."})
	require.Equal(t, http.StatusBadRequest, status, string(raw))

	onDisk, err := os.ReadFile(filepath.Join(tempDefaults, "scopes.yaml"))
	require.NoError(t, err)
	require.Equal(t, `scopes: ["work"]`+"\n", string(onDisk), "a rejected create must not touch the file at all")
}

// TestCreateScopeRejectsDuplicateOfBoundScope confirms a name already in the server's
// live, effective scope set (bound at construction here, never declared or discovered) is
// treated as a duplicate, not silently accepted.
func TestCreateScopeRejectsDuplicateOfBoundScope(t *testing.T) {
	pool := testutil.NewDB(t)
	server, _ := newScopesServer(t, pool, store.Scopes{"work", "client-globex"})

	status, raw := postJSON(t, server, "/api/scopes", map[string]any{"name": "work"})
	require.Equal(t, http.StatusConflict, status, string(raw))
}

// TestCreateScopeRejectsDuplicateOfDeclaredScope covers the other half: a name declared by
// an earlier successful create in the same running server.
func TestCreateScopeRejectsDuplicateOfDeclaredScope(t *testing.T) {
	pool := testutil.NewDB(t)
	server, _ := newScopesServer(t, pool, store.Scopes{"work"})

	status, raw := postJSON(t, server, "/api/scopes", map[string]any{"name": "once-only"})
	require.Equal(t, http.StatusCreated, status, string(raw))

	status, raw = postJSON(t, server, "/api/scopes", map[string]any{"name": "once-only"})
	require.Equal(t, http.StatusConflict, status, string(raw))
}

func TestCreateScopeRejectsEmptyName(t *testing.T) {
	pool := testutil.NewDB(t)
	server, _ := newScopesServer(t, pool, store.Scopes{"work"})

	status, raw := postJSON(t, server, "/api/scopes", map[string]any{"name": ""})
	require.Equal(t, http.StatusBadRequest, status, string(raw))
}

func TestCreateScopeRejectsOverLongName(t *testing.T) {
	pool := testutil.NewDB(t)
	server, _ := newScopesServer(t, pool, store.Scopes{"work"})

	status, raw := postJSON(t, server, "/api/scopes", map[string]any{"name": strings.Repeat("a", 101)})
	require.Equal(t, http.StatusBadRequest, status, string(raw))
}
