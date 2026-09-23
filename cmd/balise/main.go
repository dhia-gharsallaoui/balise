package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/dhia/balise/internal/api"
	"github.com/dhia/balise/internal/cli"
	"github.com/dhia/balise/internal/compile"
	"github.com/dhia/balise/internal/embed"
	"github.com/dhia/balise/internal/importer"
	"github.com/dhia/balise/internal/mcp"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

const defaultExtractClaimsModel = "claude-sonnet-5"

const defaultDSN = "postgresql://balise:balise@localhost:5432/balise"

// loadEmbedder attempts to load the optional semantic-search embedder configured via
// embed.ModelEnv (BALISE_EMBED_MODEL). Unset, embed.TryLoad returns (nil, nil) immediately --
// the expected, silent, no-network default -- and this returns nil with no message. Set but
// failing to load (bad model reference, offline, corrupt cache) is reported to stderr as a
// one-line, non-fatal diagnostic, and nil is still returned: semantic search must never be
// the reason `balise serve`, `balise mcp --stdio`, or `balise reindex` fails to start or run
// -- a missing or broken model degrades every caller to lexical-only search, silently to
// callers, but visibly (once, here) to whoever is running the process.
func loadEmbedder(cmd *cobra.Command) *embed.Embedder {
	embedder, err := embed.TryLoad()
	if err != nil {
		cmd.PrintErrf("semantic search disabled: %v\n", err)
		return nil
	}
	return embedder
}

func main() {
	if err := root().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func root() *cobra.Command {
	var dsn string
	cmd := &cobra.Command{Use: "balise", Short: "A knowledge system for agents"}
	cmd.PersistentFlags().StringVar(&dsn, "dsn", envOr("BALISE_DSN", defaultDSN), "Postgres DSN")

	cmd.AddCommand(initCmd(), importCmd(), reindexCmd(&dsn), serveCmd(&dsn), compileCmd(), tokenCmd(&dsn), mcpCmd(&dsn))
	return cmd
}

// compileCmd groups the LLM compile tasks (section 9) under "balise compile ...".
// extract_claims is the first of these; further compile tasks add subcommands
// here rather than growing extractClaimsCmd.
func compileCmd() *cobra.Command {
	// SilenceUsage: a runtime failure partway through a compile task (a bad
	// page, a store error) is not a cobra argument-parsing mistake, so cobra's
	// usage block must not bury the actual error underneath it (fix 4).
	cmd := &cobra.Command{Use: "compile", Short: "Run an LLM compile task against the vault", SilenceUsage: true}
	cmd.AddCommand(extractClaimsCmd())
	return cmd
}

// extractClaimsCmd wires compile.Run to the CLI. --dry-run and --limit are the
// cost controls section 9.3 requires; per-page progress and the run's total
// token counts are printed unconditionally so a real run's cost is always
// visible, not just available in the returned Report.
func extractClaimsCmd() *cobra.Command {
	var scope, model, defaults string
	var limit int
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "extract-claims <vault>",
		Short: "Propose claim keep/reword/retire/add changes for eligible pages",
		Args:  cobra.ExactArgs(1),
		// SilenceUsage/SilenceErrors: this command's RunE prints its own detailed
		// per-page report and summary before returning an error, so cobra's
		// default "print the error, then dump the usage block" behavior would
		// only push the real diagnostic off the top of the terminal (fix 4).
		// Errors are still surfaced by main()'s own fmt.Fprintln, so
		// SilenceErrors does not mean the error goes unreported.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			pages, err := store.OpenGit(args[0])
			if err != nil {
				return err
			}
			defaultsDir, cleanup, err := resolveDefaultsDir(cmd, "defaults", defaults, args[0])
			if err != nil {
				return err
			}
			defer cleanup()
			reg, err := registry.Load(filepath.Join(defaultsDir, "types"))
			if err != nil {
				return fmt.Errorf("load type registry: %w", err)
			}
			client, err := compile.NewAnthropicClientFromEnv()
			if err != nil {
				return fmt.Errorf("build LLM client: %w", err)
			}

			report, runErr := compile.Run(cmd.Context(), pages, reg, client, compile.Options{
				Scope: scope, Limit: limit, DryRun: dryRun, Model: model,
			})

			// Fix 2: print whatever the report holds even when Run returns a
			// structural error — a mid-run commit failure, say — still leaves
			// earlier successes on record, and their proposals and commits are
			// real regardless of what happened afterward.
			printExtractClaimsReport(cmd, report, dryRun)

			if runErr != nil {
				return fmt.Errorf("extract-claims: %w", runErr)
			}
			// Fix 2: exit non-zero only when every attempted page failed; a
			// single bad page among otherwise-successful ones is not a reason
			// to fail the whole command.
			if report.AllAttemptedPagesFailed() {
				return fmt.Errorf("extract-claims: every attempted page failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "restrict to one scope (default: every scope)")
	cmd.Flags().IntVar(&limit, "limit", 0, "cap the number of pages attempted (0 = unlimited)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "render prompts and exit without calling the model or writing anything")
	cmd.Flags().StringVar(&model, "model", defaultExtractClaimsModel, "model name passed to the LLM client")
	cmd.Flags().StringVar(&defaults, "defaults", "defaults", defaultsFlagHelp)
	return cmd
}

// printExtractClaimsReport prints one line per candidate page (skip/dry-run/
// FAIL/done, the last annotated with how many of its claims were flagged
// over-length — fix 1), followed by the run's succeeded/skipped/failed/
// over-length counts, total token spend, and every batch commit's SHA (fix 3).
// It is safe to call with a zero-value or partial Report, since fix 2 means
// Run can return a non-empty report alongside a structural error.
func printExtractClaimsReport(cmd *cobra.Command, report compile.Report, dryRun bool) {
	for _, p := range report.Pages {
		switch {
		case p.Skipped:
			cmd.Printf("skip  %s (%s)\n", p.Path, p.SkipReason)
		case dryRun:
			cmd.Printf("dry-run %s\n%s\n", p.Path, p.Prompt)
		case p.Failed:
			cmd.Printf("FAIL  %s (%s)\n", p.Path, p.FailReason)
		default:
			flag := ""
			if n := len(p.Warnings); n > 0 {
				flag = fmt.Sprintf(" [%d claim(s) over-length]", n)
			}
			superseded := ""
			if p.SupersededProposalID != "" {
				superseded = fmt.Sprintf(" (superseded pending proposal %s)", p.SupersededProposalID)
			}
			cmd.Printf("done  %s -> %s (in=%d out=%d)%s%s\n",
				p.Path, p.ProposalPath, p.InputTokens, p.OutputTokens, flag, superseded)
		}
	}

	succeeded, skipped, failed := report.Counts()
	cmd.Printf("summary: %d succeeded, %d skipped, %d failed, %d claim(s) flagged over-length\n",
		succeeded, skipped, failed, report.OverLengthClaimCount())
	if n := report.SupersededCount(); n > 0 {
		cmd.Printf("superseded %d pending proposal(s) with fresh extractions (fix 1)\n", n)
	}
	cmd.Printf("total tokens: %d in, %d out\n", report.TotalInputTokens, report.TotalOutputTokens)
	for _, sha := range report.CommitSHAs {
		cmd.Printf("committed %s\n", sha[:8])
	}
}

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use: "init <vault>", Short: "Create an empty vault", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := store.InitGit(args[0]); err != nil {
				return err
			}
			cmd.Printf("initialised vault at %s\n", args[0])
			return nil
		},
	}
}

func importCmd() *cobra.Command {
	var corpus, artifacts, defaults string
	cmd := &cobra.Command{
		Use: "import <vault>", Short: "Import a Claude Code memory directory",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pages, err := store.OpenGit(args[0])
			if err != nil {
				return err
			}
			defaultsDir, cleanup, err := resolveDefaultsDir(cmd, "defaults", defaults, args[0])
			if err != nil {
				return err
			}
			defer cleanup()
			// Which customer/* facet values name an actual tenant — as opposed to the
			// datacenter (colo) or the owner's own internal categories (platform, intranet)
			// that also live in the customer/ facet namespace — is the security partition
			// governing scope assignment, so it is loaded from owner-editable data
			// (defaults/tenants.yaml) rather than baked into this binary.
			tenants, err := registry.LoadTenants(filepath.Join(defaultsDir, "tenants.yaml"))
			if err != nil {
				return err
			}
			report, err := importer.ImportCorpus(corpus, artifacts, pages, tenants)
			if err != nil {
				return err
			}
			cmd.Printf("imported %d pages (%d classified, %d unclassified), %d claims, %d multi-customer, %d superseded paths removed — %s\n",
				report.Pages, report.Classified, report.Unclassified, report.Claims,
				report.MultiCustomer, report.Removed, report.Commit[:8])
			return nil
		},
	}
	cmd.Flags().StringVar(&corpus, "corpus", "", "Claude Code memory directory")
	cmd.Flags().StringVar(&artifacts, "artifacts", "", "factory-validation/out directory")
	cmd.Flags().StringVar(&defaults, "defaults", "defaults", defaultsFlagHelp)
	_ = cmd.MarkFlagRequired("corpus")
	_ = cmd.MarkFlagRequired("artifacts")
	return cmd
}

// reindexCmd's --defaults flag is resolved by resolveDefaultsDir (explicit flag, then
// <vault>/defaults, then defaults/ beside the vault, then the embedded copy) and the
// result is passed straight through to cli.Reindex as an explicit argument. It must
// never be a literal baked into this function: go test runs internal/cli's acceptance
// tests with internal/cli as their working directory, which is why cli.Reindex takes
// defaultsDir as a parameter at all rather than hardcoding a relative path itself.
func reindexCmd(dsn *string) *cobra.Command {
	var defaults string
	cmd := &cobra.Command{
		Use: "reindex <vault>", Short: "Rebuild the index from the vault", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			pages, err := store.OpenGit(args[0])
			if err != nil {
				return err
			}
			defaultsDir, cleanup, err := resolveDefaultsDir(cmd, "defaults", defaults, args[0])
			if err != nil {
				return err
			}
			defer cleanup()
			// The vault's own directory layout (scope = directory, 04 section 14) is
			// consulted so a scope that only ever exists on disk so far — such as one the
			// importer's multi-customer quarantine logic names on the fly — is authorised
			// for this reindex rather than rejected by store.ErrScopeDenied.
			vaultScopes, err := store.DiscoverVaultScopes(pages)
			if err != nil {
				return err
			}
			pool, q, err := openQueries(ctx, *dsn, vaultScopes)
			if err != nil {
				return err
			}
			defer pool.Close()

			embedder := loadEmbedder(cmd)
			defer embedder.Close()

			report, err := cli.Reindex(ctx, q, pages, defaultsDir, embedder)
			if err != nil {
				return err
			}
			cmd.Printf("indexed %d pages (%d changed, %d pruned), %d findings, %d collisions, %d embedded\n",
				report.Pages, report.Changed, report.Pruned, report.Findings, report.AliasCollisions, report.Embedded)

			// Link health is read back from storage (cli.ComputeLinkHealth), not from this
			// run's own report.Links*: an incremental reindex only computes findings for the
			// pages it actually walked (indexer.IndexPage skips a noop page's findings
			// entirely), so report.Links* alone would understate a mostly-unchanged vault's
			// true link health — and its own zero-denominator guard would then print a false
			// 100% for a run that simply touched no links. Storage reflects every page's most
			// recent findings regardless of which pages this particular run touched.
			health, err := cli.ComputeLinkHealth(ctx, q)
			if err != nil {
				return fmt.Errorf("compute link health: %w", err)
			}
			countsLine, ratiosLine := cli.FormatLinkHealth(health)
			cmd.Println(countsLine)
			cmd.Println(ratiosLine)
			return nil
		},
	}
	cmd.Flags().StringVar(&defaults, "defaults", "defaults", defaultsFlagHelp)
	return cmd
}

func serveCmd(dsn *string) *cobra.Command {
	var addr, defaults, password string
	var mcpRateLimit int
	cmd := &cobra.Command{
		Use: "serve <vault>", Short: "Serve the API", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Fail closed on the thing that matters: a non-loopback bind address with
			// no owner password would serve every scope — including client data — to
			// anyone who can reach it. Loopback with no password stays open, so plain
			// `make up`/local dev is unaffected. Checked before anything else opens
			// (the vault, the database) so the refusal is immediate and unambiguous,
			// not a timeout buried behind a background process's log file.
			if err := requirePasswordForNonLoopback(addr, password); err != nil {
				return err
			}

			ctx := context.Background()

			// serve opens the same vault reindex does, and for the same reason: Home's
			// "Waiting for you" and "Changed recently" blocks (02-ui-design-v1.md section
			// 5.1) read review/*.md proposals and git history directly off the vault, not
			// out of the database, so the server needs a live PageStore, not just Queries.
			pages, err := store.OpenGit(args[0])
			if err != nil {
				return err
			}
			defaultsDir, cleanup, err := resolveDefaultsDir(cmd, "defaults", defaults, args[0])
			if err != nil {
				return err
			}
			defer cleanup()
			// When --defaults falls all the way through to the embedded copy (see
			// resolveDefaultsDir), defaultsDir is a temp directory that vanishes at
			// process exit. POST /api/scopes (internal/api/scopes.go) appends to
			// scopes.yaml there, so a scope declared that way would not survive a
			// restart. That is an accepted consequence of running serve with no
			// persistent registry available -- not a bug in this fix -- and it is
			// exactly why the vault-relative and vault-sibling tiers exist: point
			// --defaults, or ship a defaults/ directory, at anything you want writes
			// like that to persist across restarts.
			vaultScopes, err := store.DiscoverVaultScopes(pages)
			if err != nil {
				return err
			}
			// Scopes declared in defaults/scopes.yaml (POST /api/scopes appends to this
			// file) exist even with zero pages and zero database rows — that is the whole
			// point of declaring one ahead of time — so they must be folded into the
			// initial scope set here too, not just picked up by the live rebuild that
			// runs when the API creates a new one after this process is already up.
			declaredScopes, err := registry.LoadDeclaredScopes(filepath.Join(defaultsDir, "scopes.yaml"))
			if err != nil {
				return err
			}
			pool, q, err := openQueries(ctx, *dsn, store.UnionScopes(vaultScopes, declaredScopes.Names()))
			if err != nil {
				return err
			}
			defer pool.Close()

			spaces, err := registry.LoadSpaces(filepath.Join(defaultsDir, "spaces.yaml"))
			if err != nil {
				return err
			}
			order, err := registry.LoadOrder(filepath.Join(defaultsDir, "order.yaml"))
			if err != nil {
				return err
			}

			embedder := loadEmbedder(cmd)
			defer embedder.Close()

			// The MCP server is mounted at /mcp alongside the REST API, inside one outer
			// mux — api.New's own returned handler registers only /api/... routes, with
			// no catch-all, so this needs no change inside internal/api. Each MCP tool
			// call constructs its own per-token-scoped Queries from the raw pool: q above
			// is bound to every vault-discovered scope, which is the right breadth for the
			// owner-facing REST API but far too broad to hand an individual agent token,
			// whose scopes almost always narrow that set (04 section 14).
			//
			// WithTokenPool(pool) makes the REST API's agent/scope endpoints reuse this
			// same pool instead of lazily opening a second one from BALISE_DSN — same
			// database either way, but one live connection pool per process is the
			// intent, and now serve actually delivers that.
			outer := http.NewServeMux()
			outer.Handle("/", api.New(q, pages, spaces, order, defaultsDir,
				api.WithOwnerPassword(password), api.WithTokenPool(pool), api.WithEmbedder(embedder)))
			outer.Handle("/mcp", mcp.NewHandler(pool, pages, order, mcpRateLimit, embedder))

			cmd.Printf("listening on http://%s (API at /api, MCP at /mcp)\n", addr)
			return http.ListenAndServe(addr, outer)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8080", "listen address")
	cmd.Flags().StringVar(&defaults, "defaults", "defaults", defaultsFlagHelp)
	cmd.Flags().IntVar(&mcpRateLimit, "mcp-rate-limit", 0, "MCP calls allowed per token per minute (0 = default, 60)")
	cmd.Flags().StringVar(&password, "password", envOr("BALISE_PASSWORD", ""),
		"owner password gating /api (env BALISE_PASSWORD); required when --addr is not loopback")
	return cmd
}

// requirePasswordForNonLoopback is the fail-closed rule: binding /api to anything other
// than loopback with no owner password configured must refuse to start outright, naming
// BALISE_PASSWORD, rather than silently serving every scope -- including client data --
// to whatever can reach that address. Loopback with no password stays open, so `make up`
// and everyday local use are unaffected.
func requirePasswordForNonLoopback(addr, password string) error {
	if password != "" || isLoopbackAddr(addr) {
		return nil
	}
	return fmt.Errorf("refusing to start: --addr %s is not loopback and no owner password is set; "+
		"set BALISE_PASSWORD (or pass --password) before binding /api to a non-loopback address", addr)
}

// isLoopbackAddr reports whether addr (host:port, as passed to --addr) binds only
// loopback. An empty host ("", as in ":8080") binds every interface, which is not
// loopback; "localhost" and any IP for which net.IP.IsLoopback is true both are.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// mcpCmd runs balise's MCP server standalone. Today that means exactly one
// mode -- stdio, for local single-user use (04 section 12: "balise mcp
// --stdio") -- gated behind an explicit --stdio flag rather than being the
// bare command's default, so a future non-stdio mode (or simply running
// "balise mcp" with no flag by mistake) fails loudly instead of silently
// picking a transport.
//
// See internal/mcp.RunStdio's own doc comment for the full security
// reasoning behind requiring --token/BALISE_MCP_TOKEN here rather than a
// bare --scopes flag: a stdio session can only ever do what that token's
// own scopes and capabilities already allow, checked by the exact same
// authenticate() path every other transport uses.
func mcpCmd(dsn *string) *cobra.Command {
	var stdio bool
	var token, defaults string
	cmd := &cobra.Command{
		Use:          "mcp <vault>",
		Short:        "Run balise's MCP server standalone (currently: --stdio only)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !stdio {
				return fmt.Errorf("mcp: --stdio is required (no other transport is implemented by this subcommand yet)")
			}
			if token == "" {
				token = os.Getenv("BALISE_MCP_TOKEN")
			}
			if token == "" {
				return fmt.Errorf("mcp --stdio: --token (or BALISE_MCP_TOKEN) is required")
			}

			ctx := context.Background()

			pages, err := store.OpenGit(args[0])
			if err != nil {
				return err
			}
			// Resolved here rather than left as the literal "defaults": an MCP host
			// spawns this subcommand with no cwd assumption at all (it is not run
			// from this repo, or from any particular directory), so a cwd-relative
			// default would fail for exactly the documented README config. See
			// resolveDefaultsDir for the fallback chain that fixes this.
			defaultsDir, cleanup, err := resolveDefaultsDir(cmd, "defaults", defaults, args[0])
			if err != nil {
				return err
			}
			defer cleanup()
			vaultScopes, err := store.DiscoverVaultScopes(pages)
			if err != nil {
				return err
			}
			pool, _, err := openQueries(ctx, *dsn, vaultScopes)
			if err != nil {
				return err
			}
			defer pool.Close()

			order, err := registry.LoadOrder(filepath.Join(defaultsDir, "order.yaml"))
			if err != nil {
				return err
			}

			embedder := loadEmbedder(cmd)
			defer embedder.Close()

			return mcp.RunStdio(ctx, pool, pages, order, token, embedder)
		},
	}
	cmd.Flags().BoolVar(&stdio, "stdio", false, "run a stdio MCP session for a local, single-user client")
	cmd.Flags().StringVar(&token, "token", "", "bearer token whose scopes/capabilities this stdio session uses (or set BALISE_MCP_TOKEN; the env var is safer -- a flag value is visible via ps and shell history)")
	cmd.Flags().StringVar(&defaults, "defaults", "defaults", defaultsFlagHelp)
	return cmd
}

// openPool connects and migrates, without binding any scope set — the token subcommands
// operate on agent_tokens/audit_log, which are not scope-derived tables and need no Queries
// at all (see internal/store/tokens.go).
func openPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := store.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// tokenCmd groups token lifecycle management under "balise token ..." — 01-design-spec-v0.4.md
// section 6.8 F-70.
func tokenCmd(dsn *string) *cobra.Command {
	cmd := &cobra.Command{Use: "token", Short: "Manage agent tokens for the MCP server"}
	cmd.AddCommand(tokenCreateCmd(dsn), tokenListCmd(dsn), tokenRevokeCmd(dsn))
	return cmd
}

// tokenCreateCmd prints the raw token exactly once (04 section 14: "32 random bytes,
// base64url, shown once; sha256 stored") — there is no `balise token show` because there is
// nothing left to show after this line scrolls past.
func tokenCreateCmd(dsn *string) *cobra.Command {
	var scopes, capabilities []string
	var expires string
	cmd := &cobra.Command{
		Use:          "create <name>",
		Short:        "Mint a new agent token and print its secret once",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			pool, err := openPool(ctx, *dsn)
			if err != nil {
				return err
			}
			defer pool.Close()

			var expiresAt *time.Time
			if expires != "" {
				t, err := time.Parse("2006-01-02", expires)
				if err != nil {
					return fmt.Errorf("parse --expires (want YYYY-MM-DD): %w", err)
				}
				expiresAt = &t
			}

			id, raw, err := store.CreateToken(ctx, pool, args[0], scopes, capabilities, expiresAt)
			if err != nil {
				return err
			}
			cmd.Printf("id:    %s\n", id)
			cmd.Printf("token: %s\n", raw)
			cmd.Println("Store this token now — it is shown once, and only its hash is kept.")
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&scopes, "scopes", nil, "comma-separated scopes this token may access")
	cmd.Flags().StringSliceVar(&capabilities, "capabilities", nil, "comma-separated capabilities: read,remember,propose")
	cmd.Flags().StringVar(&expires, "expires", "", "expiry date YYYY-MM-DD (default: never)")
	_ = cmd.MarkFlagRequired("scopes")
	_ = cmd.MarkFlagRequired("capabilities")
	return cmd
}

func tokenListCmd(dsn *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List agent tokens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			pool, err := openPool(ctx, *dsn)
			if err != nil {
				return err
			}
			defer pool.Close()

			tokens, err := store.ListTokens(ctx, pool)
			if err != nil {
				return err
			}
			now := time.Now()
			for _, t := range tokens {
				status := "active"
				switch {
				case t.Revoked():
					status = "revoked"
				case t.Expired(now):
					status = "expired"
				}
				cmd.Printf("%s  %-20s  scopes=%v  capabilities=%v  %s\n",
					t.ID, t.Name, t.Scopes, t.Capabilities, status)
			}
			return nil
		},
	}
	return cmd
}

func tokenRevokeCmd(dsn *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "revoke <name-or-id>",
		Short:        "Revoke an agent token",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			pool, err := openPool(ctx, *dsn)
			if err != nil {
				return err
			}
			defer pool.Close()

			if err := store.RevokeToken(ctx, pool, args[0]); err != nil {
				return err
			}
			cmd.Printf("revoked %s\n", args[0])
			return nil
		},
	}
	return cmd
}

// openQueries discovers the scopes already present in the vault's database and binds a Queries
// to them. On a brand-new, empty database store.DiscoverScopes returns nothing — there is no
// document yet whose scope column could be distinct-selected — so it falls back to the known
// default scope set. Without that fallback the importer's very first write would name a scope
// nothing has authorized yet and be refused by store.ErrScopeDenied.
//
// client-colo is deliberately not in that default set any more: colo is a datacenter, not a
// tenant (see internal/importer/claudememory.go's clientScopes comment), so the importer never
// produces it. extraScopes carries scopes reindexCmd has already found on disk (vault
// directories, i.e. scope = directory per 04 section 14) — including any this repo has never
// hardcoded, such as a client scope the importer's multi-customer quarantine logic derives from
// a customer name on the fly — so a reindex against a fresh database never rejects a page whose
// scope the vault itself already contains. serveCmd, which has no vault-derived scopes of its
// own to offer, passes nil.
//
// The scope-discovery query itself lives in internal/store/queries.go (store.DiscoverScopes),
// not here: internal/guard's TestOnlyQueriesFileNamesDerivedTables allows only that file to
// name a derived table such as documents in a SQL-shaped string literal.
func openQueries(ctx context.Context, dsn string, extraScopes []string) (*pgxpool.Pool, *store.Queries, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("connect: %w", err)
	}
	if err := store.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, nil, err
	}
	found, err := store.DiscoverScopes(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("read scopes: %w", err)
	}
	base := []string{"work", "client-globex", "personal"}
	if len(found) > 0 {
		base = found
	}
	scopes := store.Scopes(store.UnionScopes(base, extraScopes))
	return pool, store.NewQueries(pool, scopes), nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
