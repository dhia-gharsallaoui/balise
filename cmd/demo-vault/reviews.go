package main

import (
	"fmt"

	"github.com/dhia/balise/internal/compile"
)

// reviewProposal pairs a compile.Proposal with the rendered path it lands on and the
// free-text rationale that follows its frontmatter fence (compile.Proposal.Render's second
// argument) — the same two-part shape internal/compile/pending.go reads back.
type reviewProposal struct {
	path      string
	proposal  compile.Proposal
	rationale string
}

// reviewUID mirrors uid() but offset into the 900s so review-proposal ids never collide with
// a page uid, while staying just as deterministic (no vault.NewUID, no time.Now).
func reviewUID(n int) string {
	return uid(900 + n)
}

// reviewProposals is the vault's four pending review proposals — one per scope — so the
// Review screen has real, variously-shaped items (a plain add, a keep+reword, a
// keep+retire+add) to act on instead of an empty inbox. Every one is built from
// internal/compile's own exported Proposal/Render, the exact type Balise's compile pipeline
// itself produces, rather than hand-rolled YAML that could drift from that shape.
func reviewProposals() []reviewProposal {
	return []reviewProposal{
		{
			path: "review/" + reviewUID(1) + ".md",
			proposal: compile.Proposal{
				ID: reviewUID(1), Kind: "claims", Scope: "work",
				Target:     "terraform-subscription-foreach-reorder",
				Confidence: 0.82, CreatedBy: "compile:2026-09-19T09:14:00Z",
				Evidence: []compile.ProposalEvidence{{
					Source: "work/gotchas/terraform-subscription-foreach-reorder.md",
					Span:   []int{9, 10},
					Text:   "Initech onboarding hit the same reorder when a subscription alias was renamed mid-rollout.",
				}},
				Change: compile.ProposalChange{Claims: compile.ProposalClaims{
					Keep: []string{"c1", "c2"},
					Add: []compile.ProposalAdd{{
						Status: "active",
						Text:   "Recurred during Initech onboarding when a subscription alias was renamed mid-rollout",
					}},
				}},
				Status: "pending",
			},
			rationale: "Extraction run over the latest Initech onboarding notes turned up a second, " +
				"independent occurrence of this reorder. Keeping both existing claims as-is and " +
				"adding the new one rather than rewording either, since neither original claim is " +
				"wrong — the bug just recurred somewhere new.\n",
		},
		{
			path: "review/" + reviewUID(2) + ".md",
			proposal: compile.Proposal{
				ID: reviewUID(2), Kind: "claims", Scope: "client-acme",
				Target:     "acme-fabric-monitoring-alert-noise",
				Confidence: 0.7, CreatedBy: "compile:2026-09-15T11:02:00Z",
				Evidence: []compile.ProposalEvidence{{
					Source: "client-acme/issues/acme-fabric-monitoring-alert-noise.md",
					Span:   []int{5, 6},
					Text:   "The threshold that shipped pages past ninety seconds of SLA breach, not the two minutes originally proposed.",
				}},
				Change: compile.ProposalChange{Claims: compile.ProposalClaims{
					Keep: []string{"c1"},
					Reword: []compile.ProposalReword{{
						ID:   "c2",
						Text: "Threshold that shipped pages only when SLA breach exceeds ninety seconds, not the two minutes first proposed",
					}},
				}},
				Status: "pending",
			},
			rationale: "The alerting change that actually shipped uses a ninety-second threshold, " +
				"tighter than the two-minute figure the page currently states. c1 (why the alert " +
				"was noisy in the first place) is unaffected and kept as written.\n",
		},
		{
			path: "review/" + reviewUID(3) + ".md",
			proposal: compile.Proposal{
				ID: reviewUID(3), Kind: "claims", Scope: "client-globex",
				Target:     "globex-storage-account-immutability-lock-blocks-lifecycle",
				Confidence: 0.65, CreatedBy: "compile:2026-09-16T08:40:00Z",
				Evidence: []compile.ProposalEvidence{{
					Source: "client-globex/gotchas/globex-storage-account-immutability-lock-blocks-lifecycle.md",
					Span:   []int{7, 8},
					Text:   "A support-assisted early removal path exists for a locked account, contradicting the page's current c3.",
				}},
				Change: compile.ProposalChange{Claims: compile.ProposalClaims{
					Keep: []string{"c1", "c2"},
					Retire: []compile.ProposalRetire{{
						ID: "c3", AsOf: "2026-09-16",
					}},
					Add: []compile.ProposalAdd{{
						Status: "active",
						Text:   "Azure support can remove an immutability lock early through a documented support-assisted process",
					}},
				}},
				Status: "pending",
			},
			rationale: "c3 claimed the only way past a lock was waiting out the immutability period. " +
				"A support-assisted early-removal path was confirmed to exist, so c3 is retired as of " +
				"this run rather than reworded — it was not imprecise, it was incomplete — and the " +
				"replacement claim below states the actual path.\n",
		},
		{
			path: "review/" + reviewUID(4) + ".md",
			proposal: compile.Proposal{
				ID: reviewUID(4), Kind: "claims", Scope: "client-initech",
				Target:     "initech-standardize-on-b-series-vms",
				Confidence: 0.9, CreatedBy: "compile:2026-09-20T10:05:00Z",
				Evidence: []compile.ProposalEvidence{{
					Source: "client-initech/incidents/initech-batch-job-backlog-2026-06.md",
					Span:   []int{3, 4},
					Text:   "A D-series exception was granted for the two largest batch workloads after the June backlog.",
				}},
				Change: compile.ProposalChange{Claims: compile.ProposalClaims{
					Keep: []string{"c1", "c2"},
					Add: []compile.ProposalAdd{{
						Status: "active",
						Text:   "The two largest batch workloads run an approved D-series exception instead of B-series",
					}},
				}},
				Status: "pending",
			},
			rationale: "The June batch backlog incident resulted in a standing exception for the two " +
				"largest workloads. Both existing claims about the B-series default still hold; this " +
				"adds the exception rather than replacing anything.\n",
		},
	}
}

// render produces this proposal file's full content via compile.Proposal.Render — the same
// method Balise's own compile pipeline calls — so the YAML shape is guaranteed to match what
// internal/compile/pending.go and internal/api/review.go already know how to parse.
func (rp reviewProposal) render() (string, error) {
	out, err := rp.proposal.Render(rp.rationale)
	if err != nil {
		return "", fmt.Errorf("render review proposal %s: %w", rp.proposal.ID, err)
	}
	return out, nil
}
