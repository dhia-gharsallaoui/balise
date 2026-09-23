package mcp

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dhia/balise/internal/embed"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
)

// RunStdio runs balise's MCP server over stdio, for local, single-user use
// (04 section 12: "balise mcp --stdio"). It blocks until the stdio session
// ends (client disconnect, or ctx is cancelled).
//
// # Where a stdio session's scopes come from
//
// An HTTP caller identifies itself with an Authorization header, which
// authenticate (auth.go) reads off req.Extra.Header on every single tool
// call. A stdio session has no HTTP request at all, so there is nothing
// there to read -- something else has to tell every tool call which
// token, and therefore which scopes and capabilities, is calling.
//
// The mechanism chosen here is: require a real token. rawToken must name a
// live (unexpired, unrevoked) row in agent_tokens, exactly the same as an
// HTTP bearer token would -- validated once, right here, through
// lookupLiveToken, the identical path HTTP auth already uses, so a bad
// token fails fast at startup rather than on the first tool call. A
// receiving middleware (stdioAuthMiddleware, below) then stamps a
// synthetic Authorization header onto every incoming CallToolRequest,
// which means every handler's own authenticate() call -- completely
// unmodified from the HTTP path -- resolves that same token and enforces
// the exact same capability and scope checks an HTTP caller would face.
// Nothing in pages.go, related.go, context.go, search.go, fetch.go, or
// remember.go had to change for stdio to work.
//
// A bare --scopes flag (letting a caller simply assert which scopes a
// stdio session gets) was considered and rejected: it would grant access
// backed by no agent_tokens row at all -- no expiry, no revocation, and no
// accountable identity for the audit log's token_id column -- which is
// exactly the kind of scope-widening-with-no-accountability the "scope is
// the security boundary" invariant exists to prevent. Requiring a real
// token means a stdio session can never do more than that token's own
// scopes and capabilities already allow, checked by the same code every
// other transport uses, and every call it makes is attributed to a real,
// revocable token in the audit log.
//
// The caveat worth stating plainly (also in the completion report): a
// --token flag's value is visible to any other process on the same
// machine via `ps aux`, and lands in shell history if typed interactively.
// That is an accepted tradeoff for a local, single-user tool -- 04's own
// framing for this mode -- but an environment variable is the safer way to
// pass the same value, and is what cmd/balise's mcp subcommand supports
// alongside the flag.
func RunStdio(
	ctx context.Context, pool *pgxpool.Pool, pages store.PageStore, order *registry.Order,
	rawToken string, embedder *embed.Embedder,
) error {
	if _, err := lookupLiveToken(ctx, pool, rawToken); err != nil {
		return fmt.Errorf("mcp stdio: invalid token: %w", err)
	}

	server := newMCPServer(pool, pages, order, embedder)
	server.AddReceivingMiddleware(stdioAuthMiddleware(rawToken))

	return server.Run(ctx, &sdk.StdioTransport{})
}

// stdioAuthMiddleware stamps a fixed "Authorization: Bearer <rawToken>"
// header onto every CallToolRequest's Extra, so authenticate (auth.go)
// resolves the same token for every call in this stdio session that an
// HTTP caller presenting that same raw secret would resolve. It is
// registered only on the server RunStdio builds -- NewHandler's HTTP path
// never adds it -- so this stdio-only mechanism can never affect what an
// HTTP caller can do.
func stdioAuthMiddleware(rawToken string) sdk.Middleware {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+rawToken)
	return func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			if call, ok := req.(*sdk.CallToolRequest); ok {
				call.Extra = &sdk.RequestExtra{Header: header}
			}
			return next(ctx, method, req)
		}
	}
}
