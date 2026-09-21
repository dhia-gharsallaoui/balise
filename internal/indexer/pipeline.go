package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/vault"
)

// maxHeadlineWords is the headline word budget: balise-docs/01-design-spec-v0.4.md section 3
// caps the title — the page's most important claim — at 15 words. A title that exceeds it is
// never truncated (truncating a claim mid-sentence destroys its meaning); it is surfaced
// instead as a long_headline finding for a reviewer to shorten by hand.
const maxHeadlineWords = 15

// historicalStatuses are the instance statuses that exclude a page from packs by default.
// Historical is derived from status, never from type — these are status values, not type
// names, so the domain-neutrality guard is unaffected.
var historicalStatuses = map[string]bool{
	"superseded": true, "retired": true, "resolved": true,
}

// Context carries what indexing one page needs beyond the page itself.
type Context struct {
	Registry *registry.Registry
	Facets   *registry.Facets
	Slugs    map[vault.ScopedSlug]string
	Aliases  vault.AliasRegistry
}

// Result reports what happened to one page. A malformed page yields Indexed=false and
// a finding, never an error: 04 section 7 requires one bad file not to fail a reindex.
type Result struct {
	Indexed        bool
	Changed        bool
	UID            string
	AssignedUID    string
	NeedsWriteback bool
	Findings       []store.Finding
}

type claimFrontmatter struct {
	Text   string `yaml:"text"`
	Status string `yaml:"status"`
	ID     string `yaml:"id"`
	AsOf   string `yaml:"as_of"`
	Span   []int  `yaml:"span"`
}

type frontmatter struct {
	UID                 string             `yaml:"uid"`
	Slug                string             `yaml:"slug"`
	Type                string             `yaml:"type"`
	Scope               string             `yaml:"scope"`
	Title               string             `yaml:"title"`
	Status              string             `yaml:"status"`
	Owner               string             `yaml:"owner"`
	Aliases             []string           `yaml:"aliases"`
	Tags                []string           `yaml:"tags"`
	Claims              []claimFrontmatter `yaml:"claims"`
	NeedsClassification bool               `yaml:"needs_classification"`
	NeedsSplit          bool               `yaml:"needs_split"`
}

// IndexPage runs one page through parse, identity, validation, edges, tags and chunking,
// writing the result through q. A malformed page reports a finding and returns
// Indexed=false with a nil error rather than failing — 04 section 7: one bad file must
// never fail a reindex of many.
func IndexPage(
	ctx context.Context, q *store.Queries, pagePath, raw, gitVersion string, ic Context,
) (Result, error) {
	page, fm, badFinding := parsePage(raw)
	if badFinding != nil {
		// A page that failed to parse has no frontmatter and therefore no uid, so its
		// finding cannot be attached to a document row. It used to be returned to the
		// caller and nothing else: the reindex counted it in its stdout total and then
		// dropped it, leaving the page invisible in the UI with nothing anywhere saying
		// why. It is written as a global finding instead, carrying the path in Detail
		// (the only identity it has) and the path's scope so it stays behind the same
		// boundary every other row does. /api/tree reads these back.
		finding := *badFinding
		finding.Detail = pagePath + ": " + finding.Detail
		finding.Scope = vault.ScopeOf(pagePath)
		// A page sitting in a scope this Queries does not hold cannot have a finding filed
		// under that scope, and must not take the whole run down with it either: 04 section 7
		// is the governing rule of this pipeline — one bad file must never fail a reindex of
		// many — and a file that is both unparseable and out of scope is exactly the case it
		// exists for. The finding still travels back in Result, so the run's own counts and
		// any caller inspecting them still see it; only the write is skipped.
		if err := q.UpsertFinding(ctx, "", finding); err != nil &&
			!errors.Is(err, store.ErrScopeDenied) {
			return Result{}, fmt.Errorf("finding for %s: %w", pagePath, err)
		}
		return Result{Findings: []store.Finding{finding}}, nil
	}

	scope, slug, uid, assignedUID, needsWriteback := resolveIdentity(fm, pagePath)

	// This path just parsed successfully, so any earlier "unparseable" global finding filed
	// under it (see the badFinding branch above) no longer applies — resolve it now. This
	// runs even when the page turns out to be a content noop below: a page recovering from
	// unparseable can never itself be a noop (isNoop compares against an existing document
	// row, and a page that failed to parse never got one), but resolving here rather than
	// after the noop check keeps this independent of that unrelated short-circuit. Scoped to
	// this exact path, so a hypothetical future partial reindex touching only some paths can
	// never resolve another path's unparseable finding — see ResolveGlobalFindingsForPath's
	// doc comment. ErrScopeDenied is swallowed for the same reason the write above swallows
	// it: a page outside this Queries' scopes must not take the whole run down.
	if err := q.ResolveGlobalFindingsForPath(ctx, "unparseable", scope, pagePath); err != nil &&
		!errors.Is(err, store.ErrScopeDenied) {
		return Result{}, fmt.Errorf("resolve unparseable finding for %s: %w", pagePath, err)
	}

	bodyHash := hashOf(page.Body)

	noop, err := isNoop(ctx, q, scope, slug, bodyHash, gitVersion)
	if err != nil {
		return Result{}, fmt.Errorf("check existing page %s: %w", pagePath, err)
	}
	if noop {
		return Result{Indexed: true, Changed: false, UID: uid}, nil
	}

	result := Result{
		Indexed: true, Changed: true, UID: uid,
		AssignedUID: assignedUID, NeedsWriteback: needsWriteback,
	}

	typeName := orDefault(fm.Type, "note")
	tokens := CountTokens(page.Body)
	tags, tagFindings := classifyTags(fm.Tags, ic.Facets)
	result.Findings = append(result.Findings, tagFindings...)
	result.Findings = append(result.Findings, validationFindings(ic.Registry, typeName, tokens, fm)...)

	doc := buildDocument(fm, documentArgs{
		uid: uid, slug: slug, scope: scope, typeName: typeName, pagePath: pagePath,
		body: page.Body, bodyHash: bodyHash, tokens: tokens, gitVersion: gitVersion,
	}, tags)
	if err := q.UpsertDocument(ctx, doc); err != nil {
		return Result{}, fmt.Errorf("index %s: %w", pagePath, err)
	}
	claims, claimFindings := buildClaims(fm.Claims)
	result.Findings = append(result.Findings, claimFindings...)
	if err := q.ReplaceClaims(ctx, uid, claims); err != nil {
		return Result{}, fmt.Errorf("claims for %s: %w", pagePath, err)
	}

	edgeFindings, err := indexEdges(ctx, q, page, uid, scope, ic)
	if err != nil {
		return Result{}, fmt.Errorf("edges for %s: %w", pagePath, err)
	}
	result.Findings = append(result.Findings, edgeFindings...)

	if err := q.ReplaceChunks(ctx, uid, buildChunks(page.Body)); err != nil {
		return Result{}, fmt.Errorf("chunks for %s: %w", pagePath, err)
	}
	if err := writeFindings(ctx, q, uid, result.Findings); err != nil {
		return Result{}, fmt.Errorf("findings for %s: %w", pagePath, err)
	}
	return result, nil
}

// parsePage parses frontmatter and decodes it into the pipeline's shape. A malformed page
// yields a finding instead of an error — see IndexPage's doc comment.
func parsePage(raw string) (vault.Page, frontmatter, *store.Finding) {
	page, err := vault.Parse(raw)
	if err != nil {
		return vault.Page{}, frontmatter{}, &store.Finding{
			Rule: "unparseable", Severity: "error", Detail: err.Error(),
		}
	}
	var fm frontmatter
	if err := page.Decode(&fm); err != nil {
		return vault.Page{}, frontmatter{}, &store.Finding{
			Rule: "unparseable", Severity: "error", Detail: err.Error(),
		}
	}
	return page, fm, nil
}

// resolveIdentity fills in scope, slug and uid from the frontmatter, falling back to the
// page's path when the frontmatter omits them, and mints a fresh UID (flagged for
// writeback) when the page has none yet.
func resolveIdentity(fm frontmatter, pagePath string) (scope, slug, uid, assignedUID string, needsWriteback bool) {
	scope = orDefault(fm.Scope, vault.ScopeOf(pagePath))
	slug = orDefault(fm.Slug, vault.SlugFromFilename(path.Base(pagePath)))
	uid = fm.UID
	if uid == "" {
		uid = vault.NewUID()
		assignedUID, needsWriteback = uid, true
	}
	return scope, slug, uid, assignedUID, needsWriteback
}

func hashOf(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// isNoop reports whether the stored page already matches this content and git version,
// comparing both: matching only the hash would treat a page amended at a new commit but
// with identical text as unresolved, and matching only the version would treat a genuinely
// edited file re-committed at the same version as unchanged. GetPage's own error (a real
// lookup failure, not "not found" — it returns nil, nil for that) is propagated rather than
// swallowed, since silently proceeding to re-index would mask a real database problem.
//
// The lookup is by (scope, slug) — the page's actual identity — not by slug alone. Keyed on
// slug alone it compared against whichever scope's row sorted first, which broke twice for a
// slug held in two scopes: the losing page never matched and so re-upserted on every reindex,
// making Changed useless as an acceptance signal; and, because gitVersion is a per-file blob
// hash, two byte-identical same-slug pages in different scopes made the second one compare
// equal to the first and report Indexed=true, Changed=false — never indexed at all, with no
// finding to say so.
func isNoop(ctx context.Context, q *store.Queries, scope, slug, bodyHash, gitVersion string) (bool, error) {
	existing, err := q.GetPage(ctx, scope, slug)
	if err != nil {
		return false, fmt.Errorf("get page %s/%s: %w", scope, slug, err)
	}
	if existing == nil {
		return false, nil
	}
	return existing.BodyHash == bodyHash && existing.GitVersion == gitVersion, nil
}

// classifyTags converts each tag to its ltree path via the facet registry, reporting any
// tag outside the known vocabulary instead of dropping it silently.
func classifyTags(rawTags []string, facets *registry.Facets) ([]string, []store.Finding) {
	var tags []string
	var findings []store.Finding
	for _, tag := range rawTags {
		ltree, ok := facets.ToLtree(tag)
		if !ok {
			findings = append(findings, store.Finding{
				Rule: "out_of_vocabulary", Severity: "warn", Detail: tag,
			})
			continue
		}
		tags = append(tags, ltree)
	}
	return tags, findings
}

// validationFindings reports size, claim-count and classification problems that should not
// block indexing but do need surfacing. reg.Limits and reg.Traits both return usable
// defaults for a type absent from the registry, so this never panics on an unknown type —
// see the domain-neutrality guard in internal/guard/neutral_test.go.
func validationFindings(reg *registry.Registry, typeName string, tokens int, fm frontmatter) []store.Finding {
	maxTokens, maxClaims := reg.Limits(typeName)
	var findings []store.Finding
	if tokens > maxTokens {
		findings = append(findings, store.Finding{
			Rule: "oversize", Severity: "warn", Action: "split",
			Detail: fmt.Sprintf("%d tokens > max %d", tokens, maxTokens),
		})
	}
	if len(fm.Claims) > maxClaims {
		findings = append(findings, store.Finding{
			Rule: "too_many_claims", Severity: "warn", Action: "split",
			Detail: fmt.Sprintf("%d > max %d", len(fm.Claims), maxClaims),
		})
	}
	if reg.Traits(typeName)["indexed"] && len(fm.Claims) == 0 {
		findings = append(findings, store.Finding{Rule: "missing_claims", Severity: "warn"})
	}
	if fm.NeedsClassification {
		findings = append(findings, store.Finding{
			Rule: "missing_classification", Severity: "warn", Action: "classify",
			Detail: "no Pass A classification; imported as note",
		})
	}
	if fm.NeedsSplit {
		findings = append(findings, store.Finding{Rule: "multi_customer", Severity: "warn", Action: "split"})
	}
	if words := len(strings.Fields(fm.Title)); words > maxHeadlineWords {
		findings = append(findings, store.Finding{
			Rule: "long_headline", Severity: "warn",
			Detail: fmt.Sprintf("%d words > max %d", words, maxHeadlineWords),
		})
	}
	return findings
}

// documentArgs groups buildDocument's scalar inputs; the frontmatter and tags are passed
// separately since they need their own conversions (title fallback, ltree tags).
type documentArgs struct {
	uid, slug, scope, typeName, pagePath string
	body, bodyHash, gitVersion           string
	tokens                               int
}

// buildDocument assembles the store row for one page.
func buildDocument(fm frontmatter, args documentArgs, tags []string) store.Document {
	return store.Document{
		UID: args.uid, Slug: args.slug, Scope: args.scope, Type: args.typeName,
		Path: args.pagePath, Title: orDefault(fm.Title, args.slug),
		Aliases: fm.Aliases, Tags: tags, Status: fm.Status, Owner: fm.Owner,
		BodyMD: args.body, BodyHash: args.bodyHash, Tokens: args.tokens,
		ClaimsCount: len(fm.Claims), Historical: historicalStatuses[fm.Status],
		GitVersion: args.gitVersion,
	}
}

// buildClaims converts frontmatter claims into store rows, defaulting a missing id to a
// positional c1, c2... and a missing status to active. A claim whose as_of fails to parse
// (a typo, or "June 2026" written by a human editing frontmatter by hand) is indexed with
// AsOf left nil plus an invalid_date finding, rather than silently discarding the date:
// losing as_of silently would leave a "superseded" claim with no date and no signal that
// anything went wrong, which is exactly the kind of swallowed error this pipeline reports
// instead of hiding.
func buildClaims(raw []claimFrontmatter) ([]store.Claim, []store.Finding) {
	claims := make([]store.Claim, 0, len(raw))
	var findings []store.Finding
	for i, rc := range raw {
		claim := store.Claim{
			ClaimID: orDefault(rc.ID, fmt.Sprintf("c%d", i+1)),
			Ord:     i, Text: rc.Text, Status: orDefault(rc.Status, "active"),
		}
		if rc.AsOf != "" {
			if parsed, err := time.Parse("2006-01-02", rc.AsOf); err == nil {
				claim.AsOf = &parsed
			} else {
				findings = append(findings, store.Finding{
					Rule: "invalid_date", Severity: "warn",
					Detail: fmt.Sprintf("as_of: %s", rc.AsOf),
				})
			}
		}
		if len(rc.Span) == 2 {
			claim.SpanStart, claim.SpanEnd = &rc.Span[0], &rc.Span[1]
		}
		claims = append(claims, claim)
	}
	return claims, findings
}

// indexEdges resolves each edge from the page against the vault's slug/alias tables,
// recording resolved_ref alongside dangling_ref — Task 15 computes link recovery as
// resolved over resolved+dangling, so a resolved reference must be counted too — and
// writes every edge, resolved or not, through UpsertEdge.
//
// The resolved_ref/dangling_ref findings count distinct references (by ToRef), not edges:
// EdgesFrom dedupes on (ToRef, Kind, Field), so the same target named in both a frontmatter
// ref field and a body link produces two edges but must count once for link recovery — "link
// recovery" means references recovered, and double-counting one reference as two would skew
// Task 15's resolved/(resolved+dangling) ratio. Every edge is still written through
// UpsertEdge regardless of whether its ToRef already produced a finding.
//
// EdgesFrom already dedupes and sorts its output (by ToRef, then Kind, then Field), so
// this deliberately does not re-sort it.
func indexEdges(
	ctx context.Context, q *store.Queries, page vault.Page, uid, scope string, ic Context,
) ([]store.Finding, error) {
	var findings []store.Finding
	countedRefs := map[string]bool{}
	for _, edge := range EdgesFrom(page, ic.Registry) {
		resolution := vault.Resolve(edge.ToRef, scope, ic.Slugs, ic.Aliases)
		kind := edge.Kind
		if resolution.UID == "" {
			kind = "dangling"
		}
		if !countedRefs[edge.ToRef] {
			countedRefs[edge.ToRef] = true
			findings = append(findings, refFinding(edge.ToRef, resolution))
		}
		if err := q.UpsertEdge(ctx, uid, resolution.UID, edge.ToRef, kind, edge.Field); err != nil {
			return nil, fmt.Errorf("edge %s: %w", edge.ToRef, err)
		}
	}
	return findings, nil
}

// refFinding reports one reference as resolved or dangling.
func refFinding(toRef string, resolution vault.Resolution) store.Finding {
	if resolution.UID == "" {
		return store.Finding{
			Rule: "dangling_ref", Severity: "warn", Action: "create_stub", Detail: toRef,
		}
	}
	return store.Finding{
		Rule: "resolved_ref", Severity: "suggest", Detail: toRef + " via " + resolution.Rung,
	}
}

func buildChunks(body string) []store.Chunk {
	chunks := ChunkBody(body, DefaultMaxChunkTokens)
	stored := make([]store.Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		hash := sha256.Sum256([]byte(chunk.Text))
		stored = append(stored, store.Chunk{
			Ord: chunk.Ord, HeadingPath: chunk.HeadingPath, Text: chunk.Text,
			Tokens: chunk.Tokens, Hash: hex.EncodeToString(hash[:]),
		})
	}
	return stored
}

// writeFindings makes uid's active findings exactly the set this pass just computed:
// ReplaceFindings upserts each one and soft-resolves any finding still active on uid that
// this pass did not raise again — a dangling_ref whose target got created, a long_headline
// whose title got shortened, an out_of_vocabulary tag that got reclassified. Using
// ReplaceFindings rather than looping UpsertFinding (as this used to do) is the fix: a plain
// upsert loop can reactivate a finding but has no way to ever resolve one, which is why a
// fixed page's findings never used to clear. See ReplaceFindings' doc comment for why this is
// safe even for a hypothetical future partial reindex: it only ever compares uid's own old
// finding set against its own new one.
func writeFindings(ctx context.Context, q *store.Queries, uid string, findings []store.Finding) error {
	if err := q.ReplaceFindings(ctx, uid, findings); err != nil {
		return fmt.Errorf("replace findings: %w", err)
	}
	return nil
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
