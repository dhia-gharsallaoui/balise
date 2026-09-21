package vault

import "strings"

// ScopeOf returns the top-level directory, which 04 section 14 makes the security boundary.
func ScopeOf(path string) string {
	parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[0]
}

// IndexPath is where a scope's generated index lives.
func IndexPath(scope string) string { return scope + "/index.md" }

// reservedTopLevel is the closed set of vault-root entries that 04-technical-spec-v1.md
// section 3.1 reserves for system use rather than scope content: `.balise/` (registry —
// types, facets, prompts, some of which are themselves .md files) and `review/` (pending
// proposals, which describe a change to a page but are not pages themselves). Named
// explicitly, and kept to exactly what section 3.1 documents, rather than guessed by
// pattern (path shape, filename shape, or anything else that "looks like" system content):
// a proposal's own filename is a raw ULID, which is not a pattern anything else in the
// vault is guaranteed to avoid, so matching on shape would be both fragile and wrong. Every
// other top-level directory — including a scope no defaults/ file mentions yet, and a
// future top-level content area such as a `memory/` rollup, which would hold real pages
// needing to be indexed same as any other scope — is left alone by this check. That is
// deliberate: the set below only ever grows when section 3.1 itself reserves something new,
// not when a new scope or content area is onboarded.
var reservedTopLevel = map[string]bool{".balise": true, "review": true}

// IsReservedPath reports whether p, a vault-relative slash-separated path, falls under one
// of reservedTopLevel's entries rather than under a scope directory.
func IsReservedPath(p string) bool {
	top := strings.SplitN(strings.TrimPrefix(p, "/"), "/", 2)[0]
	return reservedTopLevel[top]
}
