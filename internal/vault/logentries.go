package vault

import (
	"regexp"
	"strings"
)

// logEntryStart matches the first line of one entry exactly as internal/mcp/remember.go's
// formatMemoryEntry writes it: "- **15:04:05 UTC** text". The timestamp is markup, not part
// of what was remembered, so it is captured separately from the text.
var logEntryStart = regexp.MustCompile(`^- \*\*(\d{2}:\d{2}:\d{2}) UTC\*\* (.*)$`)

// LogEntry is one remembered note from a log-trait page: its UTC time of day and its text.
type LogEntry struct {
	Time string
	Text string
}

// LogEntries parses a log-trait page body (an agent's daily memory rollup) into its entries,
// in order. remember appends text verbatim, so an entry may run over several lines; every
// following line up to the next entry belongs to it and is joined with a single space. Lines
// before the first entry, and entries with no text, are dropped. Shared by the indexer (which
// makes each entry a searchable claim) and compile's propose_pages (which proposes where each
// entry belongs), so both read a log exactly the same way.
func LogEntries(body string) []LogEntry {
	var entries []LogEntry
	for _, line := range strings.Split(body, "\n") {
		if m := logEntryStart.FindStringSubmatch(line); m != nil {
			entries = append(entries, LogEntry{Time: m[1], Text: strings.TrimSpace(m[2])})
			continue
		}
		cont := strings.TrimSpace(line)
		if cont == "" || len(entries) == 0 {
			continue
		}
		last := &entries[len(entries)-1]
		last.Text = strings.TrimSpace(last.Text + " " + cont)
	}
	out := entries[:0]
	for _, e := range entries {
		if e.Text != "" {
			out = append(out, e)
		}
	}
	return out
}
