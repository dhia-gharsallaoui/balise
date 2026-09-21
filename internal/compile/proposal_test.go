package compile_test

import (
	"strings"
	"testing"
	"time"

	"github.com/dhia/balise/internal/compile"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func sampleBody() string {
	return "Any PUT on the gateway replaces the resource.\n" +
		"This deletes every ExpressRoute connection immediately.\n" +
		"Re-create the connections after any update."
}

func fixedNow() time.Time {
	return time.Date(2026, 9, 18, 8, 0, 12, 0, time.UTC)
}

func TestBuildProposalSetsTheFixedTopLevelFields(t *testing.T) {
	change := compile.ClaimsChange{Keep: []string{"c1"}}
	p := compile.BuildProposal("work/gotcha/azapi-ergw-connection-deletion.md", "work",
		"azapi-ergw-connection-deletion", change, 0.9, sampleBody(), fixedNow())

	require.NotEmpty(t, p.ID, "id must be a generated ULID, never empty")
	require.Equal(t, "claims", p.Kind)
	require.Equal(t, "work", p.Scope)
	require.Equal(t, "azapi-ergw-connection-deletion", p.Target)
	require.InDelta(t, 0.9, p.Confidence, 0.0001)
	require.Equal(t, "compile:2026-09-18T08:00:12Z", p.CreatedBy)
	require.Equal(t, "pending", p.Status)
}

func TestBuildProposalGeneratesADistinctIDPerCall(t *testing.T) {
	change := compile.ClaimsChange{Keep: []string{"c1"}}
	first := compile.BuildProposal("a.md", "work", "a", change, 0.9, "", fixedNow())
	second := compile.BuildProposal("a.md", "work", "a", change, 0.9, "", fixedNow())
	require.NotEqual(t, first.ID, second.ID)
}

func TestBuildProposalPreservesKeepIDsVerbatim(t *testing.T) {
	change := compile.ClaimsChange{Keep: []string{"c1", "c2"}}
	p := compile.BuildProposal("a.md", "work", "a", change, 0.9, "", fixedNow())
	require.Equal(t, []string{"c1", "c2"}, p.Change.Claims.Keep)
}

func TestBuildProposalConvertsRewordEntries(t *testing.T) {
	change := compile.ClaimsChange{
		Reword: []compile.RewordClaim{{ID: "c3", Text: "Re-create connections only when a full replace is unavoidable"}},
	}
	p := compile.BuildProposal("a.md", "work", "a", change, 0.9, "", fixedNow())
	require.Equal(t, []compile.ProposalReword{
		{ID: "c3", Text: "Re-create connections only when a full replace is unavoidable"},
	}, p.Change.Claims.Reword)
}

func TestBuildProposalStampsRetireWithTheRunDateAsAsOf(t *testing.T) {
	change := compile.ClaimsChange{Retire: []compile.RetireClaim{{ID: "c4", Reason: "provider fixed the underlying bug"}}}
	p := compile.BuildProposal("a.md", "work", "a", change, 0.9, "", fixedNow())
	require.Equal(t, []compile.ProposalRetire{{ID: "c4", AsOf: "2026-09-18"}}, p.Change.Claims.Retire)
}

func TestBuildProposalKeepsAddStatusExplicit(t *testing.T) {
	change := compile.ClaimsChange{
		Add: []compile.AddClaim{{Text: "AzAPI PUT deletes connections", Status: "active", Span: []int{1, 2}}},
	}
	p := compile.BuildProposal("a.md", "work", "a", change, 0.9, sampleBody(), fixedNow())
	require.Len(t, p.Change.Claims.Add, 1)
	require.Equal(t, "active", p.Change.Claims.Add[0].Status)
	require.Equal(t, []int{1, 2}, p.Change.Claims.Add[0].Span)
}

func TestBuildProposalCitesEvidenceForAddEntriesWithAResolvableSpan(t *testing.T) {
	change := compile.ClaimsChange{
		Add: []compile.AddClaim{{Text: "AzAPI PUT deletes connections", Status: "active", Span: []int{1, 2}}},
	}
	p := compile.BuildProposal("work/gotcha/foo.md", "work", "foo", change, 0.9, sampleBody(), fixedNow())
	require.Len(t, p.Evidence, 1)
	require.Equal(t, "work/gotcha/foo.md", p.Evidence[0].Source)
	require.Equal(t, []int{1, 2}, p.Evidence[0].Span)
	require.Contains(t, p.Evidence[0].Text, "gateway")
}

func TestBuildProposalSkipsEvidenceForAnAddEntryWithNoSpan(t *testing.T) {
	change := compile.ClaimsChange{
		Add: []compile.AddClaim{{Text: "AzAPI PUT deletes connections", Status: "active"}},
	}
	p := compile.BuildProposal("work/gotcha/foo.md", "work", "foo", change, 0.9, sampleBody(), fixedNow())
	require.Empty(t, p.Evidence)
}

func TestBuildProposalSkipsEvidenceForASpanOutsideTheBody(t *testing.T) {
	change := compile.ClaimsChange{
		Add: []compile.AddClaim{{Text: "AzAPI PUT deletes connections", Status: "active", Span: []int{40, 44}}},
	}
	p := compile.BuildProposal("work/gotcha/foo.md", "work", "foo", change, 0.9, sampleBody(), fixedNow())
	require.Empty(t, p.Evidence, "a span past the end of the body must not produce a fabricated citation")
}

func TestProposalRenderProducesFrontmatterFencedYAML(t *testing.T) {
	change := compile.ClaimsChange{Keep: []string{"c1"}}
	p := compile.BuildProposal("work/gotcha/foo.md", "work", "foo", change, 0.82, "", fixedNow())

	rendered, err := p.Render("")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(rendered, "---\n"))
	require.Equal(t, 2, strings.Count(rendered, "---\n"), "exactly an opening and closing fence, no rationale")

	body := strings.TrimSuffix(strings.TrimPrefix(rendered, "---\n"), "---\n")
	var decoded map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(body), &decoded))
	require.Equal(t, "claims", decoded["kind"])
	require.Equal(t, "pending", decoded["status"])
}

func TestProposalRenderAppendsARationaleAfterTheClosingFence(t *testing.T) {
	change := compile.ClaimsChange{Keep: []string{"c1"}}
	p := compile.BuildProposal("work/gotcha/foo.md", "work", "foo", change, 0.82, "", fixedNow())

	rendered, err := p.Render("kept c1 unchanged; it still matches the body")
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(rendered, "kept c1 unchanged; it still matches the body\n"))
}

func TestProposalRenderKeyOrderMatchesSection35(t *testing.T) {
	change := compile.ClaimsChange{Keep: []string{"c1"}}
	p := compile.BuildProposal("work/gotcha/foo.md", "work", "foo", change, 0.82, "", fixedNow())

	rendered, err := p.Render("")
	require.NoError(t, err)

	order := []string{"id:", "kind:", "scope:", "target:", "confidence:", "created_by:", "change:", "status:"}
	lastIdx := -1
	for _, key := range order {
		idx := strings.Index(rendered, key)
		require.Greater(t, idx, lastIdx, "expected %q to appear after the previous key", key)
		lastIdx = idx
	}
}

// Fix 1: a ClaimWarning passed into BuildProposal must attach to the specific
// reword/add entry it describes and survive into the rendered YAML, so the
// Review screen can show it without re-deriving anything from the raw change.
func TestBuildProposalAttachesAWarningToTheMatchingRewordEntryByID(t *testing.T) {
	change := compile.ClaimsChange{
		Reword: []compile.RewordClaim{{ID: "c1", Text: "a reworded but still overlong claim about the gateway"}},
	}
	warning := compile.ClaimWarning{Bucket: "reword", ID: "c1", Text: change.Reword[0].Text, Code: "over_word_limit"}
	p := compile.BuildProposal("work/gotcha/foo.md", "work", "foo", change, 0.65, sampleBody(), fixedNow(), warning)

	require.Len(t, p.Change.Claims.Reword, 1)
	require.Equal(t, []string{"over_word_limit"}, p.Change.Claims.Reword[0].Warnings)
}

func TestBuildProposalAttachesAWarningToTheMatchingAddEntryByText(t *testing.T) {
	overlong := "this claim just keeps going on and on and on well past the fifteen word limit"
	change := compile.ClaimsChange{
		Add: []compile.AddClaim{{Text: overlong, Status: "active"}},
	}
	warning := compile.ClaimWarning{Bucket: "add", Text: overlong, Code: "over_word_limit"}
	p := compile.BuildProposal("work/gotcha/foo.md", "work", "foo", change, 0.65, sampleBody(), fixedNow(), warning)

	require.Len(t, p.Change.Claims.Add, 1)
	require.Equal(t, []string{"over_word_limit"}, p.Change.Claims.Add[0].Warnings)
}

func TestBuildProposalLeavesWarningsEmptyWhenNoneArePassed(t *testing.T) {
	change := compile.ClaimsChange{Keep: []string{"c1"}}
	p := compile.BuildProposal("work/gotcha/foo.md", "work", "foo", change, 0.9, sampleBody(), fixedNow())
	require.Empty(t, p.Change.Claims.Reword)
	require.Empty(t, p.Change.Claims.Add)
}

func TestProposalRenderSerializesAWarningAsAYAMLFlowList(t *testing.T) {
	overlong := "this claim just keeps going on and on and on well past the fifteen word limit"
	change := compile.ClaimsChange{
		Add: []compile.AddClaim{{Text: overlong, Status: "active"}},
	}
	warning := compile.ClaimWarning{Bucket: "add", Text: overlong, Code: "over_word_limit"}
	p := compile.BuildProposal("work/gotcha/foo.md", "work", "foo", change, 0.65, sampleBody(), fixedNow(), warning)

	rendered, err := p.Render("")
	require.NoError(t, err)

	body := strings.TrimSuffix(strings.TrimPrefix(rendered, "---\n"), "---\n")
	var decoded map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(body), &decoded))
	change_ := decoded["change"].(map[string]any)
	claims := change_["claims"].(map[string]any)
	adds := claims["add"].([]any)
	require.Len(t, adds, 1)
	add := adds[0].(map[string]any)
	warnings, ok := add["warnings"].([]any)
	require.True(t, ok, "warnings must serialize as a YAML list under the add entry")
	require.Equal(t, []any{"over_word_limit"}, warnings)
}
