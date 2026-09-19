package discover

import (
	"fmt"

	bench "github.com/grafana/loki/v3/pkg/logql/bench"
)

// QueryResolutionResult holds the outcome of resolving a single query template.
type QueryResolutionResult struct {
	// Definition is the query as loaded from the registry.
	Definition bench.QueryDefinition

	// Suite identifies which suite this query came from.
	Suite bench.Suite

	// Err is nil when resolution succeeded; non-nil describes the gap
	// (e.g. "no streams with log format: json").
	Err error
}

// SuiteStats holds pass/fail counts for a single suite.
type SuiteStats struct {
	Total  int
	Passed int
	Failed int
}

// ValidationResult summarises the outcome of running all queries in the
// selected suites through the metadata resolver.
type ValidationResult struct {
	// Results is the per-query resolution outcome, in load order.
	Results []QueryResolutionResult

	// TotalQueries is the total number of non-skipped queries evaluated.
	TotalQueries int

	// PassedQueries is the number of queries that resolved successfully.
	PassedQueries int

	// FailedQueries is the number of queries that failed to resolve.
	FailedQueries int

	// BySuite holds per-suite counters.
	BySuite map[bench.Suite]SuiteStats

	// UnreferencedKeys lists bounded-set members not referenced by any query.
	UnreferencedKeys []UnreferencedKey
}

// UnreferencedKey represents a member of a bounded set that no query references.
type UnreferencedKey struct {
	// BoundedSet names the set this key belongs to
	// ("keywords", "unwrappable_fields", "structured_metadata", "label_keys").
	BoundedSet string

	// Key is the member that is unreferenced.
	Key string
}

// RunValidation loads all non-skipped queries from the given suites and
// resolves each one against the provided metadata. It returns a ValidationResult
// containing per-query outcomes and a list of unreferenced bounded-set members.
//
// RunValidation returns (nil, error) when queriesDir is empty (required) or
// when the registry fails to load.
func RunValidation(metadata *bench.DatasetMetadata, queriesDir string, suites []bench.Suite) (*ValidationResult, error) {
	if queriesDir == "" {
		return nil, fmt.Errorf("queries-dir is required for validation")
	}

	registry := bench.NewQueryRegistry(queriesDir)
	if err := registry.Load(suites...); err != nil {
		return nil, fmt.Errorf("failed to load query registry: %w", err)
	}

	resolver := bench.NewMetadataVariableResolver(metadata, 42)

	result := &ValidationResult{
		BySuite: make(map[bench.Suite]SuiteStats),
	}

	for _, suite := range suites {
		queries := registry.GetQueries(false, suite)
		stats := SuiteStats{Total: len(queries)}

		for _, q := range queries {
			isInstant := q.Kind == "metric"
			_, err := resolver.ResolveQuery(q.Query, q.Requires, isInstant)

			qr := QueryResolutionResult{
				Definition: q,
				Suite:      suite,
				Err:        err,
			}
			result.Results = append(result.Results, qr)
			result.TotalQueries++

			if err != nil {
				result.FailedQueries++
				stats.Failed++
			} else {
				result.PassedQueries++
				stats.Passed++
			}
		}

		result.BySuite[suite] = stats
	}

	result.UnreferencedKeys = findUnreferencedKeys(registry, suites)
	return result, nil
}

// findUnreferencedKeys walks all non-skipped queries across the given suites
// and returns bounded-set members not referenced by any query.
//
// DetectedFields are intentionally excluded — they form a dynamic set rather
// than a fixed bounded set, so membership cannot be pre-enumerated.
func findUnreferencedKeys(registry *bench.QueryRegistry, suites []bench.Suite) []UnreferencedKey {
	// Collect all referenced keys from query requirements.
	referencedKeywords := make(map[string]bool)
	referencedUnwrappable := make(map[string]bool)
	referencedStructuredMetadata := make(map[string]bool)
	referencedLabelKeys := make(map[string]bool)

	for _, suite := range suites {
		for _, q := range registry.GetQueries(false, suite) {
			for _, kw := range q.Requires.Keywords {
				referencedKeywords[kw] = true
			}
			for _, f := range q.Requires.UnwrappableFields {
				referencedUnwrappable[f] = true
			}
			for _, k := range q.Requires.StructuredMetadata {
				referencedStructuredMetadata[k] = true
			}
			for _, l := range q.Requires.Labels {
				referencedLabelKeys[l] = true
			}
		}
	}

	var unreferenced []UnreferencedKey

	for _, kw := range bench.FilterableKeywords {
		if !referencedKeywords[kw] {
			unreferenced = append(unreferenced, UnreferencedKey{BoundedSet: "keywords", Key: kw})
		}
	}
	for _, f := range bench.UnwrappableFields {
		if !referencedUnwrappable[f] {
			unreferenced = append(unreferenced, UnreferencedKey{BoundedSet: "unwrappable_fields", Key: f})
		}
	}
	for _, k := range bench.StructuredMetadataKeys {
		if !referencedStructuredMetadata[k] {
			unreferenced = append(unreferenced, UnreferencedKey{BoundedSet: "structured_metadata", Key: k})
		}
	}
	for _, l := range bench.LabelKeys {
		if !referencedLabelKeys[l] {
			unreferenced = append(unreferenced, UnreferencedKey{BoundedSet: "label_keys", Key: l})
		}
	}

	return unreferenced
}
