package indexer_test

import (
	"strings"
	"testing"

	"github.com/dhia/balise/internal/indexer"
	"github.com/dhia/balise/internal/registry"
	"github.com/stretchr/testify/require"
)

var indexPages = []indexer.IndexEntry{
	{Title: "ER circuit live in the primary region", Slug: "globex-er", Type: "state"},
	{Title: "AzAPI update deletes connections", Slug: "ergw", Type: "gotcha"},
	{Title: "Connections lost after resize", Slug: "inc-1", Type: "incident", Historical: true},
}

func testOrder(t *testing.T) *registry.Order {
	t.Helper()
	order, err := registry.LoadOrder("../../defaults/order.yaml")
	require.NoError(t, err)
	return order
}

func TestRendersOneLinePerPage(t *testing.T) {
	require.Contains(t, indexer.RenderIndex(indexPages, testOrder(t)),
		"- ER circuit live in the primary region → globex-er [state]")
}

func TestGroupsInAgentOrder(t *testing.T) {
	out := indexer.RenderIndex(indexPages, testOrder(t))
	require.Less(t, strings.Index(out, "## state"), strings.Index(out, "## gotcha"))
}

func TestHistoricalGoesLastUnderItsOwnHeading(t *testing.T) {
	out := indexer.RenderIndex(indexPages, testOrder(t))
	require.Greater(t, strings.Index(out, "## Historical"), strings.Index(out, "## gotcha"))
	require.Contains(t, strings.SplitN(out, "## Historical", 2)[1], "inc-1")
}

func TestAgentOrderMatchesTheSpec(t *testing.T) {
	require.Equal(t,
		[]string{"state", "decision", "gotcha", "procedure", "issue", "incident"},
		testOrder(t).Names()[:6])
}

func TestEmptyInputRendersAHeadingOnly(t *testing.T) {
	require.True(t, strings.HasPrefix(strings.TrimSpace(indexer.RenderIndex(nil, testOrder(t))), "#"))
}
