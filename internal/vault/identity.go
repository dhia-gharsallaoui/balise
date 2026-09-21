package vault

import (
	"crypto/rand"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// typePrefixes lists every type-prefix spelling a filename or wikilink target may carry.
// The real corpus writes wikilinks with a hyphenated prefix (`[[project-globex-onboarding]]`)
// while frontmatter aliases and filenames use the underscored form (`project_globex_onboarding`)
// — both must be recognised here, or the normalised rung of the resolution ladder silently
// fails to strip the prefix from one spelling while stripping it from the other, so two
// references to the same page normalise to different strings and the link reads as dangling.
var typePrefixes = []string{
	"project_", "feedback_", "reference_",
	"project-", "feedback-", "reference-",
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// entropy is a monotonic ULID source seeded from crypto/rand and guarded by a mutex
// (via LockedMonotonicReader), so concurrent NewUID calls never race on the underlying
// reader. uid is the documents primary key; an unsynchronised source could mint the
// same ULID twice under concurrent callers and silently merge two pages.
var entropy = &ulid.LockedMonotonicReader{MonotonicReader: ulid.Monotonic(rand.Reader, 0)}

// NewUID returns a fresh 26-character ULID, lexically sortable by creation time. It is
// safe to call concurrently.
func NewUID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()
}

// SlugFromFilename returns the filename stem as a slug, with a leading type prefix removed.
func SlugFromFilename(name string) string {
	stem := strings.TrimSuffix(name, ".md")
	lowered := strings.ToLower(stem)
	for _, prefix := range typePrefixes {
		if strings.HasPrefix(lowered, prefix) {
			stem = stem[len(prefix):]
			break
		}
	}
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(stem), "-"), "-")
}

// Normalise folds a reference to its comparable form for the resolution ladder.
func Normalise(ref string) string {
	return SlugFromFilename(strings.TrimSpace(ref))
}
