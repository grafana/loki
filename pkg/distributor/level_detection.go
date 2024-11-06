package distributor

import (
	"bytes"
	"strconv"
	"strings"
	"unicode"
	"unsafe"

	"github.com/buger/jsonparser"
	"github.com/prometheus/prometheus/model/labels"
	"go.opentelemetry.io/collector/pdata/plog"

	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log/logfmt"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

var allowedLabelsForLevel = map[string]struct{}{
	"level": {}, "LEVEL": {}, "Level": {},
	"severity": {}, "SEVERITY": {}, "Severity": {},
	"lvl": {}, "LVL": {}, "Lvl": {},
}

type LevelDetector struct {
	validationContext validationContext
}

func (l *LevelDetector) shouldDiscoverLogLevels() bool {
	return l.validationContext.allowStructuredMetadata && l.validationContext.discoverLogLevels
}

func (l *LevelDetector) extractLogLevel(labels labels.Labels, structuredMetadata labels.Labels, entry logproto.Entry) (logproto.LabelAdapter, bool) {
	levelFromLabel, hasLevelLabel := hasAnyLevelLabels(labels)
	var logLevel string
	if hasLevelLabel {
		logLevel = levelFromLabel
	} else if levelFromMetadata, ok := hasAnyLevelLabels(structuredMetadata); ok {
		logLevel = levelFromMetadata
	} else {
		logLevel = detectLogLevelFromLogEntry(entry, structuredMetadata)
	}

	if logLevel == "" {
		return logproto.LabelAdapter{}, false
	}
	return logproto.LabelAdapter{
		Name:  constants.LevelLabel,
		Value: logLevel,
	}, true
}

func hasAnyLevelLabels(l labels.Labels) (string, bool) {
	for lbl := range allowedLabelsForLevel {
		if l.Has(lbl) {
			return l.Get(lbl), true
		}
	}
	return "", false
}

func detectLogLevelFromLogEntry(entry logproto.Entry, structuredMetadata labels.Labels) string {
	// otlp logs have a severity number, using which we are defining the log levels.
	// Significance of severity number is explained in otel docs here https://opentelemetry.io/docs/specs/otel/logs/data-model/#field-severitynumber
	if otlpSeverityNumberTxt := structuredMetadata.Get(push.OTLPSeverityNumber); otlpSeverityNumberTxt != "" {
		otlpSeverityNumber, err := strconv.Atoi(otlpSeverityNumberTxt)
		if err != nil {
			return constants.LogLevelInfo
		}
		if otlpSeverityNumber == int(plog.SeverityNumberUnspecified) {
			return constants.LogLevelUnknown
		} else if otlpSeverityNumber <= int(plog.SeverityNumberTrace4) {
			return constants.LogLevelTrace
		} else if otlpSeverityNumber <= int(plog.SeverityNumberDebug4) {
			return constants.LogLevelDebug
		} else if otlpSeverityNumber <= int(plog.SeverityNumberInfo4) {
			return constants.LogLevelInfo
		} else if otlpSeverityNumber <= int(plog.SeverityNumberWarn4) {
			return constants.LogLevelWarn
		} else if otlpSeverityNumber <= int(plog.SeverityNumberError4) {
			return constants.LogLevelError
		} else if otlpSeverityNumber <= int(plog.SeverityNumberFatal4) {
			return constants.LogLevelFatal
		}
		return constants.LogLevelUnknown
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
		return constants.LogLevelTrace
	case bytes.EqualFold(v, []byte("debug")), bytes.EqualFold(v, []byte("dbg")):
		return constants.LogLevelDebug
	case bytes.EqualFold(v, []byte("info")), bytes.EqualFold(v, []byte("inf")):
		return constants.LogLevelInfo
	case bytes.EqualFold(v, []byte("warn")), bytes.EqualFold(v, []byte("wrn")), bytes.EqualFold(v, []byte("warning")):
		return constants.LogLevelWarn
	case bytes.EqualFold(v, []byte("error")), bytes.EqualFold(v, []byte("err")):
		return constants.LogLevelError
	case bytes.EqualFold(v, []byte("critical")):
		return constants.LogLevelCritical
	case bytes.EqualFold(v, []byte("fatal")):
		return constants.LogLevelFatal
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
		if _, ok := allowedLabelsForLevel[string(d.Key())]; ok {
			return (d.Value())
		}
	}
	return nil
}

func getValueUsingJSONParser(log []byte) []byte {
	for allowedLabel := range allowedLabelsForLevel {
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
		return constants.LogLevelInfo
	}
	if strings.Contains(log, "err:") || strings.Contains(log, "ERR:") ||
		strings.Contains(log, "error") || strings.Contains(log, "ERROR") {
		return constants.LogLevelError
	}
	if strings.Contains(log, "warn:") || strings.Contains(log, "WARN:") ||
		strings.Contains(log, "warning") || strings.Contains(log, "WARNING") {
		return constants.LogLevelWarn
	}
	if strings.Contains(log, "CRITICAL:") || strings.Contains(log, "critical:") {
		return constants.LogLevelCritical
	}
	if strings.Contains(log, "debug:") || strings.Contains(log, "DEBUG:") {
		return constants.LogLevelDebug
	}
	return constants.LogLevelUnknown
}
