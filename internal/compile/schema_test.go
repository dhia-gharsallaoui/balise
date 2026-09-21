package compile

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func decodeAsAny(t *testing.T, jsonText string) any {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal([]byte(jsonText), &v))
	return v
}

func TestClaimsChangeSchemaAcceptsAWellFormedResponse(t *testing.T) {
	schema, err := claimsChangeSchema()
	require.NoError(t, err)

	value := decodeAsAny(t, `{
		"keep": ["c1"],
		"reword": [{"id": "c2", "text": "AzAPI PUT on an ER gateway deletes all expressRouteConnections"}],
		"retire": [{"id": "c3", "reason": "no longer observed"}],
		"add": [{"text": "azapi_update_resource keeps connections intact", "status": "active", "span": [10, 12]}]
	}`)
	require.NoError(t, schema.Validate(value))
}

func TestClaimsChangeSchemaAcceptsAllEmptyBuckets(t *testing.T) {
	schema, err := claimsChangeSchema()
	require.NoError(t, err)

	value := decodeAsAny(t, `{"keep": [], "reword": [], "retire": [], "add": []}`)
	require.NoError(t, schema.Validate(value))
}

func TestClaimsChangeSchemaRejectsMissingBucket(t *testing.T) {
	schema, err := claimsChangeSchema()
	require.NoError(t, err)

	value := decodeAsAny(t, `{"keep": [], "reword": [], "retire": []}`)
	require.Error(t, schema.Validate(value))
}

func TestClaimsChangeSchemaRejectsUnknownTopLevelField(t *testing.T) {
	schema, err := claimsChangeSchema()
	require.NoError(t, err)

	value := decodeAsAny(t, `{"keep": [], "reword": [], "retire": [], "add": [], "extra": true}`)
	require.Error(t, schema.Validate(value))
}

func TestClaimsChangeSchemaRejectsInvalidAddStatus(t *testing.T) {
	schema, err := claimsChangeSchema()
	require.NoError(t, err)

	value := decodeAsAny(t, `{"keep": [], "reword": [], "retire": [],
		"add": [{"text": "some claim", "status": "maybe"}]}`)
	require.Error(t, schema.Validate(value))
}

func TestClaimsChangeSchemaAllowsNullSpan(t *testing.T) {
	schema, err := claimsChangeSchema()
	require.NoError(t, err)

	value := decodeAsAny(t, `{"keep": [], "reword": [], "retire": [],
		"add": [{"text": "some claim", "status": "active", "span": null}]}`)
	require.NoError(t, schema.Validate(value))
}

func TestClaimsChangeInputSchemaMatchesTheValidatorShape(t *testing.T) {
	asMap, err := claimsChangeInputSchema()
	require.NoError(t, err)
	require.Equal(t, "object", asMap["type"])

	props, ok := asMap["properties"].(map[string]any)
	require.True(t, ok, "properties must decode as a map")
	for _, key := range []string{"keep", "reword", "retire", "add"} {
		require.Contains(t, props, key)
	}
}
