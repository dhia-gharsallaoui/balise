package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dhia/balise/internal/compile"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/vault"
)

// maxProposedClaims mirrors the 1-6 claims per page the design allows (01 section 4).
const maxProposedClaims = 6

// agentProposalConfidence is what an agent's own proposal records. It is not calibrated
// against anything; it states that the agent asserts it verified this (shown as "likely"),
// above a compile proposal that needed a retry ("possible").
const agentProposalConfidence = 0.8

// ProposeInput is propose's argument shape: claims for an existing page (Target), or a new
// page (NewPage), never both. The proposal lands in review/ for a human to accept; nothing an
// agent proposes reaches a page on its own (01 section 3, principle 6).
type ProposeInput struct {
	// Scope must be one of the token's own scopes; it narrows, never widens (04 section 14).
	Scope string `json:"scope"`
	// Target is the slug of an existing page in Scope to add Claims to (find it with search).
	Target string `json:"target,omitempty"`
	// NewPage describes a page to create when no existing page fits.
	NewPage *ProposeNewPage `json:"new_page,omitempty"`
	// Claims are 1-6 short, self-contained statements of fact.
	Claims []string `json:"claims"`
	// Evidence says what the agent observed or verified, shown to the reviewer first.
	Evidence string `json:"evidence"`
}

// ProposeNewPage is the page a new_page proposal would create.
type ProposeNewPage struct {
	Type  string `json:"type"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// ProposeOutput identifies the proposal written.
type ProposeOutput struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Path   string `json:"path"`
}

func (d *deps) propose(ctx context.Context, req *sdk.CallToolRequest, in ProposeInput) (*sdk.CallToolResult, ProposeOutput, error) {
	start := time.Now()
	var out ProposeOutput

	tok, err := authenticate(ctx, d.pool, req)
	if err != nil {
		return nil, out, err
	}
	fail := func(scopes []string, err error) (*sdk.CallToolResult, ProposeOutput, error) {
		recordAudit(ctx, d.pool, tok.ID, "propose", scopes, in.Target, nil, 0, start)
		return nil, out, err
	}
	if !tok.HasCapability("propose") {
		return fail(tok.Scopes, errors.New("missing capability: propose"))
	}
	if in.Scope == "" || !tok.AllowsScope(in.Scope) {
		return fail(tok.Scopes, fmt.Errorf("scope %q not allowed for this token", in.Scope))
	}
	scopes := []string{in.Scope}

	proposal, err := d.buildAgentProposal(tok.Name, in, time.Now())
	if err != nil {
		return fail(scopes, err)
	}
	if id, pending, err := compile.PendingProposalID(d.pages, in.Scope, proposal.Target); err != nil {
		return fail(scopes, fmt.Errorf("check pending proposals: %w", err))
	} else if pending {
		return fail(scopes, fmt.Errorf("%s already has a pending proposal %s; it must be reviewed before another is proposed",
			proposal.Target, id))
	}

	rendered, err := proposal.Render("")
	if err != nil {
		return fail(scopes, fmt.Errorf("render proposal: %w", err))
	}
	p := "review/" + proposal.ID + ".md"
	// Authored "agent:<name>" like remember's commits, so the vault's history shows which
	// agent proposed what; accept later credits the same agent via created_by.
	message := fmt.Sprintf("propose: %s %s", proposal.Kind, proposal.Target)
	if _, err := d.pages.Commit([]store.Change{{Path: p, Data: []byte(rendered)}}, "agent:"+tok.Name, message); err != nil {
		return fail(scopes, fmt.Errorf("commit proposal: %w", err))
	}

	out = ProposeOutput{ID: proposal.ID, Kind: proposal.Kind, Target: proposal.Target, Path: p}
	recordAudit(ctx, d.pool, tok.ID, "propose", scopes, in.Target, []string{p}, len(in.Claims), start)
	return nil, out, nil
}

// buildAgentProposal validates in and turns it into a pending proposal, checking everything
// accept will later rely on: the target page exists in the scope (resolved by walking the
// vault, exactly as accept does), or the new page has an indexed type and a free, well-formed
// slug; and every claim is a real statement.
func (d *deps) buildAgentProposal(agent string, in ProposeInput, now time.Time) (compile.Proposal, error) {
	if (in.Target == "") == (in.NewPage == nil) {
		return compile.Proposal{}, errors.New("give exactly one of target (an existing page's slug) or new_page")
	}
	if len(in.Claims) == 0 || len(in.Claims) > maxProposedClaims {
		return compile.Proposal{}, fmt.Errorf("propose takes 1 to %d claims, got %d", maxProposedClaims, len(in.Claims))
	}
	if strings.TrimSpace(in.Evidence) == "" {
		return compile.Proposal{}, errors.New("evidence is required: say what you observed or verified")
	}
	adds := make([]compile.ProposalAdd, len(in.Claims))
	for i, text := range in.Claims {
		text = strings.TrimSpace(text)
		problem, warning := compile.ValidateClaimWord(text)
		if problem != "" {
			return compile.Proposal{}, fmt.Errorf("claim %q %s", text, problem)
		}
		adds[i] = compile.ProposalAdd{Text: text, Status: "active"}
		if warning != "" {
			adds[i].Warnings = []string{warning}
		}
	}

	proposal := compile.Proposal{
		ID: vault.NewUID(), Scope: in.Scope, Confidence: agentProposalConfidence,
		CreatedBy: agent + ":" + now.UTC().Format(time.RFC3339), Status: "pending",
		Evidence: []compile.ProposalEvidence{{Source: "agent:" + agent, Text: strings.TrimSpace(in.Evidence)}},
		Change:   compile.ProposalChange{Claims: compile.ProposalClaims{Add: adds}},
	}

	if in.Target != "" {
		_, _, version, err := compile.FindPageBySlug(d.pages, in.Scope, in.Target)
		if err != nil {
			return compile.Proposal{}, fmt.Errorf("no page %q in %s; search for the right slug, or propose a new_page", in.Target, in.Scope)
		}
		proposal.Kind, proposal.Target, proposal.TargetVersion = compile.KindClaims, in.Target, version
		return proposal, nil
	}

	np := in.NewPage
	def, ok := d.types.Get(np.Type)
	if !ok || !def.Traits["indexed"] {
		return compile.Proposal{}, fmt.Errorf("type %q is not an indexed page type; use one of: %s",
			np.Type, strings.Join(d.indexedTypeNames(), ", "))
	}
	if !compile.ValidSlug(np.Slug) {
		return compile.Proposal{}, fmt.Errorf("slug %q must be lowercase words joined by hyphens, at most 80 characters", np.Slug)
	}
	if _, _, _, err := compile.FindPageBySlug(d.pages, in.Scope, np.Slug); err == nil {
		return compile.Proposal{}, fmt.Errorf("slug %q is already taken in %s; propose claims for that page with target instead, or choose another slug",
			np.Slug, in.Scope)
	}
	if strings.TrimSpace(np.Title) == "" || strings.TrimSpace(np.Body) == "" {
		return compile.Proposal{}, errors.New("new_page needs a title and a body")
	}
	proposal.Kind, proposal.Target = compile.KindNewPage, np.Slug
	proposal.Page = &compile.ProposalPage{Type: np.Type, Title: strings.TrimSpace(np.Title), Body: strings.TrimSpace(np.Body)}
	return proposal, nil
}

// indexedTypeNames lists the types a new page may take, for a refusal that tells the agent
// what it may use instead.
func (d *deps) indexedTypeNames() []string {
	var names []string
	for _, name := range d.types.Names() {
		if d.types.Traits(name)["indexed"] {
			names = append(names, name)
		}
	}
	return names
}
