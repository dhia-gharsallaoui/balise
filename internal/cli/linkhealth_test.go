package cli_test

import (
	"context"
	"testing"

	"github.com/dhia/balise/internal/cli"
	"github.com/stretchr/testify/require"
)

// TestComputeLinkHealthSurvivesAnIncrementalRun is bug 2's regression test. Before the fix,
// `balise reindex`'s printed "links:"/"within-scope recovery" lines came straight from
// ReindexReport, which only tallies findings for pages that particular invocation actually
// indexed. A second, idempotent reindex touches zero pages (see TestReindexIsIdempotent), so
// its own report.LinksTotal/LinksResolved/etc are all zero — and
// report.WithinScopeRecovery()/ReferentialIntegrity()'s zero-denominator guards then render
// that as a false 100%, even though the vault is full of real, previously-indexed links.
// cli.ComputeLinkHealth reads the same lint_findings back from storage instead, so it must
// report the vault's true link health regardless of how little a given run touched.
func TestComputeLinkHealthSurvivesAnIncrementalRun(t *testing.T) {
	pages, q := importedVault(t)
	ctx := context.Background()

	first, err := cli.Reindex(ctx, q, pages, "../../defaults", nil)
	require.NoError(t, err)
	require.Positive(t, first.LinksTotal, "a freshly reindexed real corpus must have some links")

	healthAfterFull, err := cli.ComputeLinkHealth(ctx, q)
	require.NoError(t, err)
	require.Equal(t, first.LinksTotal, healthAfterFull.Total)
	require.Equal(t, first.LinksResolved, healthAfterFull.Resolved)
	require.Equal(t, first.LinksCrossScope, healthAfterFull.CrossScope)
	require.Equal(t, first.LinksMissing, healthAfterFull.Missing)

	second, err := cli.Reindex(ctx, q, pages, "../../defaults", nil)
	require.NoError(t, err)
	require.Zero(t, second.Changed, "re-indexing unchanged files must be a no-op")
	// This is the precondition the bug depended on: an incremental run's own in-memory
	// tally is empty, because indexer.IndexPage never computes findings for a noop page.
	require.Zero(t, second.LinksTotal,
		"a fully-idempotent run's own report must be empty — that emptiness is exactly what made the old printed line lie")

	healthAfterNoop, err := cli.ComputeLinkHealth(ctx, q)
	require.NoError(t, err)
	require.Equal(t, healthAfterFull, healthAfterNoop,
		"link health read from storage must not collapse just because the latest run touched no pages")

	countsLine, ratiosLine := cli.FormatLinkHealth(healthAfterNoop)
	require.Contains(t, countsLine, "links:")
	require.NotContains(t, ratiosLine, "n/a", "the real corpus has links on both sides of the ratio, so this must be a real percentage")
}

// TestFormatLinkHealthGuardsZeroDenominators is a pure unit test for the formatting rules
// bug 2 also requires directly: a zero-denominator ratio must never render as "100.0%".
func TestFormatLinkHealthGuardsZeroDenominators(t *testing.T) {
	t.Run("nothing indexed at all", func(t *testing.T) {
		_, ratios := cli.FormatLinkHealth(cli.LinkHealth{})
		require.NotContains(t, ratios, "100.0%")
		require.Contains(t, ratios, "n/a")
	})

	t.Run("only cross-scope links, nothing resolved or missing", func(t *testing.T) {
		// Referential integrity is genuinely 100% here: none of the 5 links are missing,
		// they're cross-scope (the target exists, just under a different scope), so the
		// "target exists somewhere" ratio is truthfully whole. Only within-scope recovery —
		// which has neither a resolved nor a missing link to divide — has nothing to report.
		_, ratios := cli.FormatLinkHealth(cli.LinkHealth{Total: 5, CrossScope: 5})
		require.Contains(t, ratios, "within-scope recovery n/a")
		require.Contains(t, ratios, "referential integrity 100.0%")
	})

	t.Run("a real mix renders real percentages", func(t *testing.T) {
		_, ratios := cli.FormatLinkHealth(cli.LinkHealth{Total: 10, Resolved: 8, Missing: 2})
		require.Equal(t, "within-scope recovery 80.0%, referential integrity 80.0%", ratios)
	})
}
