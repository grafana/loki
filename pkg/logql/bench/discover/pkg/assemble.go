package discover

import (
	"time"

	bench "github.com/grafana/loki/v3/pkg/logql/bench"
	"github.com/grafana/loki/v3/pkg/logql/bench/discover/pkg/tsdb"
)

// AssembleMetadata builds a DatasetMetadata from the four pipeline result types
// produced by the discover pipeline stages. The function is pure — it does not mutate
// any of its inputs.
//
// Each inverted-index map is copied directly from its source: no re-sorting,
// no re-keying. This preserves the ordering guarantees established by each
// pipeline stage.
//
// The MetadataBySelector field is copied as-is from RangeResult. Streams absent
// from ranges.MetadataBySelector are simply not present in the output map — the
// caller (RunValidation, metadata_resolver) handles missing entries gracefully.
func AssembleMetadata(
	discover *tsdb.StructuralResult,
	classify *ClassifyResult,
	keywords *KeywordResult,
	ranges *tsdb.RangeResult,
	cfg DiscoverConfig,
) *bench.DatasetMetadata {
	return &bench.DatasetMetadata{
		Version:              bench.MetadataVersion,
		AllSelectors:         discover.AllSelectors,
		ByServiceName:        discover.ByServiceName,
		ByFormat:             classify.ByFormat,
		ByUnwrappableField:   classify.ByUnwrappableField,
		ByDetectedField:      classify.ByDetectedField,
		ByStructuredMetadata: classify.ByStructuredMetadata,
		ByLabelKey:           classify.ByLabelKey,
		ByKeyword:            keywords.ByKeyword,
		MetadataBySelector:   ranges.MetadataBySelector,
		TimeRange: bench.TimeRange{
			Start: cfg.effectiveFrom(),
			End:   cfg.effectiveTo(),
		},
		Statistics: assembleStatistics(discover, classify),
	}
}

// assembleStatistics computes summary statistics from the discover and classify results.
func assembleStatistics(discover *tsdb.StructuralResult, classify *ClassifyResult) bench.DatasetStatistics {
	streamsByFormat := make(map[bench.LogFormat]int)
	for fmt, sels := range classify.ByFormat {
		streamsByFormat[fmt] = len(sels)
	}

	streamsByService := make(map[string]int)
	for svc, sels := range discover.ByServiceName {
		streamsByService[svc] = len(sels)
	}

	return bench.DatasetStatistics{
		Generated:        time.Now(),
		TotalStreams:     len(discover.AllSelectors),
		StreamsByFormat:  streamsByFormat,
		StreamsByService: streamsByService,
	}
}
