package vault_test

import (
	"strings"
	"testing"

	"github.com/dhia/balise/internal/vault"
	"github.com/stretchr/testify/require"
)

const sample = `---
uid: 01J8Z3K9V6Q2M4N7P8R9S0T1U2
type: gotcha
title: AzAPI ER gateway update DELETES its connections
originSessionId: bf52f88e
claims:
  - text: Updating the gateway through AzAPI removes every connection
    status: active
    id: c1
---
Body text here.
`

func TestParseReadsMetaAndBody(t *testing.T) {
	page, err := vault.Parse(sample)
	require.NoError(t, err)

	var fm struct {
		Type   string `yaml:"type"`
		Claims []struct {
			ID string `yaml:"id"`
		} `yaml:"claims"`
	}
	require.NoError(t, page.Decode(&fm))
	require.Equal(t, "gotcha", fm.Type)
	require.Equal(t, "c1", fm.Claims[0].ID)
	require.Equal(t, "Body text here.", strings.TrimSpace(page.Body))
}

func TestParsePreservesUnknownKeys(t *testing.T) {
	page, err := vault.Parse(sample)
	require.NoError(t, err)
	require.NotNil(t, page.Get("originSessionId"))
	require.Equal(t, "bf52f88e", page.Get("originSessionId").Value)
}

func TestRoundTripPreservesKeyOrder(t *testing.T) {
	page, err := vault.Parse(sample)
	require.NoError(t, err)

	rendered, err := page.Render()
	require.NoError(t, err)

	again, err := vault.Parse(rendered)
	require.NoError(t, err)
	require.Equal(t, keys(t, page), keys(t, again))
	require.Contains(t, keys(t, again), "originSessionId")
}

func TestWithReturnsANewPage(t *testing.T) {
	page, err := vault.Parse(sample)
	require.NoError(t, err)

	updated, err := page.With("scope", "work")
	require.NoError(t, err)
	require.Nil(t, page.Get("scope"), "the original must not be mutated")
	require.Equal(t, "work", updated.Get("scope").Value)
}

func TestWithReplacesAnExistingKeyInPlace(t *testing.T) {
	page, _ := vault.Parse(sample)
	updated, err := page.With("type", "decision")
	require.NoError(t, err)
	require.Equal(t, "decision", updated.Get("type").Value)
	require.Equal(t, keys(t, page), keys(t, updated), "order must not change")
}

func TestBodyWithoutFrontmatterParses(t *testing.T) {
	page, err := vault.Parse("Just a body.\n")
	require.NoError(t, err)
	require.Nil(t, page.Meta)
	require.Equal(t, "Just a body.", strings.TrimSpace(page.Body))
}

func TestUnparseableYAMLReturnsAnError(t *testing.T) {
	_, err := vault.Parse("---\n\tbad: [unclosed\n---\nbody\n")
	require.Error(t, err)
}

func keys(t *testing.T, p vault.Page) []string {
	t.Helper()
	if p.Meta == nil {
		return nil
	}
	var out []string
	for i := 0; i < len(p.Meta.Content); i += 2 {
		out = append(out, p.Meta.Content[i].Value)
	}
	return out
}
