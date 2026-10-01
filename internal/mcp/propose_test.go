package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dhia/balise/internal/compile"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/vault"
)

const proposeTargetPage = "---\nuid: 01J8Z3K9V6Q2M4N7P8R9S0T1E1\nslug: ergw\ntype: gotcha\nscope: work\n" +
	"title: ER gateway update deletes connections\nclaims:\n  - id: c1\n    text: Updating the ER gateway deletes its connections\n    status: active\n---\nbody\n"

func seedProposeVault(t *testing.T, d *deps) {
	t.Helper()
	_, err := d.pages.Commit([]store.Change{
		{Path: "work/gotchas/ergw.md", Data: []byte(proposeTargetPage)},
		{Path: "client-globex/gotchas/secret.md", Data: []byte(strings.ReplaceAll(strings.ReplaceAll(
			proposeTargetPage, "slug: ergw", "slug: secret"), "scope: work", "scope: client-globex"))},
	}, "t <t@x>", "seed")
	require.NoError(t, err)
}

func readOnlyProposal(t *testing.T, d *deps) compile.Proposal {
	t.Helper()
	paths, err := d.pages.List("review/")
	require.NoError(t, err)
	require.Len(t, paths, 1)
	raw, _, err := d.pages.Read(paths[0])
	require.NoError(t, err)
	page, err := vault.Parse(string(raw))
	require.NoError(t, err)
	var p compile.Proposal
	require.NoError(t, page.Decode(&p))
	return p
}

func callPropose(t *testing.T, d *deps, raw string, in ProposeInput) (ProposeOutput, error) {
	t.Helper()
	_, out, err := d.propose(context.Background(), reqWithToken(raw), in)
	return out, err
}

// An agent that verified a fact about a page it found proposes it as added claims: a pending
// claims proposal pinned to the page's current version, crediting the agent, with its
// evidence, and nothing touches the page itself.
func TestProposeAddsClaimsToAnExistingPage(t *testing.T) {
	d := newTestDeps(t)
	seedProposeVault(t, d)
	raw := mintToken(t, d.pool, "dapple-controlplane", []string{"work"}, []string{"read", "propose"})

	out, err := callPropose(t, d, raw, ProposeInput{
		Scope: "work", Target: "ergw",
		Claims:   []string{"azapi update patches the ER gateway in place and keeps connections"},
		Evidence: "Ran terraform apply with azapi_update_resource; both connections stayed up.",
	})
	require.NoError(t, err)
	require.Equal(t, compile.KindClaims, out.Kind)
	require.Equal(t, "review/"+out.ID+".md", out.Path)

	p := readOnlyProposal(t, d)
	require.Equal(t, "pending", p.Status)
	require.Equal(t, "work", p.Scope)
	require.Equal(t, "ergw", p.Target)
	require.NotEmpty(t, p.TargetVersion, "pinned so accept refuses if the page moved on")
	require.True(t, strings.HasPrefix(p.CreatedBy, "dapple-controlplane:"), p.CreatedBy)
	require.Equal(t, []compile.ProposalAdd{{Text: "azapi update patches the ER gateway in place and keeps connections", Status: "active"}},
		p.Change.Claims.Add)
	require.Equal(t, "Ran terraform apply with azapi_update_resource; both connections stayed up.", p.Evidence[0].Text)

	raw2, _, err := d.pages.Read("work/gotchas/ergw.md")
	require.NoError(t, err)
	require.Equal(t, proposeTargetPage, string(raw2), "proposing never edits the page")
}

// A durable finding with no page yet becomes a new_page proposal.
func TestProposeCreatesANewPageProposal(t *testing.T) {
	d := newTestDeps(t)
	seedProposeVault(t, d)
	raw := mintToken(t, d.pool, "dapple-controlplane", []string{"work"}, []string{"propose"})

	out, err := callPropose(t, d, raw, ProposeInput{
		Scope: "work",
		NewPage: &ProposeNewPage{Type: "state", Slug: "miamisburg-spines",
			Title: "Miamisburg spines are Dell, not NVIDIA", Body: "LLDP and the switch CLI both show Dell Z9864F-ON."},
		Claims:   []string{"Miamisburg spines are Dell Z9864F-ON switches", "The BOM wrongly lists NVIDIA SN5610 spines"},
		Evidence: "show version on both spines",
	})
	require.NoError(t, err)
	require.Equal(t, compile.KindNewPage, out.Kind)

	p := readOnlyProposal(t, d)
	require.Equal(t, "miamisburg-spines", p.Target)
	require.Equal(t, &compile.ProposalPage{Type: "state", Title: "Miamisburg spines are Dell, not NVIDIA",
		Body: "LLDP and the switch CLI both show Dell Z9864F-ON."}, p.Page)
	require.Len(t, p.Change.Claims.Add, 2)
}

// Every refusal leaves review/ empty: nothing is written unless the whole proposal is valid.
func TestProposeRefusesInvalidProposals(t *testing.T) {
	newPage := func(mut func(*ProposeNewPage)) *ProposeNewPage {
		np := &ProposeNewPage{Type: "state", Slug: "a-new-page", Title: "A new page", Body: "Body."}
		mut(np)
		return np
	}
	okClaims := []string{"a perfectly fine claim about the system"}
	cases := []struct {
		name         string
		capabilities []string
		in           ProposeInput
		want         string
	}{
		{"no propose capability", []string{"read", "remember"},
			ProposeInput{Scope: "work", Target: "ergw", Claims: okClaims, Evidence: "x"}, "missing capability: propose"},
		{"scope not on the token", []string{"propose"},
			ProposeInput{Scope: "client-globex", Target: "secret", Claims: okClaims, Evidence: "x"}, `scope "client-globex" not allowed`},
		{"target only exists in another scope", []string{"propose"},
			ProposeInput{Scope: "work", Target: "secret", Claims: okClaims, Evidence: "x"}, `no page "secret" in work`},
		{"neither target nor new page", []string{"propose"},
			ProposeInput{Scope: "work", Claims: okClaims, Evidence: "x"}, "exactly one of target"},
		{"both target and new page", []string{"propose"},
			ProposeInput{Scope: "work", Target: "ergw", NewPage: newPage(func(*ProposeNewPage) {}), Claims: okClaims, Evidence: "x"},
			"exactly one of target"},
		{"no claims", []string{"propose"},
			ProposeInput{Scope: "work", Target: "ergw", Evidence: "x"}, "1 to 6 claims"},
		{"a claim that is a subject, not a statement", []string{"propose"},
			ProposeInput{Scope: "work", Target: "ergw", Claims: []string{"Gateways"}, Evidence: "x"}, "reads like a subject"},
		{"no evidence", []string{"propose"},
			ProposeInput{Scope: "work", Target: "ergw", Claims: okClaims}, "evidence"},
		{"new page slug already taken", []string{"propose"},
			ProposeInput{Scope: "work", NewPage: newPage(func(np *ProposeNewPage) { np.Slug = "ergw" }), Claims: okClaims, Evidence: "x"},
			`slug "ergw" is already taken`},
		{"new page of an unknown type", []string{"propose"},
			ProposeInput{Scope: "work", NewPage: newPage(func(np *ProposeNewPage) { np.Type = "no-such-type" }), Claims: okClaims, Evidence: "x"},
			"not an indexed page type"},
		{"new page of a non-indexed type", []string{"propose"},
			ProposeInput{Scope: "work", NewPage: newPage(func(np *ProposeNewPage) { np.Type = "memory" }), Claims: okClaims, Evidence: "x"},
			"not an indexed page type"},
		{"malformed slug", []string{"propose"},
			ProposeInput{Scope: "work", NewPage: newPage(func(np *ProposeNewPage) { np.Slug = "../escape" }), Claims: okClaims, Evidence: "x"},
			"lowercase words joined by hyphens"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newTestDeps(t)
			seedProposeVault(t, d)
			raw := mintToken(t, d.pool, "agent", []string{"work"}, tc.capabilities)
			_, err := callPropose(t, d, raw, tc.in)
			require.ErrorContains(t, err, tc.want)
			paths, err := d.pages.List("review/")
			require.NoError(t, err)
			require.Empty(t, paths)
		})
	}
}

// Two pending proposals on one page would let accepting the first invalidate the second, so a
// page already under review takes no new proposal until it is decided.
func TestProposeRefusesAPageAlreadyUnderReview(t *testing.T) {
	d := newTestDeps(t)
	seedProposeVault(t, d)
	raw := mintToken(t, d.pool, "agent", []string{"work"}, []string{"propose"})
	in := ProposeInput{Scope: "work", Target: "ergw", Claims: []string{"a first claim about the gateway"}, Evidence: "x"}

	first, err := callPropose(t, d, raw, in)
	require.NoError(t, err)
	_, err = callPropose(t, d, raw, in)
	require.ErrorContains(t, err, "already has a pending proposal "+first.ID)
}
