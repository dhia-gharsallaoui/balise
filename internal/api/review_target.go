package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/dhia/balise/internal/compile"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/vault"
	"gopkg.in/yaml.v3"
)

// reviewTarget is the page a proposal applies to, as accept, edit-accept and the detail view
// all need it: where it lives, its current content, and the version the staleness check
// compares against. For a new_page proposal the page does not exist yet: it is assembled from
// the proposal with no claims, so applying Change.Claims.Add to it goes through exactly the
// same ApplyClaims path an add to an existing page does.
type reviewTarget struct {
	path    string
	page    vault.Page
	version string
	isNew   bool
}

// reviewNewPageJSON is what the Review screen shows for a new_page proposal in place of a
// before/after diff of an existing page.
type reviewNewPageJSON struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	Path  string `json:"path"`
	Body  string `json:"body"`
}

// resolveReviewTarget returns the page proposal applies to, or the HTTP status and reason it
// cannot be applied. For an existing page that is the stale check every accept shares; for a
// new page it is the checks that keep acceptance from overwriting anything.
func (s *server) resolveReviewTarget(proposal compile.Proposal) (reviewTarget, int, error) {
	if proposal.Kind == compile.KindNewPage {
		return s.newPageTarget(proposal)
	}
	pagePath, page, version, err := findPageBySlug(s.pages, proposal.Scope, proposal.Target)
	if err != nil {
		return reviewTarget{}, http.StatusNotFound, errors.New("target page not found")
	}
	// Fix 1, part B: verify the target still matches what this proposal was built against
	// before applying anything. An empty proposal.TargetVersion (a proposal built before
	// this field existed) skips the check, the same way PageStore.Write treats an empty
	// ifVersion as "nothing to compare, allow it".
	if proposal.TargetVersion != "" && proposal.TargetVersion != version {
		return reviewTarget{}, http.StatusConflict, fmt.Errorf(
			"proposal %s is stale: %s/%s changed since this proposal was built (expected version %s, found %s); reject this proposal and re-run compile",
			proposal.ID, proposal.Scope, proposal.Target, proposal.TargetVersion, version)
	}
	return reviewTarget{path: pagePath, page: page, version: version}, 0, nil
}

// newPageTarget assembles the page a new_page proposal would create, after every check that
// keeps acceptance from overwriting anything: the slug may not already be taken in the scope,
// by any page in any folder, because accepting would otherwise either overwrite that page or
// create a second page answering to the same slug (409).
func (s *server) newPageTarget(proposal compile.Proposal) (reviewTarget, int, error) {
	target, status, err := s.describeNewPage(proposal)
	if err != nil {
		return reviewTarget{}, status, err
	}
	if _, _, _, err := findPageBySlug(s.pages, proposal.Scope, proposal.Target); err == nil {
		return reviewTarget{}, http.StatusConflict, fmt.Errorf(
			"a page with slug %q already exists in %s; reject this proposal", proposal.Target, proposal.Scope)
	}
	if _, _, err := s.pages.Read(target.path); err == nil || !errors.Is(err, os.ErrNotExist) {
		return reviewTarget{}, http.StatusConflict, fmt.Errorf("%s already exists; reject this proposal", target.path)
	}
	return target, 0, nil
}

// describeNewPage builds the would-be page and its path without the conflict checks, which is
// all the detail view needs: a proposal whose slug has since been taken must still be
// viewable, so the reviewer can see it and reject it.
func (s *server) describeNewPage(proposal compile.Proposal) (reviewTarget, int, error) {
	if proposal.Page == nil {
		return reviewTarget{}, http.StatusUnprocessableEntity, errors.New("new_page proposal has no page")
	}
	if err := vault.ValidateSegment(proposal.Scope); err != nil {
		return reviewTarget{}, http.StatusUnprocessableEntity, fmt.Errorf("scope: %w", err)
	}
	if err := vault.ValidateSegment(proposal.Target); err != nil {
		return reviewTarget{}, http.StatusUnprocessableEntity, fmt.Errorf("slug: %w", err)
	}
	reg, err := registry.Load(filepath.Join(s.defaultsDir, "types"))
	if err != nil {
		return reviewTarget{}, http.StatusInternalServerError, fmt.Errorf("load types: %w", err)
	}
	def, ok := reg.Get(proposal.Page.Type)
	if !ok || !def.Traits["indexed"] {
		return reviewTarget{}, http.StatusUnprocessableEntity,
			fmt.Errorf("type %q is not a known indexed page type", proposal.Page.Type)
	}

	folder := strings.Trim(def.Folder, "/")
	prefix := proposal.Scope + "/"
	if folder != "" {
		prefix += folder + "/"
	}
	pagePath := path.Clean(prefix + proposal.Target + ".md")
	if !strings.HasPrefix(pagePath, prefix) {
		return reviewTarget{}, http.StatusUnprocessableEntity, fmt.Errorf("path %q escapes %q", pagePath, prefix)
	}
	page, err := renderNewPage(proposal)
	if err != nil {
		return reviewTarget{}, http.StatusInternalServerError, err
	}
	return reviewTarget{path: pagePath, page: page, isNew: true}, 0, nil
}

// newPageMeta is a new page's frontmatter, in the order every hand-written page uses. The
// claims key is added afterwards by page.With, exactly as for an existing page.
type newPageMeta struct {
	UID   string `yaml:"uid"`
	Slug  string `yaml:"slug"`
	Type  string `yaml:"type"`
	Scope string `yaml:"scope"`
	Title string `yaml:"title"`
}

func renderNewPage(proposal compile.Proposal) (vault.Page, error) {
	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	meta := newPageMeta{
		UID: vault.NewUID(), Slug: proposal.Target, Type: proposal.Page.Type,
		Scope: proposal.Scope, Title: proposal.Page.Title,
	}
	if err := enc.Encode(meta); err != nil {
		return vault.Page{}, fmt.Errorf("encode new page frontmatter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return vault.Page{}, fmt.Errorf("close new page encoder: %w", err)
	}
	body := strings.TrimSpace(proposal.Page.Body) + "\n"
	page, err := vault.Parse("---\n" + sb.String() + "---\n" + body)
	if err != nil {
		return vault.Page{}, fmt.Errorf("parse new page: %w", err)
	}
	return page, nil
}

// newPageJSON is the detail view's description of the page a new_page proposal creates, or
// nil for every other kind.
func newPageJSON(proposal compile.Proposal, target reviewTarget) *reviewNewPageJSON {
	if !target.isNew || proposal.Page == nil {
		return nil
	}
	return &reviewNewPageJSON{
		Type: proposal.Page.Type, Title: proposal.Page.Title, Path: target.path,
		Body: strings.TrimSpace(proposal.Page.Body),
	}
}
