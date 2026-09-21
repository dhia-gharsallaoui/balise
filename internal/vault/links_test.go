package vault_test

import (
	"testing"

	"github.com/dhia/balise/internal/vault"
	"github.com/stretchr/testify/require"
)

var (
	slugs = map[vault.ScopedSlug]string{{Scope: "work", Slug: "aks-pod-cidr"}: "u-aks"}
	reg   = vault.AliasRegistry{}.Register("AKS pod CIDR — overlay, not routed", "u-aks", "work")
)

func TestResolutionLadder(t *testing.T) {
	for _, tc := range []struct{ name, ref, scope, wantUID, wantRung string }{
		{"exact slug", "aks-pod-cidr", "work", "u-aks", "slug"},
		{"exact alias", "AKS pod CIDR — overlay, not routed", "work", "u-aks", "alias"},
		{"normalised", "project_aks_pod_cidr.md", "work", "u-aks", "normalised"},
		{"unresolved", "no-such-page", "work", "", "dangling"},
		{"wrong scope", "aks-pod-cidr", "personal", "", "dangling"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := vault.Resolve(tc.ref, tc.scope, slugs, reg)
			require.Equal(t, tc.wantUID, got.UID)
			require.Equal(t, tc.wantRung, got.Rung)
		})
	}
}

func TestNormalisedRungPrefersSlugOverAlias(t *testing.T) {
	slugsHere := map[vault.ScopedSlug]string{{Scope: "work", Slug: "aks-pod-cidr"}: "u-slug-owner"}
	regHere := vault.AliasRegistry{}.Register("project_aks_pod_cidr.md", "u-alias-owner", "work")

	got := vault.Resolve("AKS_POD_CIDR.md", "work", slugsHere, regHere)
	require.Equal(t, "u-slug-owner", got.UID)
	require.Equal(t, "normalised", got.Rung)
}

func TestExtractLinks(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       []string
	}{
		{"plain wikilink", "see [[aks-pod-cidr]] for detail", []string{"aks-pod-cidr"}},
		{"display and heading stripped", "[[aks-pod-cidr|Pod CIDR#Overlay]]", []string{"aks-pod-cidr"}},
		{"relative markdown link", "see [Pod CIDR](project_aks_pod_cidr.md)", []string{"project_aks_pod_cidr.md"}},
		{"external links ignored", "[docs](https://example.com/x.md)", nil},
		{"document order preserved", "[[b]] then [[a]]", []string{"b", "a"}},
		{"duplicates kept for the caller to dedupe", "[[a]] and [[a]]", []string{"a", "a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, vault.ExtractLinks(tc.body))
		})
	}
}
