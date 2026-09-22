// Package main implements the demo-vault generator: a deterministic, re-runnable builder
// of a small, entirely fictional vault used for README screenshots and local exploration.
// Every identifier below (uid, claim id, date, author) is hardcoded rather than derived from
// vault.NewUID() or time.Now(), so two runs against a clean output directory produce
// byte-identical git trees (commit SHAs included, since author/committer timestamps are
// fixed too) — see cmd/demo-vault/main.go's doc comment for the full rationale.
package main

import (
	"fmt"
	"strings"
)

// claim is one frontmatter claims[] entry. AsOf is only set for a claim whose status is
// superseded/retired, matching defaults/fixtures/azapi-ergw-connection-deletion.md's c6.
type claim struct {
	ID     string
	Text   string
	Status string
	AsOf   string
}

// page is one markdown page's full content: enough to render frontmatter matching the shape
// every defaults/fixtures/*.md file already uses, plus a markdown body.
type page struct {
	UID   string
	Slug  string
	Type  string // decision | entity | gotcha | incident | issue | note | procedure | state
	Scope string
	Title string

	Aliases []string

	// Comment, when set, is one or more pre-formatted "# ...\n" lines inserted directly above
	// the type-specific fields block — used exactly once, on the deliberate cross-scope
	// dangling reference, to explain the dangling ref the way
	// defaults/fixtures/globex-er-circuit.md already does.
	Comment string

	// State-only fields (defaults/types/state.yaml's fields:).
	InstanceOf   string
	SupersededBy string
	About        []string
	AsOf         string

	// Gotcha-only field (defaults/types/gotcha.yaml's fields:).
	Vendor string

	Tags   []string
	Claims []claim

	Status       string
	Owner        string
	LastVerified string

	Body string
}

// folderFor maps a page type to its defaults/types/<type>.yaml folder — the same mapping
// every defaults/types/*.yaml file declares (decisions/, entities/, gotchas/, incidents/,
// issues/, notes/, procedures/, state/).
func folderFor(pageType string) string {
	switch pageType {
	case "decision":
		return "decisions"
	case "entity":
		return "entities"
	case "gotcha":
		return "gotchas"
	case "incident":
		return "incidents"
	case "issue":
		return "issues"
	case "note":
		return "notes"
	case "procedure":
		return "procedures"
	case "state":
		return "state"
	default:
		panic("demo-vault: unknown page type " + pageType)
	}
}

// path returs this page's vault-relative path: scope/folder/slug.md.
func (p page) path() string {
	return fmt.Sprintf("%s/%s/%s.md", p.Scope, folderFor(p.Type), p.Slug)
}

// yq double-quotes a YAML scalar unconditionally. Claim text is free English prose that may
// contain punctuation a plain YAML scalar cannot safely carry inside a flow mapping (a comma
// ends a flow entry; ": " opens a mapping) — quoting once here removes the need to police
// every claim for those characters by hand.
func yq(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// render produces this page's full file content: YAML frontmatter fenced by "---" lines,
// field order matching defaults/fixtures/*.md exactly (uid, slug, type, scope, title,
// [aliases], [comment], [state/gotcha-specific fields], tags, [as_of], claims, status, owner,
// last_verified), followed by the markdown body.
func (p page) render() string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "uid: %s\n", p.UID)
	fmt.Fprintf(&b, "slug: %s\n", p.Slug)
	fmt.Fprintf(&b, "type: %s\n", p.Type)
	fmt.Fprintf(&b, "scope: %s\n", p.Scope)
	fmt.Fprintf(&b, "title: %s\n", p.Title)
	if len(p.Aliases) > 0 {
		fmt.Fprintf(&b, "aliases: [%s]\n", strings.Join(p.Aliases, ", "))
	}
	if p.Comment != "" {
		b.WriteString(p.Comment)
	}
	if p.Type == "state" {
		if p.InstanceOf != "" {
			fmt.Fprintf(&b, "instance_of: %s\n", p.InstanceOf)
		}
		if p.SupersededBy != "" {
			fmt.Fprintf(&b, "superseded_by: %s\n", p.SupersededBy)
		}
		if len(p.About) > 0 {
			fmt.Fprintf(&b, "about: [%s]\n", strings.Join(p.About, ", "))
		}
	}
	if p.Type == "gotcha" && p.Vendor != "" {
		fmt.Fprintf(&b, "vendor: %s\n", p.Vendor)
	}
	fmt.Fprintf(&b, "tags: [%s]\n", strings.Join(p.Tags, ", "))
	if p.Type == "state" {
		fmt.Fprintf(&b, "as_of: %s\n", p.AsOf)
	}
	b.WriteString("claims:\n")
	for _, c := range p.Claims {
		fmt.Fprintf(&b, "  - {text: %s, status: %s", yq(c.Text), c.Status)
		if c.AsOf != "" {
			fmt.Fprintf(&b, ", as_of: %s", c.AsOf)
		}
		fmt.Fprintf(&b, ", id: %s}\n", c.ID)
	}
	fmt.Fprintf(&b, "status: %s\n", p.Status)
	fmt.Fprintf(&b, "owner: %s\n", p.Owner)
	fmt.Fprintf(&b, "last_verified: %s\n", p.LastVerified)
	b.WriteString("---\n")
	b.WriteString(p.Body)
	return b.String()
}

// uid deterministically derives a 26-character ULID-shaped identifier from a small integer,
// so every page's uid is fixed across runs without depending on vault.NewUID's crypto/rand +
// time.Now() (which would make the vault different on every regeneration). The shape mirrors
// defaults/fixtures/*.md's own placeholder uids (e.g. "01JAAAAAAAAAAAAAAAAAAAAAA1", 26 chars).
func uid(n int) string {
	return fmt.Sprintf("01JDEMO%019d", n)
}
