package compile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// claimsChangeSchemaJSON is the single source of truth for the shape of an
// extract_claims response. It is compiled once into a *jsonschema.Schema for
// validating the model's answer, and decoded once into a map[string]any for the
// Anthropic API's tool.input_schema parameter, so the two can never drift apart.
//
// Its shape mirrors 04-technical-spec-v1.md section 3.5's
// change.claims{keep,reword,retire,add} exactly: the model's answer requires no
// reshaping before it becomes a proposal file, and the four buckets are exactly
// how intra-page supersession is expressed (a claim moves to retire, others stay
// in keep/reword, and add can itself declare a superseded claim the model newly
// noticed evidence for).
const claimsChangeSchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://balise.dev/schema/claims-change.json",
  "type": "object",
  "additionalProperties": false,
  "required": ["keep", "reword", "retire", "add"],
  "properties": {
    "keep": {
      "type": "array",
      "description": "IDs of existing claims that remain correct exactly as written.",
      "items": {"type": "string", "minLength": 1}
    },
    "reword": {
      "type": "array",
      "description": "Existing claims that are still true but should be restated.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id", "text"],
        "properties": {
          "id": {"type": "string", "minLength": 1},
          "text": {"type": "string", "minLength": 1}
        }
      }
    },
    "retire": {
      "type": "array",
      "description": "Existing claims that are no longer true (superseded).",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id"],
        "properties": {
          "id": {"type": "string", "minLength": 1},
          "reason": {"type": "string"}
        }
      }
    },
    "add": {
      "type": "array",
      "description": "Newly observed claims not present in the existing list.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["text", "status"],
        "properties": {
          "text": {"type": "string", "minLength": 1},
          "status": {"type": "string", "enum": ["active", "superseded"]},
          "span": {
            "type": ["array", "null"],
            "items": {"type": "integer"},
            "minItems": 2,
            "maxItems": 2
          }
        }
      }
    }
  }
}`

var (
	schemaOnce        sync.Once
	compiledSchema    *jsonschema.Schema
	compiledSchemaMap map[string]any
	schemaCompileErr  error
)

// claimsChangeSchema returns the compiled validator, compiling it exactly once
// per process.
func claimsChangeSchema() (*jsonschema.Schema, error) {
	schemaOnce.Do(compileClaimsChangeSchema)
	if schemaCompileErr != nil {
		return nil, schemaCompileErr
	}
	return compiledSchema, nil
}

// claimsChangeInputSchema returns the map[string]any shape Anthropic's
// tool.input_schema parameter expects, decoded from the same JSON text
// claimsChangeSchema compiles.
func claimsChangeInputSchema() (map[string]any, error) {
	schemaOnce.Do(compileClaimsChangeSchema)
	if schemaCompileErr != nil {
		return nil, schemaCompileErr
	}
	return compiledSchemaMap, nil
}

func compileClaimsChangeSchema() {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(claimsChangeSchemaJSON)))
	if err != nil {
		schemaCompileErr = fmt.Errorf("parse claims-change schema: %w", err)
		return
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("claims-change.json", doc); err != nil {
		schemaCompileErr = fmt.Errorf("add claims-change schema resource: %w", err)
		return
	}
	schema, err := compiler.Compile("claims-change.json")
	if err != nil {
		schemaCompileErr = fmt.Errorf("compile claims-change schema: %w", err)
		return
	}
	var asMap map[string]any
	if err := json.Unmarshal([]byte(claimsChangeSchemaJSON), &asMap); err != nil {
		schemaCompileErr = fmt.Errorf("decode claims-change schema as map: %w", err)
		return
	}
	compiledSchema = schema
	compiledSchemaMap = asMap
}
