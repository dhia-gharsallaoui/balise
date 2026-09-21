package api

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/dhia/balise/internal/compile"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/vault"
)

// homeRecentCommitLimit is "the last ~15 commits touching pages" — 02-ui-design-v1.md
// section 5.1's "Changed recently" block.
const homeRecentCommitLimit = 15

// homeProposalJSON is one review/<id>.md proposal, as read directly off the vault rather
// than a database row — proposals are deliberately not given a table (section 5.1 says so
// explicitly), so /api/home is the one handler that reads the filesystem through PageStore
// instead of going through Queries for this one block.
type homeProposalJSON struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"`
	Scope      string  `json:"scope"`
	Target     string  `json:"target"`
	Confidence float64 `json:"confidence"`
	CreatedBy  string  `json:"created_by"`
	Status     string  `json:"status"`
}

type homeKindCountJSON struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// homeWaitingJSON is "Waiting for you": the proposal queue, grouped by kind, plus the raw
// list Review's own minimal screen reuses so the frontend fetches this data exactly once.
type homeWaitingJSON struct {
	Total     int                 `json:"total"`
	ByKind    []homeKindCountJSON `json:"by_kind"`
	Proposals []homeProposalJSON  `json:"proposals"`
}

// homePageRefJSON identifies a page well enough for the frontend to open it or to filter a
// list down to it, without carrying the whole page.
type homePageRefJSON struct {
	Scope string `json:"scope"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// homeAttentionJSON is one lint rule's findings, rendered as a plain sentence rather than a
// count next to a rule name — "no numbers larger than the body text" (section 5.1) rules out
// a tile or a badge here. Pages is what a click on the sentence filters the page list to.
type homeAttentionJSON struct {
	Rule     string            `json:"rule"`
	Sentence string            `json:"sentence"`
	Count    int               `json:"count"`
	Pages    []homePageRefJSON `json:"pages"`
}

// homeChangeJSON is one commit touching a page, with its author resolved to the plain word
// section 5.1 asks for ("you", "a connector", "an agent") rather than a raw git identity.
type homeChangeJSON struct {
	SHA        string `json:"sha"`
	Path       string `json:"path"`
	Message    string `json:"message"`
	AuthorWord string `json:"author_word"`
	When       string `json:"when"`
	// Scope, Slug and Title identify the page this commit touched, resolved via
	// store.Queries.GetPage, so a "Changed recently" row can navigate to the real page. All
	// three are left empty together — never partially filled — when the commit's path does
	// not resolve to an indexed document (confirmed live: a memory-import commit touches
	// work/memory/claude-code/<date>.md, a real tracked file, but GetPage has never heard of
	// it because the importer never indexes it as a page). The frontend treats an empty Slug
	// as "not navigable" rather than guessing a link that would 404.
	Scope string `json:"scope,omitempty"`
	Slug  string `json:"slug,omitempty"`
	Title string `json:"title,omitempty"`
}

func (s *server) handleHome(w http.ResponseWriter, r *http.Request) {
	waiting, err := s.homeWaiting()
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	attention, err := s.homeAttention(r.Context())
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	changes, err := s.homeChanges(r.Context())
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	owner, err := s.homeOwner(r.Context())
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"waiting": waiting, "attention": attention, "changes": changes, "owner": owner,
	})
}

// homeOwner names the vault's greeting recipient: the most common non-empty Owner across every
// indexed page (today that is "dhia" on all 126 live pages, but it is computed rather than a
// literal in the frontend so a vault with different or mixed ownership gets an honest answer
// instead of a name that happens to be right by coincidence). The count and the alphabetical
// tie-break both happen in SQL now (store.Queries.ModalOwner's `group by owner`) rather than by
// fetching every page's row — claims JSON and all — through ListPages just to throw the rest of
// it away in a Go-side count.
func (s *server) homeOwner(ctx context.Context) (string, error) {
	owner, err := s.q().ModalOwner(ctx, true)
	if err != nil {
		return "", fmt.Errorf("modal owner: %w", err)
	}
	return owner, nil
}

// homeWaiting reads every review/*.md proposal the caller's scopes may see and groups it by
// kind. Proposals carry their own scope in frontmatter and are not filtered by any SQL — this
// is the one read in the whole handler that bypasses Queries entirely, so the scope check
// here (via s.q().AllowedScopes) is the only thing standing between a scoped caller and
// another tenant's proposals.
func (s *server) homeWaiting() (homeWaitingJSON, error) {
	empty := homeWaitingJSON{ByKind: []homeKindCountJSON{}, Proposals: []homeProposalJSON{}}
	if s.pages == nil {
		return empty, nil
	}
	paths, err := s.pages.List("review/")
	if err != nil {
		return homeWaitingJSON{}, fmt.Errorf("list proposals: %w", err)
	}
	allowed := s.q().AllowedScopes()

	counts := map[string]int{}
	var proposals []homeProposalJSON
	for _, p := range paths {
		if !strings.HasSuffix(p, ".md") {
			continue
		}
		proposal, ok, err := readProposal(s.pages, p)
		if err != nil {
			return homeWaitingJSON{}, err
		}
		if !ok || !scopeAllowed(allowed, proposal.Scope) || proposal.Status != "pending" {
			continue
		}
		counts[proposal.Kind]++
		proposals = append(proposals, homeProposalJSON{
			ID: proposal.ID, Kind: proposal.Kind, Scope: proposal.Scope, Target: proposal.Target,
			Confidence: proposal.Confidence, CreatedBy: proposal.CreatedBy, Status: proposal.Status,
		})
	}
	sort.Slice(proposals, func(i, j int) bool { return proposals[i].ID < proposals[j].ID })

	kinds := make([]string, 0, len(counts))
	for kind := range counts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	byKind := make([]homeKindCountJSON, 0, len(kinds))
	for _, kind := range kinds {
		byKind = append(byKind, homeKindCountJSON{Kind: kind, Count: counts[kind]})
	}

	return homeWaitingJSON{Total: len(proposals), ByKind: byKind, Proposals: proposals}, nil
}

// readProposal parses one review/*.md file into a compile.Proposal, reusing vault.Parse's
// frontmatter splitting rather than writing a second YAML-splitting implementation. A file
// with no parseable frontmatter is reported as ok=false, not an error: a stray non-proposal
// file under review/ should not take Home down.
func readProposal(pages store.PageStore, p string) (compile.Proposal, bool, error) {
	raw, _, err := pages.Read(p)
	if err != nil {
		return compile.Proposal{}, false, fmt.Errorf("read %s: %w", p, err)
	}
	parsed, err := vault.Parse(string(raw))
	if err != nil || parsed.Meta == nil {
		return compile.Proposal{}, false, nil
	}
	var proposal compile.Proposal
	if err := parsed.Decode(&proposal); err != nil {
		return compile.Proposal{}, false, nil
	}
	return proposal, true, nil
}

// scopeAllowed reports whether scope is one of scopes. store.Scopes.allows does the same
// check but is unexported, so this package (which only ever sees the defensive copy
// AllowedScopes returns) keeps its own tiny copy rather than reaching into store internals.
func scopeAllowed(scopes store.Scopes, scope string) bool {
	for _, allowed := range scopes {
		if allowed == scope {
			return true
		}
	}
	return false
}

// homeAttention aggregates unresolved lint findings by rule and renders each as a sentence.
// Three filters stand between a raw lint_findings row and a sentence a person reads here,
// each one there because without it Home said something untrue:
//
//   - A suggest-severity finding is dropped outright. resolved_ref is the only rule at that
//     severity — bookkeeping for Task 15's link-recovery metric, written for every link that
//     already resolved cleanly — and showing "N links were resolved fine" under "Needs
//     attention" is not attention anything needs.
//   - A dangling_ref finding whose target exists under a different scope is dropped too:
//     vault.Resolve refuses to cross a scope boundary by design (04 section 14), so that
//     "dangling" ref is the security model working correctly, not a broken link the owner
//     can fix by editing this vault. index.CrossScope (built from SlugScopeIndex, the same
//     classification cli.Reindex applies live) is what tells the two apart.
//   - unparseable — the one finding with no document to attach to, since a page that failed
//     to parse never got a document row — is merged in from GlobalFindings. Without this,
//     an unparseable page was invisible everywhere Home looks: FindingsByRule joins against
//     documents, so a global finding can never appear in it no matter how badly a page fails.
func (s *server) homeAttention(ctx context.Context) ([]homeAttentionJSON, error) {
	found, err := s.q().FindingsByRule(ctx)
	if err != nil {
		return nil, fmt.Errorf("findings by rule: %w", err)
	}
	globals, err := s.q().GlobalFindings(ctx)
	if err != nil {
		return nil, fmt.Errorf("global findings: %w", err)
	}
	index, err := s.q().SlugScopeIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("slug scope index: %w", err)
	}

	var order []string
	seenRule := map[string]bool{}
	addRule := func(rule string) {
		if !seenRule[rule] {
			seenRule[rule] = true
			order = append(order, rule)
		}
	}

	byRule := map[string][]store.RuleFinding{}
	for _, rf := range found {
		if rf.Severity == "suggest" {
			continue
		}
		if rf.Rule == "dangling_ref" && index.CrossScope(rf.Detail, rf.Scope) {
			continue
		}
		addRule(rf.Rule)
		byRule[rf.Rule] = append(byRule[rf.Rule], rf)
	}

	globalCounts := map[string]int{}
	for _, finding := range globals {
		if finding.Severity == "suggest" {
			continue
		}
		addRule(finding.Rule)
		globalCounts[finding.Rule]++
	}
	sort.Strings(order)

	out := make([]homeAttentionJSON, 0, len(order))
	for _, rule := range order {
		rows := byRule[rule]
		count := len(rows) + globalCounts[rule]
		out = append(out, homeAttentionJSON{
			Rule: rule, Sentence: sentenceFor(rule, count), Count: count,
			Pages: attentionPageRefs(rows),
		})
	}
	return out, nil
}

// attentionPageRefs lists the distinct pages a rule's findings named, in the order
// FindingsByRule already returns (by scope then slug), so a rule firing more than once on
// the same page (dangling_ref, once per broken link) does not list that page twice in the
// "linking to a filtered list" destination.
func attentionPageRefs(rows []store.RuleFinding) []homePageRefJSON {
	seen := map[string]bool{}
	out := make([]homePageRefJSON, 0, len(rows))
	for _, rf := range rows {
		key := rf.Scope + "/" + rf.Slug
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, homePageRefJSON{Scope: rf.Scope, Slug: rf.Slug, Title: rf.Title})
	}
	return out
}

// ruleSentence gives every rule the indexer can emit (internal/indexer/pipeline.go) its own
// plain-English sentence, matching 02-ui-design-v1.md section 5.1's style ("3 pages not
// verified in 90 days") rather than a rule name next to a count. resolved_ref is the one
// deliberate omission: it is suggest-severity bookkeeping for Task 15's link-recovery metric,
// filtered out of homeAttention before sentenceFor is ever consulted, so it never needs an
// entry here. A genuinely new, unanticipated rule still gets a grammatical generic sentence
// from sentenceFor rather than being dropped from the block or panicking.
var ruleSentence = map[string]struct{ singular, plural string }{
	"oversize":       {"page is too long to fit an agent's context", "pages are too long to fit an agent's context"},
	"dangling_ref":   {"link points at a page that does not exist", "links point at pages that do not exist"},
	"long_headline":  {"headline is longer than 15 words", "headlines are longer than 15 words"},
	"missing_claims": {"page has no claims yet", "pages have no claims yet"},
	"missing_classification": {
		"page was imported without a classification and needs review",
		"pages were imported without a classification and need review",
	},
	"multi_customer": {
		"page mentions more than one customer and should be split",
		"pages mention more than one customer and should be split",
	},
	"out_of_vocabulary": {"tag is not in the known vocabulary", "tags are not in the known vocabulary"},
	"too_many_claims": {
		"page has more claims than its type allows",
		"pages have more claims than their type allows",
	},
	"invalid_date": {"claim has a date that could not be parsed", "claims have dates that could not be parsed"},
	"unparseable":  {"page could not be parsed", "pages could not be parsed"},
}

func sentenceFor(rule string, count int) string {
	phrase, ok := ruleSentence[rule]
	if !ok {
		return fmt.Sprintf("%d unresolved %s finding%s", count, rule, pluralSuffix(count))
	}
	if count == 1 {
		return fmt.Sprintf("%d %s", count, phrase.singular)
	}
	return fmt.Sprintf("%d %s", count, phrase.plural)
}

func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

// pageCommit pairs a store.Commit with the page path homeChanges was walking when it found
// it. store.Commit itself carries no path — History is scoped to the single path it was
// called with — so this is the merge step's own bookkeeping, not part of the store package's
// contract.
type pageCommit struct {
	store.Commit
	Path string
}

// homeChanges merges PageStore.History across every real content page into one
// newest-first, deduplicated-by-SHA list — History itself only ever answers for one path at
// a time. Any commit within the true global top-N most recent commits must appear within
// every individual touched page's own top-N history (a commit cannot be newer than itself),
// so calling History(p, homeRecentCommitLimit) per page is sufficient to reconstruct the
// global top homeRecentCommitLimit without either over- or under-fetching.
func (s *server) homeChanges(ctx context.Context) ([]homeChangeJSON, error) {
	if s.pages == nil {
		return []homeChangeJSON{}, nil
	}
	paths, err := s.pages.List("")
	if err != nil {
		return nil, fmt.Errorf("list pages: %w", err)
	}
	allowed := s.q().AllowedScopes()

	byHash := map[string]pageCommit{}
	for _, p := range paths {
		if !isRealPage(p) || !scopeAllowed(allowed, vault.ScopeOf(p)) {
			continue
		}
		history, err := s.pages.History(p, homeRecentCommitLimit)
		if err != nil {
			return nil, fmt.Errorf("history for %s: %w", p, err)
		}
		for _, commit := range history {
			if _, exists := byHash[commit.SHA]; !exists {
				byHash[commit.SHA] = pageCommit{Commit: commit, Path: p}
			}
		}
	}

	merged := make([]pageCommit, 0, len(byHash))
	for _, commit := range byHash {
		merged = append(merged, commit)
	}
	sort.Slice(merged, func(i, j int) bool {
		if !merged[i].When.Equal(merged[j].When) {
			return merged[i].When.After(merged[j].When)
		}
		return merged[i].SHA < merged[j].SHA
	})
	if len(merged) > homeRecentCommitLimit {
		merged = merged[:homeRecentCommitLimit]
	}

	out := make([]homeChangeJSON, 0, len(merged))
	for _, commit := range merged {
		change := homeChangeJSON{
			SHA: commit.SHA, Path: commit.Path, Message: commit.Message,
			AuthorWord: authorWord(commit.Message), When: commit.When.UTC().Format(time.RFC3339),
		}
		ref, ok, err := s.resolveChangeRef(ctx, commit.Path)
		if err != nil {
			return nil, err
		}
		if ok {
			change.Scope, change.Slug, change.Title = ref.Scope, ref.Slug, ref.Title
		}
		out = append(out, change)
	}
	return out, nil
}

// resolveChangeRef resolves a commit's path to the page it belongs to, via the same
// (scope, slug) derivation vault.Resolve itself uses, so "Changed recently" rows can navigate
// there. It reports ok=false — never a partial ref — for any path GetPage does not recognise
// as a document: a scope/slug pair with nothing behind it is a broken link, not a degraded
// one. A real query failure is still propagated rather than swallowed as "not found".
func (s *server) resolveChangeRef(ctx context.Context, p string) (homePageRefJSON, bool, error) {
	scope := vault.ScopeOf(p)
	if scope == "" {
		return homePageRefJSON{}, false, nil
	}
	slug := vault.SlugFromFilename(path.Base(p))
	row, err := s.q().GetPage(ctx, scope, slug)
	if err != nil {
		return homePageRefJSON{}, false, fmt.Errorf("resolve change ref for %s: %w", p, err)
	}
	if row == nil {
		return homePageRefJSON{}, false, nil
	}
	return homePageRefJSON{Scope: scope, Slug: slug, Title: row.Title}, true, nil
}

// isRealPage reports whether p is a real content page rather than a proposal, vault
// registry file, or index file. vault.IsReservedPath carries the one authoritative list of
// non-scope top-level entries (04 section 3.1); this and internal/compile's own
// isEligiblePath (run.go) both build on it rather than each keeping its own copy of which
// prefixes are reserved.
func isRealPage(p string) bool {
	return strings.HasSuffix(p, ".md") && path.Base(p) != "index.md" && !vault.IsReservedPath(p)
}

// authorWord classifies a commit by its message prefix rather than its git author identity:
// both the importer (internal/importer/claudememory.go) and compile (internal/compile/run.go)
// commit as the identical author "balise <balise@localhost>", so the message each one writes
// — "balise: import claude-code memory" vs. "compile: extract_claims" — is the only thing
// that actually distinguishes them. Anything else falls back to "you": no production code
// path calls PageStore.Write's generic "update <path>" today, so the only real commits that
// reach this default are the ones this repo does not yet generate — a human editing a page
// directly outside balise (Obsidian, vim), which is exactly who "you" means.
func authorWord(message string) string {
	switch {
	case strings.HasPrefix(message, "compile:"):
		return "an agent"
	case strings.HasPrefix(message, "balise:"):
		return "a connector"
	default:
		return "you"
	}
}
