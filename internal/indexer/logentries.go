package indexer

import (
	"fmt"

	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/vault"
)

// logEntryClaims turns each entry of a log-trait page (an agent's daily memory rollup) into
// one claim. Without this, remembered notes were write-only: search, context and the
// semantic arm all read the claims table, and a memory page has no frontmatter claims, so
// nothing an agent remembered could ever be found again. IDs ("e1", "e2", ...) and ords
// continue after the page's own frontmatter claims, and are stable because a log is
// append-only.
func logEntryClaims(body string, firstOrd int) []store.Claim {
	entries := vault.LogEntries(body)
	claims := make([]store.Claim, len(entries))
	for i, e := range entries {
		claims[i] = store.Claim{ClaimID: fmt.Sprintf("e%d", i+1), Ord: firstOrd + i, Text: e.Text, Status: "active"}
	}
	return claims
}
