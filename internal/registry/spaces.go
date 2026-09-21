package registry

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Space is one declared, nestable saved facet filter. Soft: it never crosses a scope.
type Space struct {
	Name       string
	Scope      string
	TagFilters []string
	Children   []Space
}

// Spaces is the loaded space tree.
type Spaces struct {
	roots []Space
	flat  map[string]Space
}

type rawSpace struct {
	Name   string `yaml:"name"`
	Scope  string `yaml:"scope"`
	Filter struct {
		Tags []string `yaml:"tags"`
	} `yaml:"filter"`
	Children []rawSpace `yaml:"children"`
}

// LoadSpaces reads spaces.yaml, or spaces.example.yaml when the real file is absent (see
// realOrExample) — a fresh clone still gets a working, generic default.
func LoadSpaces(path string) (*Spaces, error) {
	path = realOrExample(path)
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var raw []rawSpace
	if err := yaml.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	spaces := &Spaces{flat: map[string]Space{}}
	for _, node := range raw {
		spaces.roots = append(spaces.roots, spaces.build(node, node.Scope, nil))
	}
	return spaces, nil
}

func (s *Spaces) build(node rawSpace, scope string, inherited []string) Space {
	filters := append(append([]string(nil), inherited...), node.Filter.Tags...)
	space := Space{Name: node.Name, Scope: scope, TagFilters: filters}
	for _, child := range node.Children {
		space.Children = append(space.Children, s.build(child, scope, filters))
	}
	s.flat[space.Name] = space
	return space
}

// Tree returns the root spaces. Each Space, and its TagFilters and Children, is a
// deep copy, so a caller cannot mutate the registry's own tree through the result.
func (s *Spaces) Tree() []Space { return copySpaces(s.roots) }

func copySpaces(spaces []Space) []Space {
	if spaces == nil {
		return nil
	}
	out := make([]Space, len(spaces))
	for i, space := range spaces {
		out[i] = Space{
			Name:       space.Name,
			Scope:      space.Scope,
			TagFilters: append([]string(nil), space.TagFilters...),
			Children:   copySpaces(space.Children),
		}
	}
	return out
}

// ScopeOf returns the scope a space belongs to, or "" when unknown.
func (s *Spaces) ScopeOf(name string) string { return s.flat[name].Scope }

// Matches reports whether a page's tags satisfy every filter on the named space.
// Filters are ANDed; within a filter, any tag may match.
//
// Space names are only unique within flat's own bookkeeping, not across the declared
// tree: defaults/spaces.yaml legitimately declares two different spaces both named
// "Azure" (one under Platform, one under a client tenant), and flat keeps whichever
// LoadSpaces visited last. A caller that already holds the actual Space node it means —
// e.g. one obtained by walking Tree() — should call that node's own Matches(tags) instead
// of this method, which cannot disambiguate a reused name.
func (s *Spaces) Matches(name string, tags []string) bool {
	space, ok := s.flat[name]
	if !ok {
		return false
	}
	return space.Matches(tags)
}

// Matches reports whether tags satisfy every one of this space's own filters (which
// already include everything inherited from its ancestors — see build). Unlike
// (*Spaces).Matches(name, tags), this checks the node itself and never goes through the
// name-keyed registry, so it stays correct even when two different branches declare a
// space with the same name.
func (sp Space) Matches(tags []string) bool {
	for _, filter := range sp.TagFilters {
		if !anyTagMatches(filter, tags) {
			return false
		}
	}
	return true
}

func anyTagMatches(pattern string, tags []string) bool {
	for _, tag := range tags {
		if tagMatches(pattern, tag) {
			return true
		}
	}
	return false
}

func tagMatches(pattern, tag string) bool {
	if prefix, ok := strings.CutSuffix(pattern, "/**"); ok {
		return tag == prefix || strings.HasPrefix(tag, prefix+"/")
	}
	return tag == pattern
}
