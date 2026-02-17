package distributor

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unsafe"

	"github.com/buger/jsonparser"
	"github.com/prometheus/prometheus/model/labels"
	"go.opentelemetry.io/collector/pdata/plog"

	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/log/jsonexpr"
	"github.com/grafana/loki/v3/pkg/logql/log/logfmt"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"
)

var (
	traceBytes = []byte("trace")
	traceAbbrv = []byte("trc")
	debug      = []byte("debug")
	debugAbbrv = []byte("dbg")
	info       = []byte("info")
	infoAbbrv  = []byte("inf")
	infoFull   = []byte("information")
	warn       = []byte("warn")
	warnAbbrv  = []byte("wrn")
	warning    = []byte("warning")
	errorStr   = []byte("error")
	errorAbbrv = []byte("err")
	critical   = []byte("critical")
	fatal      = []byte("fatal")

	errKeyFound = errors.New("key found")
)

func allowedLabelsForLevel(allowedFields []string) []string {
	if len(allowedFields) == 0 {
		return validation.DefaultAllowedLevelFields
	}

	return allowedFields
}

type FieldDetector struct {
	validationContext        validationContext
	allowedLevelLabelsMap    map[string]struct{}
	allowedLevelLabels       []string
	logLevelFromJSONMaxDepth int
}

func newFieldDetector(validationContext validationContext) *FieldDetector {
	allowedLevelLabels := allowedLabelsForLevel(validationContext.logLevelFields)
	allowedLevelLabelsMap := make(map[string]struct{}, len(allowedLevelLabels))
	for _, field := range allowedLevelLabels {
		allowedLevelLabelsMap[field] = struct{}{}
	}

	return &FieldDetector{
		validationContext:        validationContext,
		allowedLevelLabelsMap:    allowedLevelLabelsMap,
		allowedLevelLabels:       allowedLevelLabels,
		logLevelFromJSONMaxDepth: validationContext.logLevelFromJSONMaxDepth,
	}
}

func (l *FieldDetector) shouldDiscoverLogLevels() bool {
	return l.validationContext.allowStructuredMetadata && l.validationContext.discoverLogLevels
}

func (l *FieldDetector) shouldDiscoverGenericFields() bool {
	return l.validationContext.allowStructuredMetadata && len(l.validationContext.discoverGenericFields) > 0
}

func (l *FieldDetector) extractLogLevel(labels labels.Labels, structuredMetadata labels.Labels, entry logproto.Entry) (logproto.LabelAdapter, bool) {
	// Check if detected_level is already present in entry.StructuredMetadata and normalize it
	for i, sm := range entry.StructuredMetadata {
		if sm.Name == constants.LevelLabel {
			normalizedLevel := normalizeLogLevel(sm.Value)
			if sm.Value != normalizedLevel {
				// Update the value in-place with the normalized version
				entry.StructuredMetadata[i].Value = normalizedLevel
			}
			// Level already exists and has been normalized if needed
			return logproto.LabelAdapter{}, false
		}
	}

	levelFromLabel, hasLevelLabel := labelsContainAny(labels, l.allowedLevelLabels)
	var logLevel string
	if hasLevelLabel {
		logLevel = normalizeLogLevel(levelFromLabel)
	} else if levelFromMetadata, ok := labelsContainAny(structuredMetadata, l.allowedLevelLabels); ok {
		logLevel = normalizeLogLevel(levelFromMetadata)
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

// normalizeLogLevel normalizes log level strings to lowercase standard values
func normalizeLogLevel(level string) string {
	levelBytes := unsafe.Slice(unsafe.StringData(level), len(level)) // #nosec G103 -- we know the string is not mutated -- nosemgrep: use-of-unsafe-block
	switch {
	case bytes.EqualFold(levelBytes, traceBytes), bytes.EqualFold(levelBytes, traceAbbrv):
		return constants.LogLevelTrace
	case bytes.EqualFold(levelBytes, debug), bytes.EqualFold(levelBytes, debugAbbrv):
		return constants.LogLevelDebug
	case bytes.EqualFold(levelBytes, info), bytes.EqualFold(levelBytes, infoAbbrv), bytes.EqualFold(levelBytes, infoFull):
		return constants.LogLevelInfo
	case bytes.EqualFold(levelBytes, warn), bytes.EqualFold(levelBytes, warnAbbrv), bytes.EqualFold(levelBytes, warning):
		return constants.LogLevelWarn
	case bytes.EqualFold(levelBytes, errorStr), bytes.EqualFold(levelBytes, errorAbbrv):
		return constants.LogLevelError
	case bytes.EqualFold(levelBytes, critical):
		return constants.LogLevelCritical
	case bytes.EqualFold(levelBytes, fatal):
		return constants.LogLevelFatal
	default:
		// Return the original value if it doesn't match any known level
		return level
	}
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
		v = getValueUsingJSONParser(lineBytes, hints)
	} else if isLogFmt(lineBytes) {
		v = getValueUsingLogfmtParser(lineBytes, hints)
	}
	return string(v)
}

func (l *FieldDetector) extractLogLevelFromLogLine(log string) string {
	lineBytes := unsafe.Slice(unsafe.StringData(log), len(log))
	var v []byte
	if isJSON(log) {
		v = getLevelUsingJSONParser(lineBytes, l.allowedLevelLabelsMap, l.logLevelFromJSONMaxDepth)
	} else if isLogFmt(lineBytes) {
		v = getValueUsingLogfmtParser(lineBytes, l.allowedLevelLabels)
	} else {
		return detectLevelFromLogLine(log)
	}

	switch {
	case bytes.EqualFold(v, traceBytes), bytes.EqualFold(v, traceAbbrv):
		return constants.LogLevelTrace
	case bytes.EqualFold(v, debug), bytes.EqualFold(v, debugAbbrv):
		return constants.LogLevelDebug
	case bytes.EqualFold(v, info), bytes.EqualFold(v, infoAbbrv), bytes.EqualFold(v, infoFull):
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

func getValueUsingLogfmtParser(line []byte, hints []string) []byte {
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

func getValueUsingJSONParser(line []byte, hints []string) []byte {
	var res []byte
	for _, allowedLabel := range hints {
		parsed, err := jsonexpr.Parse(allowedLabel, false)
		if err != nil {
			continue
		}
		l, _, _, err := jsonparser.Get(line, log.JSONPathToStrings(parsed)...)
		if err != nil {
			continue
		}
		return l
	}
	return res
}

func getLevelUsingJSONParser(line []byte, allowedLevelFields map[string]struct{}, maxDepth int) []byte {
	var result []byte
	var detectLevel func([]byte, int) error
	detectLevel = func(data []byte, depth int) error {
		// maxDepth <= 0 means no limit
		if maxDepth > 0 && depth >= maxDepth {
			return nil
		}

		return jsonparser.ObjectEach(data, func(key []byte, value []byte, dataType jsonparser.ValueType, _ int) error {
			switch dataType {
			case jsonparser.String:
				if _, ok := allowedLevelFields[unsafe.String(unsafe.SliceData(key), len(key))]; ok {
					result = value
					// ErrKeyFound is used to stop parsing once we find the desired key
					return errKeyFound
				}
			case jsonparser.Object:
				if err := detectLevel(value, depth+1); err != nil {
					return err
				}
			}

			return nil
		})
	}

	_ = detectLevel(line, 0)
	return result
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

func isWordBoundary(s string, pos int) bool {
	if pos < 0 || pos >= len(s) {
		return true // Start/end of string is a boundary
	}
	r := rune(s[pos])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
}

func logHasBoundedLevel(log, level string) bool {
	pos := strings.Index(log, level)
	if pos == -1 {
		return false
	}

	// make sure the word occurs on its own, with boundaries on both sides
	if isWordBoundary(log, pos-1) && isWordBoundary(log, pos+len(level)) {
		return true
	}

	return false
}

func detectLevelFromLogLine(log string) string {
	levelPatterns := []struct {
		word  string
		level string
	}{
		{"trace", constants.LogLevelTrace},
		{"debug", constants.LogLevelDebug},
		{"fatal", constants.LogLevelFatal},
		{"critical", constants.LogLevelCritical},
		{"error", constants.LogLevelError},
		{"err", constants.LogLevelError},
		{"warning", constants.LogLevelWarn},
		{"warn", constants.LogLevelWarn},
		{"info", constants.LogLevelInfo},
	}
	lowerLog := strings.ToLower(log)
	for _, level := range levelPatterns {
		if logHasBoundedLevel(lowerLog, level.word) {
			return level.level
		}
	}

	return constants.LogLevelUnknown
}
