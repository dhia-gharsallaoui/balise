package compile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"time"

	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/vault"
)

// proposeStatePath records every note propose_pages has already decided, keyed by NoteKey,
// so a note is proposed at most once however often the task runs. A note is recorded only
// once its proposal (or its skip) is committed alongside this file.
const proposeStatePath = ".balise/compile/propose-pages-state.json"

const (
	// Large enough that one scope's notes from a working session usually share one call: a new
	// page proposed in one batch is invisible to the next, so splitting related notes across
	// batches can propose the same subject twice.
	defaultPlacementBatch = 20
	candidatesPerNote     = 5
	maxCandidates         = 20
)

// CandidateFinder returns the existing pages most related to a note, searched within exactly
// one scope. The engine's search lives in internal/store, which this package does not reach
// into; the CLI supplies an implementation backed by store.Queries.SearchClaims.
type CandidateFinder interface {
	Candidates(ctx context.Context, scope, text string, limit int) ([]CandidatePage, error)
}

// ProposeOptions configures one propose_pages run.
type ProposeOptions struct {
	Scope     string // restrict to one scope; "" means every scope
	Limit     int    // cap on notes attempted; 0 means unlimited
	BatchSize int    // notes per model call; 0 means defaultPlacementBatch
	DryRun    bool   // render prompts and exit without calling the model or writing anything
	Model     string
}

// BatchReport is one model call's outcome.
type BatchReport struct {
	Scope        string
	Notes        int
	Failed       bool
	FailReason   string
	Prompt       string // populated only on DryRun
	Proposals    []string
	Deferred     int // notes left for a later run because their target already had a pending proposal
	InputTokens  int
	OutputTokens int
}

// ProposeReport summarises a whole propose_pages run.
type ProposeReport struct {
	Batches      []BatchReport
	NotesPending int // undecided notes found before Limit applied
	CommitSHAs   []string
}

// Totals sums the run's batches for the CLI summary.
func (r ProposeReport) Totals() (proposals, failed, inputTokens, outputTokens int) {
	for _, b := range r.Batches {
		proposals += len(b.Proposals)
		if b.Failed {
			failed++
		}
		inputTokens += b.InputTokens
		outputTokens += b.OutputTokens
	}
	return proposals, failed, inputTokens, outputTokens
}

// RunProposePages finds every remembered note not yet decided, batches them by scope, and for
// each batch matches candidate pages, asks the model where each note belongs, and writes the
// resulting proposals to review/. Each batch commits on its own together with the state file,
// so a later batch's failure never discards an earlier one's spend. A failed batch is reported
// and skipped, never recorded, so its notes are simply tried again next run.
func RunProposePages(ctx context.Context, pages store.PageStore, reg *registry.Registry,
	finder CandidateFinder, client LLMClient, opts ProposeOptions) (ProposeReport, error) {
	vaultScan, err := scanForNotes(pages, reg)
	if err != nil {
		return ProposeReport{}, err
	}
	state, err := loadProposeState(pages)
	if err != nil {
		return ProposeReport{}, err
	}
	pending, err := pendingProposalsByTarget(pages)
	if err != nil {
		return ProposeReport{}, err
	}
	types := indexedTypes(reg)

	byScope := map[string][]Note{}
	report := ProposeReport{}
	for _, n := range vaultScan.notes {
		if (opts.Scope != "" && n.Scope != opts.Scope) || state.Notes[n.Key].Action != "" {
			continue
		}
		report.NotesPending++
		byScope[n.Scope] = append(byScope[n.Scope], n)
	}

	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = defaultPlacementBatch
	}
	scopes := make([]string, 0, len(byScope))
	for scope := range byScope {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)

	attempted := 0
	now := time.Now()
	for _, scope := range scopes {
		notes := byScope[scope]
		for start := 0; start < len(notes); start += batchSize {
			if opts.Limit > 0 && attempted >= opts.Limit {
				return report, nil
			}
			end := min(start+batchSize, len(notes))
			if opts.Limit > 0 {
				end = min(end, start+opts.Limit-attempted)
			}
			batch := notes[start:end]
			attempted += len(batch)

			in, err := placementInput(ctx, pages, finder, scope, batch, types, vaultScan, pending, opts.Model)
			if err != nil {
				return report, err
			}
			br := BatchReport{Scope: scope, Notes: len(batch)}
			if opts.DryRun {
				system, user := RenderPlacementPrompt(in)
				br.Prompt = system + "\n\n" + user
				report.Batches = append(report.Batches, br)
				continue
			}

			res, err := ProposePlacement(ctx, client, in)
			br.InputTokens, br.OutputTokens = res.InputTokens, res.OutputTokens
			if err != nil {
				br.Failed, br.FailReason = true, err.Error()
				report.Batches = append(report.Batches, br)
				continue
			}
			changes, nextState, br := stageOutcome(BuildPlacementProposals(in, res, now), in, pending, state, br, now)
			if len(changes) > 0 {
				sha, err := pages.Commit(changes, "balise <balise@localhost>", "compile: propose_pages")
				if err != nil {
					return report, fmt.Errorf("commit propose_pages batch: %w", err)
				}
				report.CommitSHAs = append(report.CommitSHAs, sha)
				state = nextState
				// A slug claimed by this batch's new pages is taken for every later batch too.
				for _, p := range br.Proposals {
					vaultScan.slugs[scope][p] = true
				}
			}
			report.Batches = append(report.Batches, br)
		}
	}
	return report, nil
}

// stageOutcome renders one batch's proposals plus the updated state file into a single
// commit's changes. A claims proposal whose target already has a pending proposal is dropped
// (accepting two proposals against one page is the hazard pending.go exists to prevent), and
// the notes behind it are left unrecorded so a later run retries them.
func stageOutcome(out PlacementOutcome, in PlacementInput, pending map[string]pendingProposal,
	state proposeState, br BatchReport, now time.Time) ([]store.Change, proposeState, BatchReport) {
	deferred := map[string]bool{}
	var changes []store.Change
	for _, p := range out.Proposals {
		if _, busy := pending[targetKey(p.Scope, p.Target)]; busy && p.Kind == KindClaims {
			for _, e := range p.Evidence {
				deferred[e.Source+"\x00"+e.Text] = true
			}
			continue
		}
		rendered, err := p.Render("")
		if err != nil {
			// Unreachable for a proposal built from validated input; treat it as deferring
			// these notes rather than committing a half-written batch.
			for _, e := range p.Evidence {
				deferred[e.Source+"\x00"+e.Text] = true
			}
			continue
		}
		changes = append(changes, store.Change{Path: "review/" + p.ID + ".md", Data: []byte(rendered)})
		br.Proposals = append(br.Proposals, p.Target)
	}

	next := state
	for _, n := range in.Notes {
		action, ok := out.Actions[n.Key]
		if !ok {
			continue
		}
		if deferred[n.Path+"\x00"+n.Text] {
			br.Deferred++
			continue
		}
		next = next.withDecided(n.Key, action, now)
	}
	if len(next.Notes) == len(state.Notes) && len(changes) == 0 {
		return nil, state, br
	}
	data, err := next.render()
	if err != nil {
		return nil, state, br
	}
	return append(changes, store.Change{Path: proposeStatePath, Data: data}), next, br
}

// placementInput assembles one batch's model input: candidates found per note in the batch's
// scope (deduplicated, capped), every indexed type a new page may take, and the slugs a new
// page may not reuse.
func placementInput(ctx context.Context, pages store.PageStore, finder CandidateFinder, scope string,
	batch []Note, types []TypeOption, scan noteScan, pending map[string]pendingProposal, model string) (PlacementInput, error) {
	seen := map[string]bool{}
	var candidates []CandidatePage
	for _, n := range batch {
		found, err := finder.Candidates(ctx, scope, n.Text, candidatesPerNote)
		if err != nil {
			return PlacementInput{}, fmt.Errorf("match candidates for %s: %w", n.Path, err)
		}
		for _, c := range found {
			if seen[c.Slug] || len(candidates) >= maxCandidates {
				continue
			}
			seen[c.Slug] = true
			if c.Path != "" {
				if _, version, err := pages.Read(c.Path); err == nil {
					c.Version = version
				}
			}
			candidates = append(candidates, c)
		}
	}

	taken := map[string]bool{}
	for slug := range scan.slugs[scope] {
		taken[slug] = true
	}
	for _, p := range pending {
		if p.Proposal.Scope == scope {
			taken[p.Proposal.Target] = true
		}
	}
	return PlacementInput{Scope: scope, Notes: batch, Candidates: candidates, Types: types, TakenSlugs: taken, Model: model}, nil
}

// noteScan is one pass over the vault: every remembered note, and every slug in use per scope.
type noteScan struct {
	notes []Note
	slugs map[string]map[string]bool
}

func scanForNotes(pages store.PageStore, reg *registry.Registry) (noteScan, error) {
	paths, err := pages.List("")
	if err != nil {
		return noteScan{}, fmt.Errorf("list vault: %w", err)
	}
	scan := noteScan{slugs: map[string]map[string]bool{}}
	for _, p := range paths {
		if !isEligiblePath(p) {
			continue
		}
		raw, _, err := pages.Read(p)
		if err != nil {
			return noteScan{}, fmt.Errorf("read %s: %w", p, err)
		}
		page, err := vault.Parse(string(raw))
		if err != nil || page.Meta == nil {
			continue
		}
		var fm pageFrontmatter
		if err := page.Decode(&fm); err != nil {
			continue
		}
		scope := orDefault(fm.Scope, vault.ScopeOf(p))
		if scan.slugs[scope] == nil {
			scan.slugs[scope] = map[string]bool{}
		}
		scan.slugs[scope][orDefault(fm.Slug, vault.SlugFromFilename(path.Base(p)))] = true
		if fm.Type == "" || !reg.Traits(fm.Type)["log"] {
			continue
		}
		for _, e := range vault.LogEntries(page.Body) {
			scan.notes = append(scan.notes, Note{
				Key: NoteKey(p, e.Time, e.Text), Path: p, Scope: scope, Time: e.Time, Text: e.Text,
			})
		}
	}
	return scan, nil
}

// indexedTypes lists every type a new page may take: those with the indexed trait, which are
// the pages search and extract_claims treat as knowledge (a log type is where notes come from,
// never where they go).
func indexedTypes(reg *registry.Registry) []TypeOption {
	var out []TypeOption
	for _, name := range reg.Names() {
		if !reg.Traits(name)["indexed"] {
			continue
		}
		def, _ := reg.Get(name)
		out = append(out, TypeOption{Name: name, Description: def.Description})
	}
	return out
}

// proposeState is the propose_pages run-state file, immutable by convention like State.
type proposeState struct {
	Notes map[string]noteDecisionState `json:"notes"`
}

type noteDecisionState struct {
	Action string `json:"action"`
	RanAt  string `json:"ran_at"`
}

func loadProposeState(pages store.PageStore) (proposeState, error) {
	data, _, err := pages.Read(proposeStatePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return proposeState{Notes: map[string]noteDecisionState{}}, nil
		}
		return proposeState{}, fmt.Errorf("read %s: %w", proposeStatePath, err)
	}
	var s proposeState
	if err := json.Unmarshal(data, &s); err != nil {
		return proposeState{}, fmt.Errorf("parse %s: %w", proposeStatePath, err)
	}
	if s.Notes == nil {
		s.Notes = map[string]noteDecisionState{}
	}
	return s, nil
}

func (s proposeState) withDecided(key, action string, at time.Time) proposeState {
	next := make(map[string]noteDecisionState, len(s.Notes)+1)
	for k, v := range s.Notes {
		next[k] = v
	}
	next[key] = noteDecisionState{Action: action, RanAt: at.UTC().Format(time.RFC3339)}
	return proposeState{Notes: next}
}

func (s proposeState) render() ([]byte, error) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render %s: %w", proposeStatePath, err)
	}
	return append(data, '\n'), nil
}
