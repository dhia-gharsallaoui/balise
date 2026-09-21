package compile_test

import (
	"strings"
	"testing"

	"github.com/dhia/balise/internal/compile"
	"github.com/stretchr/testify/require"
)

func oneExistingClaim() []compile.ExistingClaim {
	return []compile.ExistingClaim{
		{ID: "c1", Status: "active", Text: "AzAPI PUT on an ER gateway deletes all expressRouteConnections"},
	}
}

func containsProblemMatching(t *testing.T, problems []string, substr string) {
	t.Helper()
	for _, p := range problems {
		if strings.Contains(p, substr) {
			return
		}
	}
	t.Fatalf("expected a problem containing %q, got: %v", substr, problems)
}

func containsWarningWithCode(t *testing.T, warnings []compile.ClaimWarning, code string) compile.ClaimWarning {
	t.Helper()
	for _, w := range warnings {
		if w.Code == code {
			return w
		}
	}
	t.Fatalf("expected a warning with code %q, got: %v", code, warnings)
	return compile.ClaimWarning{}
}

func TestValidateSemanticsAcceptsAKeepOnlyChange(t *testing.T) {
	change := compile.ClaimsChange{Keep: []string{"c1"}}
	problems, warnings := compile.ValidateSemantics(change, oneExistingClaim(), 6)
	require.Empty(t, problems)
	require.Empty(t, warnings)
}

func TestValidateSemanticsRejectsAnUnclassifiedExistingClaim(t *testing.T) {
	change := compile.ClaimsChange{
		Add: []compile.AddClaim{{Text: "azapi_update_resource patches gateways without deleting connections", Status: "active"}},
	}
	problems, _ := compile.ValidateSemantics(change, oneExistingClaim(), 6)
	containsProblemMatching(t, problems, "not classified")
}

func TestValidateSemanticsRejectsADoubleClassifiedExistingClaim(t *testing.T) {
	change := compile.ClaimsChange{
		Keep:   []string{"c1"},
		Retire: []compile.RetireClaim{{ID: "c1"}},
	}
	problems, _ := compile.ValidateSemantics(change, oneExistingClaim(), 6)
	containsProblemMatching(t, problems, "more than once")
}

func TestValidateSemanticsRejectsAHallucinatedID(t *testing.T) {
	change := compile.ClaimsChange{Keep: []string{"c1", "c99"}}
	problems, _ := compile.ValidateSemantics(change, oneExistingClaim(), 6)
	containsProblemMatching(t, problems, "does not exist")
}

func TestValidateSemanticsRejectsABareSubjectAsAddedClaimText(t *testing.T) {
	change := compile.ClaimsChange{
		Keep: []string{"c1"},
		Add:  []compile.AddClaim{{Text: "azapi-ergw-connection-deletion", Status: "active"}},
	}
	problems, _ := compile.ValidateSemantics(change, oneExistingClaim(), 6)
	containsProblemMatching(t, problems, "subject, not a statement")
}

// Fix 1: an over-length claim is a warning, never a hard problem — a run over
// the full corpus must not discard an otherwise-good page over one long claim.
func TestValidateSemanticsWarnsOnAnOverlongAddedClaimWithoutRejectingIt(t *testing.T) {
	text := "this claim just keeps going on and on and on well past the fifteen word limit for a single claim"
	change := compile.ClaimsChange{
		Keep: []string{"c1"},
		Add:  []compile.AddClaim{{Text: text, Status: "active"}},
	}
	problems, warnings := compile.ValidateSemantics(change, oneExistingClaim(), 6)
	require.Empty(t, problems, "an over-length claim must never be a hard problem")
	w := containsWarningWithCode(t, warnings, "over_word_limit")
	require.Equal(t, "add", w.Bucket)
	require.Equal(t, text, w.Text)
}

func TestValidateSemanticsWarnsOnAnOverlongRewordedClaimWithoutRejectingIt(t *testing.T) {
	text := "this reworded claim just keeps going on and on and on well past the fifteen word limit"
	change := compile.ClaimsChange{
		Reword: []compile.RewordClaim{{ID: "c1", Text: text}},
	}
	problems, warnings := compile.ValidateSemantics(change, oneExistingClaim(), 6)
	require.Empty(t, problems, "an over-length claim must never be a hard problem")
	w := containsWarningWithCode(t, warnings, "over_word_limit")
	require.Equal(t, "reword", w.Bucket)
	require.Equal(t, "c1", w.ID)
	require.Equal(t, text, w.Text)
}

func TestValidateSemanticsRejectsZeroTotalClaims(t *testing.T) {
	problems, _ := compile.ValidateSemantics(compile.ClaimsChange{}, nil, 6)
	require.NotEmpty(t, problems, "an empty change with no existing claims must fail (zero claims)")
}

func TestValidateSemanticsRejectsExceedingMaxClaims(t *testing.T) {
	change := compile.ClaimsChange{
		Keep: []string{"c1"},
		Add: []compile.AddClaim{
			{Text: "first newly observed claim about the gateway", Status: "active"},
			{Text: "second newly observed claim about the gateway", Status: "active"},
		},
	}
	problems, _ := compile.ValidateSemantics(change, oneExistingClaim(), 2)
	containsProblemMatching(t, problems, "max_claims")
}

func TestValidateSemanticsCountsRetiredClaimsTowardTheTotal(t *testing.T) {
	// A retired claim stays on the page (status flips to superseded, per the
	// azapi-ergw-connection-deletion fixture) — it must still count toward
	// max_claims, not be treated as removed.
	change := compile.ClaimsChange{Retire: []compile.RetireClaim{{ID: "c1", Reason: "outdated"}}}
	problems, _ := compile.ValidateSemantics(change, oneExistingClaim(), 6)
	require.Empty(t, problems)
}
