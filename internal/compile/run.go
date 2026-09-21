package compile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/vault"
)

// CommitBatchSize is how many succeeded pages' proposals (plus the updated
// state file) are batched into one commit (fix 3). 20 rather than 1 or
// "everything": with a 138-page corpus, committing per-page would produce ~138
// commits per run — noisy history and 138 round trips through the store's
// Commit for no real safety gain. Committing everything in one shot at the end
// is the opposite problem this fix removes: a late page's failure, or a crash,
// discarded every dollar already spent. Batches of 20 land ~7 commits for a
// full run, bound the worst-case loss to under 20 pages of spend, and the
// existing body-hash skip state (state.go) makes re-running the tool cheap, so
// picking the run back up after a partial batch is safe.
const CommitBatchSize = 20

// Options configures one extract_claims run.
type Options struct {
	Scope  string // restrict to one scope; "" means every scope
	Limit  int    // cap on pages actually attempted (skipped pages don't count); 0 means unlimited
	DryRun bool   // render prompts and exit without calling the model or writing anything
	Model  string
}

// PageReport is one candidate page's outcome within a Run. Skipped and Failed
// are mutually exclusive; a page report with neither set (and not a dry run)
// succeeded and has a ProposalPath.
type PageReport struct {
	Path         string
	Skipped      bool
	SkipReason   string
	Failed       bool // fix 2: this page's extraction/build failed; the run moved on regardless
	FailReason   string
	Prompt       string // system+user prompt, populated only on DryRun
	ProposalPath string
	Change       ClaimsChange
	Warnings     []ClaimWarning // fix 1: non-blocking per-claim flags, e.g. over_word_limit
	InputTokens  int
	OutputTokens int
	// SupersededProposalID is set when this page already had a pending proposal in review/
	// before this run started; that older proposal was moved to review/.rejected/ with a
	// "superseded" reason as part of committing this page's fresh one (fix 1, part A). Empty
	// means no existing pending proposal was found for this target.
	SupersededProposalID string
}

// Report summarizes a whole extract_claims run — section 9.3 requires the
// budget per task to be recorded, which is what TotalInputTokens/
// TotalOutputTokens are for. CommitSHAs holds one entry per batch actually
// committed (fix 3): it may be empty (dry run, or nothing to propose) or have
// more than one entry on a run large enough to cross CommitBatchSize.
type Report struct {
	Pages             []PageReport
	TotalInputTokens  int
	TotalOutputTokens int
	CommitSHAs        []string
}

// Counts summarizes a Report's pages into how many succeeded (a proposal was
// written), were skipped as unchanged, or failed outright — the breakdown the
// CLI's end-of-run summary and exit code both depend on (fix 2).
func (r Report) Counts() (succeeded, skipped, failed int) {
	for _, p := range r.Pages {
		switch {
		case p.Skipped:
			skipped++
		case p.Failed:
			failed++
		default:
			succeeded++
		}
	}
	return succeeded, skipped, failed
}

// AllAttemptedPagesFailed reports whether every page this run actually
// attempted (i.e. excluding pages skipped because they were unchanged) ended in
// failure. Fix 2 requires one bad page to never abort the other 137, but a run
// where nothing at all succeeded is still worth exiting non-zero for.
func (r Report) AllAttemptedPagesFailed() bool {
	succeeded, _, failed := r.Counts()
	return failed > 0 && succeeded == 0
}

// OverLengthClaimCount totals how many claims across the whole run were
// flagged over_word_limit (fix 1) — used for the CLI's end-of-run summary.
func (r Report) OverLengthClaimCount() int {
	n := 0
	for _, p := range r.Pages {
		n += len(p.Warnings)
	}
	return n
}

// SupersededCount totals how many of this run's fresh proposals superseded an older pending
// proposal for the same target (fix 1, part A) — used for the CLI's end-of-run summary so a
// run that quietly cleaned up a stale duplicate says so rather than leaving it undiscoverable.
func (r Report) SupersededCount() int {
	n := 0
	for _, p := range r.Pages {
		if p.SupersededProposalID != "" {
			n++
		}
	}
	return n
}

// pageFrontmatter is the subset of a page's frontmatter extract_claims needs.
type pageFrontmatter struct {
	Slug   string             `yaml:"slug"`
	Type   string             `yaml:"type"`
	Scope  string             `yaml:"scope"`
	Title  string             `yaml:"title"`
	Claims []claimFrontmatter `yaml:"claims"`
}

type claimFrontmatter struct {
	Text   string `yaml:"text"`
	Status string `yaml:"status"`
	ID     string `yaml:"id"`
}

// candidate is one page eligible for extract_claims, with everything read once
// up front so the main loop never re-touches the store mid-page.
type candidate struct {
	Path      string
	Scope     string
	Slug      string
	TypeName  string
	TypeDesc  string
	Title     string
	Body      string
	Claims    []claimFrontmatter
	MaxClaims int
	BodyHash  string
	// Version is the page's current PageStore version (git blob SHA), read at the same time
	// as everything else above. It becomes the built proposal's TargetVersion (fix 1, part
	// B) — the baseline accept later checks the page still matches before applying anything.
	Version string
}

// Run extracts claims for every eligible page under opts.Scope, up to
// opts.Limit (skipped pages don't count). A dry run renders prompts and returns
// without ever calling client or writing anything — cost control from section
// 9.3.
//
// A single page's hard failure — extraction still invalid after one retry, or
// a proposal that fails to render — is recorded on that page's PageReport
// (Failed=true, FailReason set) and Run moves on to the next candidate; it
// never aborts the other pages over one bad page (fix 2 — this was tried
// against the full 138-page corpus, where a single over-length claim on page
// one used to discard the entire run's output). Run itself only returns a
// non-nil error for a structural failure that prevents it from doing anything
// at all (listing pages, loading state, or a commit itself failing); the CLI
// decides whether per-page failures warrant a non-zero exit via
// Report.AllAttemptedPagesFailed.
//
// Successful pages' proposals are committed incrementally in batches of
// CommitBatchSize (fix 3), together with the state file, rather than as one
// commit at the very end — so a crash or a later page's failure never discards
// spend already accounted for. The existing body-hash skip state makes a
// resumed run cheap, so partial progress is safe to keep.
func Run(ctx context.Context, pages store.PageStore, reg *registry.Registry, client LLMClient, opts Options) (Report, error) {
	candidates, err := candidatePages(pages, reg, opts.Scope)
	if err != nil {
		return Report{}, fmt.Errorf("list candidate pages: %w", err)
	}
	state, err := LoadState(pages)
	if err != nil {
		return Report{}, fmt.Errorf("load extract_claims state: %w", err)
	}
	// Fix 1, part A: scanned fresh on every run, independent of state — see
	// pendingProposalsByTarget's own doc comment for why the body-hash skip state cannot
	// answer "does this target already have a pending proposal".
	pendingByTarget, err := pendingProposalsByTarget(pages)
	if err != nil {
		return Report{}, fmt.Errorf("list existing pending proposals: %w", err)
	}

	report := Report{}
	var pending []store.Change
	attempted := 0
	now := time.Now()

	commit := func() (Report, error) {
		if len(pending) == 0 {
			return report, nil
		}
		stateBytes, err := state.Render()
		if err != nil {
			return report, fmt.Errorf("render extract_claims state: %w", err)
		}
		changes := append(append([]store.Change{}, pending...), store.Change{Path: statePath, Data: stateBytes})
		sha, err := pages.Commit(changes, "balise <balise@localhost>", "compile: extract_claims")
		if err != nil {
			return report, fmt.Errorf("commit extract_claims proposals: %w", err)
		}
		report.CommitSHAs = append(report.CommitSHAs, sha)
		pending = nil
		return report, nil
	}

	for _, c := range candidates {
		if opts.Limit > 0 && attempted >= opts.Limit {
			break
		}
		if state.Unchanged(c.Path, c.BodyHash) {
			report.Pages = append(report.Pages, PageReport{
				Path: c.Path, Skipped: true, SkipReason: "unchanged since last successful extraction",
			})
			continue
		}
		attempted++

		pr, outcome := processPage(ctx, client, c, opts, now)
		report.Pages = append(report.Pages, pr)
		report.TotalInputTokens += pr.InputTokens
		report.TotalOutputTokens += pr.OutputTokens
		if opts.DryRun || pr.Failed {
			continue
		}

		rendered, err := outcome.proposal.Render("")
		if err != nil {
			// This page's problem, not the whole run's — same handling as an
			// extraction failure (fix 2).
			report.Pages[len(report.Pages)-1] = PageReport{
				Path: c.Path, Failed: true, FailReason: fmt.Sprintf("render proposal: %v", err),
				InputTokens: pr.InputTokens, OutputTokens: pr.OutputTokens,
			}
			continue
		}
		pending = append(pending, store.Change{Path: pr.ProposalPath, Data: []byte(rendered)})
		state = state.WithRecorded(c.Path, c.BodyHash, now)

		// Fix 1, part A: this target already had a pending proposal from before this run
		// started (most often: an earlier run was interrupted after writing the proposal
		// file but before its state-file update landed in the same commit, so a resumed run
		// re-extracts and would otherwise mint a second one). Supersede it in the same
		// batch as the replacement, rather than leaving both pending — accepting both would
		// silently corrupt the page (see pending.go's targetKey doc comment).
		if old, ok := pendingByTarget[targetKey(c.Scope, c.Slug)]; ok {
			delChange, addChange, serr := supersede(old, fmt.Sprintf(
				"superseded by a fresh extraction (%s) for the same target", outcome.proposal.ID))
			if serr != nil {
				return report, serr
			}
			pending = append(pending, delChange, addChange)
			delete(pendingByTarget, targetKey(c.Scope, c.Slug))
			pr.SupersededProposalID = old.Proposal.ID
			report.Pages[len(report.Pages)-1] = pr
		}

		if len(pending) >= CommitBatchSize {
			if report, err = commit(); err != nil {
				return report, err
			}
		}
	}

	if opts.DryRun {
		return report, nil
	}
	return commit()
}

// pageOutcome carries the built Proposal alongside its PageReport so Run can
// render and stage it without recomputing anything.
type pageOutcome struct {
	proposal Proposal
}

// processPage renders the prompt for one page and, unless opts.DryRun, calls
// the model and builds its proposal. A page-level extraction failure never
// returns a Go error (fix 2) — the returned PageReport carries Failed=true and
// FailReason describing what went wrong, and pageOutcome is the zero value
// (nothing to render or commit for this page). Tokens spent on a failed
// attempt are still recorded on the PageReport so the run's totals reflect
// real spend.
func processPage(ctx context.Context, client LLMClient, c candidate, opts Options, now time.Time) (PageReport, pageOutcome) {
	in := ExtractInput{
		Path: c.Path, Scope: c.Scope, Slug: c.Slug, TypeName: c.TypeName, TypeDesc: c.TypeDesc,
		Title: c.Title, Body: c.Body, ExistingClaims: toExistingClaims(c.Claims),
		MaxClaims: c.MaxClaims, Model: opts.Model,
	}

	if opts.DryRun {
		system, user := RenderPrompt(in)
		return PageReport{Path: c.Path, Prompt: system + "\n\n" + user}, pageOutcome{}
	}

	result, err := ExtractClaims(ctx, client, in)
	if err != nil {
		return PageReport{
			Path: c.Path, Failed: true, FailReason: err.Error(),
			InputTokens: result.InputTokens, OutputTokens: result.OutputTokens,
		}, pageOutcome{}
	}
	proposal := BuildProposal(c.Path, c.Scope, c.Slug, result.Change, result.Confidence, c.Body, now, result.Warnings...)
	proposal = proposal.WithTargetVersion(c.Version)
	pr := PageReport{
		Path: c.Path, ProposalPath: "review/" + proposal.ID + ".md",
		Change: result.Change, Warnings: result.Warnings,
		InputTokens: result.InputTokens, OutputTokens: result.OutputTokens,
	}
	return pr, pageOutcome{proposal: proposal}
}

func toExistingClaims(claims []claimFrontmatter) []ExistingClaim {
	out := make([]ExistingClaim, len(claims))
	for i, c := range claims {
		out[i] = ExistingClaim{ID: c.ID, Text: c.Text, Status: c.Status}
	}
	return out
}

// candidatePages lists every page eligible for extract_claims: an indexed-trait
// type, optionally restricted to one scope, sorted by path for deterministic runs.
func candidatePages(pages store.PageStore, reg *registry.Registry, scope string) ([]candidate, error) {
	paths, err := pages.List("")
	if err != nil {
		return nil, fmt.Errorf("list pages: %w", err)
	}

	var out []candidate
	for _, p := range paths {
		c, ok, err := loadCandidate(pages, reg, p)
		if err != nil {
			return nil, err
		}
		if !ok || (scope != "" && c.Scope != scope) {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// loadCandidate reads and decodes one page, reporting ok=false for anything
// extract_claims does not apply to: a non-markdown path, a proposal file, a page
// with no frontmatter, or a type that doesn't carry the "indexed" trait. Those
// are not errors — most of a vault's files are exactly one of these. A real I/O
// failure reading a path that List just returned is a genuine error and is
// propagated rather than swallowed.
func loadCandidate(pages store.PageStore, reg *registry.Registry, p string) (candidate, bool, error) {
	if !isEligiblePath(p) {
		return candidate{}, false, nil
	}
	raw, version, err := pages.Read(p)
	if err != nil {
		return candidate{}, false, fmt.Errorf("read %s: %w", p, err)
	}
	page, err := vault.Parse(string(raw))
	if err != nil || page.Meta == nil {
		return candidate{}, false, nil
	}
	var fm pageFrontmatter
	if err := page.Decode(&fm); err != nil {
		return candidate{}, false, nil
	}
	if fm.Type == "" || !reg.Traits(fm.Type)["indexed"] {
		return candidate{}, false, nil
	}

	typeDef, _ := reg.Get(fm.Type)
	_, maxClaims := reg.Limits(fm.Type)
	return candidate{
		Path: p, Scope: orDefault(fm.Scope, vault.ScopeOf(p)), Slug: orDefault(fm.Slug, vault.SlugFromFilename(path.Base(p))),
		TypeName: fm.Type, TypeDesc: typeDef.Description, Title: fm.Title, Body: page.Body,
		Claims: fm.Claims, MaxClaims: maxClaims, BodyHash: bodyHash(page.Body), Version: version,
	}, true, nil
}

// isEligiblePath excludes proposals, vault registry files and generated indexes.
// vault.IsReservedPath is the one authoritative list of non-scope top-level entries (04
// section 3.1); internal/api's isRealPage (home.go) builds on the same helper rather than
// each package keeping its own copy of which prefixes are reserved.
func isEligiblePath(p string) bool {
	return strings.HasSuffix(p, ".md") && path.Base(p) != "index.md" && !vault.IsReservedPath(p)
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func bodyHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}
