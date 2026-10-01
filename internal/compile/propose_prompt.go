package compile

import (
	"fmt"
	"strings"
)

// RenderPlacementPrompt builds the system and user prompts for one propose_pages call. Pure
// (no I/O, no randomness) so --dry-run can print it without a client. Page types appear only
// as the registry's own names and descriptions, never as vocabulary baked in here.
func RenderPlacementPrompt(in PlacementInput) (system, user string) {
	var s strings.Builder
	s.WriteString(`You file notes that coding agents remembered while working into a team knowledge base.
Each note is a raw observation. Decide, for every note, what to do with it:

- attach: the note adds a durable fact to one of the listed candidate pages, because it is about
  the same subject. Restate it as one claim for that page.
- new_page: the note is durable but no candidate page is about its subject. Put it on a new page,
  naming that page by its slug in the decision. Group notes about the same subject onto the same
  new page, and list each new page exactly once in new_pages.
- skip: the note is not worth keeping as knowledge (progress chatter, a transient status, a
  one-off command output, something already stated by a candidate page's claims).

Rules:
- Every note gets at least one decision. Use the note numbers as listed. A note that holds several
  distinct facts may get several attach/new_page decisions, one claim each, possibly on different
  pages. skip must be a note's only decision.
- attach only to a slug from the candidate list. Never invent a target.
- A claim is one self-contained statement of fact, `)
	fmt.Fprintf(&s, "%d to %d words, true on its own without the note beside it.\n", minClaimWords, maxClaimWords)
	s.WriteString(`- State only what the notes say. Never add facts, numbers or names they do not contain.
- A new page needs: a type from the list below, a slug (lowercase words joined by hyphens, not
  already taken), a title that states the main finding in at most `)
	fmt.Fprintf(&s, "%d words, and a short markdown body that\n", maxTitleWords)
	s.WriteString(`  summarises its notes for a reader who has not seen them.
- Prefer attach over new_page when a candidate is genuinely about the same subject; prefer one
  new page holding several related notes over several thin pages.

Page types a new page may take:
`)
	for _, t := range in.Types {
		fmt.Fprintf(&s, "- %s: %s\n", t.Name, t.Description)
	}

	var u strings.Builder
	fmt.Fprintf(&u, "Scope: %s\n\nNotes:\n", in.Scope)
	for i, n := range in.Notes {
		fmt.Fprintf(&u, "%d. %s\n", i+1, n.Text)
	}
	u.WriteString("\nCandidate pages in this scope:\n")
	if len(in.Candidates) == 0 {
		u.WriteString("(none: every durable note needs a new page)\n")
	}
	for _, c := range in.Candidates {
		fmt.Fprintf(&u, "\n- slug: %s\n  type: %s\n  title: %s\n", c.Slug, c.Type, c.Title)
		for _, claim := range c.Claims {
			fmt.Fprintf(&u, "  claim: %s\n", claim)
		}
	}
	return s.String(), u.String()
}
