package indexer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dhia/balise/internal/registry"
)

// IndexEntry is one line of a generated index.md.
type IndexEntry struct {
	Title      string
	Slug       string
	Type       string
	Historical bool
}

// RenderIndex writes the per-scope index: active pages grouped by type in agent order,
// historical last under their own heading. The order is passed in rather than hardcoded,
// so this file names no type — see internal/guard/neutral_test.go.
func RenderIndex(pages []IndexEntry, order *registry.Order) string {
	var sb strings.Builder
	sb.WriteString("# Index\n\n")

	var live, past []IndexEntry
	for _, page := range pages {
		if page.Historical {
			past = append(past, page)
		} else {
			live = append(live, page)
		}
	}

	seen := map[string]bool{}
	var types []string
	for _, page := range live {
		if !seen[page.Type] {
			seen[page.Type] = true
			types = append(types, page.Type)
		}
	}
	sort.SliceStable(types, func(i, j int) bool { return order.Rank(types[i]) < order.Rank(types[j]) })

	for _, typeName := range types {
		fmt.Fprintf(&sb, "## %s\n\n", typeName)
		for _, page := range live {
			if page.Type == typeName {
				sb.WriteString(line(page))
			}
		}
		sb.WriteString("\n")
	}
	if len(past) > 0 {
		sb.WriteString("## Historical\n\n")
		for _, page := range past {
			sb.WriteString(line(page))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func line(page IndexEntry) string {
	return fmt.Sprintf("- %s → %s [%s]\n", page.Title, page.Slug, page.Type)
}
