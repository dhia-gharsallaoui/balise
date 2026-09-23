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
	"gopkg.in/yaml.v3"

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

	slug := vault.SlugFromFilename(path.Base(p))
	entry := formatMemoryEntry(when, in.Text)

	// A brand-new day's file gets its frontmatter (defaults/types/memory.yaml: type
	// memory, fields agent + date) written exactly once, right here, before the first
	// entry -- this is the fix for the gap indexer/pipeline.go's
	// orDefault(fm.Type, "note") papered over: with no frontmatter at all, every memory
	// file used to index as a plain "note". Every later remember call for the same day
	// only appends a bullet to the tail of the file, so this header, once written, is
	// never touched again.
	var newContent []byte
	if len(existing) == 0 {
		header, err := renderMemoryHeader(memoryMeta{
			UID: vault.NewUID(), Slug: slug, Type: "memory", Scope: in.Scope,
			Title: fmt.Sprintf("%s -- %s", tok.Name, when.UTC().Format("2006-01-02")),
			Agent: tok.Name, Date: when.UTC().Format("2006-01-02"),
		}, tok.Name, when)
		if err != nil {
			recordAudit(ctx, d.pool, tok.ID, "remember", []string{in.Scope}, in.Text, nil, 0, start)
			return nil, out, fmt.Errorf("render memory header: %w", err)
		}
		newContent = append(newContent, header...)
	} else {
		newContent = append(newContent, existing...)
	}
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
	out.Slug = slug

	// last_used_at is recorded once, centrally, by authenticate() at the
	// top of this call -- see auth.go's doc comment on authenticate for
	// why that is the one right place for it, covering all six tool
	// handlers uniformly instead of each one (this one included)
	// remembering to call store.TouchToken for itself.
	tokensOut := len(strings.Fields(in.Text))
	recordAudit(ctx, d.pool, tok.ID, "remember", []string{in.Scope}, in.Text, []string{p}, tokensOut, start)
	return nil, out, nil
}

// formatMemoryEntry renders one dated, timestamped entry to append to a memory file. The
// file's own frontmatter and level-1 heading are a separate concern, written once by
// renderMemoryHeader when the file is created -- this function only ever adds one bullet
// line, whether that is the first entry of the day or the fifth.
func formatMemoryEntry(when time.Time, text string) []byte {
	return []byte(fmt.Sprintf("- **%s UTC** %s\n", when.UTC().Format("15:04:05"), text))
}

// memoryMeta is the frontmatter written once, at file creation, for a memory page --
// defaults/types/memory.yaml's own fields (agent, date) plus the generic uid/slug/type/
// scope/title every other page in this vault carries. It deliberately mirrors
// internal/api/sources.go's sourceMeta (a plain struct encoded with a yaml.Encoder, not a
// hand-built vault.Page/yaml.Node) -- the same small pattern internal/importer's renderMeta
// also uses, reproduced here rather than factored into a shared helper across three
// unrelated packages for ten lines of code.
type memoryMeta struct {
	UID   string `yaml:"uid"`
	Slug  string `yaml:"slug"`
	Type  string `yaml:"type"`
	Scope string `yaml:"scope"`
	Title string `yaml:"title"`
	Agent string `yaml:"agent"`
	Date  string `yaml:"date"`
}

// renderMemoryHeader encodes meta as YAML frontmatter, fenced by "---" lines, followed by
// the level-1 "# agent -- date" heading formatMemoryEntry's first bullet used to follow
// directly -- so a file written under the new code looks exactly like one written under
// the old code, with one YAML block inserted above it.
func renderMemoryHeader(meta memoryMeta, agent string, when time.Time) (string, error) {
	var sb strings.Builder
	encoder := yaml.NewEncoder(&sb)
	encoder.SetIndent(2)
	if err := encoder.Encode(meta); err != nil {
		return "", fmt.Errorf("encode frontmatter: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("close encoder: %w", err)
	}
	heading := fmt.Sprintf("# %s -- %s\n\n", agent, when.UTC().Format("2006-01-02"))
	return "---\n" + sb.String() + "---\n" + heading, nil
}
