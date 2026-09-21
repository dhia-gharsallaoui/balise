package compile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const anthropicVersion = "2023-06-01"

// LLMClient is the seam every compile task calls through. The only production
// implementation is AnthropicClient; tests use a fake so no unit test ever makes a
// network call.
type LLMClient interface {
	CallTool(ctx context.Context, call ToolCall) (ToolResult, error)
}

// ToolCall is one forced-tool-use request: the model is required to answer by
// calling ToolName, with its input constrained by InputSchema (a JSON Schema
// rendered as the map Anthropic's API expects).
type ToolCall struct {
	Model       string
	System      string
	User        string
	ToolName    string
	ToolDesc    string
	InputSchema map[string]any
	MaxTokens   int

	// Temperature is nil by default and then omitted from the request body
	// entirely. claude-sonnet-5, called through this project's APIM gateway,
	// returns HTTP 400 ("temperature is deprecated for this model") for ANY
	// temperature value, including 0 — verified against the live gateway.
	// 04-technical-spec-v1.md section 9.2 says "Temperature 0"; leaving this
	// nil is a deliberate, reported deviation from that line, not an oversight.
	Temperature *float64
}

// ToolResult is the model's structured answer plus token usage, needed for the
// per-task cost recording section 9.3 requires.
type ToolResult struct {
	Input        json.RawMessage
	InputTokens  int
	OutputTokens int
}

// AnthropicClient calls the Anthropic Messages API through the caller's own
// gateway. BaseURL and APIKey always come from the environment: this project's
// stated policy is "never call vendor APIs direct" — ANTHROPIC_BASE_URL points at
// an internal APIM AI Gateway, never api.anthropic.com — so neither is ever
// hardcoded here.
type AnthropicClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewAnthropicClient builds a client against an explicit base URL and key. It
// exists separately from NewAnthropicClientFromEnv so tests can point it at an
// httptest server; production code should call NewAnthropicClientFromEnv instead.
func NewAnthropicClient(baseURL, apiKey string, httpClient *http.Client) *AnthropicClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 120 * time.Second}
	}
	return &AnthropicClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    httpClient,
	}
}

// NewAnthropicClientFromEnv reads ANTHROPIC_API_KEY and ANTHROPIC_BASE_URL. Both
// are required: a client with no key or no gateway URL fails loudly at startup
// rather than silently falling back to a hardcoded vendor endpoint.
func NewAnthropicClientFromEnv() (*AnthropicClient, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY is not set")
	}
	baseURL := os.Getenv("ANTHROPIC_BASE_URL")
	if baseURL == "" {
		return nil, fmt.Errorf("ANTHROPIC_BASE_URL is not set")
	}
	return NewAnthropicClient(baseURL, apiKey, nil), nil
}

var _ LLMClient = (*AnthropicClient)(nil)

// CallTool sends one forced-tool-use request and returns its structured input.
func (c *AnthropicClient) CallTool(ctx context.Context, call ToolCall) (ToolResult, error) {
	httpReq, err := c.buildRequest(ctx, call)
	if err != nil {
		return ToolResult{}, err
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return ToolResult{}, fmt.Errorf("call model: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ToolResult{}, fmt.Errorf("read model response: %w", err)
	}
	return parseToolResponse(resp.StatusCode, raw, call.ToolName)
}

type messagesRequest struct {
	Model       string            `json:"model"`
	MaxTokens   int               `json:"max_tokens"`
	System      string            `json:"system,omitempty"`
	Messages    []messagePayload  `json:"messages"`
	Tools       []toolPayload     `json:"tools"`
	ToolChoice  toolChoicePayload `json:"tool_choice"`
	Temperature *float64          `json:"temperature,omitempty"`
}

type messagePayload struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type toolPayload struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type toolChoicePayload struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// buildRequest constructs the outgoing *http.Request for one ToolCall.
func (c *AnthropicClient) buildRequest(ctx context.Context, call ToolCall) (*http.Request, error) {
	body := messagesRequest{
		Model:     call.Model,
		MaxTokens: call.MaxTokens,
		System:    call.System,
		Messages:  []messagePayload{{Role: "user", Content: call.User}},
		Tools: []toolPayload{{
			Name:        call.ToolName,
			Description: call.ToolDesc,
			InputSchema: call.InputSchema,
		}},
		ToolChoice:  toolChoicePayload{Type: "tool", Name: call.ToolName},
		Temperature: call.Temperature,
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/messages", bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)
	return req, nil
}

type messagesResponse struct {
	Content []contentBlock `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *apiError `json:"error"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type apiError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// parseToolResponse decodes one Messages API response and extracts the tool_use
// block matching toolName. It is a free function (no receiver) so tests can
// exercise response parsing directly, without an HTTP round trip.
func parseToolResponse(status int, raw []byte, toolName string) (ToolResult, error) {
	var parsed messagesResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ToolResult{}, fmt.Errorf("parse response (status %d): %w", status, err)
	}
	if status != http.StatusOK {
		return ToolResult{}, apiStatusError(status, raw, parsed.Error)
	}
	for _, block := range parsed.Content {
		if block.Type == "tool_use" && block.Name == toolName {
			return ToolResult{
				Input:        block.Input,
				InputTokens:  parsed.Usage.InputTokens,
				OutputTokens: parsed.Usage.OutputTokens,
			}, nil
		}
	}
	return ToolResult{}, fmt.Errorf("model response contained no %q tool_use block", toolName)
}

// apiStatusError renders a non-200 response, preferring the gateway's own
// {"error":{"type","message"}} shape when present.
func apiStatusError(status int, raw []byte, apiErr *apiError) error {
	if apiErr != nil {
		return fmt.Errorf("model call failed (status %d, %s): %s", status, apiErr.Type, apiErr.Message)
	}
	return fmt.Errorf("model call failed (status %d): %s", status, string(raw))
}
