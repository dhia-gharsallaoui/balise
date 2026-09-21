package compile_test

import (
	"strings"
	"testing"

	"github.com/dhia/balise/internal/compile"
	"github.com/stretchr/testify/require"
)

func sampleInput() compile.ExtractInput {
	return compile.ExtractInput{
		Path:     "work/gotcha/azapi-ergw-connection-deletion.md",
		Scope:    "work",
		Slug:     "azapi-ergw-connection-deletion",
		TypeName: "gotcha",
		TypeDesc: "A trap or non-obvious failure mode worth remembering.",
		Title:    "AzAPI PUT on an ER gateway deletes all connections",
		Body:     "Line one.\nLine two mentions the gateway.\nLine three.",
		ExistingClaims: []compile.ExistingClaim{
			{ID: "c1", Status: "active", Text: "AzAPI PUT on an ER gateway deletes all expressRouteConnections"},
		},
		MaxClaims: 6,
		Model:     "claude-sonnet-5",
	}
}

func TestRenderPromptIsPureAndDeterministic(t *testing.T) {
	system1, user1 := compile.RenderPrompt(sampleInput())
	system2, user2 := compile.RenderPrompt(sampleInput())
	require.Equal(t, system1, system2)
	require.Equal(t, user1, user2)
}

func TestRenderPromptSystemStatesStatementVsSubjectRule(t *testing.T) {
	system, _ := compile.RenderPrompt(sampleInput())
	require.Contains(t, system, "AzAPI PUT on an ER gateway deletes all expressRouteConnections")
	require.Contains(t, system, "azapi-ergw-connection-deletion")
	require.Contains(t, system, "15 words")
}

func TestRenderPromptSystemStatesExhaustiveClassificationRule(t *testing.T) {
	system, _ := compile.RenderPrompt(sampleInput())
	require.Contains(t, strings.ToLower(system), "keep")
	require.Contains(t, strings.ToLower(system), "reword")
	require.Contains(t, strings.ToLower(system), "retire")
}

func TestRenderPromptSystemUsesMaxClaimsFromInputNeverHardcoded(t *testing.T) {
	in := sampleInput()
	in.MaxClaims = 3
	system, _ := compile.RenderPrompt(in)
	require.Contains(t, system, "between 1 and 3 claims")
}

func TestRenderPromptUserListsExistingClaimsWithIDAndStatus(t *testing.T) {
	_, user := compile.RenderPrompt(sampleInput())
	require.Contains(t, user, "id=c1")
	require.Contains(t, user, "status=active")
	require.Contains(t, user, "AzAPI PUT on an ER gateway deletes all expressRouteConnections")
}

func TestRenderPromptUserSaysNoneWhenPageHasNoExistingClaims(t *testing.T) {
	in := sampleInput()
	in.ExistingClaims = nil
	_, user := compile.RenderPrompt(in)
	require.Contains(t, user, "(none)")
}

func TestRenderPromptUserNumbersBodyLinesForSpanCitation(t *testing.T) {
	_, user := compile.RenderPrompt(sampleInput())
	require.Contains(t, user, "1: Line one.")
	require.Contains(t, user, "2: Line two mentions the gateway.")
	require.Contains(t, user, "3: Line three.")
}

func TestRenderPromptUserIncludesPageMetadataGenerically(t *testing.T) {
	_, user := compile.RenderPrompt(sampleInput())
	require.Contains(t, user, "work/gotcha/azapi-ergw-connection-deletion.md")
	require.Contains(t, user, "Scope: work")
	require.Contains(t, user, "Slug: azapi-ergw-connection-deletion")
	require.Contains(t, user, "Type: gotcha")
	require.Contains(t, user, "A trap or non-obvious failure mode worth remembering.")
}
