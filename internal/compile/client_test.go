package compile_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dhia/balise/internal/compile"
	"github.com/stretchr/testify/require"
)

func TestNewAnthropicClientFromEnvRequiresAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", "https://gateway.example/anthropic")

	_, err := compile.NewAnthropicClientFromEnv()
	require.ErrorContains(t, err, "ANTHROPIC_API_KEY")
}

func TestNewAnthropicClientFromEnvRequiresBaseURL(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "")

	_, err := compile.NewAnthropicClientFromEnv()
	require.ErrorContains(t, err, "ANTHROPIC_BASE_URL")
}

func TestNewAnthropicClientFromEnvNeverHardcodesVendorHost(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "https://internal-gateway.example/anthropic")

	client, err := compile.NewAnthropicClientFromEnv()
	require.NoError(t, err)
	require.NotNil(t, client)
}

// TestCallToolOmitsTemperatureByDefault locks in the verified, spec-deviating
// behaviour: claude-sonnet-5 through this gateway returns HTTP 400 ("temperature
// is deprecated for this model") for ANY temperature value. ToolCall.Temperature
// is nil unless a caller sets it, and json:",omitempty" must drop the key
// entirely rather than send "temperature":0.
func TestCallToolOmitsTemperatureByDefault(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		require.Equal(t, "test-key", r.Header.Get("x-api-key"))
		require.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		writeToolUseResponse(w, "extract_claims", `{"keep":[]}`, 10, 5)
	}))
	defer server.Close()

	client := compile.NewAnthropicClient(server.URL, "test-key", server.Client())
	_, err := client.CallTool(context.Background(), compile.ToolCall{
		Model: "claude-sonnet-5", User: "hi", ToolName: "extract_claims",
		ToolDesc: "test", InputSchema: map[string]any{"type": "object"}, MaxTokens: 100,
	})
	require.NoError(t, err)
	_, hasTemperature := gotBody["temperature"]
	require.False(t, hasTemperature, "temperature must be omitted when ToolCall.Temperature is nil")
}

func TestCallToolIncludesTemperatureWhenSet(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		writeToolUseResponse(w, "extract_claims", `{"keep":[]}`, 10, 5)
	}))
	defer server.Close()

	zero := 0.0
	client := compile.NewAnthropicClient(server.URL, "test-key", server.Client())
	_, err := client.CallTool(context.Background(), compile.ToolCall{
		Model: "claude-haiku", User: "hi", ToolName: "extract_claims",
		ToolDesc: "test", InputSchema: map[string]any{"type": "object"}, MaxTokens: 100,
		Temperature: &zero,
	})
	require.NoError(t, err)
	require.InDelta(t, 0.0, gotBody["temperature"], 0.0001)
}

func TestCallToolReturnsInputAndUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeToolUseResponse(w, "extract_claims", `{"keep":["c1"],"add":[]}`, 234, 36)
	}))
	defer server.Close()

	client := compile.NewAnthropicClient(server.URL, "test-key", server.Client())
	result, err := client.CallTool(context.Background(), compile.ToolCall{
		Model: "claude-sonnet-5", User: "hi", ToolName: "extract_claims",
		ToolDesc: "test", InputSchema: map[string]any{"type": "object"}, MaxTokens: 100,
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"keep":["c1"],"add":[]}`, string(result.Input))
	require.Equal(t, 234, result.InputTokens)
	require.Equal(t, 36, result.OutputTokens)
}

// TestCallToolSurfacesGatewayError is a regression test for the exact failure
// observed against the live gateway when a "temperature" field was sent: a 400
// with an {"error":{"type","message"}} body, which must come back as a Go error
// naming both fields rather than a generic "unexpected status" message.
func TestCallToolSurfacesGatewayError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"temperature is deprecated for this model."}}`))
	}))
	defer server.Close()

	client := compile.NewAnthropicClient(server.URL, "test-key", server.Client())
	_, err := client.CallTool(context.Background(), compile.ToolCall{
		Model: "claude-sonnet-5", User: "hi", ToolName: "extract_claims",
		ToolDesc: "test", InputSchema: map[string]any{"type": "object"}, MaxTokens: 100,
	})
	require.ErrorContains(t, err, "invalid_request_error")
	require.ErrorContains(t, err, "temperature is deprecated")
}

func TestCallToolErrorsWhenNoToolUseBlockPresent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"no tool call here"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	client := compile.NewAnthropicClient(server.URL, "test-key", server.Client())
	_, err := client.CallTool(context.Background(), compile.ToolCall{
		Model: "claude-sonnet-5", User: "hi", ToolName: "extract_claims",
		ToolDesc: "test", InputSchema: map[string]any{"type": "object"}, MaxTokens: 100,
	})
	require.ErrorContains(t, err, "no")
	require.ErrorContains(t, err, "tool_use")
}

// writeToolUseResponse writes a minimal Messages API success response carrying
// one tool_use content block plus token usage.
func writeToolUseResponse(w http.ResponseWriter, toolName, inputJSON string, inputTokens, outputTokens int) {
	body := map[string]any{
		"content": []map[string]any{
			{"type": "tool_use", "name": toolName, "input": json.RawMessage(inputJSON)},
		},
		"usage": map[string]any{"input_tokens": inputTokens, "output_tokens": outputTokens},
	}
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
