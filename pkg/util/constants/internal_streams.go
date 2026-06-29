package constants

const (
	// AggregatedMetricLabel is the label added to streams containing aggregated metrics
	AggregatedMetricLabel = "__aggregated_metric__"
	// PatternLabel is the label added to streams containing detected patterns
	PatternLabel = "__pattern__"
	// BackfillLabel marks streams ingested via the backfill path (X-Loki-Backfill-Day header).
	BackfillLabel = "__backfill__"
	// BackfillDayLabel partitions backfilled streams by the backfill day (YYYY-MM-DD).
	BackfillDayLabel = "__backfill_day__"
	// LogsDrilldownAppName is the app name used to identify requests from Logs Drilldown
	LogsDrilldownAppName = "grafana-lokiexplore-app"
)
