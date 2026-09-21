package vault

// ValidateSegment is the single boundary check every caller-supplied value destined to
// become exactly one path component -- an agent name, a scope -- must pass before it is
// trusted anywhere in the tree. It lives here, in internal/vault, rather than in
// internal/mcp (where it originated as an unexported check backing rememberPath) or in
// internal/store (which now also needs it, for CreateToken): internal/store must not
// import internal/mcp, since mcp already depends on store and store depending on mcp too
// would invert that relationship. internal/vault already owns identity, slug, and path
// concerns (SlugFromFilename, Normalise, ScopeOf), both internal/mcp and internal/store
// already import it, and it imports neither of them -- so it is the natural, cycle-free
// home for one validator shared by both call sites. Do not duplicate this logic at either
// call site: a second copy would drift from this one, silently, which is exactly how a
// boundary check stops matching the thing it guards.

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// ErrInvalidSegment is returned when a caller-supplied value is not safe to use as a single
// path component. Hostile input -- "../", an absolute path, a null byte, a Unicode
// line/paragraph separator, a whitespace-only string -- is rejected outright, never
// sanitised-and-hoped.
var ErrInvalidSegment = errors.New("invalid path segment")

// ValidateSegment reports whether s is safe to use as exactly one path component:
// non-empty, not made up only of whitespace, containing no path separator ("/" or "\"),
// not "." or "..", and free of control characters (which catches a null byte among others)
// and Unicode line/paragraph separators (U+2028, U+2029) that some filesystems or tools
// treat specially.
//
// This is deliberately a rejection list, not a sanitiser: ValidateSegment never rewrites s,
// it only ever accepts it unchanged or refuses it.
func ValidateSegment(s string) error {
	if s == "" {
		return fmt.Errorf("%w: empty", ErrInvalidSegment)
	}
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("%w: %q is whitespace only", ErrInvalidSegment, s)
	}
	if strings.ContainsAny(s, "/\\") {
		return fmt.Errorf("%w: %q contains a path separator", ErrInvalidSegment, s)
	}
	if s == "." || s == ".." {
		return fmt.Errorf("%w: %q is a relative path component", ErrInvalidSegment, s)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: %q contains a control character", ErrInvalidSegment, s)
		}
		if unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return fmt.Errorf("%w: %q contains a unicode line/paragraph separator", ErrInvalidSegment, s)
		}
	}
	return nil
}
