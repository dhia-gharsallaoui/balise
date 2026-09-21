package compile

import (
	"fmt"
	"sort"
	"strings"
)

const (
	minClaimWords = 2  // rules out a bare slug/subject with no spaces
	maxClaimWords = 15 // 01-design-spec-v0.4.md section 1.2 principle 2

	// overWordLimitCode is the flag attached to a ClaimWarning for a claim over
	// maxClaimWords. It is also the exact string persisted into a proposal's
	// per-claim `warnings:` list (proposal.go), so the Review screen and any
	// other consumer can match on it verbatim.
	overWordLimitCode = "over_word_limit"
)

// ClaimWarning flags a claim that is usable but imperfect — unlike a problem, a
// warning never blocks acceptance (fix 1: a run over the full 138-page corpus
// must not discard an otherwise-good page over one 16-word claim). Bucket is
// "reword" or "add"; ID is set only for a reword (an existing claim id), since
// an add has no id yet and is matched back to its ProposalAdd entry by Text.
type ClaimWarning struct {
	Bucket string
	ID     string
	Text   string
	Code   string
}

// claimTextEntry pairs a claim's text with the identifier ValidateSemantics'
// callers need to attribute a warning to the right claim: a reword's existing
// id, or (for an add, which has no id yet) the claim's own text.
type claimTextEntry struct {
	ID   string
	Text string
}

// ValidateSemantics checks business rules that JSON Schema cannot express: every
// existing claim id must be classified exactly once, no id may be invented, each
// new or reworded claim must read like a statement rather than a subject
// (principle 2), and the page must end up with a claim count the type's
// registry entry allows. Those are hard problems — the change is unusable and
// must not become a proposal. A claim over the 15-word limit is different: the
// content is still good, so it comes back as a ClaimWarning instead of a
// problem (fix 1 — see the `long_headline` precedent in
// internal/indexer/pipeline.go, which warns rather than rejecting for the same
// reason). problems is sorted for deterministic output; both are nil when the
// change is entirely clean.
func ValidateSemantics(change ClaimsChange, existing []ExistingClaim, maxClaims int) ([]string, []ClaimWarning) {
	existingIDs := make(map[string]bool, len(existing))
	for _, c := range existing {
		existingIDs[c.ID] = true
	}

	var problems []string
	problems = append(problems, validateExhaustiveClassification(change, existingIDs)...)
	problems = append(problems, validateTotalClaimCount(change, maxClaims)...)

	rewordProblems, rewordWarnings := validateClaimText("reword", rewordEntries(change.Reword))
	addProblems, addWarnings := validateClaimText("add", addEntries(change.Add))
	problems = append(problems, rewordProblems...)
	problems = append(problems, addProblems...)

	sort.Strings(problems)

	var warnings []ClaimWarning
	warnings = append(warnings, rewordWarnings...)
	warnings = append(warnings, addWarnings...)
	return problems, warnings
}

// validateExhaustiveClassification requires every existing claim id to appear in
// exactly one of keep/reword/retire, and rejects any id the model invented.
func validateExhaustiveClassification(change ClaimsChange, existingIDs map[string]bool) []string {
	var problems []string
	seen := map[string]int{}
	for _, id := range change.Keep {
		seen[id]++
	}
	for _, r := range change.Reword {
		seen[r.ID]++
	}
	for _, r := range change.Retire {
		seen[r.ID]++
	}

	for id := range existingIDs {
		switch seen[id] {
		case 0:
			problems = append(problems, fmt.Sprintf(
				"existing claim %q was not classified into keep, reword, or retire", id))
		case 1:
			// exactly right
		default:
			problems = append(problems, fmt.Sprintf(
				"existing claim %q was classified more than once across keep/reword/retire", id))
		}
	}
	for id := range seen {
		if !existingIDs[id] {
			problems = append(problems, fmt.Sprintf("claim id %q does not exist on this page", id))
		}
	}
	return problems
}

// validateClaimText applies principle 2's statement-not-subject rule. A claim
// under minClaimWords still reads like a bare slug and is a hard problem. A
// claim over maxClaimWords is usable content in the wrong shape: it comes back
// as a ClaimWarning (fix 1) rather than a problem, so one long claim can no
// longer sink an otherwise-good page.
func validateClaimText(bucket string, entries []claimTextEntry) ([]string, []ClaimWarning) {
	var problems []string
	var warnings []ClaimWarning
	for _, e := range entries {
		words := strings.Fields(e.Text)
		switch {
		case len(words) < minClaimWords:
			problems = append(problems, fmt.Sprintf(
				"%s claim %q reads like a subject, not a statement (fewer than %d words)",
				bucket, e.Text, minClaimWords))
		case len(words) > maxClaimWords:
			warnings = append(warnings, ClaimWarning{
				Bucket: bucket, ID: e.ID, Text: e.Text, Code: overWordLimitCode,
			})
		}
	}
	return problems, warnings
}

// validateTotalClaimCount enforces 04-technical-spec-v1.md section 3.2's "claims
// required on indexed types (1-max_claims)": retire does not delete a claim, it
// only changes its status to superseded, so the post-change list is
// keep+reword+retire+add — matching how defaults/fixtures/*.md pages carry
// superseded claims permanently alongside active ones.
func validateTotalClaimCount(change ClaimsChange, maxClaims int) []string {
	total := len(change.Keep) + len(change.Reword) + len(change.Retire) + len(change.Add)
	if total < 1 {
		return []string{"the change would leave the page with zero claims"}
	}
	if maxClaims > 0 && total > maxClaims {
		return []string{fmt.Sprintf(
			"the change would leave the page with %d claims, over max_claims of %d", total, maxClaims)}
	}
	return nil
}

// ValidateClaimWord applies principle 2's statement-not-subject word-count rule
// (minClaimWords/maxClaimWords, the same thresholds validateClaimText enforces
// per proposed claim) to a single piece of claim text. internal/api's edit-accept
// endpoint calls this directly so a claim a reviewer edits before accepting is
// held to exactly the same rule as one extract_claims proposes — never a
// stricter gate, never a looser one. problem is non-empty only for a hard
// failure (too short); warningCode is non-empty only for a non-blocking flag
// (too long, always overWordLimitCode today). At most one of the two is ever
// set.
func ValidateClaimWord(text string) (problem string, warningCode string) {
	words := strings.Fields(text)
	switch {
	case len(words) < minClaimWords:
		return fmt.Sprintf("reads like a subject, not a statement (fewer than %d words)", minClaimWords), ""
	case len(words) > maxClaimWords:
		return "", overWordLimitCode
	default:
		return "", ""
	}
}

func rewordEntries(rewords []RewordClaim) []claimTextEntry {
	out := make([]claimTextEntry, len(rewords))
	for i, r := range rewords {
		out[i] = claimTextEntry{ID: r.ID, Text: r.Text}
	}
	return out
}

func addEntries(adds []AddClaim) []claimTextEntry {
	out := make([]claimTextEntry, len(adds))
	for i, a := range adds {
		out[i] = claimTextEntry{Text: a.Text}
	}
	return out
}
