package compile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// placementSchemaJSON is the single source of truth for a propose_pages answer, compiled
// once for validation and decoded once for the API's tool.input_schema, exactly as
// claimsChangeSchemaJSON is for extract_claims. Every note gets at least one decision (a dense
// note may hold several facts that belong in different places); new pages are
// declared once in new_pages and referenced by their slug from the decisions that fill them
// (04 section 9.2's "tmp_id", with the slug serving as it). A separate opaque id was tried
// first and models reliably referenced the slug instead, failing every batch.
const placementSchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://balise.dev/schema/placement.json",
  "type": "object",
  "additionalProperties": false,
  "required": ["decisions", "new_pages"],
  "properties": {
    "decisions": {
      "type": "array",
      "description": "At least one decision per note, in any order. A note holding several facts may get several attach/new_page decisions, one claim each; skip must be a note's only decision.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["note", "action", "reason"],
        "properties": {
          "note": {"type": "integer", "minimum": 1, "description": "The note's number as listed."},
          "action": {"type": "string", "enum": ["attach", "new_page", "skip"]},
          "target": {"type": "string", "description": "attach: the slug of one listed candidate page."},
          "page": {"type": "string", "description": "new_page: the slug of one entry in new_pages."},
          "claim": {"type": "string", "description": "attach/new_page: the note restated as one self-contained claim."},
          "reason": {"type": "string", "minLength": 1, "description": "One short sentence: why this action."}
        }
      }
    },
    "new_pages": {
      "type": "array",
      "description": "Pages to create for notes that belong on no listed candidate.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["type", "slug", "title", "body"],
        "properties": {
          "type": {"type": "string", "minLength": 1},
          "slug": {"type": "string", "minLength": 1},
          "title": {"type": "string", "minLength": 1},
          "body": {"type": "string", "minLength": 1}
        }
      }
    }
  }
}`

var (
	placementSchemaOnce sync.Once
	placementSchema     *jsonschema.Schema
	placementSchemaMap  map[string]any
	placementSchemaErr  error
)

// placementSchemas returns the compiled validator and the tool input-schema map, building
// both exactly once per process.
func placementSchemas() (*jsonschema.Schema, map[string]any, error) {
	placementSchemaOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(placementSchemaJSON)))
		if err != nil {
			placementSchemaErr = fmt.Errorf("parse placement schema: %w", err)
			return
		}
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource("placement.json", doc); err != nil {
			placementSchemaErr = fmt.Errorf("add placement schema resource: %w", err)
			return
		}
		if placementSchema, err = compiler.Compile("placement.json"); err != nil {
			placementSchemaErr = fmt.Errorf("compile placement schema: %w", err)
			return
		}
		if err := json.Unmarshal([]byte(placementSchemaJSON), &placementSchemaMap); err != nil {
			placementSchemaErr = fmt.Errorf("decode placement schema as map: %w", err)
		}
	})
	return placementSchema, placementSchemaMap, placementSchemaErr
}
