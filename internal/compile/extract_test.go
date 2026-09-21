package compile_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dhia/balise/internal/compile"
	"github.com/stretchr/testify/require"
)

var errTransport = errors.New("simulated transport failure")

// fakeLLMClient is the only LLMClient implementation used in this package's unit
// tests — it never touches the network. Responses is consumed in order, one per
// call, so a test can script "invalid first, valid on retry."
type fakeLLMClient struct {
	responses []fakeResponse
	calls     []compile.ToolCall
}

type fakeResponse struct {
	input        string
	inputTokens  int
	outputTokens int
	err          error
}

func (f *fakeLLMClient) CallTool(_ context.Context, call compile.ToolCall) (compile.ToolResult, error) {
	f.calls = append(f.calls, call)
	idx := len(f.calls) - 1
	if idx >= len(f.responses) {
		panic("fakeLLMClient: more calls made than responses scripted")
	}
	r := f.responses[idx]
	if r.err != nil {
		return compile.ToolResult{}, r.err
	}
	return compile.ToolResult{
		Input:        json.RawMessage(r.input),
		InputTokens:  r.inputTokens,
		OutputTokens: r.outputTokens,
	}, nil
}

func extractSampleInput() compile.ExtractInput {
	return compile.ExtractInput{
		Path: "work/gotcha/azapi-ergw-connection-deletion.md", Scope: "work",
		Slug: "azapi-ergw-connection-deletion", TypeName: "gotcha",
		Body: "Any PUT on the gateway replaces the resource and drops connections.",
		ExistingClaims: []compile.ExistingClaim{
			{ID: "c1", Status: "active", Text: "AzAPI PUT on an ER gateway deletes all expressRouteConnections"},
		},
		MaxClaims: 6, Model: "claude-sonnet-5",
	}
}

func TestExtractClaimsSucceedsOnFirstValidResponse(t *testing.T) {
	client := &fakeLLMClient{responses: []fakeResponse{
		{input: `{"keep":["c1"],"reword":[],"retire":[],"add":[]}`, inputTokens: 100, outputTokens: 20},
	}}
	result, err := compile.ExtractClaims(context.Background(), client, extractSampleInput())
	require.NoError(t, err)
	require.Equal(t, 1, result.Attempts)
	require.Equal(t, []string{"c1"}, result.Change.Keep)
	require.Equal(t, 100, result.InputTokens)
	require.Equal(t, 20, result.OutputTokens)
	require.InDelta(t, 0.9, result.Confidence, 0.0001)
	require.Len(t, client.calls, 1)
}

func TestExtractClaimsRetriesOnceOnSchemaViolationThenSucceeds(t *testing.T) {
	client := &fakeLLMClient{responses: []fakeResponse{
		// first attempt: violates schema (retire missing entirely)
		{input: `{"keep":["c1"],"reword":[],"add":[]}`, inputTokens: 100, outputTokens: 20},
		// retry: valid
		{input: `{"keep":["c1"],"reword":[],"retire":[],"add":[]}`, inputTokens: 120, outputTokens: 25},
	}}
	result, err := compile.ExtractClaims(context.Background(), client, extractSampleInput())
	require.NoError(t, err)
	require.Equal(t, 2, result.Attempts)
	require.Equal(t, 220, result.InputTokens, "tokens from both attempts must be summed")
	require.Equal(t, 45, result.OutputTokens)
	require.InDelta(t, 0.65, result.Confidence, 0.0001)
	require.Len(t, client.calls, 2)
	// the retry prompt must name the specific problem, not just "try again"
	require.Contains(t, client.calls[1].User, "rejected")
}

func TestExtractClaimsRetriesOnceOnSemanticViolationThenSucceeds(t *testing.T) {
	client := &fakeLLMClient{responses: []fakeResponse{
		// first attempt: c1 never classified (semantic violation, passes JSON schema)
		{input: `{"keep":[],"reword":[],"retire":[],"add":[{"text":"a brand new claim about the gateway","status":"active"}]}`, inputTokens: 90, outputTokens: 15},
		{input: `{"keep":["c1"],"reword":[],"retire":[],"add":[]}`, inputTokens: 95, outputTokens: 18},
	}}
	result, err := compile.ExtractClaims(context.Background(), client, extractSampleInput())
	require.NoError(t, err)
	require.Equal(t, 2, result.Attempts)
	require.Contains(t, client.calls[1].User, "not classified")
}

func TestExtractClaimsFailsHardAfterTwoInvalidAttempts(t *testing.T) {
	client := &fakeLLMClient{responses: []fakeResponse{
		{input: `{"keep":[],"reword":[],"retire":[],"add":[]}`, inputTokens: 50, outputTokens: 10}, // zero claims: c1 unclassified AND empty
		{input: `{"keep":[],"reword":[],"retire":[],"add":[]}`, inputTokens: 55, outputTokens: 12},
	}}
	result, err := compile.ExtractClaims(context.Background(), client, extractSampleInput())
	require.Error(t, err)
	require.Equal(t, compile.ClaimsChange{}, result.Change, "no proposal-worthy change on a hard failure")
	require.Empty(t, result.Warnings)
	require.Equal(t, 2, result.Attempts)
	require.Equal(t, 105, result.InputTokens, "tokens spent on both failed attempts are still real cost")
	require.Equal(t, 22, result.OutputTokens)
	require.Len(t, client.calls, 2, "must retry exactly once, never more")
}

// Fix 1: an over-length claim triggers the existing retry (it might come back
// shorter), but if the retry is still over the limit the page succeeds anyway
// with the claim flagged, rather than failing.
func TestExtractClaimsRetriesOnAnOverlongClaimAndAcceptsItFlaggedIfStillLong(t *testing.T) {
	overlong := "this claim just keeps going on and on and on well past the fifteen word limit for a single claim"
	client := &fakeLLMClient{responses: []fakeResponse{
		{input: `{"keep":["c1"],"reword":[],"retire":[],"add":[{"text":"` + overlong + `","status":"active"}]}`, inputTokens: 100, outputTokens: 20},
		{input: `{"keep":["c1"],"reword":[],"retire":[],"add":[{"text":"` + overlong + `","status":"active"}]}`, inputTokens: 105, outputTokens: 22},
	}}
	result, err := compile.ExtractClaims(context.Background(), client, extractSampleInput())
	require.NoError(t, err, "a still-overlong claim after the retry must not fail the page")
	require.Equal(t, 2, result.Attempts)
	require.Len(t, client.calls, 2, "an over-length claim gets exactly one retry chance")
	require.Len(t, result.Change.Add, 1)
	require.Len(t, result.Warnings, 1)
	require.Equal(t, "over_word_limit", result.Warnings[0].Code)
	require.Equal(t, "add", result.Warnings[0].Bucket)
}

// The mirror case: the retry comes back short, so the page succeeds with no
// warning at all — the common case the task expects "a retry often fixes it".
func TestExtractClaimsAcceptsWithNoWarningWhenTheRetryFixesTheLength(t *testing.T) {
	overlong := "this claim just keeps going on and on and on well past the fifteen word limit for a single claim"
	client := &fakeLLMClient{responses: []fakeResponse{
		{input: `{"keep":["c1"],"reword":[],"retire":[],"add":[{"text":"` + overlong + `","status":"active"}]}`, inputTokens: 100, outputTokens: 20},
		{input: `{"keep":["c1"],"reword":[],"retire":[],"add":[{"text":"a much shorter claim about the gateway","status":"active"}]}`, inputTokens: 105, outputTokens: 22},
	}}
	result, err := compile.ExtractClaims(context.Background(), client, extractSampleInput())
	require.NoError(t, err)
	require.Empty(t, result.Warnings, "a corrected retry must not carry the earlier warning forward")
}

func TestExtractClaimsNeverRetriesOnATransportError(t *testing.T) {
	failing := &fakeLLMClient{responses: []fakeResponse{
		{err: errTransport},
	}}
	_, err := compile.ExtractClaims(context.Background(), failing, extractSampleInput())
	require.ErrorIs(t, err, errTransport)
	require.Len(t, failing.calls, 1, "a transport failure is not a schema violation and must not be retried")
}

func TestExtractClaimsSendsRequestedModel(t *testing.T) {
	client := &fakeLLMClient{responses: []fakeResponse{
		{input: `{"keep":["c1"],"reword":[],"retire":[],"add":[]}`, inputTokens: 10, outputTokens: 2},
	}}
	in := extractSampleInput()
	in.Model = "claude-haiku-test"
	_, err := compile.ExtractClaims(context.Background(), client, in)
	require.NoError(t, err)
	require.Equal(t, "claude-haiku-test", client.calls[0].Model)
}
