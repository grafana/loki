package drain

import (
	"regexp"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	FormatLogfmt  = "logfmt"
	FormatJSON    = "json"
	FormatUnknown = "unknown"
	TooFewTokens  = "too_few_tokens"
	TooManyTokens = "too_many_tokens"
	LineTooLong   = "line_too_long"
)

var logfmtRegex = regexp.MustCompile("^(\\w+?=([^\"]\\S*?|\".+?\") )*?(\\w+?=([^\"]\\S*?|\".+?\"))+$")

// DetectLogFormat guesses at how the logs are encoded based on some simple heuristics.
// It only runs on the first log line when a new stream is created, so it could do some more complex parsing or regex.
func DetectLogFormat(line string) string {
	if len(line) < 2 {
		return FormatUnknown
	} else if line[0] == '{' && line[len(line)-1] == '}' {
		return FormatJSON
	} else if logfmtRegex.MatchString(line) {
		return FormatLogfmt
	}
	return FormatUnknown
}

type Metrics struct {
	PatternsEvictedTotal  prometheus.Counter
	PatternsPrunedTotal   prometheus.Counter
	PatternsDetectedTotal prometheus.Counter
	LinesSkipped          *prometheus.CounterVec
	TokensPerLine         prometheus.Observer
	StatePerLine          prometheus.Observer
}
