// Package vault reads and writes the markdown pages that are the system of record.
package vault

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const delim = "---"

// Page is a parsed markdown page. Meta is a YAML mapping node rather than a map so
// that key order and unknown keys survive a round trip — 04 section 3.2 requires that
// unknown fields are kept, and a map would silently reorder every reindex.
type Page struct {
	Meta *yaml.Node
	Body string
}

// Parse splits frontmatter from body. A page without frontmatter is not an error.
func Parse(text string) (Page, error) {
	if !strings.HasPrefix(text, delim) {
		return Page{Body: text}, nil
	}
	rest := text[len(delim):]
	end := strings.Index(rest, "\n"+delim)
	if end == -1 {
		return Page{Body: text}, nil
	}
	head, body := rest[:end], rest[end+len(delim)+1:]

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(head), &doc); err != nil {
		return Page{}, fmt.Errorf("frontmatter: %w", err)
	}
	if len(doc.Content) == 0 {
		return Page{Body: strings.TrimLeft(body, "\n")}, nil
	}
	meta := doc.Content[0]
	if meta.Kind != yaml.MappingNode {
		return Page{}, fmt.Errorf("frontmatter: want a mapping, got kind %d", meta.Kind)
	}
	return Page{Meta: meta, Body: strings.TrimLeft(body, "\n")}, nil
}

// Render renders the page back to markdown, frontmatter first. Encoding failures are
// reported rather than silently dropped: a page rendered without its frontmatter would
// write to the vault with no uid, slug, type, or claims, which is silent data loss on
// the system of record.
func (p Page) Render() (string, error) {
	if p.Meta == nil {
		return p.Body, nil
	}
	var sb strings.Builder
	encoder := yaml.NewEncoder(&sb)
	encoder.SetIndent(2)
	if err := encoder.Encode(p.Meta); err != nil {
		return "", fmt.Errorf("render frontmatter: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("render frontmatter: %w", err)
	}
	return delim + "\n" + sb.String() + delim + "\n" + p.Body, nil
}

// Get returns the value node for a top-level key, or nil.
func (p Page) Get(key string) *yaml.Node {
	if p.Meta == nil {
		return nil
	}
	for i := 0; i+1 < len(p.Meta.Content); i += 2 {
		if p.Meta.Content[i].Value == key {
			return p.Meta.Content[i+1]
		}
	}
	return nil
}

// Decode unmarshals the frontmatter into v, ignoring keys v does not declare.
func (p Page) Decode(v any) error {
	if p.Meta == nil {
		return nil
	}
	if err := p.Meta.Decode(v); err != nil {
		return fmt.Errorf("decode frontmatter: %w", err)
	}
	return nil
}

// With returns a copy carrying key=value. The receiver is never modified: an existing
// key is replaced where it sits, a new one is appended, so order is stable.
func (p Page) With(key string, value any) (Page, error) {
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		return Page{}, fmt.Errorf("encode %s: %w", key, err)
	}

	meta := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if p.Meta != nil {
		meta.Content = make([]*yaml.Node, len(p.Meta.Content))
		copy(meta.Content, p.Meta.Content)
	}
	for i := 0; i+1 < len(meta.Content); i += 2 {
		if meta.Content[i].Value == key {
			meta.Content[i+1] = &node
			return Page{Meta: meta, Body: p.Body}, nil
		}
	}
	meta.Content = append(meta.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &node)
	return Page{Meta: meta, Body: p.Body}, nil
}
