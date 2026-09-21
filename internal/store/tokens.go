package store

// Agent tokens and the audit log — 04 section 6 (schema), section 14 (security model),
// section 12 (MCP server). agent_tokens and audit_log are deliberately absent from
// internal/guard's derivedTables list: they are the access-control and audit surface in
// front of the scope-derived knowledge (documents/claims/chunks/edges/lint_findings), not
// part of it. Every function here takes *pgxpool.Pool directly rather than *Queries — see
// internal/guard's pool_getter_test.go and queries_constructor_test.go doc comments, which
// explicitly bless a function that merely takes a pool as a parameter. None of these
// functions' signatures mention Queries, and none construct one: this file is a wholly
// independent surface, on purpose, so a new hole here cannot widen what Queries is trusted
// to guard.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dhia/balise/internal/vault"
)

// ErrInvalidCapability means CreateToken was asked to mint a token with a capability outside
// the fixed set 04 section 6 names in its capabilities column comment.
var ErrInvalidCapability = errors.New("invalid capability")

// ErrTokenNotFound means RevokeToken found no matching, not-yet-revoked row.
var ErrTokenNotFound = errors.New("token not found")

// validCapabilities is the fixed set 04 section 6 defines: read, remember, propose.
var validCapabilities = map[string]bool{"read": true, "remember": true, "propose": true}

// AgentToken is one row of agent_tokens. The raw secret is never stored — only TokenHash
// (sha256 of the base64url value shown once at creation) persists, per section 14's "32
// random bytes, base64url, shown once; sha256 stored."
type AgentToken struct {
	ID            string
	Name          string
	TokenHash     string
	Scopes        []string
	Capabilities  []string
	DefaultSpace  string
	DefaultBudget int
	ExpiresAt     *time.Time
	CreatedAt     time.Time
	LastUsedAt    *time.Time
	RevokedAt     *time.Time
}

// HasCapability reports whether t carries the named capability (read, remember, propose).
func (t AgentToken) HasCapability(capability string) bool {
	for _, c := range t.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

// AllowsScope reports whether scope is one of t's own scopes. This is the ceiling every MCP
// tool call must narrow against, never widen past — see internal/mcp.
func (t AgentToken) AllowsScope(scope string) bool {
	for _, s := range t.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// Expired reports whether t's expiry, if any, has passed as of now.
func (t AgentToken) Expired(now time.Time) bool {
	return t.ExpiresAt != nil && now.After(*t.ExpiresAt)
}

// Revoked reports whether t has been revoked.
func (t AgentToken) Revoked() bool {
	return t.RevokedAt != nil
}

// CreateToken mints a new agent token: 32 random bytes from crypto/rand, base64url-encoded
// (no padding) as the raw value returned to the caller exactly once. Only sha256(raw), hex
// encoded, is persisted — section 14's "shown once; sha256 stored" requirement. There is no
// way to recover raw again once this call returns; losing it means revoking this id and
// minting a new one.
//
// name and every entry of scopes are validated with vault.ValidateSegment before anything is
// inserted: both end up as a single path component inside the vault
// ("<scope>/memory/<name>/" — see internal/mcp's rememberPath), so a value that could never
// work there — "../", an absolute path, a null byte, a bare Unicode separator, a
// whitespace-only string — must not be accepted here either. Rejecting it at mint time,
// rather than only when an agent first tries to write, is the point of this check: a token
// that cannot work should not be creatable in the first place, and the agent_tokens table
// should not accumulate rows a future code path might trust.
func CreateToken(ctx context.Context, pool *pgxpool.Pool, name string, scopes, capabilities []string, expiresAt *time.Time) (id, raw string, err error) {
	for _, c := range capabilities {
		if !validCapabilities[c] {
			return "", "", fmt.Errorf("%w: %q", ErrInvalidCapability, c)
		}
	}
	if err := vault.ValidateSegment(name); err != nil {
		return "", "", fmt.Errorf("invalid agent name: %w", err)
	}
	for _, scope := range scopes {
		if err := vault.ValidateSegment(scope); err != nil {
			return "", "", fmt.Errorf("invalid scope: %w", err)
		}
	}

	// agent_tokens.scopes and .capabilities are both "not null" (migrations/00004): a nil Go
	// slice marshals to SQL NULL, not an empty array, so a caller passing nil for either
	// (a legitimately empty, but present, list — nothing in this function's contract requires
	// a caller to already know to pass []string{} instead) would otherwise fail an opaque
	// not-null-constraint error from Postgres rather than getting the empty list it asked for.
	if scopes == nil {
		scopes = []string{}
	}
	if capabilities == nil {
		capabilities = []string{}
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(secret)
	sum := sha256.Sum256([]byte(raw))
	hash := hex.EncodeToString(sum[:])

	id = vault.NewUID()
	if _, err := pool.Exec(ctx,
		`insert into agent_tokens (id, name, token_hash, scopes, capabilities, expires_at) values ($1, $2, $3, $4, $5, $6)`,
		id, name, hash, scopes, capabilities, expiresAt,
	); err != nil {
		return "", "", fmt.Errorf("create token: %w", err)
	}
	return id, raw, nil
}

// LookupToken finds the token whose sha256 hash (hex-encoded) equals hash. It returns
// (nil, nil), not an error, when no row matches — mirroring Queries.GetPage's convention —
// so the MCP auth middleware can turn a nil result into a 401 without a sentinel error
// threaded through every layer. LookupToken does not itself check expiry or revocation:
// callers must check Expired/Revoked, since "not found" and "found but no longer valid" are
// different states a caller may want to log differently.
func LookupToken(ctx context.Context, pool *pgxpool.Pool, hash string) (*AgentToken, error) {
	var t AgentToken
	err := pool.QueryRow(ctx,
		`select id, name, token_hash, scopes, capabilities, coalesce(default_space, ''), coalesce(default_budget, 0), expires_at, created_at, last_used_at, revoked_at from agent_tokens where token_hash = $1`,
		hash,
	).Scan(&t.ID, &t.Name, &t.TokenHash, &t.Scopes, &t.Capabilities, &t.DefaultSpace, &t.DefaultBudget, &t.ExpiresAt, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup token: %w", err)
	}
	return &t, nil
}

// TouchToken records that id was just used successfully. Callers should treat a failure here
// as non-fatal to the request it is auditing: recording "last used" is a courtesy, not part
// of the security boundary, and must never turn a successful auth into a failed request.
func TouchToken(ctx context.Context, pool *pgxpool.Pool, id string) error {
	if _, err := pool.Exec(ctx, `update agent_tokens set last_used_at = now() where id = $1`, id); err != nil {
		return fmt.Errorf("touch token: %w", err)
	}
	return nil
}

// ListTokens returns every agent token, newest first, including revoked ones — so
// `balise token list` can show a token's true state rather than silently hide it.
func ListTokens(ctx context.Context, pool *pgxpool.Pool) ([]AgentToken, error) {
	rows, err := pool.Query(ctx,
		`select id, name, token_hash, scopes, capabilities, coalesce(default_space, ''), coalesce(default_budget, 0), expires_at, created_at, last_used_at, revoked_at from agent_tokens order by created_at desc`,
	)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	defer rows.Close()

	var tokens []AgentToken
	for rows.Next() {
		var t AgentToken
		if err := rows.Scan(&t.ID, &t.Name, &t.TokenHash, &t.Scopes, &t.Capabilities, &t.DefaultSpace, &t.DefaultBudget, &t.ExpiresAt, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt); err != nil {
			return nil, fmt.Errorf("list tokens: scan: %w", err)
		}
		tokens = append(tokens, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	return tokens, nil
}

// RevokeToken marks the token identified by idOrName (matched against either column)
// revoked, returning ErrTokenNotFound if no not-yet-revoked row matched. Matching against
// name as well as id is a CLI convenience only ("balise token revoke <name>" reads
// naturally); name carries no uniqueness constraint, so this can revoke more than one row if
// several tokens share a name — accepted deliberately, since over-revoking fails closed and
// under-revoking would not.
func RevokeToken(ctx context.Context, pool *pgxpool.Pool, idOrName string) error {
	tag, err := pool.Exec(ctx,
		`update agent_tokens set revoked_at = now() where (id = $1 or name = $1) and revoked_at is null`,
		idOrName,
	)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTokenNotFound
	}
	return nil
}

// GetToken finds one agent_tokens row by id, or (nil, nil) if no such row exists — the same
// nil-error convention as LookupToken, so a caller (the REST agents/activity handlers) can
// turn "no row" into a 404 without a sentinel error.
func GetToken(ctx context.Context, pool *pgxpool.Pool, id string) (*AgentToken, error) {
	var t AgentToken
	err := pool.QueryRow(ctx,
		`select id, name, token_hash, scopes, capabilities, coalesce(default_space, ''), coalesce(default_budget, 0), expires_at, created_at, last_used_at, revoked_at from agent_tokens where id = $1`,
		id,
	).Scan(&t.ID, &t.Name, &t.TokenHash, &t.Scopes, &t.Capabilities, &t.DefaultSpace, &t.DefaultBudget, &t.ExpiresAt, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}
	return &t, nil
}

// AuditEntry is one row recorded to audit_log — section 12's "every call writes one
// audit_log row: token id, tool, scopes, query truncated to 300 chars, returned uids, tokens
// out, latency."
type AuditEntry struct {
	TokenID      string
	Tool         string
	Scopes       []string
	Query        string
	Params       map[string]any
	ReturnedUIDs []string
	TokensOut    int
	Coverage     string
	LatencyMS    int
}

// maxAuditQueryLen is section 14's "query truncated to 300 chars" — chars, not bytes, so
// truncation below is done on runes to avoid splitting a multi-byte character in half.
const maxAuditQueryLen = 300

// RecordAudit inserts one audit_log row for a completed MCP tool call. It is called
// unconditionally by internal/mcp's tool handlers, on both success and tool-error paths —
// an audited failure is still an audited call.
func RecordAudit(ctx context.Context, pool *pgxpool.Pool, e AuditEntry) error {
	query := e.Query
	if runes := []rune(query); len(runes) > maxAuditQueryLen {
		query = string(runes[:maxAuditQueryLen])
	}

	var params any
	if e.Params != nil {
		b, err := json.Marshal(e.Params)
		if err != nil {
			return fmt.Errorf("record audit: marshal params: %w", err)
		}
		params = b
	}

	if _, err := pool.Exec(ctx,
		`insert into audit_log (token_id, tool, scopes, query, params, returned_uids, tokens_out, coverage, latency_ms) values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		e.TokenID, e.Tool, e.Scopes, query, params, e.ReturnedUIDs, e.TokensOut, e.Coverage, e.LatencyMS,
	); err != nil {
		return fmt.Errorf("record audit: %w", err)
	}
	return nil
}

// ActivityEntry is one audit_log row paired with the current name of the token that made it
// (a LEFT JOIN, not an inner one: audit_log carries no foreign key to agent_tokens by design —
// an audited call must still be recorded even if its token row is ever gone by the time
// anyone reads the log — so AgentName can come back "" for an orphaned row). This is 04
// section 13's `GET /agents/{id}/activity (from audit_log)`, generalised with an optional
// TokenID filter so the same query can also serve an all-agents feed if a future screen wants
// one.
type ActivityEntry struct {
	ID          int64
	TS          time.Time
	TokenID     string
	AgentName   string
	Tool        string
	Scopes      []string
	Query       string
	ResultCount int
	TokensOut   int
	Coverage    string
	LatencyMS   int
}

// defaultActivityLimit is the page size ListActivity falls back to when limit <= 0.
const defaultActivityLimit = 50

// ListActivity returns audit_log rows newest-first. tokenID narrows the result to one agent
// ("" means every agent); limit caps how many rows come back (callers that want a different
// cap than defaultActivityLimit should clamp it themselves — this function only refuses a
// non-positive one); beforeID (0 for the first page) resumes just after the oldest id already
// returned, giving callers a cheap cursor over a table with no natural upper bound on size.
func ListActivity(ctx context.Context, pool *pgxpool.Pool, tokenID string, limit int, beforeID int64) ([]ActivityEntry, error) {
	if limit <= 0 {
		limit = defaultActivityLimit
	}
	rows, err := pool.Query(ctx, `
		select a.id, a.ts, coalesce(a.token_id, ''), coalesce(t.name, ''), coalesce(a.tool, ''),
		       coalesce(a.scopes, '{}'::text[]), coalesce(a.query, ''),
		       coalesce(array_length(a.returned_uids, 1), 0), coalesce(a.tokens_out, 0),
		       coalesce(a.coverage, ''), coalesce(a.latency_ms, 0)
		from audit_log a
		left join agent_tokens t on t.id = a.token_id
		where ($1 = '' or a.token_id = $1) and ($2 <= 0 or a.id < $2)
		order by a.id desc
		limit $3`,
		tokenID, beforeID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list activity: %w", err)
	}
	defer rows.Close()

	var entries []ActivityEntry
	for rows.Next() {
		var e ActivityEntry
		if err := rows.Scan(&e.ID, &e.TS, &e.TokenID, &e.AgentName, &e.Tool, &e.Scopes, &e.Query, &e.ResultCount, &e.TokensOut, &e.Coverage, &e.LatencyMS); err != nil {
			return nil, fmt.Errorf("list activity: scan: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list activity: %w", err)
	}
	return entries, nil
}
