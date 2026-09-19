package executor

import "github.com/grafana/loki/v3/pkg/xcap"

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

// Task cache statistics.
var (
	TaskCacheHits    = xcap.NewStatisticInt64("task.cache.hits", xcap.AggregationTypeSum)
	TaskCacheMisses  = xcap.NewStatisticInt64("task.cache.misses", xcap.AggregationTypeSum)
	TaskCacheBatches = xcap.NewStatisticInt64("task.cache.batches", xcap.AggregationTypeSum)
	TaskCacheBytes   = xcap.NewStatisticInt64("task.cache.bytes", xcap.AggregationTypeSum)
)

// DataObjScan cache statistics.
var (
	DataObjScanCacheHits    = xcap.NewStatisticInt64("dataobjscan.cache.hits", xcap.AggregationTypeSum)
	DataObjScanCacheMisses  = xcap.NewStatisticInt64("dataobjscan.cache.misses", xcap.AggregationTypeSum)
	DataObjScanCacheBatches = xcap.NewStatisticInt64("dataobjscan.cache.batches", xcap.AggregationTypeSum)
	DataObjScanCacheBytes   = xcap.NewStatisticInt64("dataobjscan.cache.bytes", xcap.AggregationTypeSum)
)
