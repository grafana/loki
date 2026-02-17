package executor

import "github.com/grafana/loki/v3/pkg/xcap"

// xcap statistics used in executor pkg.
var (
	// Common statistics tracked for all pipeline nodes.
	statRowsOut      = xcap.NewStatisticInt64("rows_out", xcap.AggregationTypeSum)
	statReadCalls    = xcap.NewStatisticInt64("read_calls", xcap.AggregationTypeSum)
	statReadDuration = xcap.NewStatisticInt64("read_duration_ns", xcap.AggregationTypeSum)

	// [ColumnCompat] statistics.
	statCompatCollisionFound = xcap.NewStatisticFlag("collision_found")
)
