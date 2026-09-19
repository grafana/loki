package discover

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bench "github.com/grafana/loki/v3/pkg/logql/bench"
)

// writeQueryFixture writes a minimal YAML fixture to tempDir/<suite>/filename
func writeQueryFixture(t *testing.T, tempDir string, suite bench.Suite, filename, content string) {
	t.Helper()
	suiteDir := filepath.Join(tempDir, string(suite))
	require.NoError(t, os.MkdirAll(suiteDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(suiteDir, filename), []byte(content), 0o644))
}

// TestRunValidationEmptyDir verifies that RunValidation returns an error when
// queriesDir is empty (not just a skip with warning).
func TestRunValidationEmptyDir(t *testing.T) {
	md := &bench.DatasetMetadata{}
	result, err := RunValidation(md, "", []bench.Suite{bench.SuiteFast})
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "queries-dir")
}

// TestRunValidationAllPass verifies that RunValidation returns TotalQueries=1,
// PassedQueries=1, FailedQueries=0 when a single no-requirements query is used
// with a fully-populated metadata.
func TestRunValidationAllPass(t *testing.T) {
	tempDir := t.TempDir()

	// Write a minimal query with no requirements so it always resolves.
	// ${SELECTOR} requires at least one stream, so use a static selector.
	writeQueryFixture(t, tempDir, bench.SuiteFast, "test.yaml", `
queries:
  - description: "always passes"
    query: 'count_over_time({app="test"}[1m])'
    kind: metric
    time_range:
      length: 1h
`)

	// Build metadata with enough content to let the query pass.
	// The query is static (no ${SELECTOR} placeholder), so an empty metadata
	// is fine — ResolveQuery won't do any resolution.
	md := &bench.DatasetMetadata{
		AllSelectors:         []string{`{app="test"}`},
		ByFormat:             map[bench.LogFormat][]string{},
		ByUnwrappableField:   map[string][]string{},
		ByDetectedField:      map[string][]string{},
		ByStructuredMetadata: map[string][]string{},
		ByLabelKey:           map[string][]string{},
		ByKeyword:            map[string][]string{},
		MetadataBySelector:   map[string]*bench.SerializableStreamMetadata{},
	}

	result, err := RunValidation(md, tempDir, []bench.Suite{bench.SuiteFast})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.TotalQueries)
	assert.Equal(t, 1, result.PassedQueries)
	assert.Equal(t, 0, result.FailedQueries)
}

// TestRunValidationFailure verifies that RunValidation records FailedQueries=1
// when a query requires json format but no json streams are present in metadata.
func TestRunValidationFailure(t *testing.T) {
	tempDir := t.TempDir()

	writeQueryFixture(t, tempDir, bench.SuiteFast, "test.yaml", `
queries:
  - description: "requires json format"
    query: '${SELECTOR} | json'
    kind: log
    time_range:
      length: 1h
    requires:
      log_format: json
`)

	// Empty metadata: no json streams.
	md := &bench.DatasetMetadata{
		AllSelectors:         []string{},
		ByFormat:             map[bench.LogFormat][]string{},
		ByUnwrappableField:   map[string][]string{},
		ByDetectedField:      map[string][]string{},
		ByStructuredMetadata: map[string][]string{},
		ByLabelKey:           map[string][]string{},
		ByKeyword:            map[string][]string{},
		MetadataBySelector:   map[string]*bench.SerializableStreamMetadata{},
	}

	result, err := RunValidation(md, tempDir, []bench.Suite{bench.SuiteFast})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.TotalQueries)
	assert.Equal(t, 0, result.PassedQueries)
	assert.Equal(t, 1, result.FailedQueries)
	require.Len(t, result.Results, 1)
	assert.NotNil(t, result.Results[0].Err)
}

// TestRunValidation_GapReasons verifies ASMB-04: validation failures contain
// specific gap reasons ("no streams with keyword: X", "no streams with label: Y")
// and unreferenced bounded-set members are reported.
func TestRunValidation_GapReasons(t *testing.T) {
	tempDir := t.TempDir()

	// Three queries:
	//   A: requires json format (metadata HAS json streams) → should PASS
	//   B: requires keyword "refused" (metadata does NOT have it) → should FAIL
	//   C: requires label "region" (metadata does NOT have it) → should FAIL
	writeQueryFixture(t, tempDir, bench.SuiteFast, "gap-test.yaml", `
queries:
  - description: "Query A: json format (passes)"
    query: '${SELECTOR} | json'
    kind: log
    time_range:
      length: 1h
    requires:
      log_format: json

  - description: "Query B: requires refused keyword (fails)"
    query: '${SELECTOR} |= "refused"'
    kind: log
    time_range:
      length: 1h
    requires:
      keywords:
        - refused

  - description: "Query C: requires container label (fails)"
    query: '${SELECTOR} | container = "loki"'
    kind: log
    time_range:
      length: 1h
    requires:
      labels:
        - container
`)

	sel := `{cluster="prod", service_name="loki"}`
	md := &bench.DatasetMetadata{
		AllSelectors: []string{sel},
		ByFormat: map[bench.LogFormat][]string{
			bench.LogFormatJSON: {sel},
		},
		ByKeyword: map[string][]string{
			"error": {sel},
			"warn":  {sel},
		},
		ByLabelKey: map[string][]string{
			"cluster":      {sel},
			"service_name": {sel},
		},
		ByUnwrappableField: map[string][]string{
			"duration": {sel},
		},
		ByDetectedField: map[string][]string{
			"level": {sel},
		},
		ByStructuredMetadata: map[string][]string{
			"trace_id": {sel},
		},
		MetadataBySelector: map[string]*bench.SerializableStreamMetadata{
			sel: {MinRange: 5 * time.Minute, MinInstantRange: 10 * time.Minute},
		},
	}

	result, err := RunValidation(md, tempDir, []bench.Suite{bench.SuiteFast})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify query counts.
	assert.Equal(t, 3, result.TotalQueries)
	assert.Equal(t, 1, result.PassedQueries)
	assert.Equal(t, 2, result.FailedQueries)

	// Verify each result individually.
	require.Len(t, result.Results, 3)

	// Query A should pass.
	assert.Nil(t, result.Results[0].Err, "Query A (json format) should pass")

	// Query B should fail with specific keyword reason.
	require.NotNil(t, result.Results[1].Err, "Query B (keyword refused) should fail")
	assert.Contains(t, result.Results[1].Err.Error(), "no streams with keyword: refused",
		"keyword failure should name the specific missing keyword")

	// Query C should fail with specific label reason.
	require.NotNil(t, result.Results[2].Err, "Query C (label container) should fail")
	assert.Contains(t, result.Results[2].Err.Error(), "no streams with label: container",
		"label failure should name the specific missing label")

	// Verify UnreferencedKeys — keywords and labels that exist in bounded sets
	// but are not referenced by any test query should appear.
	unreferencedBySet := make(map[string]map[string]bool)
	for _, u := range result.UnreferencedKeys {
		if unreferencedBySet[u.BoundedSet] == nil {
			unreferencedBySet[u.BoundedSet] = make(map[string]bool)
		}
		unreferencedBySet[u.BoundedSet][u.Key] = true
		assert.NotEmpty(t, u.BoundedSet, "UnreferencedKey should have non-empty BoundedSet")
		assert.NotEmpty(t, u.Key, "UnreferencedKey should have non-empty Key")
	}

	// "refused" is referenced by Query B, so it should NOT be unreferenced.
	assert.False(t, unreferencedBySet["keywords"]["refused"],
		"referenced keyword 'refused' should not appear as unreferenced")

	// "region" is referenced by Query C, so it should NOT be unreferenced.
	assert.False(t, unreferencedBySet["label_keys"]["region"],
		"referenced label 'region' should not appear as unreferenced")

	// At least some unreferenced keywords should exist (the bounded set has 15,
	// only "timeout" is referenced).
	require.NotEmpty(t, unreferencedBySet["keywords"],
		"should have unreferenced keywords from the bounded set")
}

// TestFindUnreferencedKeys verifies that findUnreferencedKeys returns all bounded
// set members not referenced by any query. If a query requires keyword "error",
// all other FilterableKeywords should appear in UnreferencedKeys.
func TestFindUnreferencedKeys(t *testing.T) {
	tempDir := t.TempDir()

	// Use only the keyword "error" in the one query.
	writeQueryFixture(t, tempDir, bench.SuiteFast, "test.yaml", `
queries:
  - description: "filter by error keyword"
    query: '${SELECTOR} |= "error"'
    kind: log
    time_range:
      length: 1h
    requires:
      keywords:
        - error
`)

	md := &bench.DatasetMetadata{
		AllSelectors: []string{`{app="test"}`},
		ByFormat:     map[bench.LogFormat][]string{},
		ByKeyword: map[string][]string{
			"error": {`{app="test"}`},
		},
		ByUnwrappableField:   map[string][]string{},
		ByDetectedField:      map[string][]string{},
		ByStructuredMetadata: map[string][]string{},
		ByLabelKey:           map[string][]string{},
		MetadataBySelector: map[string]*bench.SerializableStreamMetadata{
			`{app="test"}`: {MinRange: 1000000000, MinInstantRange: 2000000000},
		},
	}

	result, err := RunValidation(md, tempDir, []bench.Suite{bench.SuiteFast})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Collect which keywords appear as unreferenced.
	unreferencedKeywords := map[string]bool{}
	for _, u := range result.UnreferencedKeys {
		if u.BoundedSet == "keywords" {
			unreferencedKeywords[u.Key] = true
		}
	}

	// "error" is referenced, so it should NOT be in UnreferencedKeys.
	assert.False(t, unreferencedKeywords["error"], "referenced keyword 'error' should not appear in unreferenced keys")

	// All other FilterableKeywords should appear as unreferenced.
	for _, kw := range bench.FilterableKeywords {
		if kw == "error" {
			continue
		}
		assert.True(t, unreferencedKeywords[kw], "keyword %q should be in unreferenced keys", kw)
	}
}
