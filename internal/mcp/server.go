package mcp

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dhia/balise/internal/embed"
	"github.com/dhia/balise/internal/ratelimit"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
)

// deps bundles what every tool handler needs. It carries no state beyond
// what newMCPServer is given, and is never mutated after construction.
type deps struct {
	pool  *pgxpool.Pool
	pages store.PageStore
	// order backs context's type-rank ordering (04 section 11 step 3),
	// loaded from the same defaults/order.yaml every other registry
	// consumer in this repo uses -- never a second, hardcoded copy of the
	// agent_order list.
	order *registry.Order
	// embedder is nil unless cmd/balise/main.go loaded one via
	// embed.TryLoad (i.e. unless BALISE_EMBED_MODEL was set). search.go and
	// context.go both embed the caller's query text through it, when
	// non-nil, to add a semantic ranking arm to SearchClaims's RRF fusion;
	// a nil embedder means both tools stay exactly as lexical-only as they
	// always were, with no error and no behaviour change.
	embedder *embed.Embedder
}

// newMCPServer builds the *sdk.Server shared by both transports this
// package exposes: NewHandler's streamable-HTTP handler, and RunStdio's
// stdio session (stdio.go). Every tool and prompt is registered exactly
// once, here, so the two transports can never drift out of sync with each
// other.
func newMCPServer(pool *pgxpool.Pool, pages store.PageStore, order *registry.Order, embedder *embed.Embedder) *sdk.Server {
	d := &deps{pool: pool, pages: pages, order: order, embedder: embedder}

	server := sdk.NewServer(&sdk.Implementation{
		Name:    "balise",
		Version: "0.1.0",
	}, nil)

	sdk.AddTool(server, &sdk.Tool{
		Name: "remember",
		Description: "Append a dated, timestamped note to the calling agent's own memory " +
			"file, under <scope>/memory/<agent>/. Requires the remember capability, and " +
			"scope must be one of the token's own scopes.",
	}, d.remember)

	sdk.AddTool(server, &sdk.Tool{
		Name: "search",
		Description: "Search claims and page titles across the token's allowed scopes, " +
			"optionally narrowed to a single scope. Requires the read capability.",
	}, d.search)

	sdk.AddTool(server, &sdk.Tool{
		Name: "fetch",
		Description: "Fetch one page, with its claims, by scope and slug. Requires the " +
			"read capability; an unknown slug and a slug outside the token's scopes are " +
			"reported identically, as not found.",
	}, d.fetch)

	sdk.AddTool(server, &sdk.Tool{
		Name: "pages",
		Description: "List pages, optionally narrowed by type, scope, tags, or status. " +
			"Requires the read capability.",
	}, d.listPages)

	sdk.AddTool(server, &sdk.Tool{
		Name: "related",
		Description: "List one page's outbound relations (grouped by edge kind) and inbound " +
			"backlinks, optionally following outbound edges depth hops (default 1, capped). " +
			"Requires the read capability; an unknown slug and a slug outside the token's " +
			"scopes are reported identically, as not found.",
	}, d.related)

	sdk.AddTool(server, &sdk.Tool{
		Name: "context",
		Description: "Assemble a token-budgeted context pack for a query: search hits plus " +
			"their specializes/supersedes relations, ordered by page type and packed to " +
			"budget_tokens (default 6000) using each page's real stored token count -- never " +
			"truncated mid-page. Ranking fuses lexical/trigram search with an optional " +
			"semantic (embedding) signal when a model is configured on this deployment; " +
			"without one, ranking is lexical only. Coverage is reported \"low\" whenever " +
			"nothing was packed. Requires the read capability.",
	}, d.context)

	server.AddPrompt(contextFirstPrompt, contextFirstPromptHandler)

	return server
}

// NewHandler builds balise's MCP server and wraps it in an http.Handler
// suitable for mounting at /mcp alongside the REST API in the same
// process, per the task brief's "same ASGI-equivalent app."
//
// Authentication happens at this outer HTTP layer, before the MCP
// streamable-HTTP handler ever runs: 04 section 12's "expired/revoked ->
// 401" is a transport-level concern (a request with no valid token should
// never even start an MCP session), so a missing, unknown, expired or
// revoked bearer token is rejected here with a literal 401 and never
// reaches the protocol layer. Each tool handler additionally re-resolves
// its own token from the request (see auth.go's authenticate) to check
// capability and scope, which are per-call concerns the transport layer
// has no opinion on.
//
// ratePerMinute is also enforced at this same outer layer, per resolved
// token: <= 0 falls back to RateLimiter's own default of 60/min. It is
// in-process only (internal/ratelimit) -- there is no cross-process shared
// counter -- which is the accepted limitation for this single-process
// deployment, stated here and in the completion report.
func NewHandler(
	pool *pgxpool.Pool, pages store.PageStore, order *registry.Order, ratePerMinute int,
	embedder *embed.Embedder,
) http.Handler {
	server := newMCPServer(pool, pages, order, embedder)

	mcpHandler := sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server { return server },
		&sdk.StreamableHTTPOptions{Stateless: true},
	)

	return &authMiddleware{pool: pool, next: mcpHandler, limiter: ratelimit.NewRateLimiter(ratePerMinute)}
}

// authMiddleware rejects any request whose bearer token does not resolve
// to a live (unexpired, unrevoked) agent token, before the wrapped MCP
// handler runs at all, and enforces the per-token rate limit at the same
// point -- it has already paid the cost of resolving the token, so no
// second lookup is needed to key the limiter.
type authMiddleware struct {
	pool    *pgxpool.Pool
	next    http.Handler
	limiter *ratelimit.RateLimiter
}

func (m *authMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tok, err := lookupLiveToken(r.Context(), m.pool, bearerToken(r.Header))
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if m.limiter != nil && !m.limiter.Allow(tok.ID) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	m.next.ServeHTTP(w, r)
}
