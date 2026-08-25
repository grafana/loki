package discover

import "time"

// ProbeConfig controls a single RunContentProbes invocation.
type ProbeConfig struct {
	// Parallelism bounds concurrent API calls. Defaults to DefaultParallelism.
	Parallelism int

	// From/To bound the API query time range.
	From time.Time
	To   time.Time

	// BroadSelector is the label matcher clause used for keyword probing.
	// Instead of issuing one query per stream×keyword (N×K queries),
	// the prober issues one query per keyword using this broad selector
	// (K queries), then matches returned stream labels against the known
	// stream set client-side.
	//
	// Example: `{namespace=~"loki-ops-002|mimir-ops-03"}`
	//
	// This field is required and corresponds to the --selector CLI flag.
	BroadSelector string

	// BroadKeywordLimit controls the maximum number of log lines returned per
	// broad keyword query. Higher values increase the chance of covering all
	// 500 target streams but cost more Loki resources. Defaults to 1000.
	BroadKeywordLimit int
}

// effectiveParallelism returns the resolved Parallelism, defaulting to
// DefaultParallelism when the field is zero.
func (c ProbeConfig) effectiveParallelism() int {
	if c.Parallelism > 0 {
		return c.Parallelism
	}
	return DefaultParallelism
}

// effectiveBroadKeywordLimit returns the resolved limit for broad keyword
// queries, defaulting to 1000 when the field is zero.
func (c ProbeConfig) effectiveBroadKeywordLimit() int {
	if c.BroadKeywordLimit > 0 {
		return c.BroadKeywordLimit
	}
	return 1000
}

// ContentProbeResult bundles the classify and keyword probe outputs for
// consumption by AssembleMetadata.
type ContentProbeResult struct {
	Classify *ClassifyResult
	Keywords *KeywordResult
}
