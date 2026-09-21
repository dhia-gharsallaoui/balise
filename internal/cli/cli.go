// Package cli wires the pieces into commands.
package cli

import (
	"context"
	"fmt"
	"path"
	"strings"

	"path/filepath"

	"github.com/dhia/balise/internal/indexer"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/vault"
)

// ReindexReport summarises one reindex. Link recovery is reported as four raw counts,
// not folded into a single percentage: a corpus the importer partitions into scopes (04
// section 14) can contain legitimate cross-scope references that vault.Resolve correctly
// refuses to follow — that is the scope-isolation security boundary working as designed,
// not a resolution failure — and collapsing those into one "recovery" number would make an
// architectural boundary look like a defect. WithinScopeRecovery is the acceptance
// criterion; ReferentialIntegrity and the raw counts are reported alongside it so a caller
// never sees only the flattering metric.
type ReindexReport struct {
	Pages           int
	Changed         int
	Pruned          int
	LinksTotal      int
	LinksResolved   int
	LinksCrossScope int
	LinksMissing    int
	AliasCollisions int
	Findings        int
}

// WithinScopeRecovery is resolved / (resolved + missing): dangling refs whose target
// exists in a different scope are excluded from both sides of the ratio, since the
// scope-isolation boundary blocking them is intentional (see vault.ScopedSlug's doc
// comment) and not something the resolver's link-recovery rate should be judged on. This
// is the spec's link-recovery criterion, threshold 0.95, checked in tests against this one
// run's own tally. It is scoped to the pages this particular Reindex call actually walked
// with a non-noop result — on an incremental run that only touched a handful of pages, that
// tally reflects those pages only, which is why `balise reindex`'s own printed output no
// longer calls this method: it prints LinkHealth (computed by ComputeLinkHealth from
// storage) instead, so the number a human reads is never scoped to just this run.
func (r ReindexReport) WithinScopeRecovery() float64 {
	denom := r.LinksResolved + r.LinksMissing
	if denom == 0 {
		return 1.0
	}
	return float64(r.LinksResolved) / float64(denom)
}

// ReferentialIntegrity is (total - missing) / total: the fraction of references that name
// a page which exists somewhere in the vault, in-scope or out. It is reported for context,
// not as an acceptance criterion in its own right, and — like WithinScopeRecovery above —
// is scoped to this run's own tally rather than the whole vault; see that method's comment.
func (r ReindexReport) ReferentialIntegrity() float64 {
	if r.LinksTotal == 0 {
		return 1.0
	}
	return float64(r.LinksTotal-r.LinksMissing) / float64(r.LinksTotal)
}

// Reindex walks the vault twice: once to build the slug and alias indexes (plus a
// scope-membership index used only to classify dangling refs, see crossScope), once to
// index. The two passes are why a page can link to one indexed after it.
// defaultsDir is explicit because `go test` runs each package with its own directory as CWD,
// so a repo-relative literal would resolve to internal/cli/defaults and fail.
func Reindex(
	ctx context.Context, q *store.Queries, pages store.PageStore, defaultsDir string,
) (ReindexReport, error) {
	reg, err := registry.Load(filepath.Join(defaultsDir, "types"))
	if err != nil {
		return ReindexReport{}, fmt.Errorf("load types: %w", err)
	}
	facets, err := registry.LoadFacets(filepath.Join(defaultsDir, "facets"))
	if err != nil {
		return ReindexReport{}, fmt.Errorf("load facets: %w", err)
	}

	paths, err := pages.List("")
	if err != nil {
		return ReindexReport{}, fmt.Errorf("list vault: %w", err)
	}
	var markdown []string
	for _, p := range paths {
		if strings.HasSuffix(p, ".md") && path.Base(p) != "index.md" && !vault.IsReservedPath(p) {
			markdown = append(markdown, p)
		}
	}

	ic := indexer.Context{
		Registry: reg, Facets: facets,
		Slugs: map[vault.ScopedSlug]string{}, Aliases: vault.AliasRegistry{},
	}
	// scopesForKey records, for every normalised slug or alias in the vault, which scopes
	// hold it. It is populated alongside ic in this same pass and consulted only in pass
	// two, to tell a cross-scope dangling ref (the target page exists, just under a
	// different scope) from one that is genuinely missing — a distinction vault.Resolve
	// itself deliberately never makes, since resolution must never cross a scope boundary.
	// This is the same vault.ScopeIndex a later, storage-backed caller (LinkHealth via
	// Queries.SlugScopeIndex) rebuilds from documents instead of from a git walk — see its
	// doc comment for why the two must agree.
	scopesForKey := vault.ScopeIndex{}
	// pageScope records each page's own scope, computed once here and reused in pass two
	// to classify that page's dangling refs without re-parsing its frontmatter.
	pageScope := map[string]string{}

	for _, p := range markdown {
		raw, _, err := pages.Read(p)
		if err != nil {
			return ReindexReport{}, fmt.Errorf("read %s: %w", p, err)
		}
		page, err := vault.Parse(string(raw))
		if err != nil {
			continue // reported as a finding during the indexing pass
		}
		var fm struct {
			UID     string   `yaml:"uid"`
			Slug    string   `yaml:"slug"`
			Scope   string   `yaml:"scope"`
			Aliases []string `yaml:"aliases"`
		}
		if err := page.Decode(&fm); err != nil {
			continue
		}
		scope := fm.Scope
		if scope == "" {
			scope = vault.ScopeOf(p)
		}
		pageScope[p] = scope
		if fm.UID == "" {
			continue
		}
		slug := fm.Slug
		if slug == "" {
			slug = vault.SlugFromFilename(path.Base(p))
		}
		ic.Slugs[vault.ScopedSlug{Scope: scope, Slug: slug}] = fm.UID
		ic.Aliases = ic.Aliases.Register(slug, fm.UID, scope)
		scopesForKey.Record(slug, scope)
		for _, alias := range fm.Aliases {
			ic.Aliases = ic.Aliases.Register(alias, fm.UID, scope)
			scopesForKey.Record(alias, scope)
		}
	}

	report := ReindexReport{Pages: len(markdown), AliasCollisions: len(ic.Aliases.Collisions())}

	for _, p := range markdown {
		raw, version, err := pages.Read(p)
		if err != nil {
			return ReindexReport{}, fmt.Errorf("read %s: %w", p, err)
		}
		result, err := indexer.IndexPage(ctx, q, p, string(raw), version, ic)
		if err != nil {
			return ReindexReport{}, fmt.Errorf("index %s: %w", p, err)
		}
		if result.Changed {
			report.Changed++
		}
		for _, finding := range result.Findings {
			report.Findings++
			switch finding.Rule {
			case "dangling_ref":
				report.LinksTotal++
				if scopesForKey.CrossScope(finding.Detail, pageScope[p]) {
					report.LinksCrossScope++
				} else {
					report.LinksMissing++
				}
			case "resolved_ref":
				report.LinksTotal++
				report.LinksResolved++
			}
		}
	}

	// Prune last, and only against the paths this run actually walked. Nothing else in the
	// system ever deletes a document, so before this a page that moved — a corpus
	// reclassified, a tenant corrected in defaults/tenants.yaml — left its old row behind
	// under its old scope, with its full body and claims, served forever by /api/pages,
	// /api/search and /api/graph. The row is scoped, so the prune is too: it can only
	// remove documents in scopes this Queries already holds.
	pruned, err := q.DeleteDocumentsNotIn(ctx, markdown)
	if err != nil {
		return ReindexReport{}, fmt.Errorf("prune stale documents: %w", err)
	}
	report.Pruned = pruned

	return report, nil
}

// LinkHealth summarises link recovery and referential integrity from the vault's currently
// stored lint_findings, rather than from one reindex run's in-memory tally. This is what
// fixes the false "100%" `balise reindex` could print after an incremental run: ReindexReport
// above only accumulates findings for the pages that specific invocation actually indexed
// (indexer.IndexPage skips finding computation entirely for a noop page — see its isNoop
// check), so a run touching a handful of pages produced near-zero counts, and
// WithinScopeRecovery/ReferentialIntegrity's own zero-denominator guards rendered that as a
// false 100%. LinkHealth instead reads every unresolved dangling_ref/resolved_ref finding
// across the whole vault, which stays complete and correct regardless of how partial the
// most recent reindex was: an unchanged page's prior findings are still accurate precisely
// because nothing about that page changed.
type LinkHealth struct {
	Total      int
	Resolved   int
	CrossScope int
	Missing    int
}

// ComputeLinkHealth classifies every currently unresolved dangling_ref/resolved_ref finding
// in q's allowed scopes: resolved_ref (severity suggest) counts as Resolved; dangling_ref is
// split into CrossScope and Missing using a vault.ScopeIndex rebuilt from what is currently
// indexed (Queries.SlugScopeIndex), the same classification Reindex itself applies live
// against its own git walk.
func ComputeLinkHealth(ctx context.Context, q *store.Queries) (LinkHealth, error) {
	findings, err := q.FindingsByRule(ctx)
	if err != nil {
		return LinkHealth{}, fmt.Errorf("findings by rule: %w", err)
	}
	index, err := q.SlugScopeIndex(ctx)
	if err != nil {
		return LinkHealth{}, fmt.Errorf("slug scope index: %w", err)
	}

	var health LinkHealth
	for _, rf := range findings {
		switch rf.Rule {
		case "resolved_ref":
			health.Total++
			health.Resolved++
		case "dangling_ref":
			health.Total++
			if index.CrossScope(rf.Detail, rf.Scope) {
				health.CrossScope++
			} else {
				health.Missing++
			}
		}
	}
	return health, nil
}

// WithinScopeRecovery is resolved / (resolved + missing), matching ReindexReport's metric of
// the same name but computed from LinkHealth's storage-backed counts. ok is false when there
// is nothing to divide by (no resolved or missing link anywhere in the allowed scopes) — a
// caller must never render a 0/0 ratio as a false 100%, so it checks ok rather than assuming
// pct is meaningful.
func (h LinkHealth) WithinScopeRecovery() (pct float64, ok bool) {
	denom := h.Resolved + h.Missing
	if denom == 0 {
		return 0, false
	}
	return float64(h.Resolved) / float64(denom), true
}

// ReferentialIntegrity is (total - missing) / total. ok is false when Total is 0.
func (h LinkHealth) ReferentialIntegrity() (pct float64, ok bool) {
	if h.Total == 0 {
		return 0, false
	}
	return float64(h.Total-h.Missing) / float64(h.Total), true
}

// FormatLinkHealth renders the two lines `balise reindex` prints for link health, guarding
// both ratios' 0/0 case directly rather than relying on the caller to remember to check ok:
// a ratio with nothing on either side of it must never come out as a false 100%, which is
// exactly what happened before this fix on an incremental run whose changed pages carried no
// link findings of their own.
func FormatLinkHealth(h LinkHealth) (countsLine, ratiosLine string) {
	countsLine = fmt.Sprintf("links: %d total, %d resolved, %d cross-scope (blocked by design), %d missing",
		h.Total, h.Resolved, h.CrossScope, h.Missing)

	recovery, recoveryOK := h.WithinScopeRecovery()
	integrity, integrityOK := h.ReferentialIntegrity()
	switch {
	case recoveryOK && integrityOK:
		ratiosLine = fmt.Sprintf("within-scope recovery %.1f%%, referential integrity %.1f%%", recovery*100, integrity*100)
	case recoveryOK:
		ratiosLine = fmt.Sprintf("within-scope recovery %.1f%%, referential integrity n/a (no links indexed yet)", recovery*100)
	case integrityOK:
		ratiosLine = fmt.Sprintf("within-scope recovery n/a (no in-scope links yet), referential integrity %.1f%%", integrity*100)
	default:
		ratiosLine = "within-scope recovery n/a, referential integrity n/a (no links indexed yet)"
	}
	return countsLine, ratiosLine
}
