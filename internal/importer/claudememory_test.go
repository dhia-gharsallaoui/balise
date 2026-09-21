package importer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dhia/balise/internal/importer"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/stretchr/testify/require"
)

// testTenants is a self-contained tenant fixture, not the repo's own defaults/tenants.yaml:
// that file holds the owner's real (gitignored) customer list locally, so a test asserting
// on facets like customer/globex or customer/acme would silently break depending on whether
// the real file or the generic example happened to be on disk (see
// internal/registry/tenants_test.go for the same precedent). Declaring the exact tenants
// these tests reference keeps them independent of either.
func testTenants(t *testing.T) *registry.Tenants {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tenants.yaml")
	require.NoError(t, os.WriteFile(path, []byte("tenants: [globex, acme, initech, umbrella]\n"), 0o644))
	tenants, err := registry.LoadTenants(path)
	require.NoError(t, err)
	return tenants
}

const source = `---
name: AKS pod CIDR — overlay, not routed
description: Pod IPs in AKS are virtual overlay IPs that never appear on the VNet
type: project
originSessionId: bf52f88e-6e57-499e-bedf-45803f98bc4c
---
AKS with Azure CNI Overlay uses an overlay network for pods.
`

func artifacts() *importer.Artifacts {
	return &importer.Artifacts{
		PassA: map[string]importer.Classification{
			"project_aks_pod_cidr.md": {
				Kind: "gotcha", Status: "active",
				Facets: []string{"vendor/azure/aks", "layer/network"},
			},
			"project_globex_circuit.md": {
				Kind: "state", Status: "active",
				Facets: []string{"customer/globex", "vendor/azure/expressroute"}, Identity: "circuit-1",
			},
		},
		Hooks: map[string]string{
			"project_aks_pod_cidr.md": "Pod IPs never appear on the VNet and need no route entries",
		},
		AliasTable: map[string]string{
			"AKS pod CIDR — overlay, not routed": "project_aks_pod_cidr.md",
			"aks_pod_cidr":                       "project_aks_pod_cidr.md",
		},
	}
}

func convert(t *testing.T, filename string) importer.Converted {
	t.Helper()
	got, err := importer.Convert(filename, source, artifacts(), testTenants(t))
	require.NoError(t, err)
	return got
}

func TestSlugDropsTheTypePrefix(t *testing.T) {
	require.Equal(t, "aks-pod-cidr", convert(t, "project_aks_pod_cidr.md").Meta.Slug)
}

func TestTitleComesFromTheCorpusName(t *testing.T) {
	require.Equal(t, "AKS pod CIDR — overlay, not routed",
		convert(t, "project_aks_pod_cidr.md").Meta.Title)
}

func TestTypeComesFromPassANotTheCorpusType(t *testing.T) {
	require.Equal(t, "gotcha", convert(t, "project_aks_pod_cidr.md").Meta.Type)
}

func TestUIDIsAssigned(t *testing.T) {
	require.Len(t, convert(t, "project_aks_pod_cidr.md").Meta.UID, 26)
}

func TestScopeDefaultsToWork(t *testing.T) {
	require.Equal(t, "work", convert(t, "project_aks_pod_cidr.md").Meta.Scope)
}

func TestCustomerFacetRoutesToAClientScope(t *testing.T) {
	require.Equal(t, "client-globex", convert(t, "project_globex_circuit.md").Meta.Scope)
}

// TestColoAloneRoutesToWorkNotAClientScope pins the 2026-09-17 domain correction: colo is
// the shared datacenter Globex's and Acme's infrastructure sits inside, not a tenant, so a
// page naming only customer/colo must fall through to work like any other non-tenant page
// — client-colo must never exist as an output scope again.
func TestColoAloneRoutesToWorkNotAClientScope(t *testing.T) {
	a := artifacts()
	a.PassA["project_colo_switch.md"] = importer.Classification{
		Kind: "gotcha", Status: "active", Facets: []string{"customer/colo", "layer/network"},
	}
	got, err := importer.Convert("project_colo_switch.md", source, a, testTenants(t))
	require.NoError(t, err)
	require.Equal(t, "work", got.Meta.Scope)
	require.False(t, got.Meta.NeedsSplit)
}

// TestTenantWithDatacenterFacetIsSingleTenantNotMultiCustomer pins the second half of the
// 2026-09-17 domain correction: a page tagged customer/globex + customer/colo is Globex's kit
// sitting in the shared colo datacenter — one tenant, one location — and must resolve as
// ordinary single-tenant Globex, not get wrongly quarantined as multi-customer just because
// colo also appears as a customer/* facet. This is exactly the bug that made six of the
// seven originally-flagged "multi-customer" pages false positives: colo counted as a
// tenant when it named nothing but a location.
func TestTenantWithDatacenterFacetIsSingleTenantNotMultiCustomer(t *testing.T) {
	a := artifacts()
	a.PassA["project_globex_colo_fabric.md"] = importer.Classification{
		Kind: "gotcha", Status: "active",
		Facets: []string{"customer/globex", "customer/colo", "layer/fabric"},
	}
	got, err := importer.Convert("project_globex_colo_fabric.md", source, a, testTenants(t))
	require.NoError(t, err)
	require.Equal(t, "client-globex", got.Meta.Scope)
	require.False(t, got.Meta.NeedsSplit)
}

// TestDatacenterAndInternalCategoryFacetsRouteToWork pins the datacenter-egress-zones
// case: customer/colo + customer/platform names no declared tenant at all (a datacenter
// paired with the owner's own internal category), so the page must land in work exactly
// like a page with no customer/* facets at all — not in some client-platform scope that
// was never a real tenant's.
func TestDatacenterAndInternalCategoryFacetsRouteToWork(t *testing.T) {
	a := artifacts()
	a.PassA["project_egress_zones.md"] = importer.Classification{
		Kind: "gotcha", Status: "active",
		Facets: []string{"customer/colo", "customer/platform", "layer/network"},
	}
	got, err := importer.Convert("project_egress_zones.md", source, a, testTenants(t))
	require.NoError(t, err)
	require.Equal(t, "work", got.Meta.Scope)
	require.False(t, got.Meta.NeedsSplit)
}

// TestFacetAbsentFromTenantListNeverCreatesAScope pins the general rule behind both fixes
// above: a customer/<x> facet where x is not in defaults/tenants.yaml is not a tenant, so
// on its own it must never create a scope of its own (e.g. no client-platform) — it must
// fall through to work exactly as if the facet were absent.
func TestFacetAbsentFromTenantListNeverCreatesAScope(t *testing.T) {
	a := artifacts()
	a.PassA["project_platform_only.md"] = importer.Classification{
		Kind: "gotcha", Status: "active", Facets: []string{"customer/platform", "layer/network"},
	}
	got, err := importer.Convert("project_platform_only.md", source, a, testTenants(t))
	require.NoError(t, err)
	require.Equal(t, "work", got.Meta.Scope)
	require.False(t, got.Meta.NeedsSplit)
}

// TestGenuineMultiCustomerPageIsNeverDemotedToWork pins balise-docs/01-design-spec-v0.4.md
// section 11 and balise-docs/04-technical-spec-v1.md section 14: a page naming more than
// one *declared tenant* must get the deterministic most-restrictive client scope, flagged
// needs_split, and must never fall back to work. umbrella + initech is the one page in the
// real corpus that is genuinely multi-customer under the tenant list — the six other
// originally-flagged pages all paired a real tenant with colo and are single-tenant (see
// TestTenantWithDatacenterFacetIsSingleTenantNotMultiCustomer).
func TestGenuineMultiCustomerPageIsNeverDemotedToWork(t *testing.T) {
	a := artifacts()
	a.PassA["project_umbrella_initech_ssh.md"] = importer.Classification{
		Kind: "gotcha", Status: "active",
		Facets: []string{"customer/umbrella", "customer/initech", "layer/fabric"},
	}
	got, err := importer.Convert("project_umbrella_initech_ssh.md", source, a, testTenants(t))
	require.NoError(t, err)
	require.NotEqual(t, "work", got.Meta.Scope)
	require.True(t, got.Meta.NeedsSplit)
}

// TestMultiCustomerScopeIsDeterministicRegardlessOfFacetOrder pins the fix for the
// Acme bug: the old scopeFor returned on whichever customer facet happened to appear
// first, which split one tenant's pages across two scopes purely by facet order. The
// resolved scope must be identical whichever order the same two tenants appear in.
func TestMultiCustomerScopeIsDeterministicRegardlessOfFacetOrder(t *testing.T) {
	a := artifacts()
	a.PassA["project_order_a.md"] = importer.Classification{
		Kind: "gotcha", Status: "active",
		Facets: []string{"customer/initech", "customer/umbrella", "layer/network"},
	}
	a.PassA["project_order_b.md"] = importer.Classification{
		Kind: "gotcha", Status: "active",
		Facets: []string{"customer/umbrella", "customer/initech", "layer/network"},
	}
	first, err := importer.Convert("project_order_a.md", source, a, testTenants(t))
	require.NoError(t, err)
	second, err := importer.Convert("project_order_b.md", source, a, testTenants(t))
	require.NoError(t, err)

	require.Equal(t, first.Meta.Scope, second.Meta.Scope)
	require.NotEqual(t, "work", first.Meta.Scope)
	require.True(t, first.Meta.NeedsSplit)
	require.True(t, second.Meta.NeedsSplit)
}

// TestCustomerBusinessUnitSubFacetIsNotASecondCustomer pins a real-corpus finding: Globex's own
// business units are declared in defaults/facets/customer.yaml as children of globex
// (customer/globex/consumer, customer/globex/labs), not separate tenants. A page carrying both
// customer/globex and customer/globex/consumer must resolve as ordinary single-customer Globex — not get
// wrongly quarantined as multi-customer just because two facets share a "customer/globex"
// prefix.
func TestCustomerBusinessUnitSubFacetIsNotASecondCustomer(t *testing.T) {
	a := artifacts()
	a.PassA["project_globex_flex_machine_registration.md"] = importer.Classification{
		Kind: "state", Status: "active",
		Facets: []string{"customer/globex", "customer/globex/consumer", "layer/compute"},
	}
	got, err := importer.Convert("project_globex_flex_machine_registration.md", source, a, testTenants(t))
	require.NoError(t, err)
	require.Equal(t, "client-globex", got.Meta.Scope)
	require.False(t, got.Meta.NeedsSplit)
}

// TestTwoGlobexBusinessUnitSubFacetsAreStillOneCustomer covers a page tagged with two of Globex's
// business-unit sub-facets and no bare customer/globex facet at all (customer/globex/consumer +
// customer/globex/labs) — the collapse to a single "globex" tenant must hold even without the
// parent facet present, since it is the first path segment after "customer/" that
// identifies the tenant, not the full facet string.
func TestTwoGlobexBusinessUnitSubFacetsAreStillOneCustomer(t *testing.T) {
	a := artifacts()
	a.PassA["project_globex_consumer_labs.md"] = importer.Classification{
		Kind: "state", Status: "active",
		Facets: []string{"customer/globex/consumer", "customer/globex/labs", "layer/compute"},
	}
	got, err := importer.Convert("project_globex_consumer_labs.md", source, a, testTenants(t))
	require.NoError(t, err)
	require.Equal(t, "client-globex", got.Meta.Scope)
	require.False(t, got.Meta.NeedsSplit)
}

func TestZeroCustomerFacetsRouteToWork(t *testing.T) {
	require.Equal(t, "work", convert(t, "project_aks_pod_cidr.md").Meta.Scope)
	require.False(t, convert(t, "project_aks_pod_cidr.md").Meta.NeedsSplit)
}

func TestHookBecomesTheSingleClaim(t *testing.T) {
	claims := convert(t, "project_aks_pod_cidr.md").Meta.Claims
	require.Len(t, claims, 1)
	require.Equal(t, "Pod IPs never appear on the VNet and need no route entries", claims[0].Text)
	require.Equal(t, "active", claims[0].Status)
	require.Equal(t, "c1", claims[0].ID)
}

func TestDescriptionBecomesBodyPreambleNeverAClaim(t *testing.T) {
	got := convert(t, "project_aks_pod_cidr.md")
	require.Contains(t, got.Body, "Pod IPs in AKS are virtual overlay IPs")
	for _, claim := range got.Meta.Claims {
		require.NotContains(t, claim.Text, "virtual overlay IPs",
			"a description names a subject; a claim must state something")
	}
}

func TestAliasesComeFromTheAliasTable(t *testing.T) {
	aliases := convert(t, "project_aks_pod_cidr.md").Meta.Aliases
	require.Contains(t, aliases, "aks_pod_cidr")
	require.Contains(t, aliases, "AKS pod CIDR — overlay, not routed")
}

func TestUnclassifiedFileBecomesANote(t *testing.T) {
	got := convert(t, "project_unknown.md")
	require.Equal(t, "note", got.Meta.Type)
	require.True(t, got.Meta.NeedsClassification)
}

func TestOriginSessionIDIsPreserved(t *testing.T) {
	rendered, err := convert(t, "project_aks_pod_cidr.md").Render()
	require.NoError(t, err)
	require.Contains(t, rendered, "bf52f88e-6e57-499e-bedf-45803f98bc4c")
}

func TestSupersededStatusCarriesToTheClaim(t *testing.T) {
	a := artifacts()
	entry := a.PassA["project_aks_pod_cidr.md"]
	entry.Status = "superseded"
	a.PassA["project_aks_pod_cidr.md"] = entry

	got, err := importer.Convert("project_aks_pod_cidr.md", source, a, testTenants(t))
	require.NoError(t, err)
	require.Equal(t, "superseded", got.Meta.Claims[0].Status)
}

func TestFileWithoutAHookGetsNoClaims(t *testing.T) {
	require.Empty(t, convert(t, "project_globex_circuit.md").Meta.Claims)
}

func TestPathFollowsScopeAndTypeFolder(t *testing.T) {
	require.Equal(t, "work/gotchas/aks-pod-cidr.md", convert(t, "project_aks_pod_cidr.md").Path)
}

// TestImportCorpusRejectsSlugCollisions covers the case the real corpus happens not to
// hit: two source files whose type-prefix-stripped slugs collide (project_foo.md and
// feedback_foo.md both become "foo"). ImportCorpus must fail loudly, naming both source
// files and the path they collide on, rather than silently letting the second Convert
// overwrite the first via store.Commit.
func TestImportCorpusRejectsSlugCollisions(t *testing.T) {
	corpusDir := t.TempDir()
	artifactsDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(artifactsDir, "02_pass_a.jsonl"), nil, 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(artifactsDir, "01_alias_table.json"), []byte("{}"), 0o644))

	page := []byte("---\nname: Foo\n---\nbody\n")
	require.NoError(t, os.WriteFile(filepath.Join(corpusDir, "project_foo.md"), page, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(corpusDir, "feedback_foo.md"), page, 0o644))

	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)

	_, err = importer.ImportCorpus(corpusDir, artifactsDir, pages, testTenants(t))
	require.Error(t, err)
	require.Contains(t, err.Error(), "project_foo.md")
	require.Contains(t, err.Error(), "feedback_foo.md")
	require.Contains(t, err.Error(), "work/notes/foo.md")
}
