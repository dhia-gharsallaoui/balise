package compile

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	extractToolName         = "record_claims_change"
	extractToolDesc         = "Record the classification of every existing claim (keep/reword/retire) plus any newly observed claims (add) for this page."
	defaultExtractMaxTokens = 2048

	// confidenceFirstTry and confidenceAfterRetry are a simple, explicitly
	// non-calibrated heuristic: valid on the first attempt scores higher than
	// valid only after a correction round. Nothing here claims statistical
	// calibration against ground truth.
	confidenceFirstTry   = 0.9
	confidenceAfterRetry = 0.65
)

// ExtractResult is the outcome of one successful ExtractClaims call. Warnings
// (fix 1) are non-blocking per-claim flags — e.g. a claim over the word limit —
// that still made it into Change; they exist so the caller can carry them into
// the proposal for a human reviewer to see, not to gate acceptance.
type ExtractResult struct {
	Change       ClaimsChange
	InputTokens  int
	OutputTokens int
	Attempts     int
	Confidence   float64
	Warnings     []ClaimWarning
}

// ExtractClaims calls the model once, validates its answer against both JSON
// Schema and the semantic rules in validate.go, and retries exactly once — with
// the specific problems and warnings listed back to the model — if either is
// non-empty (a retry often fixes a warning too, e.g. shortens an over-length
// claim). Only hard problems can fail the call: malformed JSON, a claim with
// empty/too-short text, or a reword/retire naming a claim id that doesn't exist
// on the page. A claim that is still over the word limit after the retry is
// accepted with its warning intact (fix 1) rather than failing the page.
//
// A transport/API failure (network error, non-200 status) is not a "schema
// violation" and is not retried here; it propagates immediately.
func ExtractClaims(ctx context.Context, client LLMClient, in ExtractInput) (ExtractResult, error) {
	system, user := RenderPrompt(in)
	schema, err := claimsChangeInputSchema()
	if err != nil {
		return ExtractResult{}, fmt.Errorf("load claims-change schema: %w", err)
	}

	first, err := callModel(ctx, client, in, system, user, schema)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("call model: %w", err)
	}
	change, problems, warnings := decodeAndValidate(first.Input, in)
	if len(problems) == 0 && len(warnings) == 0 {
		return ExtractResult{
			Change: change, InputTokens: first.InputTokens, OutputTokens: first.OutputTokens,
			Attempts: 1, Confidence: confidenceFirstTry,
		}, nil
	}

	retryUser := user + renderRetryNote(problems, warnings)
	second, err := callModel(ctx, client, in, system, retryUser, schema)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("call model on retry: %w", err)
	}
	change, problems, warnings = decodeAndValidate(second.Input, in)
	if len(problems) > 0 {
		// Tokens spent on both attempts are real cost even though the page
		// failed — the caller (run.go) still accounts for them in the run's
		// totals, so a hard-failing page's spend is never silently dropped.
		return ExtractResult{
				InputTokens:  first.InputTokens + second.InputTokens,
				OutputTokens: first.OutputTokens + second.OutputTokens,
				Attempts:     2,
			}, fmt.Errorf(
				"extract_claims failed validation on both attempts, giving up (last problem(s): %s)",
				strings.Join(problems, "; "))
	}
	return ExtractResult{
		Change:       change,
		InputTokens:  first.InputTokens + second.InputTokens,
		OutputTokens: first.OutputTokens + second.OutputTokens,
		Attempts:     2, Confidence: confidenceAfterRetry,
		Warnings: warnings,
	}, nil
}

func callModel(ctx context.Context, client LLMClient, in ExtractInput, system, user string, schema map[string]any) (ToolResult, error) {
	return client.CallTool(ctx, ToolCall{
		Model:       in.Model,
		System:      system,
		User:        user,
		ToolName:    extractToolName,
		ToolDesc:    extractToolDesc,
		InputSchema: schema,
		MaxTokens:   defaultExtractMaxTokens,
	})
}

// decodeAndValidate runs the model's raw tool input through JSON Schema
// validation, then decodes it, then runs the semantic checks. A schema
// violation or decode failure is always a hard problem (there's no partial
// change to salvage); ValidateSemantics is what splits its own findings into
// hard problems and non-blocking warnings (fix 1).
func decodeAndValidate(raw json.RawMessage, in ExtractInput) (ClaimsChange, []string, []ClaimWarning) {
	schema, err := claimsChangeSchema()
	if err != nil {
		return ClaimsChange{}, []string{fmt.Sprintf("internal error loading validator: %v", err)}, nil
	}
	var asAny any
	if err := json.Unmarshal(raw, &asAny); err != nil {
		return ClaimsChange{}, []string{fmt.Sprintf("response was not valid JSON: %v", err)}, nil
	}
	if err := schema.Validate(asAny); err != nil {
		return ClaimsChange{}, []string{fmt.Sprintf("response did not match the required schema: %v", err)}, nil
	}
	var change ClaimsChange
	if err := json.Unmarshal(raw, &change); err != nil {
		return ClaimsChange{}, []string{fmt.Sprintf("response could not be decoded: %v", err)}, nil
	}
	problems, warnings := ValidateSemantics(change, in.ExistingClaims, in.MaxClaims)
	return change, problems, warnings
}

// renderRetryNote appends the specific rejection reasons (and any non-blocking
// warnings) to the original user prompt for the single retry attempt, so the
// model can correct exactly what was wrong instead of guessing. The "rejected"
// framing is kept even when only warnings triggered the retry: a retry is a
// single combined round-trip, not a second class of request.
func renderRetryNote(problems []string, warnings []ClaimWarning) string {
	var b strings.Builder
	b.WriteString("\n\nYour previous answer was rejected for the following reason(s):\n")
	for _, p := range problems {
		fmt.Fprintf(&b, "- %s\n", p)
	}
	for _, w := range warnings {
		fmt.Fprintf(&b, "- %s\n", warningNote(w))
	}
	b.WriteString("Return corrected JSON that fixes all of the above.\n")
	return b.String()
}

func warningNote(w ClaimWarning) string {
	switch w.Code {
	case overWordLimitCode:
		return fmt.Sprintf("%s claim %q is over the %d-word limit; a shorter version is preferred", w.Bucket, w.Text, maxClaimWords)
	default:
		return fmt.Sprintf("%s claim %q was flagged: %s", w.Bucket, w.Text, w.Code)
	}
}
