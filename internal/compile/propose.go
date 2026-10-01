package compile

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/dhia/balise/internal/vault"
)

// propose_pages (04 section 9.1) places remembered notes into the vault: each note an agent
// saved with remember is either attached to an existing page as a new claim, gathered with
// related notes onto a proposed new page, or skipped as not worth keeping. Its output is
// ordinary review/ proposals, so nothing reaches a typed page without a human accepting it.

const (
	placementToolName = "record_note_placement"
	placementToolDesc = "Record, for every remembered note, whether it attaches to a listed page, starts a new page, or is skipped."
	// Output budget for one batch's answer: a claim and a reason per note plus every new
	// page's body. 4096 was measured too small live (18 notes were cut off before new_pages).
	defaultPlacementMaxTokens = 16000
	maxTitleWords             = 20
)

// slugPattern is the vault's kebab-case filename convention (lowercase words joined by single
// hyphens), the shape vault.SlugFromFilename reads back.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Note is one remembered entry awaiting placement. Key identifies it across runs (see
// NoteKey) so a note is only ever proposed once.
type Note struct {
	Key   string
	Path  string // the memory page it was remembered on: the evidence source
	Scope string
	Time  string
	Text  string
}

// NoteKey is a note's stable identity: its page, time of day and text. A log is append-only,
// so this never changes for a note once written.
func NoteKey(path, at, text string) string {
	return bodyHash(path + "\n" + at + "\n" + text)[:16]
}

// CandidatePage is an existing page in the batch's scope that a note might belong on, found
// by deterministic matching before any model call (04 section 9.2).
type CandidatePage struct {
	Path    string
	Slug    string
	Title   string
	Type    string
	Claims  []string // active claim texts, so the model can see what the page already says
	Version string   // becomes the claims proposal's TargetVersion
}

// TypeOption is a page type a new page may take: any indexed type, by its own description.
type TypeOption struct {
	Name        string
	Description string
}

// PlacementInput is one propose_pages call: a batch of notes from a single scope, the pages
// they may attach to, and what a new page may be. Notes never cross scopes, so a client
// scope's notes can only ever be proposed onto that same client's pages.
type PlacementInput struct {
	Scope      string
	Notes      []Note
	Candidates []CandidatePage
	Types      []TypeOption
	// TakenSlugs is every slug already used in Scope, by a page or by a pending proposal's
	// target; a new page may not reuse one.
	TakenSlugs map[string]bool
	Model      string
}

// NoteDecision is the model's verdict on one note (numbered from 1 as the prompt lists them).
type NoteDecision struct {
	Note   int    `json:"note"`
	Action string `json:"action"`
	Target string `json:"target,omitempty"`
	Page   string `json:"page,omitempty"`
	Claim  string `json:"claim,omitempty"`
	Reason string `json:"reason"`
}

// NewPage is one page the model proposes creating, referenced by Slug from decisions.
type NewPage struct {
	Type  string `json:"type"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Placement is a whole validated answer.
type Placement struct {
	Decisions []NoteDecision `json:"decisions"`
	NewPages  []NewPage      `json:"new_pages"`
}

// PlacementResult is one successful propose call, with token spend for the run's totals.
type PlacementResult struct {
	Placement    Placement
	Confidence   float64
	InputTokens  int
	OutputTokens int
}

// ProposePlacement asks the model where each note in the batch belongs, validates the
// answer, and retries exactly once with the specific problems listed back, the same
// contract as ExtractClaims. Over-length claims are warnings, not problems: they still
// trigger the retry, but survive it rather than failing the batch.
func ProposePlacement(ctx context.Context, client LLMClient, in PlacementInput) (PlacementResult, error) {
	_, inputSchema, err := placementSchemas()
	if err != nil {
		return PlacementResult{}, err
	}
	system, user := RenderPlacementPrompt(in)
	call := func(userPrompt string) (ToolResult, error) {
		return client.CallTool(ctx, ToolCall{
			Model: in.Model, System: system, User: userPrompt, ToolName: placementToolName,
			ToolDesc: placementToolDesc, InputSchema: inputSchema, MaxTokens: defaultPlacementMaxTokens,
		})
	}

	first, err := call(user)
	if err != nil {
		return PlacementResult{}, fmt.Errorf("call model: %w", err)
	}
	placement, problems, warnings := decodePlacement(first.Input, in)
	if len(problems) == 0 && len(warnings) == 0 {
		return PlacementResult{placement, confidenceFirstTry, first.InputTokens, first.OutputTokens}, nil
	}

	second, err := call(user + renderPlacementRetry(append(problems, warnings...)))
	if err != nil {
		return PlacementResult{}, fmt.Errorf("call model on retry: %w", err)
	}
	spent := PlacementResult{
		InputTokens: first.InputTokens + second.InputTokens, OutputTokens: first.OutputTokens + second.OutputTokens,
	}
	placement, problems, _ = decodePlacement(second.Input, in)
	if len(problems) > 0 {
		return spent, fmt.Errorf("propose_pages failed validation on both attempts (last problem(s): %s)",
			strings.Join(problems, "; "))
	}
	spent.Placement, spent.Confidence = placement, confidenceAfterRetry
	return spent, nil
}

func renderPlacementRetry(issues []string) string {
	var b strings.Builder
	b.WriteString("\n\nYour previous answer was rejected for the following reason(s):\n")
	for _, issue := range issues {
		fmt.Fprintf(&b, "- %s\n", issue)
	}
	b.WriteString("Return a corrected answer that fixes all of the above.\n")
	return b.String()
}

// decodePlacement validates raw against the schema, decodes it, and checks it against the
// batch: every note decided exactly once, attach only to a listed candidate, new pages of an
// allowed type with an unused, well-formed slug, and every claim a real statement. Hard
// problems fail the answer; warnings (an over-long claim or title) only ask for a retry.
func decodePlacement(raw json.RawMessage, in PlacementInput) (Placement, []string, []string) {
	schema, _, err := placementSchemas()
	if err != nil {
		return Placement{}, []string{err.Error()}, nil
	}
	var asAny any
	if err := json.Unmarshal(raw, &asAny); err != nil {
		return Placement{}, []string{fmt.Sprintf("response was not valid JSON: %v", err)}, nil
	}
	if err := schema.Validate(asAny); err != nil {
		return Placement{}, []string{fmt.Sprintf("response did not match the required schema: %v", err)}, nil
	}
	var p Placement
	if err := json.Unmarshal(raw, &p); err != nil {
		return Placement{}, []string{fmt.Sprintf("response could not be decoded: %v", err)}, nil
	}
	p = normalizePlacement(p, in)
	problems, warnings := validatePlacement(p, in)
	return p, problems, warnings
}

// normalizePlacement reads a new_page decision that names a listed candidate, and none of the
// answer's own new pages, as the attach it can only mean. Models make this slip; failing a
// whole batch over an unambiguous intent would throw away every other note's placement.
func normalizePlacement(p Placement, in PlacementInput) Placement {
	candidates := map[string]bool{}
	for _, c := range in.Candidates {
		candidates[c.Slug] = true
	}
	newPages := map[string]bool{}
	for _, np := range p.NewPages {
		newPages[np.Slug] = true
	}
	decisions := make([]NoteDecision, len(p.Decisions))
	for i, d := range p.Decisions {
		if d.Action == "new_page" && !newPages[d.Page] && candidates[d.Page] {
			d.Action, d.Target, d.Page = "attach", d.Page, ""
		}
		decisions[i] = d
	}
	return Placement{Decisions: decisions, NewPages: p.NewPages}
}

func validatePlacement(p Placement, in PlacementInput) (problems, warnings []string) {
	candidates := map[string]bool{}
	for _, c := range in.Candidates {
		candidates[c.Slug] = true
	}
	types := map[string]bool{}
	for _, t := range in.Types {
		types[t.Name] = true
	}

	pages := map[string]bool{}
	for _, np := range p.NewPages {
		switch {
		case pages[np.Slug]:
			problems = append(problems, fmt.Sprintf("new page slug %q is used twice", np.Slug))
		case !types[np.Type]:
			problems = append(problems, fmt.Sprintf("new page %q has type %q, which is not one of the listed types", np.Slug, np.Type))
		case !slugPattern.MatchString(np.Slug) || len(np.Slug) > 80:
			problems = append(problems, fmt.Sprintf("new page slug %q must be lowercase words joined by hyphens, at most 80 characters", np.Slug))
		case in.TakenSlugs[np.Slug]:
			problems = append(problems, fmt.Sprintf("new page slug %q is already taken; choose another", np.Slug))
		}
		if len(strings.Fields(np.Title)) > maxTitleWords {
			warnings = append(warnings, fmt.Sprintf("new page %q title is over %d words; shorten it", np.Slug, maxTitleWords))
		}
		pages[np.Slug] = true
	}

	decided := map[int]int{}
	skipped := map[int]bool{}
	filled := map[string]bool{}
	for _, d := range p.Decisions {
		if d.Note < 1 || d.Note > len(in.Notes) {
			problems = append(problems, fmt.Sprintf("decision names note %d, but notes run from 1 to %d", d.Note, len(in.Notes)))
			continue
		}
		decided[d.Note]++
		switch d.Action {
		case "skip":
			skipped[d.Note] = true
		case "attach":
			if !candidates[d.Target] {
				problems = append(problems, fmt.Sprintf("note %d attaches to %q, which is not a listed candidate slug", d.Note, d.Target))
			}
		case "new_page":
			if !pages[d.Page] {
				problems = append(problems, fmt.Sprintf("note %d names new page %q, which is not a slug in new_pages", d.Note, d.Page))
			}
			filled[d.Page] = true
		}
		if d.Action != "skip" {
			if problem, warning := ValidateClaimWord(d.Claim); problem != "" {
				problems = append(problems, fmt.Sprintf("note %d claim %q %s", d.Note, d.Claim, problem))
			} else if warning != "" {
				warnings = append(warnings, fmt.Sprintf("note %d claim is over the %d-word limit; a shorter version is preferred", d.Note, maxClaimWords))
			}
		}
	}
	for note := range skipped {
		if decided[note] > 1 {
			problems = append(problems, fmt.Sprintf("note %d is skipped and also placed; skip must be its only decision", note))
		}
	}
	for i := range in.Notes {
		if decided[i+1] == 0 {
			problems = append(problems, fmt.Sprintf("note %d has no decision", i+1))
		}
	}
	for _, np := range p.NewPages {
		if !filled[np.Slug] {
			problems = append(problems, fmt.Sprintf("new page %q is not used by any note's decision", np.Slug))
		}
	}
	return problems, warnings
}

// PlacementOutcome is a validated placement turned into review proposals, plus the action
// taken on each note so the run can record it and never propose the same note twice.
type PlacementOutcome struct {
	Proposals []Proposal
	Actions   map[string]string // note key -> "skip", or the placements it got joined by "+" (e.g. "attach+new_page")
}

// BuildPlacementProposals turns one validated placement into proposals: one add-only claims
// proposal per attached-to page (every note attached to it in this batch, together), and one
// new_page proposal per new page. Evidence on each cites the notes it came from, so the
// reviewer sees the agent's original words beside the restated claim.
func BuildPlacementProposals(in PlacementInput, res PlacementResult, now time.Time) PlacementOutcome {
	createdBy := "compile:" + now.UTC().Format(time.RFC3339)
	candidateVersion := map[string]string{}
	for _, c := range in.Candidates {
		candidateVersion[c.Slug] = c.Version
	}

	type bucket struct {
		adds     []ProposalAdd
		evidence []ProposalEvidence
	}
	attach := map[string]*bucket{}
	fill := map[string]*bucket{}
	out := PlacementOutcome{Actions: map[string]string{}}
	for _, d := range res.Placement.Decisions {
		note := in.Notes[d.Note-1]
		out.Actions[note.Key] = joinAction(out.Actions[note.Key], d.Action)
		var into map[string]*bucket
		var key string
		switch d.Action {
		case "attach":
			into, key = attach, d.Target
		case "new_page":
			into, key = fill, d.Page
		default:
			continue
		}
		b := into[key]
		if b == nil {
			b = &bucket{}
			into[key] = b
		}
		add := ProposalAdd{Text: strings.TrimSpace(d.Claim), Status: "active"}
		if _, warning := ValidateClaimWord(add.Text); warning != "" {
			add.Warnings = []string{warning}
		}
		b.adds = append(b.adds, add)
		cited := slices.ContainsFunc(b.evidence, func(e ProposalEvidence) bool {
			return e.Source == note.Path && e.Text == note.Text
		})
		if !cited {
			b.evidence = append(b.evidence, ProposalEvidence{Source: note.Path, Text: note.Text})
		}
	}

	targets := make([]string, 0, len(attach))
	for slug := range attach {
		targets = append(targets, slug)
	}
	sort.Strings(targets)
	for _, slug := range targets {
		b := attach[slug]
		out.Proposals = append(out.Proposals, Proposal{
			ID: vault.NewUID(), Kind: KindClaims, Scope: in.Scope, Target: slug,
			TargetVersion: candidateVersion[slug], Confidence: res.Confidence, CreatedBy: createdBy,
			Evidence: b.evidence, Change: ProposalChange{Claims: ProposalClaims{Add: b.adds}}, Status: "pending",
		})
	}
	for _, np := range res.Placement.NewPages {
		b := fill[np.Slug]
		if b == nil {
			continue // validation guarantees every new page is filled; never emit an empty one
		}
		out.Proposals = append(out.Proposals, Proposal{
			ID: vault.NewUID(), Kind: KindNewPage, Scope: in.Scope, Target: np.Slug,
			Confidence: res.Confidence, CreatedBy: createdBy, Evidence: b.evidence,
			Change: ProposalChange{Claims: ProposalClaims{Add: b.adds}}, Status: "pending",
			Page: &ProposalPage{Type: np.Type, Title: strings.TrimSpace(np.Title), Body: strings.TrimSpace(np.Body)},
		})
	}
	return out
}

// joinAction accumulates a note's placements for the state file ("attach", "attach+new_page"),
// each kind named once.
func joinAction(prev, action string) string {
	if prev == "" {
		return action
	}
	for _, a := range strings.Split(prev, "+") {
		if a == action {
			return prev
		}
	}
	return prev + "+" + action
}
