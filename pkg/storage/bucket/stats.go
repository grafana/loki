package bucket

import "github.com/grafana/loki/v3/pkg/xcap"

// Bucket operation statistics.
var (
	StatBucketGet        = xcap.NewStatisticInt64("bucket.get", xcap.AggregationTypeSum)
	StatBucketGetRange   = xcap.NewStatisticInt64("bucket.getrange", xcap.AggregationTypeSum)
	StatBucketIter       = xcap.NewStatisticInt64("bucket.iter", xcap.AggregationTypeSum)
	StatBucketAttributes = xcap.NewStatisticInt64("bucket.attributes", xcap.AggregationTypeSum)
)
