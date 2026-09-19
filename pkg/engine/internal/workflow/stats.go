package workflow

import "github.com/grafana/loki/v3/pkg/xcap"

var (
	// StatPrunedTasks records the number of tasks that were pruned from the
	// workflow.
	StatPrunedTasks = xcap.NewStatisticInt64("workflow.tasks.pruned", xcap.AggregationTypeSum)

	// StatPositiveCacheHits records the number of cache hit for tasks that
	// resulted in at least one value.
	StatPositiveCacheHits = xcap.NewStatisticInt64("workflow.tasks.positive.cache.hits", xcap.AggregationTypeSum)

	// StatNegativeCacheHits records the number of cache hits for tasks that
	// resulted in no value (such as filtering out all rows in the section).
	StatNegativeCacheHits = xcap.NewStatisticInt64("workflow.tasks.negative.cache.hits", xcap.AggregationTypeSum)
)
