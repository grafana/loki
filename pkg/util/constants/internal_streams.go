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
	// LogsDrilldownAppName is the app name used to identify requests from Logs Drilldown
	LogsDrilldownAppName = "grafana-lokiexplore-app"
)
