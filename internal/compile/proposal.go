package compile

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/dhia/balise/internal/vault"
	"gopkg.in/yaml.v3"
)

// Proposal is a review/<id>.md file's frontmatter, field order matching
// 04-technical-spec-v1.md section 3.5 exactly. Compile tasks only ever produce
// these — acceptance (applying change to the target page) is a separate,
// human-in-the-loop step this package does not implement.
type Proposal struct {
	ID     string `yaml:"id"`
	Kind   string `yaml:"kind"`
	Scope  string `yaml:"scope"`
	Target string `yaml:"target"`
	// TargetVersion is the target page's PageStore version (the git blob SHA) at the moment
	// this proposal was built (fix 1, part B). It is the same optimistic-concurrency idea
	// PageStore.Write's ifVersion already uses for writes; here it lets accept refuse to
	// apply a proposal whose baseline the target has since moved past, rather than silently
	// applying keep/reword/retire entries against claim ids that may no longer mean what
	// they meant when the proposal was built. Empty on any proposal built before this field
	// existed — accept treats an empty TargetVersion the same way Write treats an empty
	// ifVersion: nothing to check against, so the check is skipped rather than refused.
	TargetVersion string             `yaml:"target_version,omitempty"`
	Confidence    float64            `yaml:"confidence"`
	CreatedBy     string             `yaml:"created_by"`
	Evidence      []ProposalEvidence `yaml:"evidence,omitempty"`
	Change        ProposalChange     `yaml:"change"`
	Status        string             `yaml:"status"`
	// Rejected records the one-line reason a human gave for turning the proposal down
	// (04 section 13's reject endpoint). It is nil on every pending or accepted proposal;
	// only the copy moved into review/.rejected/ ever carries it. Fix 1 also uses this same
	// field when a fresh extraction supersedes an older pending proposal for the same
	// target — the reason just says so instead of describing a human's rejection.
	Rejected *ProposalRejection `yaml:"rejected,omitempty"`
}

// ProposalRejection is the reason a proposal was turned down, nested under `rejected:` so
// the file reads as "rejected: {reason: ...}" rather than a bare string — leaving room for a
// rejector identity or timestamp alongside it later without another format change.
type ProposalRejection struct {
	Reason string `yaml:"reason"`
}

// ProposalEvidence cites the page (or another source) span a claim was drawn from.
type ProposalEvidence struct {
	Source string `yaml:"source"`
	Span   []int  `yaml:"span,omitempty"`
	Text   string `yaml:"text"`
}

// ProposalChange wraps the claims change as section 3.5 nests it.
type ProposalChange struct {
	Claims ProposalClaims `yaml:"claims"`
}

// ProposalClaims is the exhaustive keep/reword/retire/add classification.
type ProposalClaims struct {
	Keep   []string         `yaml:"keep,omitempty"`
	Reword []ProposalReword `yaml:"reword,omitempty"`
	Retire []ProposalRetire `yaml:"retire,omitempty"`
	Add    []ProposalAdd    `yaml:"add,omitempty"`
}

// ProposalReword replaces an existing claim's text without changing its id.
// Warnings (fix 1) flags a non-blocking issue with Text — e.g. "over_word_limit"
// — for the Review screen to surface; it is never populated for a hard problem,
// since those never reach a built proposal at all.
type ProposalReword struct {
	ID       string   `yaml:"id"`
	Text     string   `yaml:"text"`
	Warnings []string `yaml:"warnings,omitempty"`
}

// ProposalRetire flips an existing claim's status to superseded as of a date.
// AsOf is stamped with the compile run's date: extraction only says *that* a
// claim is superseded, and the page body rarely states the exact date it
// stopped being true, so the run date is used as a documented approximation
// rather than left blank.
type ProposalRetire struct {
	ID   string `yaml:"id"`
	AsOf string `yaml:"as_of"`
}

// ProposalAdd is a newly observed claim. Status is included explicitly (rather
// than omitted the way section 3.5's illustrative example omits it) because a
// page's own claims always carry an explicit status (section 3.2), and a page
// can need a claim added as already-superseded to express intra-page
// supersession among claims the page never had before this run.
type ProposalAdd struct {
	Text     string   `yaml:"text"`
	Status   string   `yaml:"status"`
	Span     []int    `yaml:"span,omitempty"`
	Warnings []string `yaml:"warnings,omitempty"`
}

// BuildProposal turns one page's extraction result into a Proposal. pagePath is
// used as the evidence source; body supplies the line text a resolvable span
// points at. now is the compile run's timestamp, used for created_by and for
// any retire's as_of. warnings (fix 1) is variadic and optional — every
// existing call site that doesn't have any keeps working unchanged — and
// carries non-blocking per-claim flags (e.g. an over-length claim) through to
// the built ProposalReword/ProposalAdd entries so the Review screen can show
// them.
func BuildProposal(pagePath, scope, slug string, change ClaimsChange, confidence float64, body string, now time.Time, warnings ...ClaimWarning) Proposal {
	lines := strings.Split(body, "\n")
	return Proposal{
		ID:         vault.NewUID(),
		Kind:       "claims",
		Scope:      scope,
		Target:     slug,
		Confidence: confidence,
		CreatedBy:  "compile:" + now.UTC().Format(time.RFC3339),
		Evidence:   buildEvidence(pagePath, change.Add, lines),
		Change:     ProposalChange{Claims: buildProposalClaims(change, warnings, now)},
		Status:     "pending",
	}
}

// WithTargetVersion returns a copy of p carrying targetVersion (fix 1, part B). It is a
// separate setter rather than another BuildProposal parameter so every existing call site
// (proposal_test.go has ~16) keeps compiling unchanged; run.go calls this once, right after
// BuildProposal, with the candidate page's version as read at the start of this run.
func (p Proposal) WithTargetVersion(targetVersion string) Proposal {
	p.TargetVersion = targetVersion
	return p
}

// buildProposalClaims attaches each ClaimWarning to the ProposalReword/
// ProposalAdd entry it belongs to: a reword warning is matched by claim id
// (stable across the retry that produced it), an add warning by its exact text
// (adds have no id yet, but warnings and the change they describe always come
// from the same decodeAndValidate call, so the text is guaranteed to match).
func buildProposalClaims(change ClaimsChange, warnings []ClaimWarning, now time.Time) ProposalClaims {
	asOf := now.UTC().Format("2006-01-02")

	rewordWarnings := map[string][]string{}
	addWarnings := map[string][]string{}
	for _, w := range warnings {
		switch w.Bucket {
		case "reword":
			rewordWarnings[w.ID] = append(rewordWarnings[w.ID], w.Code)
		case "add":
			addWarnings[w.Text] = append(addWarnings[w.Text], w.Code)
		}
	}

	reword := make([]ProposalReword, len(change.Reword))
	for i, r := range change.Reword {
		reword[i] = ProposalReword{ID: r.ID, Text: r.Text, Warnings: rewordWarnings[r.ID]}
	}

	retire := make([]ProposalRetire, len(change.Retire))
	for i, r := range change.Retire {
		retire[i] = ProposalRetire{ID: r.ID, AsOf: asOf}
	}

	add := make([]ProposalAdd, len(change.Add))
	for i, a := range change.Add {
		add[i] = ProposalAdd{Text: a.Text, Status: a.Status, Span: a.Span, Warnings: addWarnings[a.Text]}
	}

	return ProposalClaims{Keep: change.Keep, Reword: reword, Retire: retire, Add: add}
}

// buildEvidence cites a body-line snippet for every add entry that carries a
// resolvable span. A span outside the body's line range is skipped rather than
// panicking or emitting an empty citation: a malformed span is exactly the kind
// of thing ValidateSemantics should catch earlier, not something evidence
// construction should paper over silently.
func buildEvidence(pagePath string, adds []AddClaim, lines []string) []ProposalEvidence {
	var evidence []ProposalEvidence
	for _, a := range adds {
		snippet, ok := lineSnippet(lines, a.Span)
		if !ok {
			continue
		}
		evidence = append(evidence, ProposalEvidence{Source: pagePath, Span: a.Span, Text: snippet})
	}
	return evidence
}

// lineSnippet returns the 1-indexed, inclusive line range span=[start,end] of
// lines, joined and trimmed. It reports false when span isn't exactly two
// values or falls outside the body's line range.
func lineSnippet(lines []string, span []int) (string, bool) {
	if len(span) != 2 {
		return "", false
	}
	start, end := span[0], span[1]
	if start < 1 || end < start || end > len(lines) {
		return "", false
	}
	return strings.TrimSpace(strings.Join(lines[start-1:end], " ")), true
}

// Render serializes the proposal as review/<id>.md's exact shape: YAML
// frontmatter fenced by "---" lines, followed by an optional human-readable
// rationale.
func (p Proposal) Render(rationale string) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(p); err != nil {
		return "", fmt.Errorf("encode proposal frontmatter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("close proposal frontmatter encoder: %w", err)
	}

	var out strings.Builder
	out.WriteString("---\n")
	out.WriteString(buf.String())
	out.WriteString("---\n")
	if rationale != "" {
		out.WriteString(rationale)
		if !strings.HasSuffix(rationale, "\n") {
			out.WriteString("\n")
		}
	}
	return out.String(), nil
}
