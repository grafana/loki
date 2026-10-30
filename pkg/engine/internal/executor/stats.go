package executor

import "github.com/grafana/loki/v4/pkg/xcap"

// Common pipeline statistics tracked across executor nodes.
var (
	// StatPipelineRowsOut counts rows produced by an executor pipeline node.
	StatPipelineRowsOut = xcap.NewStatisticInt64("rows.out", xcap.AggregationTypeSum, xcap.Local())

	// StatPipelineReadCalls counts calls to Read for an executor pipeline node.
	StatPipelineReadCalls = xcap.NewStatisticInt64("read.calls", xcap.AggregationTypeSum, xcap.Local())

	// StatPipelineReadDuration tracks wall time spent in Read for an executor
	// pipeline node, inclusive of child pipeline nodes.
	StatPipelineReadDuration = xcap.NewStatisticFloat64("read.duration", xcap.AggregationTypeSum, xcap.Local())
)
