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
	// LogsDrilldownAppName is the app name used to identify requests from Logs Drilldown
	LogsDrilldownAppName = "grafana-lokiexplore-app"
)

// IsInternalStream reports whether a stream is one Loki writes for its own use, such as
// aggregated metrics and patterns. has reports whether the stream has a given label.
func IsInternalStream(has func(name string) bool) bool {
	return has(AggregatedMetricLabel) || has(PatternLabel)
}

// IsInternalLabel reports whether name is one of the labels that label name APIs do not return.
func IsInternalLabel(name string) bool {
	switch name {
	case AggregatedMetricLabel, PatternLabel, TimeShardLabel:
		return true
	}
	return false
}
