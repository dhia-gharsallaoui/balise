package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/dhia/balise/internal/vault"
)

// newTestDeps builds a *deps backed by a freshly migrated, empty database
// (its own uniquely named schema, per testutil.NewDB) and a freshly
// initialized, empty git vault -- everything a tool handler test needs,
// with no shared state between tests. order is loaded from the repo's own
// defaults/order.yaml, the same fixture every other package's tests load it
// from (see internal/api/api_test.go and friends) -- never a second,
// hand-rolled copy of the agent_order list, and never left nil: context's
// orderContextCandidates calls order.Rank on every candidate, which panics
// on a nil *registry.Order.
func newTestDeps(t *testing.T) *deps {
	t.Helper()
	pool := testutil.NewDB(t)
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	order, err := registry.LoadOrder("../../defaults/order.yaml")
	require.NoError(t, err)
	return &deps{pool: pool, pages: pages, order: order}
}

// mintToken creates a live agent token via the same store.CreateToken path
// the real `balise token create` CLI uses, and returns only the raw
// secret -- exactly what a caller receives once, per 04 section 14.
func mintToken(t *testing.T, pool *pgxpool.Pool, name string, scopes, capabilities []string) string {
	t.Helper()
	_, raw, err := store.CreateToken(context.Background(), pool, name, scopes, capabilities, nil)
	require.NoError(t, err)
	return raw
}

// mintTokenBypassingNameValidation inserts an agent_tokens row directly, the same shape
// store.CreateToken writes, but without going through CreateToken itself -- and therefore
// without CreateToken's own name/scope validation (internal/store/tokens.go). It exists
// solely so tests can prove remember's independent runtime check (rememberPath, this
// package's path.go) still refuses a hostile agent name even if one somehow reaches the
// table by some path other than `balise token create` -- a stale row from before
// CreateToken validated its inputs, a direct database edit, a future insert path that
// forgets to call CreateToken. It must never be used to test anything CreateToken itself
// is responsible for rejecting; TestCreateTokenRejectsHostileAgentName in
// internal/store/tokens_test.go already covers that.
func mintTokenBypassingNameValidation(t *testing.T, pool *pgxpool.Pool, name string, scopes, capabilities []string) string {
	t.Helper()
	secret := make([]byte, 32)
	_, err := rand.Read(secret)
	require.NoError(t, err)
	raw := base64.RawURLEncoding.EncodeToString(secret)
	sum := sha256.Sum256([]byte(raw))
	hash := hex.EncodeToString(sum[:])

	_, err = pool.Exec(context.Background(),
		`insert into agent_tokens (id, name, token_hash, scopes, capabilities, expires_at) values ($1, $2, $3, $4, $5, $6)`,
		vault.NewUID(), name, hash, scopes, capabilities, nil,
	)
	require.NoError(t, err)
	return raw
}

// reqWithToken builds a *sdk.CallToolRequest carrying raw as an
// "Authorization: Bearer <raw>" header -- the same place authenticate
// (auth.go) looks via req.Extra.Header. Session is left nil: every
// handler in this package only ever reads req.Extra, never req.Session.
func reqWithToken(raw string) *sdk.CallToolRequest {
	h := http.Header{}
	if raw != "" {
		h.Set("Authorization", "Bearer "+raw)
	}
	return &sdk.CallToolRequest{Extra: &sdk.RequestExtra{Header: h}}
}

// seedPage upserts one page, with one searchable claim, into scope --
// enough for search and fetch to have something real to find. It uses a
// scope-bound store.Queries constructed just for the seed, the same
// NewQueries constructor production code goes through.
func seedPage(t *testing.T, pool *pgxpool.Pool, scope, slug, claimText string) {
	t.Helper()
	seedPageFull(t, pool, scope, slug, "note", claimText, 10, false)
}

// seedPageFull is seedPage with control over type, token count, and the
// historical flag -- needed by context_test.go, which cares about type
// ordering (registry.Order.Rank), budget packing against a real token
// count, and dropping historical candidates. It returns the page's uid
// (scope + "-" + slug, the same convention seedPage and every other test
// helper in this package uses) so a caller can seed edges from or to it via
// store.Queries.UpsertEdge without recomputing that convention itself.
func seedPageFull(t *testing.T, pool *pgxpool.Pool, scope, slug, typ, claimText string, tokens int, historical bool) string {
	t.Helper()
	ctx := context.Background()
	q := store.NewQueries(pool, store.Scopes{scope})
	uid := scope + "-" + slug
	require.NoError(t, q.UpsertDocument(ctx, store.Document{
		UID: uid, Slug: slug, Scope: scope, Type: typ, Path: scope + "/" + slug + ".md",
		Title:  "Title for " + slug,
		Status: "active", Owner: "test",
		BodyMD: "body for " + slug, BodyHash: "h-" + slug, GitVersion: "g-" + slug,
		Tokens: tokens, ClaimsCount: 1, Historical: historical,
	}))
	require.NoError(t, q.ReplaceClaims(ctx, uid, []store.Claim{
		{ClaimID: uid + "-c1", Ord: 0, Text: claimText, Status: "active"},
	}))
	return uid
}

// seedEdge records one outbound edge from fromUID to toUID of the given
// kind, using a store.Queries bound to fromScope (UpsertEdge's assertOwned
// check requires the caller to hold the from-side's own scope). toRef is
// set to toUID itself: real indexing uses toRef for an unresolved
// reference and lets UpsertEdge resolve to_uid separately, but tests only
// need a resolved edge, so passing toUID as its own resolved reference is
// simplest.
func seedEdge(t *testing.T, pool *pgxpool.Pool, fromScope, fromUID, toUID, kind string) {
	t.Helper()
	q := store.NewQueries(pool, store.Scopes{fromScope})
	require.NoError(t, q.UpsertEdge(context.Background(), fromUID, toUID, toUID, kind, "relations"))
}
