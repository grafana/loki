package executor

import "github.com/grafana/loki/v3/pkg/xcap"

// Common pipeline statistics tracked across executor nodes.
var (
	// StatPipelineRowsOut counts rows produced by an executor pipeline node.
	StatPipelineRowsOut = xcap.NewStatisticInt64("rows.out", xcap.AggregationTypeSum)

	// StatPipelineReadCalls counts calls to Read for an executor pipeline node.
	StatPipelineReadCalls = xcap.NewStatisticInt64("read.calls", xcap.AggregationTypeSum)

	// StatPipelineReadDuration tracks wall time spent in Read for an executor
	// pipeline node, inclusive of child pipeline nodes.
	StatPipelineReadDuration = xcap.NewStatisticFloat64("read.duration", xcap.AggregationTypeSum)

	// StatPipelineExecDuration tracks executor-specific compute time for
	// pipeline nodes that can separate input read time from local execution.
	StatPipelineExecDuration = xcap.NewStatisticFloat64("exec.duration", xcap.AggregationTypeSum)
)

// Task cache statistics.
var (
	TaskCacheHits    = xcap.NewStatisticInt64("task.cache.hits", xcap.AggregationTypeSum)
	TaskCacheMisses  = xcap.NewStatisticInt64("task.cache.misses", xcap.AggregationTypeSum)
	TaskCacheBatches = xcap.NewStatisticInt64("task.cache.batches", xcap.AggregationTypeSum)
	TaskCacheRows    = xcap.NewStatisticInt64("task.cache.rows", xcap.AggregationTypeSum)
	TaskCacheBytes   = xcap.NewStatisticInt64("task.cache.bytes", xcap.AggregationTypeSum)
)

// DataObjScan cache statistics.
var (
	DataObjScanCacheHits    = xcap.NewStatisticInt64("dataobjscan.cache.hits", xcap.AggregationTypeSum)
	DataObjScanCacheMisses  = xcap.NewStatisticInt64("dataobjscan.cache.misses", xcap.AggregationTypeSum)
	DataObjScanCacheBatches = xcap.NewStatisticInt64("dataobjscan.cache.batches", xcap.AggregationTypeSum)
	DataObjScanCacheRows    = xcap.NewStatisticInt64("dataobjscan.cache.rows", xcap.AggregationTypeSum)
	DataObjScanCacheBytes   = xcap.NewStatisticInt64("dataobjscan.cache.bytes", xcap.AggregationTypeSum)
)

// IndexMerge statistics. Count the number of equal-key collisions observed
// during a K-way index merge. A non-zero value indicates the upstream
// invariant, that each (ObjectPath, SectionIndex) is referenced by exactly one
// source index, has been violated — the merge tolerates this via last-wins, but
// we want to know how often it happens.
var (
	statIndexMergeDuplicatePostings = xcap.NewStatisticInt64("index.merge.duplicate.postings", xcap.AggregationTypeSum)
	statIndexMergeDuplicateStats    = xcap.NewStatisticInt64("index.merge.duplicate.stats", xcap.AggregationTypeSum)
)
