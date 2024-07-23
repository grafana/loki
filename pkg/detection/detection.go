package detection

import (
	"bytes"
	"strconv"
	"strings"
	"unicode"
	"unsafe"

	"github.com/buger/jsonparser"
	"go.opentelemetry.io/collector/pdata/plog"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log/logfmt"
)

const (
	AggregatedMetricLabel = "__aggregated_metric__"
	LevelLabel            = "detected_level"
	LogLevelUnknown       = "unknown"

	LogLevelDebug    = "debug"
	LogLevelInfo     = "info"
	LogLevelWarn     = "warn"
	LogLevelError    = "error"
	LogLevelFatal    = "fatal"
	LogLevelCritical = "critical"
	LogLevelTrace    = "trace"
)

var AllowedLabelsForLevel = map[string]struct{}{
	"level": {}, "LEVEL": {}, "Level": {},
	"severity": {}, "SEVERITY": {}, "Severity": {},
	"lvl": {}, "LVL": {}, "Lvl": {},
}

func DetectLogLevelFromLogEntry(entry logproto.Entry, structuredMetadata labels.Labels) string {
	// otlp logs have a severity number, using which we are defining the log levels.
	// Significance of severity number is explained in otel docs here https://opentelemetry.io/docs/specs/otel/logs/data-model/#field-severitynumber
	if otlpSeverityNumberTxt := structuredMetadata.Get(push.OTLPSeverityNumber); otlpSeverityNumberTxt != "" {
		otlpSeverityNumber, err := strconv.Atoi(otlpSeverityNumberTxt)
		if err != nil {
			return LogLevelInfo
		}
		if otlpSeverityNumber == int(plog.SeverityNumberUnspecified) {
			return LogLevelUnknown
		} else if otlpSeverityNumber <= int(plog.SeverityNumberTrace4) {
			return LogLevelTrace
		} else if otlpSeverityNumber <= int(plog.SeverityNumberDebug4) {
			return LogLevelDebug
		} else if otlpSeverityNumber <= int(plog.SeverityNumberInfo4) {
			return LogLevelInfo
		} else if otlpSeverityNumber <= int(plog.SeverityNumberWarn4) {
			return LogLevelWarn
		} else if otlpSeverityNumber <= int(plog.SeverityNumberError4) {
			return LogLevelError
		} else if otlpSeverityNumber <= int(plog.SeverityNumberFatal4) {
			return LogLevelFatal
		}
		return LogLevelUnknown
	}

	return extractLogLevelFromLogLine(entry.Line)
}

func extractLogLevelFromLogLine(log string) string {
	logSlice := unsafe.Slice(unsafe.StringData(log), len(log))
	var v []byte
	if isJSON(log) {
		v = getValueUsingJSONParser(logSlice)
	} else {
		v = getValueUsingLogfmtParser(logSlice)
	}

	switch {
	case bytes.EqualFold(v, []byte("trace")), bytes.EqualFold(v, []byte("trc")):
		return LogLevelTrace
	case bytes.EqualFold(v, []byte("debug")), bytes.EqualFold(v, []byte("dbg")):
		return LogLevelDebug
	case bytes.EqualFold(v, []byte("info")), bytes.EqualFold(v, []byte("inf")):
		return LogLevelInfo
	case bytes.EqualFold(v, []byte("warn")), bytes.EqualFold(v, []byte("wrn")):
		return LogLevelWarn
	case bytes.EqualFold(v, []byte("error")), bytes.EqualFold(v, []byte("err")):
		return LogLevelError
	case bytes.EqualFold(v, []byte("critical")):
		return LogLevelCritical
	case bytes.EqualFold(v, []byte("fatal")):
		return LogLevelFatal
	default:
		return detectLevelFromLogLine(log)
	}
}

func getValueUsingLogfmtParser(line []byte) []byte {
	equalIndex := bytes.Index(line, []byte("="))
	if len(line) == 0 || equalIndex == -1 {
		return nil
	}

	d := logfmt.NewDecoder(line)
	for !d.EOL() && d.ScanKeyval() {
		if _, ok := AllowedLabelsForLevel[string(d.Key())]; ok {
			return (d.Value())
		}
	}
	return nil
}

func getValueUsingJSONParser(log []byte) []byte {
	for allowedLabel := range AllowedLabelsForLevel {
		l, _, _, err := jsonparser.Get(log, allowedLabel)
		if err == nil {
			return l
		}
	}
	return nil
}

func isJSON(line string) bool {
	var firstNonSpaceChar rune
	for _, char := range line {
		if !unicode.IsSpace(char) {
			firstNonSpaceChar = char
			break
		}
	}

	var lastNonSpaceChar rune
	for i := len(line) - 1; i >= 0; i-- {
		char := rune(line[i])
		if !unicode.IsSpace(char) {
			lastNonSpaceChar = char
			break
		}
	}

	return firstNonSpaceChar == '{' && lastNonSpaceChar == '}'
}

func detectLevelFromLogLine(log string) string {
	if strings.Contains(log, "info:") || strings.Contains(log, "INFO:") ||
		strings.Contains(log, "info") || strings.Contains(log, "INFO") {
		return LogLevelInfo
	}
	if strings.Contains(log, "err:") || strings.Contains(log, "ERR:") ||
		strings.Contains(log, "error") || strings.Contains(log, "ERROR") {
		return LogLevelError
	}
	if strings.Contains(log, "warn:") || strings.Contains(log, "WARN:") ||
		strings.Contains(log, "warning") || strings.Contains(log, "WARNING") {
		return LogLevelWarn
	}
	if strings.Contains(log, "CRITICAL:") || strings.Contains(log, "critical:") {
		return LogLevelCritical
	}
	if strings.Contains(log, "debug:") || strings.Contains(log, "DEBUG:") {
		return LogLevelDebug
	}
	return LogLevelUnknown
}
