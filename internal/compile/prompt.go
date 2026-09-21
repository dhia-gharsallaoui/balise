package compile

import (
	"fmt"
	"strconv"
	"strings"
)

// ExistingClaim is one claim already recorded on a page's frontmatter, as decoded
// from its own claims list (id/status/text — the same shape indexer's
// claimFrontmatter uses).
type ExistingClaim struct {
	ID     string
	Text   string
	Status string
}

// ExtractInput is everything RenderPrompt and ExtractClaims need about one page.
// Every field here is generic (a slug, a type's own declared name/description, a
// body) — nothing here is ever a hardcoded type name, per internal/guard's
// domain-neutrality rule.
type ExtractInput struct {
	Path           string
	Scope          string
	Slug           string
	TypeName       string
	TypeDesc       string
	Title          string
	Body           string
	ExistingClaims []ExistingClaim
	MaxClaims      int
	Model          string
}

// RenderPrompt builds the system and user prompts for one extract_claims call. It
// is a pure function — no I/O, no randomness — so --dry-run can render and print
// it without ever constructing an LLMClient.
func RenderPrompt(in ExtractInput) (system, user string) {
	return renderSystemPrompt(in), renderUserPrompt(in)
}

func renderSystemPrompt(in ExtractInput) string {
	var b strings.Builder
	b.WriteString("You are Balise's extract_claims compile task. A claim is a " +
		"STATEMENT, never a subject. For example, write \"AzAPI PUT on an ER " +
		"gateway deletes all expressRouteConnections\" — never a bare subject or " +
		"slug like \"azapi-ergw-connection-deletion\".\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Each claim is at most 15 words.\n")
	fmt.Fprintf(&b, "- Return between 1 and %d claims total across keep, reword, and add.\n", in.MaxClaims)
	b.WriteString("- Every existing claim id must land in exactly one of keep, reword, " +
		"or retire — never left out, never duplicated.\n")
	b.WriteString("- A claim can supersede another still on the same page: mark the " +
		"outdated one retired even if the page as a whole stays active.\n")
	b.WriteString("- When you add a new claim, cite the body line(s) it comes from as a " +
		"[start, end] span over the numbered lines given below, or null if you cannot " +
		"point to a specific line.\n")
	b.WriteString("- Only use information present in the page body below; do not invent facts.\n")
	return b.String()
}

func renderUserPrompt(in ExtractInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Page: %s\n", in.Path)
	fmt.Fprintf(&b, "Scope: %s\n", in.Scope)
	fmt.Fprintf(&b, "Slug: %s\n", in.Slug)
	fmt.Fprintf(&b, "Type: %s", in.TypeName)
	if in.TypeDesc != "" {
		fmt.Fprintf(&b, " (%s)", in.TypeDesc)
	}
	b.WriteString("\n")
	if in.Title != "" {
		fmt.Fprintf(&b, "Title: %s\n", in.Title)
	}
	b.WriteString("\nExisting claims:\n")
	b.WriteString(renderExistingClaims(in.ExistingClaims))
	b.WriteString("\nBody (numbered by line):\n")
	b.WriteString(numberLines(in.Body))
	return b.String()
}

func renderExistingClaims(claims []ExistingClaim) string {
	if len(claims) == 0 {
		return "(none)\n"
	}
	var b strings.Builder
	for _, c := range claims {
		fmt.Fprintf(&b, "- id=%s status=%s text=%q\n", c.ID, c.Status, c.Text)
	}
	return b.String()
}

// numberLines renders body with a 1-based line number prefix on every line, so
// the model can cite a [start, end] span that this package can later map back to
// exact source lines.
func numberLines(body string) string {
	lines := strings.Split(body, "\n")
	width := len(strconv.Itoa(len(lines)))
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%*d: %s\n", width, i+1, line)
	}
	return b.String()
}
