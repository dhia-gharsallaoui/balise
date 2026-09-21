package indexer

import (
	"sort"

	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/vault"
	"gopkg.in/yaml.v3"
)

// Edge is one relation extracted from a page, before resolution.
type Edge struct {
	ToRef string
	Kind  string
	Field string
}

// EdgesFrom reads typed refs from frontmatter and untyped links from the body.
// Roles come from the type's field definitions, never from the field name. An
// unknown type yields no typed edges — reg.Get reports ok == false, so the ref-field
// loop is skipped — but body links still work, since a page must remain indexable
// even under a type the registry does not know (see the domain-neutrality guard).
//
// The result is sorted (ToRef, then Kind, then Field) before it is returned. def.Fields
// is a map, so the ref-field loop below visits fields in random order on every call;
// without sorting here, that randomness would reach whatever writes these edges to
// Postgres. A deterministic reindex — dropping the database and rebuilding it byte-for-
// byte the same — is an acceptance criterion for this project, so EdgesFrom removes the
// nondeterminism at the source rather than trusting every future caller to re-sort.
func EdgesFrom(page vault.Page, reg *registry.Registry) []Edge {
	var edges []Edge

	typeName := ""
	if node := page.Get("type"); node != nil {
		typeName = node.Value
	}
	if def, ok := reg.Get(typeName); ok {
		for name, field := range def.Fields {
			if field.Type != "ref" && field.Type != "ref[]" {
				continue
			}
			role := field.Role
			if role == "" {
				role = "about"
			}
			for _, target := range refValues(page.Get(name)) {
				edges = append(edges, Edge{ToRef: target, Kind: role, Field: name})
			}
		}
	}
	for _, ref := range vault.ExtractLinks(page.Body) {
		edges = append(edges, Edge{ToRef: ref, Kind: "link"})
	}
	return sortEdges(dedupe(edges))
}

// sortEdges orders edges by ToRef, then Kind, then Field, so callers — and diffs
// across reindexes — see the same order regardless of map-iteration order upstream.
func sortEdges(edges []Edge) []Edge {
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].ToRef != edges[j].ToRef {
			return edges[i].ToRef < edges[j].ToRef
		}
		if edges[i].Kind != edges[j].Kind {
			return edges[i].Kind < edges[j].Kind
		}
		return edges[i].Field < edges[j].Field
	})
	return edges
}

func refValues(node *yaml.Node) []string {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Value == "" || node.Tag == "!!null" {
			return nil
		}
		return []string{node.Value}
	case yaml.SequenceNode:
		var out []string
		for _, item := range node.Content {
			if item.Kind == yaml.ScalarNode && item.Value != "" {
				out = append(out, item.Value)
			}
		}
		return out
	default:
		return nil
	}
}

func dedupe(edges []Edge) []Edge {
	seen := map[Edge]bool{}
	var out []Edge
	for _, edge := range edges {
		if seen[edge] {
			continue
		}
		seen[edge] = true
		out = append(out, edge)
	}
	return out
}
