package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// contextFirstPrompt is 04 section 12's "balise/context-first" prompt: a
// small, static nudge that makes a client call the context tool before
// answering from its own memory, rather than a mechanism that does
// anything itself. It takes no arguments and returns one fixed message; a
// prompt this small does not need a request-argument-driven template.
var contextFirstPrompt = &sdk.Prompt{
	Name:        "balise/context-first",
	Description: "Load a context pack before answering, instead of guessing from memory alone.",
}

// contextFirstPromptText is the message body a client receives when it asks
// for the balise/context-first prompt. It names the context tool
// explicitly, and it repeats -- for the same honesty reasons context.go's
// own doc comments give -- that ranking is lexical, not semantic, and that
// a "low" coverage result means the caller should say so rather than
// silently backfilling the gap from its own memory.
const contextFirstPromptText = `Before answering, call the "context" tool with a query describing what you ` +
	`need, narrowed to a scope if you know it. Ground your answer in the pages it returns rather ` +
	`than your own memory of this vault. Its ranking is lexical (keyword/trigram) only, not ` +
	`semantic, so treat the order of returned pages as a rough signal, not a certainty. If its ` +
	`coverage comes back "low", or a page you need shows up in unloaded, say so explicitly instead ` +
	`of filling the gap from memory.`

func contextFirstPromptHandler(_ context.Context, _ *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
	return &sdk.GetPromptResult{
		Description: "Call context first",
		Messages: []*sdk.PromptMessage{
			{Role: "user", Content: &sdk.TextContent{Text: contextFirstPromptText}},
		},
	}, nil
}
