package executor

import "github.com/grafana/loki/v3/pkg/xcap"

// xcap statistics used in executor pkg.
var (
	// Common statistics tracked for all pipeline nodes.
	statRowsOut      = xcap.NewStatisticInt64("rows.out", xcap.AggregationTypeSum)
	statReadCalls    = xcap.NewStatisticInt64("read.calls", xcap.AggregationTypeSum)
	statReadDuration = xcap.NewStatisticInt64("read.duration.ns", xcap.AggregationTypeSum)

	// [ColumnCompat] statistics.
	statCompatCollisionFound = xcap.NewStatisticFlag("collision.found")
)
