package stats

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResultJSONShape pins the JSON encoding of a zero-value Result against
// a golden captured from the gogoproto-generated code before the wiresmith
// migration. The stats jsontags carry no omitempty, so the zero-value
// encoding enumerates every field and any struct-tag drift (renamed field,
// added/dropped omitempty) shows up as a diff. Loki's HTTP API exposes
// these tags directly, so this is a public-API contract.
func TestResultJSONShape(t *testing.T) {
	golden, err := os.ReadFile("testdata/result_zero.json")
	require.NoError(t, err)

	var zero Result
	got, err := json.MarshalIndent(&zero, "", "  ")
	require.NoError(t, err)

	require.JSONEq(t, string(golden), string(got))
}
