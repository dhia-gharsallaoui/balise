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
				Target:     "migration-rename-changes-apply-order",
				Confidence: 0.82, CreatedBy: "compile:2026-09-19T09:14:00Z",
				Evidence: []compile.ProposalEvidence{{
					Source: "work/gotchas/migration-rename-changes-apply-order.md",
					Span:   []int{9, 10},
					Text:   "Initech's worker fleet deploy pipeline hit the same reorder when a migration file was renamed mid-rollout.",
				}},
				Change: compile.ProposalChange{Claims: compile.ProposalClaims{
					Keep: []string{"c1", "c2", "c3"},
					Add: []compile.ProposalAdd{{
						Status: "active",
						Text:   "Recurred during Initech's worker fleet deploy pipeline when a migration file was renamed mid-rollout",
					}},
				}},
				Status: "pending",
			},
			rationale: "Extraction run over Initech's deploy pipeline logs turned up a second, " +
				"independent occurrence of this reorder. Keeping all three existing claims as-is and " +
				"adding the new one rather than rewording any of them, since none of the originals are " +
				"wrong — the bug just recurred somewhere new.\n",
		},
		{
			path: "review/" + reviewUID(2) + ".md",
			proposal: compile.Proposal{
				ID: reviewUID(2), Kind: "claims", Scope: "client-acme",
				Target:     "acme-checkout-error-rate-alert-noise",
				Confidence: 0.7, CreatedBy: "compile:2026-09-15T11:02:00Z",
				Evidence: []compile.ProposalEvidence{{
					Source: "client-acme/issues/acme-checkout-error-rate-alert-noise.md",
					Span:   []int{5, 6},
					Text:   "The threshold that shipped pages only past five percent sustained for ninety seconds, not the two minutes originally proposed.",
				}},
				Change: compile.ProposalChange{Claims: compile.ProposalClaims{
					Keep: []string{"c1"},
					Reword: []compile.ProposalReword{{
						ID:   "c2",
						Text: "Pages only when the error rate exceeds five percent sustained for ninety seconds, not the two-minute window first proposed",
					}},
				}},
				Status: "pending",
			},
			rationale: "The alerting change that actually shipped uses a tighter five-percent/ninety-second " +
				"threshold than the page currently states. c1 (why the alert was noisy in the first place) " +
				"is unaffected and kept as written.\n",
		},
		{
			path: "review/" + reviewUID(3) + ".md",
			proposal: compile.Proposal{
				ID: reviewUID(3), Kind: "claims", Scope: "client-globex",
				Target:     "globex-sso-nested-group-claim-mapping-drops-groups",
				Confidence: 0.65, CreatedBy: "compile:2026-09-16T08:40:00Z",
				Evidence: []compile.ProposalEvidence{{
					Source: "client-globex/gotchas/globex-sso-nested-group-claim-mapping-drops-groups.md",
					Span:   []int{7, 8},
					Text:   "Okta's newer recursive group-claim mapping mode resolves nested groups directly, contradicting the page's current c3.",
				}},
				Change: compile.ProposalChange{Claims: compile.ProposalClaims{
					Keep: []string{"c1", "c2"},
					Retire: []compile.ProposalRetire{{
						ID: "c3", AsOf: "2026-09-16",
					}},
					Add: []compile.ProposalAdd{{
						Status: "active",
						Text:   "Okta's newer recursive group-claim mapping mode resolves nested groups directly, without manual flattening",
					}},
				}},
				Status: "pending",
			},
			rationale: "c3 claimed manual flattening was the only confirmed workaround. Okta's newer " +
				"recursive group-claim mapping mode was confirmed to resolve nested groups directly, so " +
				"c3 is retired as of this run rather than reworded — it was not imprecise, it was " +
				"incomplete — and the replacement claim below states the actual fix.\n",
		},
		{
			path: "review/" + reviewUID(4) + ".md",
			proposal: compile.Proposal{
				ID: reviewUID(4), Kind: "claims", Scope: "client-initech",
				Target:     "initech-standardize-burstable-worker-fleet",
				Confidence: 0.9, CreatedBy: "compile:2026-09-20T10:05:00Z",
				Evidence: []compile.ProposalEvidence{{
					Source: "client-initech/incidents/initech-worker-backlog-2026-06.md",
					Span:   []int{3, 4},
					Text:   "A fixed-size-tier exception was granted for the two largest job types after the June backlog.",
				}},
				Change: compile.ProposalChange{Claims: compile.ProposalClaims{
					Keep: []string{"c1", "c2"},
					Add: []compile.ProposalAdd{{
						Status: "active",
						Text:   "The two largest job types run an approved fixed-size-tier exception instead of burstable",
					}},
				}},
				Status: "pending",
			},
			rationale: "The June worker backlog incident resulted in a standing exception for the two " +
				"largest job types. Both existing claims about the burstable default still hold; this " +
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
