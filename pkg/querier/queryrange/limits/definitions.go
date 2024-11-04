package limits

import (
	"context"
	"time"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

// Limits extends the cortex limits interface with support for per tenant splitby parameters
// They've been extracted to avoid import cycles.
type Limits interface {
	queryrangebase.Limits
	logql.Limits
	QuerySplitDuration(string) time.Duration
	InstantMetricQuerySplitDuration(string) time.Duration
	MetadataQuerySplitDuration(string) time.Duration
	RecentMetadataQuerySplitDuration(string) time.Duration
	RecentMetadataQueryWindow(string) time.Duration
	IngesterQuerySplitDuration(string) time.Duration
	MaxQuerySeries(context.Context, string) int
	MaxEntriesLimitPerQuery(context.Context, string) int
	MinShardingLookback(string) time.Duration
	// TSDBMaxQueryParallelism returns the limit to the number of split queries the
	// frontend will process in parallel for TSDB queries.
	TSDBMaxQueryParallelism(context.Context, string) int
	// TSDBMaxBytesPerShard returns the limit to the number of bytes a single shard
	TSDBMaxBytesPerShard(string) int
	TSDBShardingStrategy(userID string) string

	RequiredLabels(context.Context, string) []string
	RequiredNumberLabels(context.Context, string) int
	MaxQueryBytesRead(context.Context, string) int
	MaxQuerierBytesRead(context.Context, string) int
	MaxStatsCacheFreshness(context.Context, string) time.Duration
	MaxMetadataCacheFreshness(context.Context, string) time.Duration
	VolumeEnabled(string) bool

	ShardAggregations(string) []string
}
