package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContextRejectsUnauthenticatedCall(t *testing.T) {
	d := newTestDeps(t)
	_, _, err := d.context(context.Background(), reqWithToken("bogus"), ContextInput{Query: "widget"})
	require.ErrorIs(t, err, ErrUnauthenticated)
}

func TestContextRequiresReadCapability(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "work", "widget", "widget claim")
	raw := mintToken(t, d.pool, "writer-only", []string{"work"}, []string{"remember"})

	_, _, err := d.context(context.Background(), reqWithToken(raw), ContextInput{Query: "widget"})
	require.Error(t, err)
}

// TestContextRefusesScopeOutsideToken is the hostile cross-scope input: a
// scope argument narrows, never widens (04 section 14).
func TestContextRefusesScopeOutsideToken(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "client-globex", "secret", "secret claim")
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, out, err := d.context(context.Background(), reqWithToken(raw), ContextInput{Query: "secret", Scope: "client-globex"})
	require.Error(t, err)
	require.Empty(t, out.Pages)
}

// TestContextNegativeBudgetIsRejected is the hostile negative-budget input:
// a negative budget is not a meaningful request under any reading, so it is
// refused outright rather than silently reinterpreted as 0 or the default.
func TestContextNegativeBudgetIsRejected(t *testing.T) {
	d := newTestDeps(t)
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})
	budget := -1

	_, out, err := d.context(context.Background(), reqWithToken(raw), ContextInput{Query: "widget", BudgetTokens: &budget})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be negative")
	require.Empty(t, out.Pages)
}

// TestContextZeroBudgetPacksNothingButSucceeds is the hostile zero-budget
// input: an explicit 0 is a real, if degenerate, request -- honoured
// literally, not treated as "omitted." Every matching candidate has a
// nonzero token count, so every one of them is oversize against a
// zero-token budget: it could never fit regardless of what else was
// packed, which is exactly what "oversize" (rather than "budget") means.
func TestContextZeroBudgetPacksNothingButSucceeds(t *testing.T) {
	d := newTestDeps(t)
	seedPageFull(t, d.pool, "work", "widget", "note", "widget gizmo claim", 10, false)
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})
	budget := 0

	_, out, err := d.context(context.Background(), reqWithToken(raw), ContextInput{Query: "widget", BudgetTokens: &budget})
	require.NoError(t, err)
	require.Empty(t, out.Pages)
	require.Equal(t, "low", out.Coverage)
	require.Equal(t, 0, out.TokensUsed)
	require.Len(t, out.Unloaded, 1)
	require.Equal(t, "widget", out.Unloaded[0].Slug)
	require.Equal(t, "oversize", out.Unloaded[0].Reason)
}

// TestContextReportsOversizeWhenPageExceedsWholeBudget: a page whose own
// token count exceeds a nonzero budget can never fit regardless of packing
// order -- "oversize", not "budget".
func TestContextReportsOversizeWhenPageExceedsWholeBudget(t *testing.T) {
	d := newTestDeps(t)
	seedPageFull(t, d.pool, "work", "huge", "note", "huge widget claim", 100, false)
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})
	budget := 50

	_, out, err := d.context(context.Background(), reqWithToken(raw), ContextInput{Query: "widget", BudgetTokens: &budget})
	require.NoError(t, err)
	require.Empty(t, out.Pages)
	require.Len(t, out.Unloaded, 1)
	require.Equal(t, "oversize", out.Unloaded[0].Reason)
}

// TestContextReportsBudgetReasonWhenSpentByHigherRanked: two pages both fit
// individually, but not both together -- the lower-ranked one is reported
// unloaded with reason "budget" (it would fit fresh, just not after the
// higher-ranked one spent the budget), never "oversize" and never silently
// dropped or truncated.
func TestContextReportsBudgetReasonWhenSpentByHigherRanked(t *testing.T) {
	d := newTestDeps(t)
	// "state" ranks above "note" in defaults/order.yaml's agent_order, so
	// the state page is packed first regardless of search score.
	seedPageFull(t, d.pool, "work", "first", "state", "widget priority claim", 30, false)
	seedPageFull(t, d.pool, "work", "second", "note", "widget priority claim", 30, false)
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})
	budget := 40

	_, out, err := d.context(context.Background(), reqWithToken(raw), ContextInput{Query: "widget priority", BudgetTokens: &budget})
	require.NoError(t, err)
	require.Len(t, out.Pages, 1)
	require.Equal(t, "first", out.Pages[0].Slug)
	require.Equal(t, 30, out.TokensUsed)
	require.Len(t, out.Unloaded, 1)
	require.Equal(t, "second", out.Unloaded[0].Slug)
	require.Equal(t, "budget", out.Unloaded[0].Reason)
	require.Equal(t, "ok", out.Coverage)
}

// TestContextOrdersByRegistryTypeThenScore: both pages fit the budget, but
// the "state" page (ranked above "note" in defaults/order.yaml) must still
// come first in Pages, regardless of search score.
func TestContextOrdersByRegistryTypeThenScore(t *testing.T) {
	d := newTestDeps(t)
	seedPageFull(t, d.pool, "work", "a-note", "note", "widget claim", 10, false)
	seedPageFull(t, d.pool, "work", "b-state", "state", "widget claim", 10, false)
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, out, err := d.context(context.Background(), reqWithToken(raw), ContextInput{Query: "widget"})
	require.NoError(t, err)
	require.Len(t, out.Pages, 2)
	require.Equal(t, "b-state", out.Pages[0].Slug)
	require.Equal(t, "a-note", out.Pages[1].Slug)
}

// TestContextIncludesEdgeExpansionCandidate: a page that does not itself
// match the query, but is reached by a "specializes" edge from a page that
// does, is still packed -- Why records the edge kind, not a lexical/score
// reason, since it was not found by search at all.
func TestContextIncludesEdgeExpansionCandidate(t *testing.T) {
	d := newTestDeps(t)
	general := seedPageFull(t, d.pool, "work", "general", "note", "general unrelated text", 10, false)
	specific := seedPageFull(t, d.pool, "work", "specific", "note", "widget gizmo claim", 10, false)
	seedEdge(t, d.pool, "work", specific, general, "specializes")
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, out, err := d.context(context.Background(), reqWithToken(raw), ContextInput{Query: "widget"})
	require.NoError(t, err)

	var sawGeneral bool
	for _, p := range out.Pages {
		if p.Slug == "general" {
			sawGeneral = true
			require.Equal(t, "specializes", p.Why)
		}
	}
	require.True(t, sawGeneral, "expansion neighbour reached via a specializes edge must be packed even though it did not itself match the search query")
}

// TestContextDropsHistoricalUnlessIncluded: a historical page that matches
// the query is excluded by default, and included only when
// IncludeHistorical is set.
func TestContextDropsHistoricalUnlessIncluded(t *testing.T) {
	d := newTestDeps(t)
	seedPageFull(t, d.pool, "work", "old-widget", "note", "widget claim", 10, true)
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, out, err := d.context(context.Background(), reqWithToken(raw), ContextInput{Query: "widget"})
	require.NoError(t, err)
	require.Empty(t, out.Pages)
	require.Equal(t, "low", out.Coverage)

	_, out2, err := d.context(context.Background(), reqWithToken(raw), ContextInput{Query: "widget", IncludeHistorical: true})
	require.NoError(t, err)
	require.Len(t, out2.Pages, 1)
	require.Equal(t, "old-widget", out2.Pages[0].Slug)
}

func TestContextLowCoverageWhenNothingMatches(t *testing.T) {
	d := newTestDeps(t)
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, out, err := d.context(context.Background(), reqWithToken(raw), ContextInput{Query: "nothing-matches-anything"})
	require.NoError(t, err)
	require.Empty(t, out.Pages)
	require.Equal(t, "low", out.Coverage)
}

func TestContextRecordsAnAuditRowOnEveryCall(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "work", "widget", "widget claim")
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	before := countAuditRows(t, d.pool)
	_, _, err := d.context(context.Background(), reqWithToken(raw), ContextInput{Query: "widget"})
	require.NoError(t, err)
	require.Equal(t, before+1, countAuditRows(t, d.pool))
}
