package indexer

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/dhia/balise/internal/store"
)

// logEntryStart matches the first line of one entry exactly as internal/mcp/remember.go's
// formatMemoryEntry writes it: "- **15:04:05 UTC** text". The timestamp is markup, not part
// of what was remembered, so only the text after it is captured.
var logEntryStart = regexp.MustCompile(`^- \*\*\d{2}:\d{2}:\d{2} UTC\*\* (.*)$`)

// logEntryClaims turns each entry of a log-trait page (an agent's daily memory rollup) into
// one claim. Without this, remembered notes were write-only: search, context and the
// semantic arm all read the claims table, and a memory page has no frontmatter claims, so
// nothing an agent remembered could ever be found again.
//
// remember appends its text verbatim, so an entry may run over several lines; every
// following line up to the next entry belongs to it and is joined with a single space.
// Lines before the first entry are not part of any entry and are ignored. IDs ("e1", "e2",
// ...) and ords continue after the page's own frontmatter claims, and are stable because a
// log is append-only.
func logEntryClaims(body string, firstOrd int) []store.Claim {
	var entries []string
	for _, line := range strings.Split(body, "\n") {
		if m := logEntryStart.FindStringSubmatch(line); m != nil {
			entries = append(entries, strings.TrimSpace(m[1]))
			continue
		}
		cont := strings.TrimSpace(line)
		if cont == "" || len(entries) == 0 {
			continue
		}
		last := len(entries) - 1
		entries[last] = strings.TrimSpace(entries[last] + " " + cont)
	}

	claims := make([]store.Claim, 0, len(entries))
	for i, text := range entries {
		if text == "" {
			continue
		}
		claims = append(claims, store.Claim{
			ClaimID: fmt.Sprintf("e%d", i+1), Ord: firstOrd + len(claims), Text: text, Status: "active",
		})
	}
	return claims
}
