package executor

import "github.com/grafana/loki/v3/pkg/xcap"

// IndexMerge statistics. Count the number of equal-key collisions observed
// during a K-way index merge. A non-zero value indicates the upstream
// invariant, that each (ObjectPath, SectionIndex) is referenced by exactly one
// source index, has been violated — the merge tolerates this via last-wins, but
// we want to know how often it happens.
var (
	statIndexMergeDuplicatePostings = xcap.NewStatisticInt64("index.merge.duplicate.postings", xcap.AggregationTypeSum)
	statIndexMergeDuplicateStats    = xcap.NewStatisticInt64("index.merge.duplicate.stats", xcap.AggregationTypeSum)
)
