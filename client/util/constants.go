package util

// Constants for log levels
const (
	LevelLabel       = "detected_level"
	LogLevelUnknown  = "unknown"
	LogLevelDebug    = "debug"
	LogLevelInfo     = "info"
	LogLevelWarn     = "warn"
	LogLevelError    = "error"
	LogLevelFatal    = "fatal"
	LogLevelCritical = "critical"
	LogLevelTrace    = "trace"
)

// LogLevels is a list of all supported log levels
var LogLevels = []string{
	LogLevelUnknown,
	LogLevelDebug,
	LogLevelInfo,
	LogLevelWarn,
	LogLevelError,
	LogLevelFatal,
	LogLevelCritical,
	LogLevelTrace,
}

// Constants for metrics namespace
const (
	Loki   = "loki"
	Cortex = "cortex"
	OTLP   = "otlp"
)

// VariantLabel is the name of the label used to identify which variant a series belongs to
// in multi-variant queries.
const VariantLabel = "__variant__"

// Constants for internal streams
const (
	// AggregatedMetricLabel is the label added to streams containing aggregated metrics
	AggregatedMetricLabel = "__aggregated_metric__"
	// PatternLabel is the label added to streams containing detected patterns
	PatternLabel = "__pattern__"
	// LogsDrilldownAppName is the app name used to identify requests from Logs Drilldown
	LogsDrilldownAppName = "grafana-lokiexplore-app"
)
