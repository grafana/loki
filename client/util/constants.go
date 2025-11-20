package util

// Re-export constants from the main v3 module.
// Constants have zero dependencies, so this is safe and maintains code reuse.

import constants "github.com/grafana/loki/v3/pkg/util/constants"

// Log level constants
const (
	LevelLabel       = constants.LevelLabel
	LogLevelUnknown  = constants.LogLevelUnknown
	LogLevelDebug    = constants.LogLevelDebug
	LogLevelInfo     = constants.LogLevelInfo
	LogLevelWarn     = constants.LogLevelWarn
	LogLevelError    = constants.LogLevelError
	LogLevelFatal    = constants.LogLevelFatal
	LogLevelCritical = constants.LogLevelCritical
	LogLevelTrace    = constants.LogLevelTrace
)

// LogLevels is a list of all supported log levels
var LogLevels = constants.LogLevels

// Metrics namespace constants
const (
	Loki   = constants.Loki
	Cortex = constants.Cortex
	OTLP   = constants.OTLP
)

// VariantLabel is the name of the label used to identify which variant a series belongs to
const VariantLabel = constants.VariantLabel

// Internal stream constants
const (
	// AggregatedMetricLabel is the label added to streams containing aggregated metrics
	AggregatedMetricLabel = constants.AggregatedMetricLabel
	// PatternLabel is the label added to streams containing detected patterns
	PatternLabel = constants.PatternLabel
	// LogsDrilldownAppName is the app name used to identify requests from Logs Drilldown
	LogsDrilldownAppName = constants.LogsDrilldownAppName
)
