package constants

const (
	// AggregatedMetricLabel is the label added to streams containing aggregated metrics
	AggregatedMetricLabel = "__aggregated_metric__"
	// PatternLabel is the label added to streams containing detected patterns
	PatternLabel = "__pattern__"
	// BackfillLabel marks streams ingested via the backfill path (X-Loki-Backfill-Shard header).
	BackfillLabel = "__backfill__"
	// BackfillShardLabel partitions backfilled streams by an arbitrary shard value set by the
	// client. It implements time sharding on the client side.
	BackfillShardLabel = "__backfill_shard__"
	// TimeShardLabel partitions streams by time window when the distributor time-shards old logs.
	TimeShardLabel = "__time_shard__"
	// StreamShardLabel is the label the distributor adds when it divides a stream into smaller
	// pieces. Its value is the shard number, counting up from 0 and wrapping at the shard count.
	StreamShardLabel = "__stream_shard__"
	// LogsDrilldownAppName is the app name used to identify requests from Logs Drilldown
	LogsDrilldownAppName = "grafana-lokiexplore-app"
)

// IsInternalStream reports whether a stream is one Loki writes for its own use. has reports
// whether the stream has a given label. Today the only internal streams are the ones the pattern
// ingester writes, so this is the same as IsPatternIngesterStream, but the two may diverge if
// other components start writing streams of their own.
func IsInternalStream(has func(name string) bool) bool {
	return IsPatternIngesterStream(has)
}

// IsPatternIngesterStream reports whether a stream is one the pattern ingester writes, such as
// aggregated metrics and patterns. has reports whether the stream has a given label.
func IsPatternIngesterStream(has func(name string) bool) bool {
	return has(AggregatedMetricLabel) || has(PatternLabel)
}

// IsInternalLabel reports whether name is a label that the label names and detected labels
// endpoints leave out of their responses. The label values endpoint still returns values for it,
// so clients can query by it.
func IsInternalLabel(name string) bool {
	switch name {
	case AggregatedMetricLabel, PatternLabel, TimeShardLabel, StreamShardLabel:
		return true
	}
	return false
}
