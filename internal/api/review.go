package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dhia/balise/internal/cli"
	"github.com/dhia/balise/internal/compile"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/vault"
)

// This file is the review inbox's write side (accept/reject) plus the two reads the Review
// screen (02-ui-design-v1.md section 5.3) needs beyond what /api/home already returns:
// evidence, before/after claims marked by word, and confidence rendered as a word rather
// than the raw number /api/home's own "Waiting for you" preview carries (that endpoint is
// untouched — this task is the Review screen, not Home's widget).

// reviewListJSON is one row of the review queue (GET /api/review) — 02 section 5.3's left
// column: kind, target and confidence as a word only. Never the raw float: "confidence as a
// word (likely / possible / unsure — never a percentage in the row)" is 02 section 5.3's own
// rule, and section 5.3's table lists "percent confidence... anywhere" among the things that
// don't help a person decide.
type reviewListJSON struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Scope       string `json:"scope"`
	Target      string `json:"target"`
	TargetTitle string `json:"target_title"`
	Confidence  string `json:"confidence"`
	CreatedBy   string `json:"created_by"`
}

// reviewEvidenceJSON cites the source span a proposal drew a claim from — the review
// screen's right column shows evidence before the diff (section 5.3: "evidence first").
type reviewEvidenceJSON struct {
	Source string `json:"source"`
	Span   []int  `json:"span,omitempty"`
	Text   string `json:"text"`
}

// reviewClaimJSON is one claim in a before/after list, carrying the word ("unchanged",
// "added", "reworded", "retired") the UI marks it with — never left for the frontend to
// infer by diffing the two lists itself.
type reviewClaimJSON struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Status string `json:"status"`
	AsOf   string `json:"as_of,omitempty"`
	Mark   string `json:"mark"`
}

// reviewDetailJSON is GET /api/review/{id}'s body: everything the right column needs to
// render one item without a second round trip.
type reviewDetailJSON struct {
	ID          string               `json:"id"`
	Kind        string               `json:"kind"`
	Scope       string               `json:"scope"`
	Target      string               `json:"target"`
	TargetTitle string               `json:"target_title"`
	Confidence  string               `json:"confidence"`
	CreatedBy   string               `json:"created_by"`
	Evidence    []reviewEvidenceJSON `json:"evidence"`
	Before      []reviewClaimJSON    `json:"before"`
	After       []reviewClaimJSON    `json:"after"`
}

func (s *server) handleReviewList(w http.ResponseWriter, r *http.Request) {
	items, err := s.reviewQueue(r.Context())
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposals": items})
}

// reviewQueue lists every pending proposal the caller's scopes may see, exactly like
// homeWaiting (home.go) but rendering confidence as a word and resolving a best-effort page
// title. It intentionally does not share homeWaiting's aggregate: that one groups by kind for
// Home's dashboard tiles, this one is a flat, sorted list for the review queue's left column.
func (s *server) reviewQueue(ctx context.Context) ([]reviewListJSON, error) {
	if s.pages == nil {
		return []reviewListJSON{}, nil
	}
	paths, err := s.pages.List("review/")
	if err != nil {
		return nil, fmt.Errorf("list proposals: %w", err)
	}
	allowed := s.q().AllowedScopes()

	items := []reviewListJSON{}
	for _, p := range paths {
		if !strings.HasSuffix(p, ".md") || strings.HasPrefix(p, "review/.rejected/") {
			continue
		}
		proposal, ok, err := readProposal(s.pages, p)
		if err != nil {
			return nil, err
		}
		if !ok || !scopeAllowed(allowed, proposal.Scope) || proposal.Status != "pending" {
			continue
		}
		items = append(items, reviewListJSON{
			ID: proposal.ID, Kind: proposal.Kind, Scope: proposal.Scope, Target: proposal.Target,
			TargetTitle: s.pageTitle(ctx, proposal.Scope, proposal.Target),
			Confidence:  confidenceWord(proposal.Confidence),
			CreatedBy:   proposal.CreatedBy,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

// pageTitle is a best-effort, read-only lookup through the index for display purposes only —
// never used to decide whether a proposal may be shown or applied, since the index can be
// behind (see reindexAfterAccept). A miss falls back to the raw slug rather than failing the
// whole queue over one unindexed or renamed page.
func (s *server) pageTitle(ctx context.Context, scope, slug string) string {
	page, err := s.q().GetPage(ctx, scope, slug)
	if err != nil || page == nil || page.Title == "" {
		return slug
	}
	return page.Title
}

func (s *server) handleReviewItem(w http.ResponseWriter, r *http.Request) {
	proposal, proposalPath, status, err := s.loadPendingProposal(r.PathValue("id"))
	if err != nil {
		writeReviewLoadError(w, r, status, err)
		return
	}
	_ = proposalPath

	pagePath, page, _, err := findPageBySlug(s.pages, proposal.Scope, proposal.Target)
	if err != nil {
		writeError(w, http.StatusNotFound, "target page not found")
		return
	}
	_ = pagePath

	existing, err := existingClaims(page)
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	applied, err := compile.ApplyClaims(existing, proposal.Change.Claims, time.Now())
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	detail := reviewDetailJSON{
		ID: proposal.ID, Kind: proposal.Kind, Scope: proposal.Scope, Target: proposal.Target,
		TargetTitle: s.pageTitle(r.Context(), proposal.Scope, proposal.Target),
		Confidence:  confidenceWord(proposal.Confidence),
		CreatedBy:   proposal.CreatedBy,
		Evidence:    evidenceJSON(proposal.Evidence),
		Before:      claimsJSON(wrapUnchanged(existing)),
		After:       claimsJSON(applied),
	}
	writeJSON(w, http.StatusOK, detail)
}

// handleReviewAccept applies a proposal's change to its target page and deletes the proposal
// file, in one commit (01-design-spec-v0.4.md section 6.6 / 04 section 13). The scope check
// is the exact same scopeAllowed(s.q().AllowedScopes(), ...) boundary every other
// filesystem-backed read in this package already uses (home.go) — this task's brief is
// explicit that this is the boundary a previous review found a hole in, so accept adds no
// second path around it.
func (s *server) handleReviewAccept(w http.ResponseWriter, r *http.Request) {
	proposal, proposalPath, status, err := s.loadPendingProposal(r.PathValue("id"))
	if err != nil {
		writeReviewLoadError(w, r, status, err)
		return
	}

	pagePath, page, version, err := findPageBySlug(s.pages, proposal.Scope, proposal.Target)
	if err != nil {
		writeError(w, http.StatusNotFound, "target page not found")
		return
	}

	// Fix 1, part B: verify the target still matches what this proposal was built against
	// before applying anything. An empty proposal.TargetVersion (a proposal built before
	// this field existed) skips the check, the same way PageStore.Write treats an empty
	// ifVersion as "nothing to compare, allow it" — this only closes the gap for proposals
	// that recorded a baseline, which every proposal from this point on does.
	if proposal.TargetVersion != "" && proposal.TargetVersion != version {
		writeError(w, http.StatusConflict, fmt.Sprintf(
			"proposal %s is stale: %s/%s changed since this proposal was built (expected version %s, found %s); reject this proposal and re-run extract-claims",
			proposal.ID, proposal.Scope, proposal.Target, proposal.TargetVersion, version))
		return
	}

	existing, err := existingClaims(page)
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	applied, err := compile.ApplyClaims(existing, proposal.Change.Claims, time.Now())
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	updatedPage, err := page.With("claims", compile.Claims(applied))
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	rendered, err := updatedPage.Render()
	if err != nil {
		writeServerError(w, r, err)
		return
	}

	author := agentAuthor(proposal.CreatedBy)
	message := fmt.Sprintf("accept: %s %s", proposal.Kind, proposal.Target)
	// Exactly one Commit call, carrying both the page edit and the proposal-file deletion:
	// store.GitPageStore.Commit stages every Change and produces one SHA, so acceptance can
	// never land the page edit without also removing the proposal (or vice versa). This is
	// also what makes a second accept of the same id fail cleanly rather than double-apply:
	// once this commit lands, proposalPath no longer exists, so a repeat request's own
	// s.pages.Read (inside loadPendingProposal) returns a not-found before it ever reaches
	// ApplyClaims again.
	sha, err := s.pages.Commit([]store.Change{
		{Path: pagePath, Data: []byte(rendered)},
		{Path: proposalPath, Delete: true},
	}, author, message)
	if err != nil {
		writeServerError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"commit":      sha,
		"index_stale": s.reindexAfterAccept(r.Context()),
		"before":      claimsJSON(wrapUnchanged(existing)),
		"after":       claimsJSON(applied),
	})
}

// reviewClaimEditJSON is one entry of edit-accept's request body (04-technical-spec-v1.md
// section 13: "body = edited change"): a claim id, exactly as GET /api/review/{id}'s own
// "after" list already names it, paired with the reviewer's replacement text.
type reviewClaimEditJSON struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// reviewClaimWarningJSON flags one edited claim as usable but imperfect (currently only
// "over_word_limit"), mirroring compile.ClaimWarning's own code — an edited claim gets exactly
// the same warn-don't-block treatment a freshly proposed one gets from validateClaimText, never
// a stricter gate.
type reviewClaimWarningJSON struct {
	ID   string `json:"id"`
	Code string `json:"code"`
}

// handleReviewEditAccept is handleReviewAccept's edited sibling (04 section 13): it applies a
// proposal's change to its target page exactly like accept, but first lets the reviewer
// replace the text of one or more of the *proposal's own* added or reworded claims before the
// result is written and committed. It deliberately shares every safety property accept has —
// loadPendingProposal's scope/status checks, findPageBySlug's scope-safe lookup, and above all
// the identical TargetVersion staleness check — because editing a proposal's wording must
// never become a way to bypass the same optimistic-concurrency guard a plain accept is subject
// to.
//
// Only a claim ApplyClaims itself marked added or reworded may be edited: those are the only
// claims whose text this proposal actually introduces. A page's untouched history (unchanged)
// or a retirement's date (retired) is not "the change" the spec means by "edited change" —
// editing either would be quietly rewriting the page's own past, not correcting what the
// proposal is about to write, so applyClaimEdits refuses it (422) rather than silently
// allowing it.
//
// Provenance: the commit's author is still agentAuthor(proposal.CreatedBy) — the claim
// genuinely started life as that pipeline's proposal, and loadPendingProposal is the only way
// to reach this handler at all, so that starting point can never be lost. But the wording a
// human retyped is not the machine's own words, so the commit message names every edited claim
// id explicitly ("accept: claims foo (edited before accept: c2)") rather than silently
// crediting agent:<pipeline> with text it never produced. This mirrors how authorWord
// (home.go) already tells commits apart by *message* prefix rather than by git author alone —
// this codebase already treats the message, not just the author field, as part of a commit's
// honest record of who did what.
func (s *server) handleReviewEditAccept(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Edits []reviewClaimEditJSON `json:"edits"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	proposal, proposalPath, status, err := s.loadPendingProposal(r.PathValue("id"))
	if err != nil {
		writeReviewLoadError(w, r, status, err)
		return
	}

	pagePath, page, version, err := findPageBySlug(s.pages, proposal.Scope, proposal.Target)
	if err != nil {
		writeError(w, http.StatusNotFound, "target page not found")
		return
	}

	// Identical to handleReviewAccept's own check — see that handler's comment for why an
	// empty proposal.TargetVersion skips rather than refuses.
	if proposal.TargetVersion != "" && proposal.TargetVersion != version {
		writeError(w, http.StatusConflict, fmt.Sprintf(
			"proposal %s is stale: %s/%s changed since this proposal was built (expected version %s, found %s); reject this proposal and re-run extract-claims",
			proposal.ID, proposal.Scope, proposal.Target, proposal.TargetVersion, version))
		return
	}

	existing, err := existingClaims(page)
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	applied, err := compile.ApplyClaims(existing, proposal.Change.Claims, time.Now())
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	editedIDs, warnings, err := applyClaimEdits(applied, body.Edits)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	updatedPage, err := page.With("claims", compile.Claims(applied))
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	rendered, err := updatedPage.Render()
	if err != nil {
		writeServerError(w, r, err)
		return
	}

	author := agentAuthor(proposal.CreatedBy)
	message := fmt.Sprintf("accept: %s %s", proposal.Kind, proposal.Target)
	if len(editedIDs) > 0 {
		message = fmt.Sprintf("%s (edited before accept: %s)", message, strings.Join(editedIDs, ", "))
	}
	sha, err := s.pages.Commit([]store.Change{
		{Path: pagePath, Data: []byte(rendered)},
		{Path: proposalPath, Delete: true},
	}, author, message)
	if err != nil {
		writeServerError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"commit":      sha,
		"index_stale": s.reindexAfterAccept(r.Context()),
		"before":      claimsJSON(wrapUnchanged(existing)),
		"after":       claimsJSON(applied),
		"warnings":    warnings,
	})
}

// applyClaimEdits overrides, in place, the text of every applied claim named in edits, and
// returns the sorted list of edited ids (for the commit message) plus any over-length
// warnings. An edit naming an id ApplyClaims did not produce at all, one naming a claim marked
// unchanged or retired, or one whose text fails compile.ValidateClaimWord's hard floor
// (fewer than two words) is refused outright — never partially applied — exactly as
// compile.ApplyClaims itself refuses a reword/retire naming an id not on the page.
func applyClaimEdits(applied []compile.AppliedClaim, edits []reviewClaimEditJSON) ([]string, []reviewClaimWarningJSON, error) {
	byID := make(map[string]int, len(applied))
	for i, a := range applied {
		byID[a.Claim.ID] = i
	}

	var editedIDs []string
	var warnings []reviewClaimWarningJSON
	for _, e := range edits {
		idx, ok := byID[e.ID]
		if !ok {
			return nil, nil, fmt.Errorf("edited claim %q is not part of this proposal's result", e.ID)
		}
		mark := applied[idx].Mark
		if mark != compile.MarkAdded && mark != compile.MarkReworded {
			return nil, nil, fmt.Errorf(
				"claim %q is %s, not proposed text — only an added or reworded claim can be edited before accepting",
				e.ID, mark)
		}
		text := strings.TrimSpace(e.Text)
		problem, warningCode := compile.ValidateClaimWord(text)
		if problem != "" {
			return nil, nil, fmt.Errorf("edited claim %q %s", e.ID, problem)
		}
		if warningCode != "" {
			warnings = append(warnings, reviewClaimWarningJSON{ID: e.ID, Code: warningCode})
		}
		applied[idx].Claim.Text = text
		editedIDs = append(editedIDs, e.ID)
	}
	sort.Strings(editedIDs)
	return editedIDs, warnings, nil
}

// reindexAfterAccept reindexes the whole vault synchronously and reports whether the index is
// still stale afterward (true only if the reindex itself failed).
//
// The alternative — return a flag and leave reindexing to a background job or the next
// scheduled `balise reindex` — was rejected: F-50's own acceptance criteria says accepting a
// proposal "must actually change the page," and a user who accepts a claims edit and then
// immediately re-opens that page (or asks an agent to build a context pack from it) would see
// the pre-accept claims until some later reindex ran. Silently serving stale claims right
// after the action that was supposed to fix them would make the feature feel broken, not
// eventually-consistent.
//
// cli.Reindex does a full two-pass vault walk rather than a targeted single-page update. That
// is deliberate, not an oversight: indexer.IndexPage is a no-op for every page whose git
// blob/body hash did not change (see isNoop), so at this vault's size (138 documents) a full
// walk after one accept is cheap and reuses the exact same tested path `balise reindex`
// already runs, rather than a second, narrower single-page code path that would need to
// independently rebuild the same slug/alias context cli.Reindex's first pass builds — and
// would drift from it over time. A vault large enough for a full walk to be expensive would
// need a different answer (an async job + a stale flag the UI polls), but that is not this
// vault's problem today; the doc comment says so explicitly rather than pretending a full
// reindex scales unconditionally.
//
// index_stale=true on a reindex failure is not swept under the rug: the accept commit has
// already landed by this point (page edit + proposal deletion), so failing the whole request
// here would be a lie — the accept succeeded — while silently returning index_stale=false
// would be a different lie. The failure is logged server-side and reported to the caller as
// a stale index instead.
func (s *server) reindexAfterAccept(ctx context.Context) (stale bool) {
	if s.defaultsDir == "" {
		return true
	}
	if _, err := cli.Reindex(ctx, s.q(), s.pages, s.defaultsDir); err != nil {
		slog.Error("reindex after accept failed", "error", err)
		return true
	}
	return false
}

// handleReviewReject moves a proposal to review/.rejected/ with its one-line reason recorded,
// in one commit — same atomicity rationale as accept.
func (s *server) handleReviewReject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}
	if strings.ContainsAny(reason, "\r\n") {
		writeError(w, http.StatusBadRequest, "reason must be one line")
		return
	}

	proposal, proposalPath, status, err := s.loadPendingProposal(r.PathValue("id"))
	if err != nil {
		writeReviewLoadError(w, r, status, err)
		return
	}

	// The original rationale text (the free-form paragraph after the frontmatter fence, if
	// any) is preserved verbatim on the rejected copy — only the frontmatter is rewritten
	// (status + rejected reason), matching Proposal.Render's own shape.
	raw, _, err := s.pages.Read(proposalPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "proposal not found")
		return
	}
	parsedFile, err := vault.Parse(string(raw))
	if err != nil {
		writeServerError(w, r, err)
		return
	}

	proposal.Status = "rejected"
	proposal.Rejected = &compile.ProposalRejection{Reason: reason}
	rendered, err := proposal.Render(parsedFile.Body)
	if err != nil {
		writeServerError(w, r, err)
		return
	}

	rejectedPath := "review/.rejected/" + proposal.ID + ".md"
	// Author is the system convention used everywhere else an API request (rather than a
	// named compile pipeline) produces a commit — 01 section 6.6 only mandates
	// agent:<pipeline> for *accept*; reject has no pipeline of its own to attribute to.
	const rejectAuthor = "balise <balise@localhost>"
	message := fmt.Sprintf("reject: %s %s", proposal.Kind, proposal.Target)
	sha, err := s.pages.Commit([]store.Change{
		{Path: rejectedPath, Data: []byte(rendered)},
		{Path: proposalPath, Delete: true},
	}, rejectAuthor, message)
	if err != nil {
		writeServerError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"commit": sha,
		"path":   rejectedPath,
	})
}

var errProposalNotFound = errors.New("proposal not found")

// loadPendingProposal reads, scope-checks and status-checks the proposal named by the URL's
// {id} path value, returning the one non-recoverable HTTP status the caller should send on
// failure. A proposal outside the caller's scopes is reported identically to one that does
// not exist at all (404, via errProposalNotFound) — the same "never distinguish absent from
// out-of-scope" rule store.Queries.GetPage documents (04 section 12): a scoped caller must
// never learn that a proposal exists in a scope it cannot see.
func (s *server) loadPendingProposal(id string) (compile.Proposal, string, int, error) {
	proposalPath, err := safeProposalPath(id)
	if err != nil {
		return compile.Proposal{}, "", http.StatusBadRequest, err
	}
	proposal, ok, err := readProposal(s.pages, proposalPath)
	if err != nil {
		if isNotFound(err) {
			return compile.Proposal{}, "", http.StatusNotFound, errProposalNotFound
		}
		return compile.Proposal{}, "", http.StatusInternalServerError, err
	}
	if !ok || !scopeAllowed(s.q().AllowedScopes(), proposal.Scope) {
		return compile.Proposal{}, "", http.StatusNotFound, errProposalNotFound
	}
	if proposal.Status != "pending" {
		return compile.Proposal{}, "", http.StatusConflict,
			fmt.Errorf("proposal %s is not pending (status=%s)", id, proposal.Status)
	}
	return proposal, proposalPath, http.StatusOK, nil
}

// isNotFound reports whether err looks like a missing-file error from PageStore.Read. Both
// implementations (git.go's GitPageStore, and any future one) are expected to report a plain
// "file does not exist" condition rather than a typed sentinel today, so this checks the
// message rather than an error type — narrow, but confined to this one call site, and revisited
// the day PageStore grows a typed not-found error.
func isNotFound(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "no such file") || strings.Contains(msg, "not found") ||
		strings.Contains(msg, "does not exist") || strings.Contains(msg, "object not found")
}

// writeReviewLoadError renders loadPendingProposal's outcome as a response: status is already
// the right HTTP code, and errProposalNotFound / a bad id never get logged as server errors
// (they are ordinary client-facing conditions), while anything else still goes through
// writeServerError so it is logged with request context.
func writeReviewLoadError(w http.ResponseWriter, r *http.Request, status int, err error) {
	if status == http.StatusInternalServerError {
		writeServerError(w, r, err)
		return
	}
	writeError(w, status, err.Error())
}

var proposalIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// safeProposalPath turns a URL path value into review/<id>.md, rejecting anything that is not
// a plain token first — a slash or ".." in id could otherwise walk the read/write outside
// review/ entirely (e.g. id="../other-scope/index"), which would be a path-traversal hole
// independent of, and in addition to, the scope check every handler in this file already
// applies.
func safeProposalPath(id string) (string, error) {
	if id == "" || !proposalIDPattern.MatchString(id) {
		return "", fmt.Errorf("invalid proposal id %q", id)
	}
	return "review/" + id + ".md", nil
}

// existingClaims decodes just the claims list out of a page's frontmatter, using
// compile.ClaimRecord (which round-trips as_of and span) rather than the narrower
// claimFrontmatter types internal/compile/run.go and internal/indexer/pipeline.go each keep
// privately for their own, narrower purposes — accept must preserve every existing claim's
// as_of/span exactly, even for claims the proposal never mentions, or it would silently drop
// data no part of this change touched.
func existingClaims(page vault.Page) ([]compile.ClaimRecord, error) {
	var fm struct {
		Claims []compile.ClaimRecord `yaml:"claims"`
	}
	if err := page.Decode(&fm); err != nil {
		return nil, fmt.Errorf("decode existing claims: %w", err)
	}
	return fm.Claims, nil
}

func wrapUnchanged(records []compile.ClaimRecord) []compile.AppliedClaim {
	out := make([]compile.AppliedClaim, len(records))
	for i, c := range records {
		out[i] = compile.AppliedClaim{Claim: c, Mark: compile.MarkUnchanged}
	}
	return out
}

func claimsJSON(applied []compile.AppliedClaim) []reviewClaimJSON {
	out := make([]reviewClaimJSON, len(applied))
	for i, a := range applied {
		out[i] = reviewClaimJSON{
			ID: a.Claim.ID, Text: a.Claim.Text, Status: a.Claim.Status, AsOf: a.Claim.AsOf,
			Mark: string(a.Mark),
		}
	}
	return out
}

func evidenceJSON(evidence []compile.ProposalEvidence) []reviewEvidenceJSON {
	out := make([]reviewEvidenceJSON, len(evidence))
	for i, e := range evidence {
		out[i] = reviewEvidenceJSON{Source: e.Source, Span: e.Span, Text: e.Text}
	}
	return out
}

// confidenceWord renders a proposal's stored confidence as one of the three words 02 section
// 5.3 allows in the queue row, per the exact thresholds 04 section 3.5 documents next to the
// stored field: ">= 0.8 likely, 0.6-0.8 possible, < 0.6 unsure."
func confidenceWord(c float64) string {
	switch {
	case c >= 0.8:
		return "likely"
	case c >= 0.6:
		return "possible"
	default:
		return "unsure"
	}
}

// agentAuthor derives an accept commit's author from a proposal's created_by field (shape
// "<pipeline>:<timestamp>", e.g. "compile:2026-09-16T08:00:12Z" — BuildProposal in
// proposal.go), matching 01-design-spec-v0.4.md section 6.6's literal requirement:
// "Accepting = commit(s) with author agent:<pipeline>." The synthesized "@localhost" email
// half only exists so store.splitAuthor's "Name <email>" parsing has something to split;
// nothing in this system ever sends mail to it, matching the existing "balise
// <balise@localhost>" convention used everywhere else a commit author is not a real person.
func agentAuthor(createdBy string) string {
	pipeline := createdBy
	if i := strings.Index(createdBy, ":"); i >= 0 {
		pipeline = createdBy[:i]
	}
	name := "agent:" + pipeline
	return name + " <" + name + "@localhost>"
}

// findPageBySlug locates scope's page whose slug is slug by walking the vault directly rather
// than querying the index. This is deliberate, not a shortcut:
//
//   - It is scope-safe by construction. pages.List(scope+"/") can only ever return paths
//     physically inside that one directory, mirroring vault.ScopeOf's own directory-based
//     scope boundary (04 section 14) — there is no way for a proposal claiming scope X to
//     resolve to a page under a different scope's directory, so this lookup needs no
//     additional scope check of its own layered on top.
//   - It does not depend on the database index already being correct. Accept is itself one of
//     the things that can leave the index stale for a moment (see reindexAfterAccept); resolving
//     the target page through the index would make accept's own correctness depend on a
//     precondition accept itself is responsible for restoring.
//
// The returned version is the page's PageStore version (git blob SHA) as read here, which
// handleReviewAccept compares against a proposal's recorded TargetVersion (fix 1, part B) —
// callers that only need a read-only preview (handleReviewItem) are free to ignore it.
func findPageBySlug(pages store.PageStore, scope, slug string) (string, vault.Page, string, error) {
	paths, err := pages.List(scope + "/")
	if err != nil {
		return "", vault.Page{}, "", fmt.Errorf("list %s: %w", scope, err)
	}
	for _, p := range paths {
		if !isRealPage(p) {
			continue
		}
		raw, version, err := pages.Read(p)
		if err != nil {
			return "", vault.Page{}, "", fmt.Errorf("read %s: %w", p, err)
		}
		page, err := vault.Parse(string(raw))
		if err != nil {
			continue // malformed page: not a candidate match, not a fatal error either
		}
		var fm struct {
			Slug string `yaml:"slug"`
		}
		if err := page.Decode(&fm); err != nil {
			continue
		}
		candidate := fm.Slug
		if candidate == "" {
			candidate = vault.SlugFromFilename(path.Base(p))
		}
		if candidate == slug {
			return p, page, version, nil
		}
	}
	return "", vault.Page{}, "", fmt.Errorf("no page with slug %q in scope %q", slug, scope)
}
