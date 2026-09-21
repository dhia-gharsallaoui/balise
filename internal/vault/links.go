package vault

import (
	"regexp"
	"sort"
	"strings"
)

// ScopedSlug keys the slug index. Scope is part of the key because resolution must
// never cross a scope boundary — 04 section 14 makes scope the security boundary.
type ScopedSlug struct {
	Scope string
	Slug  string
}

// Resolution is the outcome of walking the ladder. Rung says which rung matched, which
// is what the search "why" field reports and what makes link-repair measurable.
type Resolution struct {
	UID  string
	Rung string // slug | alias | normalised | dangling
}

var (
	wikilink = regexp.MustCompile(`\[\[([^\]|#]+)(?:[#|][^\]]*)?\]\]`)
	mdlink   = regexp.MustCompile(`\[[^\]]*\]\((?:\.{0,2}/)?([^):]+\.md)\)`)
)

// Resolve walks the four rungs in order and stops at the first hit. The normalised
// rung checks both the slug and alias indexes — 04 section 4 defines it as "normalised
// ... against slug and aliases" — since a filename-derived reference like
// "project_x.md" normalises to a bare slug, not to an alias.
func Resolve(ref, scope string, slugs map[ScopedSlug]string, reg AliasRegistry) Resolution {
	if uid, ok := slugs[ScopedSlug{Scope: scope, Slug: ref}]; ok {
		return Resolution{UID: uid, Rung: "slug"}
	}
	if uid := reg.LookupExact(ref, scope); uid != "" {
		return Resolution{UID: uid, Rung: "alias"}
	}
	// Slugs are checked before aliases at this rung: a page's own slug must outrank a
	// different page's nickname when both normalise to the same string.
	if uid, ok := slugs[ScopedSlug{Scope: scope, Slug: Normalise(ref)}]; ok {
		return Resolution{UID: uid, Rung: "normalised"}
	}
	if uid := reg.LookupNormalised(ref, scope); uid != "" {
		return Resolution{UID: uid, Rung: "normalised"}
	}
	return Resolution{Rung: "dangling"}
}

// ExtractLinks returns wikilink targets and relative markdown links in document order.
// Duplicates are kept; the caller decides whether to dedupe.
func ExtractLinks(body string) []string {
	type hit struct {
		at  int
		ref string
	}
	var hits []hit
	for _, m := range wikilink.FindAllStringSubmatchIndex(body, -1) {
		hits = append(hits, hit{m[0], strings.TrimSpace(body[m[2]:m[3]])})
	}
	for _, m := range mdlink.FindAllStringSubmatchIndex(body, -1) {
		hits = append(hits, hit{m[0], strings.TrimSpace(body[m[2]:m[3]])})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].at < hits[j].at })

	var out []string
	for _, h := range hits {
		out = append(out, h.ref)
	}
	return out
}
