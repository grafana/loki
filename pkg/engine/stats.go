package engine

import (
	"github.com/grafana/loki/v3/pkg/xcap"
	"github.com/grafana/loki/v3/pkg/xcap/statid"
)

var (
	// Top-level phase durations

	statLogicalPlanDuration  = xcap.NewStatisticInt64(statid.EngineLogicalPlanDuration, "plan.logical.duration", xcap.AggregationTypeSum)
	statPhysicalPlanDuration = xcap.NewStatisticInt64(statid.EnginePhysicalPlanDuration, "plan.physical.duration", xcap.AggregationTypeSum)
	statPrepareDuration      = xcap.NewStatisticInt64(statid.EnginePrepareDuration, "prepare.duration", xcap.AggregationTypeSum)
	statExecutionDuration    = xcap.NewStatisticInt64(statid.EngineExecutionDuration, "execution.duration", xcap.AggregationTypeSum)
	statCloseDuration        = xcap.NewStatisticInt64(statid.EngineCloseDuration, "close.duration", xcap.AggregationTypeSum)

	// Physical plan subphases

	statPhysicalIndexQueryDuration = xcap.NewStatisticInt64(statid.EnginePhysicalIndexQueryDuration, "plan.physical.index_query.duration", xcap.AggregationTypeSum)
	statPhysicalOptimizeDuration   = xcap.NewStatisticInt64(statid.EnginePhysicalOptimizeDuration, "plan.physical.optimize.duration", xcap.AggregationTypeSum)

	// Execution subphases

	statReadBatchDuration    = xcap.NewStatisticInt64(statid.EngineReadBatchDuration, "execute.read_batch.duration", xcap.AggregationTypeSum)
	statProcessBatchDuration = xcap.NewStatisticInt64(statid.EngineProcessBatchDuration, "execute.process_batch.duration", xcap.AggregationTypeSum)

	// Result-collection stats, recorded once per query in (*Engine).execute and
	// surfaced on the execution-summary log line.

	statResultRows       = xcap.NewStatisticInt64(statid.EngineResultRows, "execute.result.rows", xcap.AggregationTypeSum)
	statTimeToFirstBatch = xcap.NewStatisticInt64(statid.EngineTimeToFirstBatch, "execute.result.time_to_first_batch", xcap.AggregationTypeSum)
	statResultSizeBytes  = xcap.NewStatisticInt64(statid.EngineResultSizeBytes, "execute.result.size_bytes", xcap.AggregationTypeSum)
)
