package postings

import "github.com/grafana/loki/v3/pkg/xcap"

// Postings xcap statistics.
var (
	StatPostingsPointersRead             = xcap.NewStatisticInt64("postings.pointers.read", xcap.AggregationTypeSum)
	StatPostingsBloomRowsRead            = xcap.NewStatisticInt64("postings.bloom.rows.read", xcap.AggregationTypeSum)
	StatPostingsLabelsResolved           = xcap.NewStatisticInt64("postings.labels.resolved", xcap.AggregationTypeSum)
	StatPostingsBloomDeserializeFailures = xcap.NewStatisticInt64("postings.bloom.deserialize.failures", xcap.AggregationTypeSum)
)
