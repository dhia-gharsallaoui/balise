package compile

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/vault"
)

// pendingProposal is one proposal already on disk under review/ before a run begins, together
// with the free-form rationale text that followed its frontmatter fence. The rationale is kept
// so that, if this proposal needs to be superseded, the rejected copy preserves it verbatim —
// the same shape internal/api/review.go's handleReviewReject produces for a human-driven
// reject.
type pendingProposal struct {
	Path      string
	Proposal  Proposal
	Rationale string
}

// targetKey identifies the page a proposal is about, independent of the proposal's own id. Two
// pending proposals with different ids but the same scope+target are exactly the fix-1 hazard
// this file exists to prevent: accepting both would apply two sets of add claims to one page,
// and the second proposal's keep/reword/retire entries would reference claim ids as they stood
// before the first was applied — silent corruption of the page's claim history.
func targetKey(scope, target string) string {
	return scope + "\x00" + target
}

// pendingProposalsByTarget scans review/ (excluding review/.rejected/) for every currently
// pending proposal, keyed by targetKey. Run calls this once before extracting anything, so a
// candidate about to get a fresh proposal can find — and supersede — whatever is already
// pending for the same target.
//
// This deliberately does not trust the body-hash skip state (state.go) for this question. That
// state only ever answers "does this page need re-extracting"; it has no idea whether a
// proposal is already sitting in review/ for the page, and an interrupted run followed by a
// resumed one is exactly the sequence that produces a second proposal for a target that already
// has one pending (fix 1). Scanning review/ itself, fresh, on every run is the only way to
// answer the right question.
func pendingProposalsByTarget(pages store.PageStore) (map[string]pendingProposal, error) {
	paths, err := pages.List("review/")
	if err != nil {
		return nil, fmt.Errorf("list existing proposals: %w", err)
	}
	out := map[string]pendingProposal{}
	for _, p := range paths {
		if !strings.HasSuffix(p, ".md") || strings.HasPrefix(p, "review/.rejected/") {
			continue
		}
		raw, _, err := pages.Read(p)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		parsed, err := vault.Parse(string(raw))
		if err != nil || parsed.Meta == nil {
			continue // malformed proposal file: not this run's problem to repair
		}
		var proposal Proposal
		if err := parsed.Decode(&proposal); err != nil {
			continue
		}
		if proposal.Status != "pending" {
			continue
		}
		out[targetKey(proposal.Scope, proposal.Target)] = pendingProposal{
			Path: p, Proposal: proposal, Rationale: parsed.Body,
		}
	}
	return out, nil
}

// supersede turns old into a rejected copy — same shape a human reject produces (Status
// "rejected", Rejected.Reason set, original rationale preserved verbatim) — and returns the two
// store.Changes that retire the old proposal path and write the rejected copy. It never
// deletes outright: the audit trail of what was once pending for this target stays available
// under review/.rejected/, exactly like a human-rejected proposal.
func supersede(old pendingProposal, reason string) (store.Change, store.Change, error) {
	old.Proposal.Status = "rejected"
	old.Proposal.Rejected = &ProposalRejection{Reason: reason}
	rendered, err := old.Proposal.Render(old.Rationale)
	if err != nil {
		return store.Change{}, store.Change{}, fmt.Errorf(
			"render superseded proposal %s: %w", old.Proposal.ID, err)
	}
	rejectedPath := "review/.rejected/" + old.Proposal.ID + ".md"
	return store.Change{Path: old.Path, Delete: true},
		store.Change{Path: rejectedPath, Data: []byte(rendered)},
		nil
}

// DedupePending is the one-time repair for duplicates that already exist on disk from before
// Run started preventing new ones (fix 1, part A only guards a *new* extraction from duplicating
// a target that already has a pending proposal going forward; it does nothing for the review/
// directory as it stood before that guard existed). For every target with more than one pending
// proposal, it keeps the newest — ordered by CreatedBy ("compile:"+RFC3339, which sorts
// correctly as a string) and, for the rare exact tie, by ID (a ULID, also lexicographically
// sortable by creation time) — and supersedes the rest via the same supersede() a live run uses,
// so the audit trail and file shape are identical either way. It returns how many proposals were
// superseded and commits the result in one batch; zero duplicates found means zero changes and no
// commit.
func DedupePending(pages store.PageStore) (int, error) {
	byTarget, err := pendingProposalGroups(pages)
	if err != nil {
		return 0, err
	}

	var changes []store.Change
	superseded := 0
	for _, group := range byTarget {
		if len(group) < 2 {
			continue
		}
		sort.Slice(group, func(i, j int) bool {
			if group[i].Proposal.CreatedBy != group[j].Proposal.CreatedBy {
				return group[i].Proposal.CreatedBy < group[j].Proposal.CreatedBy
			}
			return group[i].Proposal.ID < group[j].Proposal.ID
		})
		newest := group[len(group)-1]
		for _, old := range group[:len(group)-1] {
			delChange, addChange, serr := supersede(old, fmt.Sprintf(
				"superseded by newer pending proposal %s for the same target (duplicate cleanup)",
				newest.Proposal.ID))
			if serr != nil {
				return superseded, serr
			}
			changes = append(changes, delChange, addChange)
			superseded++
		}
	}
	if len(changes) == 0 {
		return 0, nil
	}
	if _, err := pages.Commit(changes, "balise <balise@localhost>", "compile: dedupe duplicate pending proposals"); err != nil {
		return superseded, fmt.Errorf("commit dedupe: %w", err)
	}
	return superseded, nil
}

// pendingProposalGroups is like pendingProposalsByTarget but keeps every pending proposal found
// for a target (as a slice) instead of letting a later one silently overwrite an earlier one in
// a map — the exact distinction DedupePending needs, since its whole job is to notice when more
// than one exists.
func pendingProposalGroups(pages store.PageStore) (map[string][]pendingProposal, error) {
	paths, err := pages.List("review/")
	if err != nil {
		return nil, fmt.Errorf("list existing proposals: %w", err)
	}
	out := map[string][]pendingProposal{}
	for _, p := range paths {
		if !strings.HasSuffix(p, ".md") || strings.HasPrefix(p, "review/.rejected/") {
			continue
		}
		raw, _, err := pages.Read(p)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		parsed, err := vault.Parse(string(raw))
		if err != nil || parsed.Meta == nil {
			continue // malformed proposal file: not this run's problem to repair
		}
		var proposal Proposal
		if err := parsed.Decode(&proposal); err != nil {
			continue
		}
		if proposal.Status != "pending" {
			continue
		}
		key := targetKey(proposal.Scope, proposal.Target)
		out[key] = append(out[key], pendingProposal{Path: p, Proposal: proposal, Rationale: parsed.Body})
	}
	return out, nil
}
