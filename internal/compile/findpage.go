package compile

import (
	"fmt"
	"path"

	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/vault"
)

// FindPageBySlug locates scope's page whose slug is slug by walking the vault directly rather
// than querying the index. It is scope-safe by construction (pages.List(scope+"/") can only
// return paths inside that one directory) and does not depend on the index being current.
// Review's accept and the MCP propose tool both resolve pages through it, so they can never
// disagree about whether a page exists. The returned version is the page's PageStore version
// (git blob SHA), the baseline a claims proposal's TargetVersion pins.
func FindPageBySlug(pages store.PageStore, scope, slug string) (string, vault.Page, string, error) {
	paths, err := pages.List(scope + "/")
	if err != nil {
		return "", vault.Page{}, "", fmt.Errorf("list %s: %w", scope, err)
	}
	for _, p := range paths {
		if !isEligiblePath(p) {
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
		if orDefault(fm.Slug, vault.SlugFromFilename(path.Base(p))) == slug {
			return p, page, version, nil
		}
	}
	return "", vault.Page{}, "", fmt.Errorf("no page with slug %q in scope %q", slug, scope)
}
