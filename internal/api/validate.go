package api

// Shared server-side validation for the two new write surfaces (agents.go's create-agent
// handler, scopes.go's create-scope handler). Both mint a name that ends up as exactly one
// path component elsewhere in the system — an agent name inside "<scope>/memory/<name>/"
// (internal/mcp's rememberPath), a scope name as a vault top-level directory and a
// defaults/scopes.yaml entry — so both go through vault.ValidateSegment, the one shared
// validator store.CreateToken and registry.AppendDeclaredScope already use. Never write a
// second copy of that rejection list here.

import (
	"fmt"
	"unicode/utf8"

	"github.com/dhia/balise/internal/vault"
)

// maxSegmentNameLength bounds how long an owner-supplied name may be before this API refuses
// it. vault.ValidateSegment deliberately carries no length limit of its own (see its doc
// comment) — it only rejects shapes that could never be a safe path component, not length —
// so a value that passes every check in that rejection list but runs to thousands of
// characters would still be accepted by it. That is not a safe thing to store in
// agent_tokens.scopes, write into defaults/scopes.yaml, or render on a screen indefinitely,
// so this backstops it locally. 100 runes is generous next to every real name already in this
// vault (the longest scope folder name is under 20 characters) while still catching a pasted
// paragraph or a deliberately oversized value.
const maxSegmentNameLength = 100

// validateSegmentName checks name against this package's own length cap and then against
// vault.ValidateSegment (checking length first avoids running ValidateSegment's rune-by-rune
// scan over an absurdly long string before rejecting it outright). field names which input
// failed ("agent name", "space name") for the 400 body. It never rewrites name, matching
// ValidateSegment's own reject-don't-sanitise contract — ValidateSegment is authoritative on
// shape, this is authoritative on length only.
func validateSegmentName(field, name string) error {
	if n := utf8.RuneCountInString(name); n > maxSegmentNameLength {
		return fmt.Errorf("%s: %d characters exceeds the %d character limit", field, n, maxSegmentNameLength)
	}
	if err := vault.ValidateSegment(name); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	return nil
}
