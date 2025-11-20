package constants

// Re-export from client module to maintain code reuse
import "github.com/grafana/loki/client/util"

const (
	// AggregatedMetricLabel is the label added to streams containing aggregated metrics
	AggregatedMetricLabel = util.AggregatedMetricLabel
	// PatternLabel is the label added to streams containing detected patterns
	PatternLabel = util.PatternLabel
	// LogsDrilldownAppName is the app name used to identify requests from Logs Drilldown
	LogsDrilldownAppName = util.LogsDrilldownAppName
)
