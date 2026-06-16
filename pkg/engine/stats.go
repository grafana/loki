package engine

import "github.com/grafana/loki/v3/pkg/xcap"

var (
	// Top-level phase durations

	statLogicalPlanDuration  = xcap.NewStatisticInt64("plan.logical.duration", xcap.AggregationTypeSum)
	statPhysicalPlanDuration = xcap.NewStatisticInt64("plan.physical.duration", xcap.AggregationTypeSum)
	statPrepareDuration      = xcap.NewStatisticInt64("prepare.duration", xcap.AggregationTypeSum)
	statExecutionDuration    = xcap.NewStatisticInt64("execution.duration", xcap.AggregationTypeSum)
	statCloseDuration        = xcap.NewStatisticInt64("close.duration", xcap.AggregationTypeSum)

	// Physical plan subphases

	statPhysicalIndexQueryDuration = xcap.NewStatisticInt64("plan.physical.index_query.duration", xcap.AggregationTypeSum)
	statPhysicalOptimizeDuration   = xcap.NewStatisticInt64("plan.physical.optimize.duration", xcap.AggregationTypeSum)

	// Execution subphases

	statReadBatchDuration    = xcap.NewStatisticInt64("execute.read_batch.duration", xcap.AggregationTypeSum)
	statProcessBatchDuration = xcap.NewStatisticInt64("execute.process_batch.duration", xcap.AggregationTypeSum)

	// Result-collection stats, recorded once per query in (*Engine).execute and
	// surfaced on the execution-summary log line.

	statResultRows       = xcap.NewStatisticInt64("execute.result.rows", xcap.AggregationTypeSum)
	statTimeToFirstBatch = xcap.NewStatisticInt64("execute.result.time_to_first_batch", xcap.AggregationTypeSum)
	statResultSizeBytes  = xcap.NewStatisticInt64("execute.result.size_bytes", xcap.AggregationTypeSum)
)
