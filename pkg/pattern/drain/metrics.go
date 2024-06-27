package drain

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	FormatLogfmt  = "logfmt"
	FormatJson    = "json"
	FormatUnknown = "unknown"
)

func DetectLogFormat(line string) string {
	format := FormatUnknown
	if line[0] == '{' && line[len(line)-1] == '}' {
		return FormatJson
	}
	if strings.Count(line, "=") > strings.Count(line, " ")-5 {
		format = FormatLogfmt
	}
	return format
}

type Metrics struct {
	PatternsEvictedTotal  *prometheus.CounterVec
	PatternsDetectedTotal *prometheus.CounterVec
	TokensPerLine         *prometheus.HistogramVec
	MetadataPerLine       *prometheus.HistogramVec
	DetectedLogFormat     string
}
