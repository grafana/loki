package constants

// Labels that automatic server-side stream sharding adds to split a single
// logical stream into shards. Unlike the internal-stream labels in this package
// (AggregatedMetricLabel, PatternLabel, ...), these do not identify a distinct
// stream: every shard belongs to the same stream. They must therefore be
// ignored when computing a stream's identity for query-time deduplication.
const (
	// StreamShardLabel is added by rate-based stream sharding. Values are
	// increasing integers starting from 0.
	StreamShardLabel = "__stream_shard__"
	// TimeShardLabel is added by time-based stream sharding.
	TimeShardLabel = "__time_shard__"
)
