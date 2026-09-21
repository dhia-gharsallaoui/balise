// Package api serves the UI. Rows come back in the shape the frontend renders, so the
// browser does no joining — 04 section 13.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
)

type server struct {
	// queriesPtr holds the *store.Queries every handler must go through, loaded via q().
	// It is an atomic.Pointer rather than a plain field because POST /api/scopes changes the
	// set of scopes this server enforces at runtime (scopes.go): store.Queries binds its
	// Scopes slice at construction and internal/guard's AST guards rest on that binding being
	// immutable, so Queries.Scopes itself must never become mutable. Instead, adding a scope
	// builds a brand new *store.Queries (same pool, old ∪ new scopes) and swaps the pointer —
	// every in-flight request finishes against the Queries it already loaded, and every
	// request after the swap sees the new scope. scopesMu (below) serializes the read-append-
	// rebuild-swap sequence across concurrent create-scope calls; it does not need to be held
	// by ordinary handlers, which only ever Load().
	queriesPtr atomic.Pointer[store.Queries]
	pages      store.PageStore
	spaces     *registry.Spaces
	order      *registry.Order
	// defaultsDir is cli.Reindex's third argument, threaded through so an accepted proposal
	// can trigger a synchronous reindex (review.go's reindexAfterAccept) using the exact same
	// defaults directory `balise reindex`/`balise serve --defaults` already resolve. Empty
	// means no defaults directory was configured; reindexAfterAccept treats that the same as
	// a reindex failure (index_stale: true) rather than silently skipping the reindex.
	defaultsDir string

	// scopesMu serializes POST /api/scopes end to end: reading defaults/scopes.yaml,
	// validating and appending the new name, recomputing the effective scope set, building
	// the replacement *store.Queries and swapping queriesPtr. Without it, two concurrent
	// create-scope requests could both read the same "before" file content and each write
	// back a two-element list missing the other's addition (registry.AppendDeclaredScope's
	// read-modify-write has no other synchronization of its own).
	scopesMu sync.Mutex

	// tokenPool, tokenPoolErr and tokenPoolOnce back the Agents screen's two read-only
	// endpoints (agents.go). agent_tokens and audit_log are deliberately outside Queries'
	// scope-derived guard (see tokens.go's package doc), so reading them needs a plain
	// *pgxpool.Pool rather than a *store.Queries — and internal/guard's pool_getter_test.go /
	// queries_constructor_test.go make it impossible to extract one from an existing Queries
	// from anywhere in package store. WithTokenPool lets a caller that already has a pool
	// (every test in this package, via testutil.NewDB) hand over the exact same one so
	// per-test schema isolation is preserved; a caller that does not supply one (the real
	// `balise serve`, whose main.go this build must not touch — see dispatch-rules.md's
	// per-agent ownership split) gets a pool opened lazily from BALISE_DSN, matching
	// cmd/balise/main.go's own default-DSN resolution.
	tokenPool     *pgxpool.Pool
	tokenPoolErr  error
	tokenPoolOnce sync.Once

	// auth is nil unless WithOwnerPassword configured a non-empty password, in which
	// case every /api request other than the auth endpoints themselves must carry a
	// valid session cookie (see auth.go's requireSession). Every existing test in this
	// package constructs New with no options and so gets a nil auth — fully open, no
	// behavior change from before this file existed.
	auth *ownerAuth
}

// Option configures New beyond its required arguments, without changing the signature every
// existing caller (cmd/balise/main.go, and every *_test.go in this package) already uses:
// trailing variadic options are optional, so none of those call sites need to change.
type Option func(*server)

// WithTokenPool supplies the pool the Agents screen's endpoints use — see the tokenPool
// field's doc comment on why this exists instead of deriving one from q.
func WithTokenPool(pool *pgxpool.Pool) Option {
	return func(s *server) { s.tokenPool = pool }
}

// WithOwnerPassword turns on owner authentication: every /api request other than
// GET /api/auth/status, POST /api/auth/login and POST /api/auth/logout must then carry a
// valid session cookie (see auth.go). An empty password is treated as "no option given at
// all" — auth stays off — so cmd/balise/main.go can call this unconditionally with
// whatever BALISE_PASSWORD/--password resolved to, without a separate "was one configured"
// branch of its own; the fail-closed refusal to even start with a non-loopback address and
// no password lives in cmd/balise/main.go, not here.
func WithOwnerPassword(password string) Option {
	return func(s *server) {
		if password == "" {
			return
		}
		s.auth = newOwnerAuth(password)
	}
}

// defaultTokenDSN mirrors cmd/balise/main.go's own defaultDSN literal. It is duplicated
// rather than imported because cmd/balise depends on internal/api, not the other way around,
// and because dispatch-rules.md puts cmd/balise/main.go out of bounds for this build; the two
// literals naming the same local dev Postgres is a one-line cost worth paying to avoid an
// import cycle or an edit to a file a concurrent agent owns.
const defaultTokenDSN = "postgresql://balise:balise@localhost:5432/balise"

// pool returns the *pgxpool.Pool the Agents endpoints should query, opening one from
// BALISE_DSN on first use if New was not given one via WithTokenPool. Lazy and memoized via
// sync.Once so tests that never touch /api/agents never pay for a pool they do not need.
func (s *server) pool() (*pgxpool.Pool, error) {
	s.tokenPoolOnce.Do(func() {
		if s.tokenPool != nil {
			return
		}
		dsn := os.Getenv("BALISE_DSN")
		if dsn == "" {
			dsn = defaultTokenDSN
		}
		pool, err := pgxpool.New(context.Background(), dsn)
		if err != nil {
			s.tokenPoolErr = fmt.Errorf("open token pool: %w", err)
			return
		}
		s.tokenPool = pool
	})
	return s.tokenPool, s.tokenPoolErr
}

// q returns the *store.Queries every handler must use for this request. It is always
// non-nil once New has run (New stores the constructor's q argument before any handler can
// be reached) and always reflects the scope set most recently committed by a successful
// POST /api/scopes (scopes.go) — see queriesPtr's doc comment on the server struct for why
// this is a swapped pointer rather than a mutable field.
func (s *server) q() *store.Queries { return s.queriesPtr.Load() }

// New returns the API handler. Scopes are already bound into q. pages is the git-backed
// vault Home's "Waiting for you" and "Changed recently" blocks read directly — proposals and
// commit history have no database representation (02-ui-design-v1.md section 5.1 says so
// explicitly for proposals), so this is the one dependency the API needs beyond Queries.
// defaultsDir is the same directory `balise reindex` uses to resolve scope defaults; the
// review-accept handler needs it to reindex the one page it just changed (see review.go).
// opts currently supports WithTokenPool; see its doc comment.
func New(q *store.Queries, pages store.PageStore, spaces *registry.Spaces, order *registry.Order, defaultsDir string, opts ...Option) http.Handler {
	s := &server{pages: pages, spaces: spaces, order: order, defaultsDir: defaultsDir}
	s.queriesPtr.Store(q)
	for _, opt := range opts {
		opt(s)
	}
	mux := http.NewServeMux()
	for _, route := range apiRoutes {
		mux.HandleFunc(route.pattern, func(w http.ResponseWriter, r *http.Request) {
			route.handler(s, w, r)
		})
	}
	return withCORS(s.requireSession(mux))
}

// routeSpec is one entry in apiRoutes: a mux pattern (Go 1.22+ "METHOD /path" form) paired
// with the handler it dispatches to, via a method expression ((*server).handleTree has
// type func(*server, http.ResponseWriter, *http.Request)) rather than a bound method value
// — that keeps every handler's own signature untouched while still letting this table be
// built, and enumerated, without calling New at all.
type routeSpec struct {
	pattern string
	handler func(*server, http.ResponseWriter, *http.Request)
	// public is true for the three auth endpoints (status/login/logout), which
	// requireSession must let through with no session at all — see auth.go. Every other
	// route defaults to false: protected, exactly the routing table's previous behavior
	// before there was any auth to gate.
	public bool
}

// path returns the route's URL path with its leading "METHOD " stripped, e.g.
// "GET /api/tree" -> "/api/tree". Used to build the public-path set requireSession checks
// (auth.go's buildPublicAuthPaths) from this same table, so the two can never drift apart.
func (rt routeSpec) path() string {
	if i := strings.IndexByte(rt.pattern, ' '); i >= 0 {
		return rt.pattern[i+1:]
	}
	return rt.pattern
}

// apiRoutes is the single source of truth for every route this server registers — the
// mux's own registration loop (in New) and ProtectedRoutePatterns (used by tests to
// enumerate 401 coverage across the whole API, rather than a hand-maintained list that
// silently goes stale as routes are added) both read from exactly this table.
var apiRoutes = []routeSpec{
	{pattern: "GET /api/tree", handler: (*server).handleTree},
	{pattern: "GET /api/pages", handler: (*server).handlePages},
	// A page is identified by (scope, slug), not by slug alone — documents is uniquely
	// keyed on both, so the same slug can name a different customer's page in each scope.
	// The route carries the scope for that reason; the list, search and graph rows all
	// already return it, so every caller has it to hand.
	{pattern: "GET /api/pages/{scope}/{slug}", handler: (*server).handlePage},
	{pattern: "GET /api/graph", handler: (*server).handleGraph},
	{pattern: "GET /api/search", handler: (*server).handleSearch},
	{pattern: "GET /api/home", handler: (*server).handleHome},
	{pattern: "GET /api/review", handler: (*server).handleReviewList},
	{pattern: "GET /api/review/{id}", handler: (*server).handleReviewItem},
	{pattern: "POST /api/review/{id}/accept", handler: (*server).handleReviewAccept},
	{pattern: "POST /api/review/{id}/edit-accept", handler: (*server).handleReviewEditAccept},
	{pattern: "POST /api/review/{id}/reject", handler: (*server).handleReviewReject},
	{pattern: "GET /api/agents", handler: (*server).handleAgents},
	{pattern: "GET /api/agents/{id}/activity", handler: (*server).handleAgentActivity},
	{pattern: "POST /api/agents", handler: (*server).handleCreateAgent},
	{pattern: "POST /api/agents/{id}/revoke", handler: (*server).handleRevokeAgent},
	{pattern: "POST /api/sources/pages", handler: (*server).handleSourcesUpload},
	{pattern: "GET /api/sources/log", handler: (*server).handleSourcesLog},
	{pattern: "GET /api/settings", handler: (*server).handleSettings},
	{pattern: "POST /api/scopes", handler: (*server).handleCreateScope},
	{pattern: "GET /api/auth/status", handler: (*server).handleAuthStatus, public: true},
	{pattern: "POST /api/auth/login", handler: (*server).handleAuthLogin, public: true},
	{pattern: "POST /api/auth/logout", handler: (*server).handleAuthLogout, public: true},
}

// ProtectedRoutePatterns returns the mux pattern (e.g. "POST /api/sources/pages") of every
// route that requires a valid owner session when one is configured — every route except
// the three public auth endpoints. Exported so internal/api's tests can enumerate 401
// coverage across the whole route table directly from it, instead of hand-listing routes
// that would silently go stale as new ones are added.
func ProtectedRoutePatterns() []string {
	patterns := make([]string, 0, len(apiRoutes))
	for _, route := range apiRoutes {
		if !route.public {
			patterns = append(patterns, route.pattern)
		}
	}
	return patterns
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already sent; log-and-continue is the only option.
		return
	}
}

func writeError(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]string{"detail": detail})
}

// writeServerError logs the real failure server-side and returns a short, stable message
// to the client. A pgx error string can carry table, column and constraint names, and
// sometimes a literal value — OCR's rule is that internal detail never crosses a public
// boundary. 04 section 19 exposes this API over Tailscale, so "single-owner tool on
// localhost" is not a permanent excuse. slog keeps the detail available for debugging
// without putting it on the wire.
func writeServerError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

// allowedDevOrigins lists the local Vite dev servers allowed to call this API in
// development. :5173 is the live dev server that knowledge.spec.ts, a11y.spec.ts and
// everyday local dev use. :5271 is the frozen-fixture-vault dev server visual.spec.ts
// points at instead, so its screenshots stop drifting with whatever /tmp/balise-live
// happens to contain on a given run (see web/tests/e2e/fixtures/run-fixture-backend.mjs).
// Both are localhost-only Vite dev servers, never a production origin.
var allowedDevOrigins = buildAllowedOrigins()

// withCORS lets the allowed Vite dev servers reach the API during development.
//
// Access-Control-Allow-Credentials is set alongside Allow-Origin, and only inside the
// allowed-origin branch — never paired with a wildcard origin, which browsers refuse
// outright — because the owner session cookie must survive the one deliberately
// cross-origin case in this codebase: visual.spec.ts's dev server on :5271 talking to a
// frozen fixture backend on :8199 (see buildAllowedOrigins). Without this header, a
// browser receiving a credentialed cross-origin response discards it even though the
// cookie itself was sent — the frontend's fetch calls carry credentials: "include" to
// match (web/src/lib/api.ts).
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowedDevOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// buildAllowedOrigins returns the browser origins the API will answer with CORS headers.
// The two localhost entries cover the dev server and the frozen-fixture server used by the
// visual tests. BALISE_EXTRA_ORIGINS adds more, comma-separated — it exists so the API can
// be reached over Tailscale (04 section 19 names Tailscale as the recommended exposure)
// without widening the bind address to 0.0.0.0, which on a host with a public interface
// would put one person's private knowledge on the internet.
func buildAllowedOrigins() map[string]bool {
	allowed := map[string]bool{
		"http://localhost:5173": true,
		"http://localhost:5271": true,
	}
	for _, origin := range strings.Split(os.Getenv("BALISE_EXTRA_ORIGINS"), ",") {
		if trimmed := strings.TrimSpace(origin); trimmed != "" {
			allowed[trimmed] = true
		}
	}
	return allowed
}
