// Package importer converts Claude Code memory pages into Balise pages.
//
// Deterministic; no LLM. Only one hook per page exists in the validation artifacts, so
// every imported page carries at most one claim — spec section 4. extract_claims fills
// the rest at M3, in Python, per spec section 3.1.
package importer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/vault"
	"gopkg.in/yaml.v3"
)

// clientScopes optionally overrides the scope name a declared tenant (defaults/tenants.yaml)
// gets; a tenant absent from this map still gets a scope, derived as "client-<name>" — see
// scopeForTenant. Today every entry here happens to equal that same default (globex -> client-globex),
// so this map exists purely as a hook for the owner to rename a tenant's scope later without
// touching code, not because any current tenant needs a different name than its default.
//
// Whether a customer/* facet names a tenant at all is no longer decided here or by any
// hardcoded list in this package: it is decided by defaults/tenants.yaml (loaded via
// registry.LoadTenants and threaded in as scopeFor's tenants argument), because that decision
// is the actual security partition and the owner must be able to correct it without a
// recompile. colo is the clearest example of why this had to move out of Go code: it is the
// shared datacenter that Globex's and Acme's infrastructure sits inside, not a tenant of its
// own, and is deliberately absent from defaults/tenants.yaml. Less obviously, a customer/*
// facet paired with a real tenant does not make a page multi-customer just because it is
// present — a page tagged customer/globex + customer/colo is Globex's kit sitting in the datacenter,
// one tenant in one location, and must resolve as ordinary single-tenant Globex. Getting this
// wrong is exactly how 01-design-spec-v0.4.md section 11's "6 multi-customer pages" figure
// was itself measured with colo counted as a customer, and inherited that modelling error.
var clientScopes = map[string]string{"globex": "client-globex"}

const defaultScope = "work"

var typeFolders = map[string]string{
	"gotcha": "gotchas", "decision": "decisions", "state": "state", "procedure": "procedures",
	"incident": "incidents", "issue": "issues", "note": "notes",
}

// Classification is one Pass A row.
type Classification struct {
	File     string   `json:"file"`
	Kind     string   `json:"kind"`
	Status   string   `json:"status"`
	Facets   []string `json:"facets"`
	Identity string   `json:"identity"`
}

// Artifacts holds everything the validation runs produced that the importer reuses.
type Artifacts struct {
	PassA      map[string]Classification
	Hooks      map[string]string
	AliasTable map[string]string
}

// Claim is one frontmatter claim as written.
type Claim struct {
	Text   string `yaml:"text"`
	Status string `yaml:"status"`
	ID     string `yaml:"id"`
}

// Meta is the Balise frontmatter the importer writes.
type Meta struct {
	UID   string `yaml:"uid"`
	Slug  string `yaml:"slug"`
	Type  string `yaml:"type"`
	Scope string `yaml:"scope"`
	Title string `yaml:"title"`
	// TitleSource records which rung of resolveTitle's ladder produced Title — name, hook,
	// description or slug — so a slug-shaped or duplicated-looking title is traceable to its
	// origin instead of a mystery. See resolveTitle.
	TitleSource string   `yaml:"title_source"`
	Aliases     []string `yaml:"aliases,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
	Claims      []Claim  `yaml:"claims"`
	Status      string   `yaml:"status"`
	Owner       string   `yaml:"owner"`
	Identity    string   `yaml:"identity,omitempty"`
	// SourceFile is the corpus filename this page was converted from, and is the page's
	// stable identity across re-imports. The slug is derived from it and the scope is not,
	// so a page whose tenant is corrected keeps the same source file while moving into a
	// different scope's directory — which is exactly what makes a move recognisable as a
	// move. Without it an import can only match a page it already holds by name, and a name
	// is shared by two different customers' pages the moment two scopes hold one slug.
	SourceFile          string `yaml:"source_file,omitempty"`
	OriginSessionID     string `yaml:"originSessionId,omitempty"`
	NeedsClassification bool   `yaml:"needs_classification,omitempty"`
	// NeedsSplit flags a page whose facets name more than one distinct customer. The
	// indexer turns this into a multi_customer finding, exactly as NeedsClassification
	// becomes missing_classification — a signal for a reviewer to actually split the page,
	// per balise-docs/04-technical-spec-v1.md section 14 ("never demote to work").
	NeedsSplit bool `yaml:"needs_split,omitempty"`
}

// Converted is one page ready to commit.
type Converted struct {
	Meta Meta
	Body string
	Path string
}

// Render returns the page as markdown, frontmatter first. Encoding failures are
// reported rather than silently dropped — same reasoning as vault.Page.Render: a page
// rendered without its frontmatter would carry no uid, slug, type or claims, which is
// silent data loss on the system of record. There is no lossy Rendered() string
// convenience here for exactly that reason.
func (c Converted) Render() (string, error) {
	return renderMeta(c.Meta, c.Body)
}

// WithUID returns a copy of c carrying uid, leaving c untouched. ImportCorpus uses it to keep
// a page's existing uid when that page is already in the vault: Convert mints a fresh ULID for
// every page it converts, which on a re-import would give every page a brand-new identity, so
// the previously-imported copy could never be recognised as the same page — and its git
// history would restart from nothing. The stable key is the source filename, which the slug is
// a pure function of (vault.SlugFromFilename) and which the importer refuses to let two source
// files share.
func (c Converted) WithUID(uid string) Converted {
	c.Meta.UID = uid
	return c
}

// Report summarises an import run. Removed counts the vault paths this run superseded — a
// page whose scope or type folder changed, so its previous file was staged for deletion in
// the same commit that wrote its replacement.
type Report struct {
	Pages         int
	Classified    int
	Unclassified  int
	Claims        int
	MultiCustomer int
	Removed       int
	Commit        string
}

// LoadArtifacts reads Pass A, the hooks and the alias table from factory-validation/out.
func LoadArtifacts(dir string) (*Artifacts, error) {
	artifacts := &Artifacts{
		PassA: map[string]Classification{}, Hooks: map[string]string{},
	}

	passA, err := os.Open(filepath.Join(dir, "02_pass_a.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("open pass A: %w", err)
	}
	defer passA.Close()
	scanner := bufio.NewScanner(passA)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row Classification
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("parse pass A: %w", err)
		}
		artifacts.PassA[row.File] = row
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read pass A: %w", err)
	}

	hookFiles, err := filepath.Glob(filepath.Join(dir, "3c_hooks", "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("glob hooks: %w", err)
	}
	sort.Strings(hookFiles)
	for _, hookPath := range hookFiles {
		body, err := os.ReadFile(hookPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", hookPath, err)
		}
		for _, line := range strings.Split(string(body), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var row struct {
				File string `json:"file"`
				Hook string `json:"hook"`
			}
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				return nil, fmt.Errorf("parse %s: %w", hookPath, err)
			}
			artifacts.Hooks[row.File] = row.Hook
		}
	}

	aliasBody, err := os.ReadFile(filepath.Join(dir, "01_alias_table.json"))
	if err != nil {
		return nil, fmt.Errorf("read alias table: %w", err)
	}
	if err := json.Unmarshal(aliasBody, &artifacts.AliasTable); err != nil {
		return nil, fmt.Errorf("parse alias table: %w", err)
	}
	return artifacts, nil
}

// Convert maps one corpus page onto Balise frontmatter. Mapping is spec section 6.
func Convert(filename, raw string, artifacts *Artifacts, tenants *registry.Tenants) (Converted, error) {
	source, err := vault.Parse(raw)
	if err != nil {
		return Converted{}, fmt.Errorf("parse %s: %w", filename, err)
	}

	classification, classified := artifacts.PassA[filename]
	if !classified {
		classification = Classification{Kind: "note", Status: "active"}
	}

	slug := vault.SlugFromFilename(filename)
	scope, multiCustomer := scopeFor(classification.Facets, tenants)

	name := nodeValue(source, "name", "")
	description := nodeValue(source, "description", "")
	hook, hasHook := artifacts.Hooks[filename]
	title, titleSource := resolveTitle(name, slug, hook, description)

	meta := Meta{
		UID: vault.NewUID(), Slug: slug, Type: classification.Kind, Scope: scope,
		Title: title, TitleSource: titleSource, Aliases: aliasesFor(filename, artifacts),
		Tags: classification.Facets, Claims: []Claim{}, Status: classification.Status,
		Owner: "dhia", Identity: classification.Identity, SourceFile: filename,
		OriginSessionID: nodeValue(source, "originSessionId", ""),
		// The indexer turns this into a missing_classification finding.
		NeedsClassification: !classified,
		// The indexer turns this into a multi_customer finding.
		NeedsSplit: multiCustomer,
	}
	if hasHook {
		meta.Claims = []Claim{{Text: hook, Status: classification.Status, ID: "c1"}}
	}

	body := source.Body
	if description != "" {
		body = strings.TrimSpace(description) + "\n\n" + body
	}

	folder, ok := typeFolders[classification.Kind]
	if !ok {
		folder = "notes"
	}
	return Converted{Meta: meta, Body: strings.TrimSpace(body) + "\n",
		Path: fmt.Sprintf("%s/%s/%s.md", scope, folder, slug)}, nil
}

// ImportCorpus converts every corpus page, stages the vault paths it supersedes for removal,
// and commits the lot as one change.
//
// Re-importing is the product's own correction workflow: edit defaults/tenants.yaml, run
// import again. That was unsafe until this function learned two things. It reads the vault's
// current contents first (readVaultIndex), so a page already present keeps its uid instead of
// being minted a new one, and it stages a removal for the path a page has moved away from, so
// correcting a customer misclassification moves that customer's page rather than leaving a
// second copy behind under the wrong tenant's scope.
//
// Both of those turn on recognising a page the vault already holds, and that match is never
// made on a name alone. A name is shared by two different customers' pages the moment two
// scopes hold one slug, and acting on a name match would then adopt one tenant's uid for
// another tenant's page and stage the first tenant's file for deletion — deleting a customer's
// page and transplanting its identity, reported as an ordinary "superseded path removed". The
// match runs on source_file (stable across a scope change) and falls back to a scope-qualified
// (scope, slug), and a removal is staged only when the prior file's own uid proves it is the
// same document. See vaultIndex and supersedes.
func ImportCorpus(
	corpusDir, artifactsDir string, pages store.PageStore, tenants *registry.Tenants,
) (Report, error) {
	artifacts, err := LoadArtifacts(artifactsDir)
	if err != nil {
		return Report{}, err
	}
	entries, err := filepath.Glob(filepath.Join(corpusDir, "*.md"))
	if err != nil {
		return Report{}, fmt.Errorf("glob corpus: %w", err)
	}
	sort.Strings(entries)

	existing, err := readVaultIndex(pages)
	if err != nil {
		return Report{}, err
	}
	plans, report, err := convertAll(entries, artifacts, tenants, existing)
	if err != nil {
		return Report{}, err
	}
	changes, removed, err := stageChanges(plans)
	if err != nil {
		return Report{}, err
	}

	sha, err := pages.Commit(changes, "balise <balise@localhost>",
		"balise: import claude-code memory")
	if err != nil {
		return Report{}, fmt.Errorf("commit import: %w", err)
	}
	report.Pages, report.Removed, report.Commit = len(plans), removed, sha
	return report, nil
}

// vaultIdentity is what the vault already holds for one page: where it lives, and the uid it
// carries.
type vaultIdentity struct{ path, uid string }

// scopedSlug is a page's identity inside the vault — the same pair the documents table is
// uniquely keyed on. Indexing the vault by slug alone is the defect this whole wave exists to
// remove, just relocated into the importer: for a vault holding one slug in two scopes,
// last-walked would win.
type scopedSlug struct{ scope, slug string }

// vaultIndex is the vault's current contents, indexed the two ways an import needs to look a
// page up.
//
// bySource is the real key. A page's source filename survives a change of scope, which is
// precisely what a tenant correction is, so it is the only key that can tell a genuine move
// from two different pages that happen to share a name.
//
// byScopeSlug is the fallback for a page written before source_file existed, or written by
// hand. It is scope-qualified, so it can only match a page landing back in the scope it is
// already in — it can never carry a uid, or a deletion, across a tenant boundary. A page that
// predates source_file *and* is moving scope therefore matches nothing: the import writes it
// to its new home and leaves the old file in place, which shows up as a stale extra file
// rather than as a deleted tenant page. That is the safe direction to fail, and it self-heals,
// since this import stamps source_file onto every page it writes.
type vaultIndex struct {
	bySource    map[string]vaultIdentity
	byScopeSlug map[scopedSlug]vaultIdentity
}

// find returns the vault's copy of the page this import is about to write, or false when the
// vault holds nothing this page supersedes.
func (v vaultIndex) find(sourceFile, scope, slug string) (vaultIdentity, bool) {
	if sourceFile != "" {
		if found, ok := v.bySource[sourceFile]; ok {
			return found, true
		}
	}
	found, ok := v.byScopeSlug[scopedSlug{scope: scope, slug: slug}]
	return found, ok
}

// readVaultIndex walks the vault and indexes what it holds.
func readVaultIndex(pages store.PageStore) (vaultIndex, error) {
	paths, err := pages.List("")
	if err != nil {
		return vaultIndex{}, fmt.Errorf("list vault: %w", err)
	}
	found := vaultIndex{
		bySource:    make(map[string]vaultIdentity, len(paths)),
		byScopeSlug: make(map[scopedSlug]vaultIdentity, len(paths)),
	}
	for _, p := range paths {
		if !strings.HasSuffix(p, ".md") || path.Base(p) == "index.md" {
			continue
		}
		raw, _, err := pages.Read(p)
		if err != nil {
			return vaultIndex{}, fmt.Errorf("read %s: %w", p, err)
		}
		identity, slug, sourceFile := readPageIdentity(p, raw)
		if sourceFile != "" {
			found.bySource[sourceFile] = identity
		}
		found.byScopeSlug[scopedSlug{scope: vault.ScopeOf(p), slug: slug}] = identity
	}
	return found, nil
}

// readPageIdentity pulls one vault page's identity out of its frontmatter. A page that cannot
// be parsed, or that declares no slug, still yields a filename-derived slug and an empty uid:
// it is better to know a page exists at this path than to treat it as absent. An empty uid is
// never proof of anything, so such a page can be overwritten in place but never superseded —
// see supersedes.
func readPageIdentity(pagePath string, raw []byte) (identity vaultIdentity, slug, sourceFile string) {
	slug = vault.SlugFromFilename(path.Base(pagePath))
	uid := ""
	if page, err := vault.Parse(string(raw)); err == nil {
		var fm struct {
			UID        string `yaml:"uid"`
			Slug       string `yaml:"slug"`
			SourceFile string `yaml:"source_file"`
		}
		if page.Decode(&fm) == nil {
			if fm.Slug != "" {
				slug = fm.Slug
			}
			uid, sourceFile = fm.UID, fm.SourceFile
		}
	}
	return vaultIdentity{path: pagePath, uid: uid}, slug, sourceFile
}

// corpusSource is where one converted page came from, kept only so a slug collision can name
// both offending corpus files and both paths they would have been written to.
type corpusSource struct{ name, path string }

// plan is one converted page together with the vault page it supersedes, if any.
type plan struct {
	page  Converted
	prior vaultIdentity // zero when the vault holds nothing this page supersedes
}

// convertAll converts every corpus entry, refusing a slug two source files share and adopting
// the uid the vault already holds for the same page.
//
// The collision guard keys on the slug, not on the output path. Keyed on the path it only
// fired when both files also landed in the same scope, so project_expressroute.md and
// feedback_expressroute.md — which both slug to "expressroute" — passed straight through
// whenever their facets routed them to different tenants, and the vault ended up with two
// different customers' pages sharing one slug. A slug is a page's name within its scope and
// the only stable key back to a source filename, so two source files claiming one slug is an
// error whatever scopes they land in, not something to disambiguate later.
func convertAll(
	entries []string, artifacts *Artifacts, tenants *registry.Tenants, existing vaultIndex,
) ([]plan, Report, error) {
	seen := make(map[string]corpusSource, len(entries))
	out := make([]plan, 0, len(entries))
	report := Report{}
	for _, entry := range entries {
		name := filepath.Base(entry)
		// MEMORY.md is the owner's hand-maintained index; the vault generates its own.
		if name == "MEMORY.md" {
			continue
		}
		raw, err := os.ReadFile(entry)
		if err != nil {
			return nil, Report{}, fmt.Errorf("read %s: %w", entry, err)
		}
		converted, err := Convert(name, string(raw), artifacts, tenants)
		if err != nil {
			return nil, Report{}, err
		}
		slug := converted.Meta.Slug
		if previous, collides := seen[slug]; collides {
			return nil, Report{}, fmt.Errorf(
				"import corpus: %s (%s) and %s (%s) both carry slug %q",
				previous.name, previous.path, name, converted.Path, slug)
		}
		seen[slug] = corpusSource{name: name, path: converted.Path}

		prior, known := existing.find(name, converted.Meta.Scope, slug)
		if !known {
			prior = vaultIdentity{}
		} else if prior.uid != "" {
			converted = converted.WithUID(prior.uid)
		}
		countPage(&report, converted)
		out = append(out, plan{page: converted, prior: prior})
	}
	return out, report, nil
}

// countPage folds one converted page into the run's counters.
func countPage(report *Report, converted Converted) {
	if converted.Meta.NeedsClassification {
		report.Unclassified++
	} else {
		report.Classified++
	}
	if converted.Meta.NeedsSplit {
		report.MultiCustomer++
	}
	report.Claims += len(converted.Meta.Claims)
}

// stageChanges renders every converted page as a write and adds a removal for each vault path
// a page has provably moved away from, reporting how many removals it staged.
//
// A prior path some other page is about to be written to is skipped, so a write and a delete
// can never target the same path in one commit.
func stageChanges(plans []plan) ([]store.Change, int, error) {
	changes := make([]store.Change, 0, len(plans))
	written := make(map[string]bool, len(plans))
	for _, item := range plans {
		rendered, err := item.page.Render()
		if err != nil {
			return nil, 0, fmt.Errorf("render %s: %w", item.page.Path, err)
		}
		changes = append(changes, store.Change{Path: item.page.Path, Data: []byte(rendered)})
		written[item.page.Path] = true
	}

	removed := 0
	for _, item := range plans {
		if !supersedes(item) || written[item.prior.path] {
			continue
		}
		changes = append(changes, store.Change{Path: item.prior.path, Delete: true})
		removed++
	}
	return changes, removed, nil
}

// supersedes reports whether item's prior vault file is provably the same document, now living
// somewhere else — the only case in which a removal may be staged.
//
// The proof is uid equality, never a shared name. A name match alone is how an import could
// take client-globex/notes/expressroute.md's uid for a corpus page routed to work and stage Globex's
// file for deletion: one tenant's page deleted and its identity transplanted onto another's,
// surfaced to the user as nothing more alarming than "1 superseded paths removed". A page
// whose uid this import did not adopt is a different document that merely shares a name, and
// is left exactly where it is. An empty uid on either side proves nothing and never qualifies.
func supersedes(item plan) bool {
	return item.prior.path != "" &&
		item.prior.path != item.page.Path &&
		item.prior.uid != "" &&
		item.prior.uid == item.page.Meta.UID
}

// scopeFor resolves a page's scope from its customer/* facets and reports whether the
// page names more than one distinct tenant. tenants (defaults/tenants.yaml, loaded via
// registry.LoadTenants) is the sole authority on whether a customer/* facet value names an
// actual tenant at all — see the clientScopes comment above for why that decision moved out
// of this package.
//
// It collects every distinct customer facet rather than returning on the first match:
// the old first-match behaviour let facet order (arbitrary — Pass A does not guarantee
// it) decide a page's scope, which is exactly how Acme's own pages ended up split
// between client-colo and work with no principled reason for either.
//
// Only the segment immediately after "customer/" identifies the candidate tenant.
// defaults/facets/customer.yaml declares globex's own business units as children
// (customer/globex/consumer, customer/globex/labs) — those are still Globex, not a second customer, so a
// facet list of {customer/globex, customer/globex/consumer} must resolve as one tenant, not two.
// Splitting on "/" and keeping only that one segment (rather than the whole remainder) is
// what makes that collapse correctly instead of manufacturing a phantom multi-customer
// page out of a business-unit sub-facet. A candidate that tenants.Is reports false for —
// colo, platform, intranet, or anything else absent from defaults/tenants.yaml — is
// dropped before counting: it is not a tenant, so it neither creates a scope of its own
// nor makes an otherwise single-tenant page look multi-customer. This is why
// customer/globex + customer/colo now resolves as plain single-tenant Globex instead of being
// wrongly quarantined as multi-customer.
//
// Zero tenants: work (this is where a lone customer/colo, customer/platform, or
// customer/colo + customer/platform page lands — no real tenant is named at all). Exactly
// one: that tenant's scope, via scopeForTenant — every declared tenant gets a scope,
// never work, regardless of whether clientScopes has an override entry for it. More than
// one: the spec forbids demoting to work at all (balise-docs/01-design-spec-v0.4.md
// section 11, balise-docs/04-technical-spec-v1.md section 14 — "most restrictive scope +
// split proposal; never demote"), so every named tenant's scope is a candidate and the
// most restrictive one is chosen deterministically: the candidate set is sorted before
// taking the first entry, so the result never depends on the order facets happen to
// appear in.
func scopeFor(facets []string, tenants *registry.Tenants) (scope string, multiCustomer bool) {
	seen := map[string]bool{}
	var customers []string
	for _, facet := range facets {
		parts := strings.Split(facet, "/")
		if len(parts) < 2 || parts[0] != "customer" {
			continue
		}
		name := parts[1]
		if !tenants.Is(name) {
			continue
		}
		if !seen[name] {
			seen[name] = true
			customers = append(customers, name)
		}
	}

	switch len(customers) {
	case 0:
		return defaultScope, false
	case 1:
		return scopeForTenant(customers[0]), false
	default:
		candidates := make([]string, 0, len(customers))
		for _, customer := range customers {
			candidates = append(candidates, scopeForTenant(customer))
		}
		sort.Strings(candidates)
		return candidates[0], true
	}
}

// scopeForTenant returns the scope a declared tenant's pages live in: its clientScopes
// override if one exists, else the default "client-<name>" derivation.
func scopeForTenant(name string) string {
	if mapped, ok := clientScopes[name]; ok {
		return mapped
	}
	return "client-" + name
}

func aliasesFor(filename string, artifacts *Artifacts) []string {
	var out []string
	for alias, target := range artifacts.AliasTable {
		if target == filename {
			out = append(out, alias)
		}
	}
	sort.Strings(out)
	return out
}

func nodeValue(page vault.Page, key, fallback string) string {
	if node := page.Get(key); node != nil && node.Value != "" {
		return node.Value
	}
	return fallback
}

// Title resolution rungs, recorded as title_source so a page's headline is always traceable
// to where it came from rather than a mystery — balise-docs/01-design-spec-v0.4.md section 3
// (the title is the page's most important claim, <=15 words) and section 1.2 principle 2 (a
// claim states what is true, never just a subject).
const (
	titleFromName        = "name"
	titleFromHook        = "hook"
	titleFromDescription = "description"
	titleFromSlug        = "slug"
)

// resolveTitle implements the title resolution ladder: the corpus `name` if it reads as a
// title, else the hook (itself the page's most important claim, exactly what a headline is
// per section 3), else the first sentence of the description, else the slug as a last resort.
//
// Measured against the real 138-file corpus: 118 of the corpus `name` values are slug-like
// (the filename stem, or a slugified branch-ish string) rather than a title. Every one of
// those 118 recovers a usable title from a lower rung — 115 from the hook, 3 from the
// description — and none falls all the way to the slug.
func resolveTitle(name, slug, hook, description string) (title, source string) {
	if isTitleLike(name, slug) {
		return name, titleFromName
	}
	if hook != "" {
		return hook, titleFromHook
	}
	if sentence := firstSentence(description); sentence != "" {
		return sentence, titleFromDescription
	}
	return slug, titleFromSlug
}

// isTitleLike reports whether a corpus `name` value reads as a title rather than being the
// filename dressed up as frontmatter. A title contains a space — a slug or a bare filename
// stem never does — and is not merely the filename stem itself: vault.Normalise applies the
// same lowercasing, extension-stripping and non-alphanumeric-collapsing to name that produced
// slug from the filename in the first place (via vault.SlugFromFilename, which additionally
// strips the type prefix), so the two compare equal exactly when name says nothing a machine
// could not already read off the path.
func isTitleLike(name, slug string) bool {
	return name != "" && strings.Contains(name, " ") && vault.Normalise(name) != slug
}

// sentenceEnd matches a ., ! or ? followed by whitespace or the end of the string — not a
// period inside a token such as "dhia.user" or "forgeiq." followed immediately by more
// non-space text or punctuation.
var sentenceEnd = regexp.MustCompile(`[.!?](\s|$)`)

// firstSentence returns the first sentence of s, trimmed. A description with no
// sentence-ending punctuation at all (the real corpus has one: a semicolon-separated list
// with no periods) is returned whole rather than truncated arbitrarily.
func firstSentence(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	if loc := sentenceEnd.FindStringIndex(trimmed); loc != nil {
		return strings.TrimSpace(trimmed[:loc[0]+1])
	}
	return trimmed
}

func renderMeta(meta Meta, body string) (string, error) {
	var sb strings.Builder
	encoder := yaml.NewEncoder(&sb)
	encoder.SetIndent(2)
	if err := encoder.Encode(meta); err != nil {
		return "", fmt.Errorf("encode frontmatter: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("close encoder: %w", err)
	}
	return "---\n" + sb.String() + "---\n" + body, nil
}
