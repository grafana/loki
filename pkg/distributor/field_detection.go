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

var (
	trace      = []byte("trace")
	traceAbbrv = []byte("trc")
	debug      = []byte("debug")
	debugAbbrv = []byte("dbg")
	info       = []byte("info")
	infoAbbrv  = []byte("inf")
	warn       = []byte("warn")
	warnAbbrv  = []byte("wrn")
	warning    = []byte("warning")
	errorStr   = []byte("error")
	errorAbbrv = []byte("err")
	critical   = []byte("critical")
	fatal      = []byte("fatal")

	defaultAllowedLevelFields = []string{"level", "LEVEL", "Level", "severity", "SEVERITY", "Severity", "lvl", "LVL", "Lvl"}
)

func allowedLabelsForLevel(allowedFields []string) []string {
	if len(allowedFields) == 0 {
		return defaultAllowedLevelFields
	}
	return allowedFields
}

type FieldDetector struct {
	validationContext  validationContext
	allowedLevelLabels []string
}

func newFieldDetector(validationContext validationContext) *FieldDetector {
	return &FieldDetector{
		validationContext:  validationContext,
		allowedLevelLabels: allowedLabelsForLevel(validationContext.logLevelFields),
	}
}

func (l *FieldDetector) shouldDiscoverLogLevels() bool {
	return l.validationContext.allowStructuredMetadata && l.validationContext.discoverLogLevels
}

func (l *FieldDetector) shouldDiscoverGenericFields() bool {
	return l.validationContext.allowStructuredMetadata && len(l.validationContext.discoverGenericFields) > 0
}

func (l *FieldDetector) extractLogLevel(labels labels.Labels, structuredMetadata labels.Labels, entry logproto.Entry) (logproto.LabelAdapter, bool) {
	levelFromLabel, hasLevelLabel := labelsContainAny(labels, l.allowedLevelLabels)
	var logLevel string
	if hasLevelLabel {
		logLevel = levelFromLabel
	} else if levelFromMetadata, ok := labelsContainAny(structuredMetadata, l.allowedLevelLabels); ok {
		logLevel = levelFromMetadata
	} else {
		logLevel = l.detectLogLevelFromLogEntry(entry, structuredMetadata)
	}

	if logLevel == "" {
		return logproto.LabelAdapter{}, false
	}
	return logproto.LabelAdapter{
		Name:  constants.LevelLabel,
		Value: logLevel,
	}, true
}

func (l *FieldDetector) extractGenericField(name string, hints []string, labels labels.Labels, structuredMetadata labels.Labels, entry logproto.Entry) (logproto.LabelAdapter, bool) {

	var value string
	if v, ok := labelsContainAny(labels, hints); ok {
		value = v
	} else if v, ok := labelsContainAny(structuredMetadata, hints); ok {
		value = v
	} else {
		value = l.detectGenericFieldFromLogEntry(entry, hints)
	}

	if value == "" {
		return logproto.LabelAdapter{}, false
	}
	return logproto.LabelAdapter{Name: name, Value: value}, true
}

func labelsContainAny(labels labels.Labels, names []string) (string, bool) {
	for _, name := range names {
		if labels.Has(name) {
			return labels.Get(name), true
		}
	}
	return "", false
}

func (l *FieldDetector) detectLogLevelFromLogEntry(entry logproto.Entry, structuredMetadata labels.Labels) string {
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

	return l.extractLogLevelFromLogLine(entry.Line)
}

func (l *FieldDetector) detectGenericFieldFromLogEntry(entry logproto.Entry, hints []string) string {
	lineBytes := unsafe.Slice(unsafe.StringData(entry.Line), len(entry.Line))
	var v []byte
	if isJSON(entry.Line) {
		v = l.getValueUsingJSONParser(lineBytes, hints)
	} else if isLogFmt(lineBytes) {
		v = l.getValueUsingLogfmtParser(lineBytes, hints)
	}
	return string(v)
}

func (l *FieldDetector) extractLogLevelFromLogLine(log string) string {
	lineBytes := unsafe.Slice(unsafe.StringData(log), len(log))
	var v []byte
	if isJSON(log) {
		v = l.getValueUsingJSONParser(lineBytes, l.allowedLevelLabels)
	} else if isLogFmt(lineBytes) {
		v = l.getValueUsingLogfmtParser(lineBytes, l.allowedLevelLabels)
	} else {
		return detectLevelFromLogLine(log)
	}

	switch {
	case bytes.EqualFold(v, trace), bytes.EqualFold(v, traceAbbrv):
		return constants.LogLevelTrace
	case bytes.EqualFold(v, debug), bytes.EqualFold(v, debugAbbrv):
		return constants.LogLevelDebug
	case bytes.EqualFold(v, info), bytes.EqualFold(v, infoAbbrv):
		return constants.LogLevelInfo
	case bytes.EqualFold(v, warn), bytes.EqualFold(v, warnAbbrv), bytes.EqualFold(v, warning):
		return constants.LogLevelWarn
	case bytes.EqualFold(v, errorStr), bytes.EqualFold(v, errorAbbrv):
		return constants.LogLevelError
	case bytes.EqualFold(v, critical):
		return constants.LogLevelCritical
	case bytes.EqualFold(v, fatal):
		return constants.LogLevelFatal
	default:
		return detectLevelFromLogLine(log)
	}
}

func (l *FieldDetector) getValueUsingLogfmtParser(line []byte, hints []string) []byte {
	d := logfmt.NewDecoder(line)
	// In order to have the same behaviour as the JSON field extraction,
	// the full line needs to be parsed to extract all possible matching fields.
	pos := len(hints) // the index of the hint that matches
	var res []byte
	for !d.EOL() && d.ScanKeyval() {
		k := unsafe.String(unsafe.SliceData(d.Key()), len(d.Key()))
		for x, hint := range hints {
			if strings.EqualFold(k, hint) && x < pos {
				res, pos = d.Value(), x
				// If there is only a single hint, or the matching hint is the first one,
				// we can stop parsing the rest of the line and return early.
				if x == 0 {
					return res
				}
			}
		}
	}
	return res
}

func (l *FieldDetector) getValueUsingJSONParser(log []byte, hints []string) []byte {
	for _, allowedLabel := range hints {
		l, _, _, err := jsonparser.Get(log, allowedLabel)
		if err == nil {
			return l
		}
	}
	return nil
}

func isLogFmt(line []byte) bool {
	equalIndex := bytes.Index(line, []byte("="))
	if len(line) == 0 || equalIndex == -1 {
		return false
	}
	return true
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
