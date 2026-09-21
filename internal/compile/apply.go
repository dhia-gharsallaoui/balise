package compile

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// ClaimRecord is one claim exactly as a page's frontmatter `claims` list stores it
// (04-technical-spec-v1.md section 3.2), including the fields extract_claims' own lighter
// claimFrontmatter (run.go) never needs to round-trip: as_of and span. Accepting a proposal
// reads a page's claims into this shape, applies ApplyClaims, and writes the result straight
// back with vault.Page.With("claims", ...) — so this type's yaml tags are the page's real
// on-disk field names, not an API-only convenience shape.
type ClaimRecord struct {
	Text   string `yaml:"text"`
	Status string `yaml:"status"`
	ID     string `yaml:"id"`
	AsOf   string `yaml:"as_of,omitempty"`
	Span   []int  `yaml:"span,omitempty"`
}

// ClaimMark labels what ApplyClaims did to one output claim. The review screen must mark
// added/reworded/retired claims "by a word" (02-ui-design-v1.md section 5.3), never by
// diffing the before/after lists itself — ApplyClaims is the one place that decision is
// made, so the API and any future caller agree with each other by construction.
type ClaimMark string

const (
	MarkUnchanged ClaimMark = "unchanged"
	MarkAdded     ClaimMark = "added"
	MarkReworded  ClaimMark = "reworded"
	MarkRetired   ClaimMark = "retired"
)

// AppliedClaim pairs one resulting claim with the mark describing how it got there.
type AppliedClaim struct {
	Claim ClaimRecord
	Mark  ClaimMark
}

// ApplyClaims applies a proposal's keep/reword/retire/add classification to a page's
// current claims (01-design-spec-v0.4.md section 6.6 / F-50):
//
//   - keep: the listed ids are copied unchanged.
//   - reword: the listed id's text is replaced; id, status, as_of and span are untouched.
//   - retire: the listed id's status becomes "superseded" and as_of is stamped with the
//     proposal's own as_of. Retire never deletes — the claim stays in the list, so it stays
//     visible in the review diff and on the page; only pack assembly (elsewhere) excludes a
//     superseded claim by default.
//   - add: each entry becomes a new claim with a freshly minted id that cannot collide with
//     any id already on the page (nextClaimIDs). An add whose status is "superseded" (used
//     to express intra-page supersession among claims the page never had before — see
//     ProposalAdd's doc comment) has no as_of of its own in the proposal shape, so it is
//     stamped with asOf, the same date used for every retire this call processes.
//
// An existing claim named in neither reword nor retire is copied unchanged — whether or not
// it was also named in keep — because "exhaustive" (ProposalClaims' doc comment) describes
// what extract_claims is expected to produce, not a contract ApplyClaims may assume blindly:
// an omitted id must never silently vanish. reword or retire naming an id that is not
// actually on the page, conversely, is treated as a malformed proposal and rejected outright
// (never silently ignored, never partially applied) rather than producing a claims list that
// quietly drops the requested change.
func ApplyClaims(existing []ClaimRecord, change ProposalClaims, asOf time.Time) ([]AppliedClaim, error) {
	rewordText := make(map[string]string, len(change.Reword))
	for _, r := range change.Reword {
		rewordText[r.ID] = r.Text
	}
	retireAsOf := make(map[string]string, len(change.Retire))
	for _, r := range change.Retire {
		retireAsOf[r.ID] = r.AsOf
	}
	matched := make(map[string]bool, len(existing))

	out := make([]AppliedClaim, 0, len(existing)+len(change.Add))
	for _, c := range existing {
		if newAsOf, ok := retireAsOf[c.ID]; ok {
			matched[c.ID] = true
			updated := c
			updated.Status = "superseded"
			updated.AsOf = newAsOf
			out = append(out, AppliedClaim{Claim: updated, Mark: MarkRetired})
			continue
		}
		if newText, ok := rewordText[c.ID]; ok {
			matched[c.ID] = true
			updated := c
			updated.Text = newText
			out = append(out, AppliedClaim{Claim: updated, Mark: MarkReworded})
			continue
		}
		out = append(out, AppliedClaim{Claim: c, Mark: MarkUnchanged})
	}

	for id := range rewordText {
		if !matched[id] {
			return nil, fmt.Errorf("reword: claim %s is not on this page", id)
		}
	}
	for id := range retireAsOf {
		if !matched[id] {
			return nil, fmt.Errorf("retire: claim %s is not on this page", id)
		}
	}

	ids := nextClaimIDs(existing, len(change.Add))
	asOfStamp := asOf.UTC().Format("2006-01-02")
	for i, a := range change.Add {
		status := a.Status
		if status == "" {
			status = "active"
		}
		rec := ClaimRecord{ID: ids[i], Text: a.Text, Status: status}
		if status == "superseded" {
			rec.AsOf = asOfStamp
		}
		if len(a.Span) == 2 {
			rec.Span = a.Span
		}
		out = append(out, AppliedClaim{Claim: rec, Mark: MarkAdded})
	}
	return out, nil
}

// Claims extracts the plain ClaimRecord list from a slice of AppliedClaim, discarding the
// marks — this is what actually gets written back to the page's frontmatter.
func Claims(applied []AppliedClaim) []ClaimRecord {
	out := make([]ClaimRecord, len(applied))
	for i, a := range applied {
		out[i] = a.Claim
	}
	return out
}

var claimIDPattern = regexp.MustCompile(`^c(\d+)$`)

// nextClaimIDs returns n fresh ids in the page's own c<N> convention (section 3.2's c1..c4
// examples), continuing from the highest numbered id already on the page so an add can never
// collide with an existing claim. A page with no claim in that convention yet starts at c1.
func nextClaimIDs(existing []ClaimRecord, n int) []string {
	max := 0
	for _, c := range existing {
		if m := claimIDPattern.FindStringSubmatch(c.ID); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil && v > max {
				max = v
			}
		}
	}
	ids := make([]string, n)
	for i := range ids {
		max++
		ids[i] = "c" + strconv.Itoa(max)
	}
	return ids
}
