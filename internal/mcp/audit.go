package mcp

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dhia/balise/internal/store"
)

// recordAudit writes one audit_log row for a completed tool call -- 04
// section 12: "every call writes one audit_log row (query truncated to 300
// chars, returned uids, tokens_out, coverage, latency)." Every tool handler
// in this package calls it exactly once per invocation, on both the
// success and the failure path: an audited refusal is still an audited
// call, which is what makes the leak test and the capability/scope
// tests meaningful against the audit log, not just against return values.
//
// tokenID may be "" (e.g. when authenticate itself failed, before any
// token was resolved); the row is still written, with an empty token_id,
// so a flood of unauthenticated attempts is not invisible to the log.
//
// A failure to write the row itself is deliberately swallowed after being
// attempted, matching store.TouchToken's own documented convention:
// recording an audit row is a courtesy to observability, not part of the
// security boundary, and must never turn an already-decided tool result
// into a different one.
func recordAudit(ctx context.Context, pool *pgxpool.Pool, tokenID, tool string, scopes []string, query string, returnedUIDs []string, tokensOut int, start time.Time) {
	_ = store.RecordAudit(ctx, pool, store.AuditEntry{
		TokenID:      tokenID,
		Tool:         tool,
		Scopes:       scopes,
		Query:        query,
		ReturnedUIDs: returnedUIDs,
		TokensOut:    tokensOut,
		LatencyMS:    int(time.Since(start).Milliseconds()),
	})
}

// recordAuditCoverage is recordAudit's sibling for the context tool, the
// only handler in this package that populates store.AuditEntry.Coverage
// (04 section 12's audit row shape lists "coverage" alongside the fields
// recordAudit already writes). recordAudit's own signature and its three
// existing call sites (remember.go, search.go, fetch.go) are left
// untouched -- adding a parameter to a shared function only used by
// coverage-blind tools would force every one of them to pass an empty
// string for a field that means nothing there.
func recordAuditCoverage(ctx context.Context, pool *pgxpool.Pool, tokenID, tool string, scopes []string, query string, returnedUIDs []string, tokensOut int, coverage string, start time.Time) {
	_ = store.RecordAudit(ctx, pool, store.AuditEntry{
		TokenID:      tokenID,
		Tool:         tool,
		Scopes:       scopes,
		Query:        query,
		ReturnedUIDs: returnedUIDs,
		TokensOut:    tokensOut,
		Coverage:     coverage,
		LatencyMS:    int(time.Since(start).Milliseconds()),
	})
}
