package registry

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxFacetDepth = 4

// facetSegment matches one ltree label exactly: Postgres's ltree extension accepts only
// alphanumeric characters and underscores per label (a hyphen is not valid ltree syntax and
// fails at the database with "ltree syntax error", not at this check) — so a hyphenated tag
// such as vendor/grafana-alloy must be rejected here as malformed, the same as any other
// syntax the ltree column could not otherwise store.
var facetSegment = regexp.MustCompile(`^[a-z0-9_]+$`)

// Facets is a loaded facet vocabulary: which tags exist and what their aliases are.
type Facets struct {
	aliases map[string]string
	known   map[string]bool
}

type rawFacetValue struct {
	Aliases  []string                 `yaml:"aliases"`
	Children map[string]rawFacetValue `yaml:"children"`
}

type rawFacet struct {
	Name   string                   `yaml:"name"`
	Values map[string]rawFacetValue `yaml:"values"`
}

// LoadFacets reads every *.yaml in dir as a facet vocabulary. A file's ".example.yaml"
// sibling is loaded in its place only when the real file is absent (see
// facetConfigFiles) — e.g. facets/customer.yaml (the owner's real, gitignored data) beats
// facets/customer.example.yaml (tracked, generic), so a fresh clone still gets a working
// default.
func LoadFacets(dir string) (*Facets, error) {
	facets := &Facets{aliases: map[string]string{}, known: map[string]bool{}}

	paths, err := facetConfigFiles(dir)
	if err != nil {
		return nil, fmt.Errorf("glob %s: %w", dir, err)
	}

	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		var raw rawFacet
		if err := yaml.Unmarshal(body, &raw); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		facets.walk(raw.Values, []string{raw.Name})
	}
	return facets, nil
}

func (f *Facets) walk(values map[string]rawFacetValue, prefix []string) {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		body := values[name]
		path := append(append([]string(nil), prefix...), name)
		full := strings.Join(path, "/")
		f.known[full] = true
		for _, alias := range body.Aliases {
			f.aliases[alias] = full
		}
		f.walk(body.Children, path)
	}
}

// ResolveAlias expands a known alias to its full path, or returns the tag unchanged.
func (f *Facets) ResolveAlias(tag string) string {
	if full, ok := f.aliases[tag]; ok {
		return full
	}
	return tag
}

// IsKnown reports whether the tag is in the vocabulary. Out-of-vocabulary tags are a
// lint finding, not an error — 04 section 7 step 9.
func (f *Facets) IsKnown(tag string) bool { return f.known[f.ResolveAlias(tag)] }

// ToLtree converts a tag to its ltree form. The bool is false when the tag is malformed,
// which the caller turns into an out_of_vocabulary finding rather than an error.
func (f *Facets) ToLtree(tag string) (string, bool) {
	segments := strings.Split(f.ResolveAlias(tag), "/")
	if len(segments) == 0 || len(segments) > maxFacetDepth {
		return "", false
	}
	for _, segment := range segments {
		if !facetSegment.MatchString(segment) {
			return "", false
		}
	}
	return strings.Join(segments, "."), true
}
