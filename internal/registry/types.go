// Package registry loads the user-definable ontology: types, facets and spaces.
package registry

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"gopkg.in/yaml.v3"
)

//go:embed schema/type.schema.json
var schemaFS embed.FS

const (
	defaultMaxTokens = 1200
	defaultMaxClaims = 6
)

// TypeDef is one page type. Engine behaviour comes from Traits, never from Name —
// see internal/guard/neutral_test.go.
type TypeDef struct {
	Name             string
	Description      string
	Folder           string
	Traits           map[string]bool
	Fields           map[string]FieldDef
	MaxTokens        int
	MaxClaims        int
	StalenessDays    int
	OverridesDefault bool
}

// FieldDef is one typed frontmatter attribute.
type FieldDef struct {
	Type string `yaml:"type"`
	To   string `yaml:"to"`
	Role string `yaml:"role"`
}

// Registry is the loaded set of type definitions.
type Registry struct {
	types  map[string]TypeDef
	errors []string
}

type rawType struct {
	Name          string              `yaml:"name"`
	Description   string              `yaml:"description"`
	Folder        string              `yaml:"folder"`
	Traits        []string            `yaml:"traits"`
	Fields        map[string]FieldDef `yaml:"fields"`
	MaxTokens     int                 `yaml:"max_tokens"`
	MaxClaims     int                 `yaml:"max_claims"`
	StalenessDays int                 `yaml:"staleness_days"`
}

// Load reads every *.yaml in dirs. Later directories override earlier ones, so a user
// file shadows a shipped default. A malformed file is recorded in Errors and skipped;
// it never fails the load, because 04 section 8 requires the registry to stay usable.
func Load(dirs ...string) (*Registry, error) {
	schema, err := compileSchema()
	if err != nil {
		return nil, err
	}
	reg := &Registry{types: map[string]TypeDef{}}

	for _, dir := range dirs {
		entries, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
		if err != nil {
			return nil, fmt.Errorf("glob %s: %w", dir, err)
		}
		sort.Strings(entries)
		for _, path := range entries {
			reg.loadOne(path, schema)
		}
	}
	return reg, nil
}

func (r *Registry) loadOne(path string, schema *jsonschema.Schema) {
	body, err := os.ReadFile(path)
	if err != nil {
		r.errors = append(r.errors, fmt.Sprintf("%s: %v", path, err))
		return
	}
	var generic any
	if err := yaml.Unmarshal(body, &generic); err != nil {
		r.errors = append(r.errors, fmt.Sprintf("%s: %v", path, err))
		return
	}
	if err := schema.Validate(normaliseForJSON(generic)); err != nil {
		r.errors = append(r.errors, fmt.Sprintf("%s: %s", path, describeSchemaError(err)))
		return
	}
	var raw rawType
	if err := yaml.Unmarshal(body, &raw); err != nil {
		r.errors = append(r.errors, fmt.Sprintf("%s: %v", path, err))
		return
	}

	traits := make(map[string]bool, len(raw.Traits))
	for _, trait := range raw.Traits {
		traits[trait] = true
	}
	_, existed := r.types[raw.Name]
	def := TypeDef{
		Name:             raw.Name,
		Description:      raw.Description,
		Folder:           orDefault(raw.Folder, raw.Name+"s/"),
		Traits:           traits,
		Fields:           raw.Fields,
		MaxTokens:        orDefaultInt(raw.MaxTokens, defaultMaxTokens),
		MaxClaims:        orDefaultInt(raw.MaxClaims, defaultMaxClaims),
		StalenessDays:    raw.StalenessDays,
		OverridesDefault: existed,
	}
	r.types[raw.Name] = def
}

// Get returns a type by name. The returned TypeDef's Traits and Fields maps are
// copies, so a caller cannot reach through them into the registry's own state.
func (r *Registry) Get(name string) (TypeDef, bool) {
	def, ok := r.types[name]
	if !ok {
		return TypeDef{}, false
	}
	def.Traits = copyTraits(def.Traits)
	def.Fields = copyFields(def.Fields)
	return def, true
}

// Names returns every loaded type name, sorted.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.types))
	for name := range r.types {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Traits returns a copy of the trait set for a type, or an empty set when unknown.
// It is a copy so a caller mutating the result cannot corrupt the registry's own state.
func (r *Registry) Traits(name string) map[string]bool {
	if def, ok := r.types[name]; ok {
		return copyTraits(def.Traits)
	}
	return map[string]bool{}
}

// Limits returns max tokens and max claims, falling back to defaults for an unknown type.
// An unknown type must still index — see the domain-neutrality guard.
func (r *Registry) Limits(name string) (int, int) {
	if def, ok := r.types[name]; ok {
		return def.MaxTokens, def.MaxClaims
	}
	return defaultMaxTokens, defaultMaxClaims
}

// Errors returns one message per file that failed to load.
func (r *Registry) Errors() []string { return append([]string(nil), r.errors...) }

// Fingerprint returns a comparable string capturing exactly the parts of typeName's
// definition that feed indexer.validationFindings: its token/claim limits (Limits) and its
// "indexed" trait (the only trait validationFindings reads, for missing_claims), plus
// whether the type is defined at all. That last part matters on its own: Limits and Traits
// both fall back to package defaults for an unknown type (see their own doc comments), so
// deleting a type's yaml file changes a page's effective limits without changing a single
// field on any remaining TypeDef — a fingerprint built only from the resolved numbers could
// miss it if the deleted type's limits happened to already match the defaults.
//
// This governs indexer's skip-unchanged optimisation: a page is re-evaluated whenever its
// type's fingerprint changes, even if the page's own content and git version have not. It is
// deliberately narrow — Description, Folder, StalenessDays, Fields and every trait besides
// "indexed" are left out because nothing in validationFindings (or EdgesFrom) reads them for
// finding purposes today [Fields does feed EdgesFrom's ref-edge detection, a separate,
// pre-existing staleness gap this fingerprint does not attempt to close]; claim_rules is
// accepted by the type schema but not yet parsed into TypeDef at all, so it cannot affect a
// finding either. Including any of those would force every page of a type to be re-evaluated
// for config churn that cannot change what a reindex reports.
func (r *Registry) Fingerprint(typeName string) string {
	_, known := r.types[typeName]
	maxTokens, maxClaims := r.Limits(typeName)
	indexed := r.Traits(typeName)["indexed"]
	return fmt.Sprintf("known=%t;tokens=%d;claims=%d;indexed=%t", known, maxTokens, maxClaims, indexed)
}

func compileSchema() (*jsonschema.Schema, error) {
	body, err := schemaFS.ReadFile("schema/type.schema.json")
	if err != nil {
		return nil, fmt.Errorf("read meta-schema: %w", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse meta-schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("type.schema.json", doc); err != nil {
		return nil, fmt.Errorf("add meta-schema: %w", err)
	}
	schema, err := compiler.Compile("type.schema.json")
	if err != nil {
		return nil, fmt.Errorf("compile meta-schema: %w", err)
	}
	return schema, nil
}

// describeSchemaError renders a jsonschema validation failure together with the
// actual rejected value(s), since the library's own message only lists what was
// allowed (e.g. "value must be one of 'indexed', ...") and not what was found —
// callers such as TestInvalidTraitIsReportedNotFatal need the bad value surfaced.
func describeSchemaError(err error) string {
	valErr, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return err.Error()
	}
	rejected := rejectedValues(valErr)
	if len(rejected) == 0 {
		return err.Error()
	}
	return fmt.Sprintf("%s (rejected value(s): %s)", err, strings.Join(rejected, ", "))
}

// rejectedValues walks a ValidationError's causes and collects the values that
// failed an "enum" check.
func rejectedValues(valErr *jsonschema.ValidationError) []string {
	var out []string
	if enum, ok := valErr.ErrorKind.(*kind.Enum); ok {
		out = append(out, fmt.Sprintf("%v", enum.Got))
	}
	for _, cause := range valErr.Causes {
		out = append(out, rejectedValues(cause)...)
	}
	return out
}

// normaliseForJSON converts yaml's map[string]any / []any tree into the shape the
// JSON Schema validator expects. yaml.v3 already yields map[string]any, so this only
// has to recurse and leave values alone.
func normaliseForJSON(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[key] = normaliseForJSON(value)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, value := range typed {
			out[i] = normaliseForJSON(value)
		}
		return out
	case int:
		return float64(typed)
	default:
		return v
	}
}

// copyTraits returns a shallow copy of a trait set, so callers cannot mutate the
// registry's own map through a returned reference.
func copyTraits(traits map[string]bool) map[string]bool {
	out := make(map[string]bool, len(traits))
	for name, value := range traits {
		out[name] = value
	}
	return out
}

// copyFields returns a shallow copy of a field map. FieldDef is a flat value struct,
// so copying the map is enough to isolate the registry's own state.
func copyFields(fields map[string]FieldDef) map[string]FieldDef {
	out := make(map[string]FieldDef, len(fields))
	for name, def := range fields {
		out[name] = def
	}
	return out
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func orDefaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}
