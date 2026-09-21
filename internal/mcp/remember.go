package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/vault"
)

// RememberInput is remember's argument shape. 04 section 12's abbreviated
// schema also lists optional "type" and "tags" fields, but those describe
// frontmatter for an indexed document; remember does not create one -- it
// appends one entry to a plain memory file (the task brief's deliverable
// 5) -- so there is no frontmatter for them to populate. They are dropped
// here rather than accepted and silently ignored, which would mislead a
// caller into thinking they had an effect.
type RememberInput struct {
	// Text is appended, verbatim, as a new dated, timestamped entry.
	Text string `json:"text"`
	// Scope selects which of the token's own scopes to write into. It
	// must already be one of the token's scopes -- 04 section 14: "a
	// scope argument narrows, never widens." A token holding only "work"
	// that passes scope "client-globex" is refused, never redirected.
	Scope string `json:"scope"`
}

// RememberOutput matches 04 section 12's {"slug":"","path":""} shape.
type RememberOutput struct {
	Slug string `json:"slug"`
	Path string `json:"path"`
}

func (d *deps) remember(ctx context.Context, req *sdk.CallToolRequest, in RememberInput) (*sdk.CallToolResult, RememberOutput, error) {
	start := time.Now()
	var out RememberOutput

	tok, err := authenticate(ctx, d.pool, req)
	if err != nil {
		return nil, out, err
	}

	if !tok.HasCapability("remember") {
		recordAudit(ctx, d.pool, tok.ID, "remember", tok.Scopes, in.Text, nil, 0, start)
		return nil, out, fmt.Errorf("missing capability: remember")
	}
	if in.Scope == "" || !tok.AllowsScope(in.Scope) {
		recordAudit(ctx, d.pool, tok.ID, "remember", tok.Scopes, in.Text, nil, 0, start)
		return nil, out, fmt.Errorf("scope %q not allowed for this token", in.Scope)
	}
	if strings.TrimSpace(in.Text) == "" {
		recordAudit(ctx, d.pool, tok.ID, "remember", []string{in.Scope}, in.Text, nil, 0, start)
		return nil, out, errors.New("text must not be empty")
	}

	when := time.Now()
	p, err := rememberPath(in.Scope, tok.Name, when)
	if err != nil {
		recordAudit(ctx, d.pool, tok.ID, "remember", []string{in.Scope}, in.Text, nil, 0, start)
		return nil, out, fmt.Errorf("build memory path: %w", err)
	}

	existing, _, readErr := d.pages.Read(p)
	if readErr != nil {
		if !errors.Is(readErr, os.ErrNotExist) {
			recordAudit(ctx, d.pool, tok.ID, "remember", []string{in.Scope}, in.Text, nil, 0, start)
			return nil, out, fmt.Errorf("read memory file: %w", readErr)
		}
		existing = nil
	}

	entry := formatMemoryEntry(existing, tok.Name, when, in.Text)
	newContent := make([]byte, 0, len(existing)+len(entry))
	newContent = append(newContent, existing...)
	newContent = append(newContent, entry...)

	// The author is exactly "agent:<name>", per the task brief -- no angle
	// brackets, so git.go's splitAuthor treats the whole string as the
	// commit author name with an empty email, which is what makes this
	// author line visibly distinct from the owner's own "dhia <...>"
	// commits. PageStore.Write is not used here because GitPageStore.Write
	// hardcodes the author to "balise <balise@localhost>"; only Commit
	// lets a caller supply its own author.
	author := "agent:" + tok.Name
	message := fmt.Sprintf("remember: %s", tok.Name)
	if _, err := d.pages.Commit([]store.Change{{Path: p, Data: newContent}}, author, message); err != nil {
		recordAudit(ctx, d.pool, tok.ID, "remember", []string{in.Scope}, in.Text, nil, 0, start)
		return nil, out, fmt.Errorf("commit memory file: %w", err)
	}

	out.Path = p
	out.Slug = vault.SlugFromFilename(path.Base(p))

	// last_used_at is recorded once, centrally, by authenticate() at the
	// top of this call -- see auth.go's doc comment on authenticate for
	// why that is the one right place for it, covering all six tool
	// handlers uniformly instead of each one (this one included)
	// remembering to call store.TouchToken for itself.
	tokensOut := len(strings.Fields(in.Text))
	recordAudit(ctx, d.pool, tok.ID, "remember", []string{in.Scope}, in.Text, []string{p}, tokensOut, start)
	return nil, out, nil
}

// formatMemoryEntry renders one dated, timestamped entry to append to a
// memory file. If existing is empty (a brand-new day's file), a level-1
// heading names the agent and date first.
func formatMemoryEntry(existing []byte, agent string, when time.Time, text string) []byte {
	var b strings.Builder
	if len(existing) == 0 {
		fmt.Fprintf(&b, "# %s -- %s\n\n", agent, when.UTC().Format("2006-01-02"))
	}
	fmt.Fprintf(&b, "- **%s UTC** %s\n", when.UTC().Format("15:04:05"), text)
	return []byte(b.String())
}
