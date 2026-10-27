package distributor

import (
	"bytes"
	"context"
	"errors"
	"runtime/pprof"
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

	// Lookup tables for isLeftWordDelim and isRightWordDelim.
	leftWordDelims = [256]bool{
		' ':  true,
		'\t': true,
		'\n': true,
		'[':  true,
		'(':  true,
		'{':  true,
		'"':  true,
		'\'': true,
		'=':  true,
		'|':  true,
	}
	rightWordDelims = [256]bool{
		' ':  true,
		'\t': true,
		'\n': true,
		'[':  true,
		']':  true,
		'(':  true,
		')':  true,
		'{':  true,
		'}':  true,
		':':  true,
		',':  true,
		'!':  true,
		'"':  true,
		'\'': true,
		'=':  true,
		'|':  true,
	}
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

func (l *FieldDetector) discoverFields(ctx context.Context, lbs labels.Labels, entry *logproto.Entry, structuredMetadata labels.Labels, sharedMetadata ...labels.Labels) {
	if l.shouldDiscoverLogLevels() {
		pprof.Do(ctx, pprof.Labels("action", "discover_log_level"), func(_ context.Context) {
			logLevel, ok := l.extractLogLevel(lbs, structuredMetadata, *entry, sharedMetadata...)
			if ok {
				entry.StructuredMetadata = append(entry.StructuredMetadata, logLevel)
			}
		})
	}
	if l.shouldDiscoverGenericFields() {
		pprof.Do(ctx, pprof.Labels("action", "discover_generic_fields"), func(_ context.Context) {
			for field, hints := range l.validationContext.discoverGenericFields {
				extracted, ok := l.extractGenericField(field, hints, lbs, structuredMetadata, *entry, sharedMetadata...)
				if ok {
					entry.StructuredMetadata = append(entry.StructuredMetadata, extracted)
				}
			}
		})
	}
}

func (l *FieldDetector) extractLogLevel(labels labels.Labels, structuredMetadata labels.Labels, entry logproto.Entry, sharedMetadata ...labels.Labels) (logproto.LabelAdapter, bool) {
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

	// Shared detected levels are normalized once before entries are processed.
	for _, shared := range sharedMetadata {
		if shared.Has(constants.LevelLabel) {
			return logproto.LabelAdapter{}, false
		}
	}

	levelFromLabel, hasLevelLabel := labelsContainAny(labels, l.allowedLevelLabels)
	var logLevel string
	if hasLevelLabel {
		logLevel = normalizeLogLevel(levelFromLabel)
	} else if levelFromMetadata, ok := labelsContainAny(structuredMetadata, l.allowedLevelLabels, sharedMetadata...); ok {
		logLevel = normalizeLogLevel(levelFromMetadata)
	} else {
		logLevel = l.detectLogLevelFromLogEntry(entry, structuredMetadata, sharedMetadata...)
	}

	if logLevel == "" {
		return logproto.LabelAdapter{}, false
	}
	return logproto.LabelAdapter{
		Name:  constants.LevelLabel,
		Value: logLevel,
	}, true
}

func (l *FieldDetector) extractGenericField(name string, hints []string, labels labels.Labels, structuredMetadata labels.Labels, entry logproto.Entry, sharedMetadata ...labels.Labels) (logproto.LabelAdapter, bool) {

	var value string
	if v, ok := labelsContainAny(labels, hints); ok {
		value = v
	} else if v, ok := labelsContainAny(structuredMetadata, hints, sharedMetadata...); ok {
		value = v
	} else {
		value = l.detectGenericFieldFromLogEntry(entry, hints)
	}

	if value == "" {
		return logproto.LabelAdapter{}, false
	}
	return logproto.LabelAdapter{Name: name, Value: value}, true
}

// Search hint names in order, then metadata sources in precedence order.
func labelsContainAny(metadata labels.Labels, names []string, sharedMetadata ...labels.Labels) (string, bool) {
	for _, name := range names {
		if metadata.Has(name) {
			return metadata.Get(name), true
		}
		for _, shared := range sharedMetadata {
			if shared.Has(name) {
				return shared.Get(name), true
			}
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
		// Return the original value if it doesn't match any known level.
		return level
	}
}

func (l *FieldDetector) detectLogLevelFromLogEntry(entry logproto.Entry, structuredMetadata labels.Labels, sharedMetadata ...labels.Labels) string {
	// otlp logs have a severity number, using which we are defining the log levels.
	// Significance of severity number is explained in otel docs here https://opentelemetry.io/docs/specs/otel/logs/data-model/#field-severitynumber
	if otlpSeverityNumberTxt, _ := labelsContainAny(structuredMetadata, []string{push.OTLPSeverityNumber}, sharedMetadata...); otlpSeverityNumberTxt != "" {
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

// isLeftWordDelim checks the character to the left of a potential match.
// Colons are excluded as these indicate a tuple such as misc:error where
// the level is not a separate word.
func isLeftWordDelim(c byte) bool {
	return leftWordDelims[c]
}

// isRightWordDelim checks the character to the right of a potential match.
// Colons are allowed here to support "level:" prefix patterns such as
// "debug: message". Equals and quotes are allowed to match key=value and
// key="value".
func isRightWordDelim(c byte) bool {
	return rightWordDelims[c]
}

// hasPrefixWithDelim checks if s starts with prefix, ASCII case-insensitve,
// and that the match consumes all of s or is followed with a delimiter.
// prefix must be lowercase ASCII letters.
func hasPrefixWithDelim(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	// or with 0x20 folds ASCII upper case to lower case.
	for i := 0; i < len(prefix); i++ {
		if s[i]|0x20 != prefix[i] {
			return false
		}
	}
	return len(s) == len(prefix) || isRightWordDelim(s[len(prefix)])
}

var levelTable = [256]string{
	'c': constants.LogLevelCritical,
	'd': constants.LogLevelDebug,
	'e': constants.LogLevelError,
	'f': constants.LogLevelFatal,
	'i': constants.LogLevelInfo,
	't': constants.LogLevelTrace,
	'w': constants.LogLevelWarn,
}

// detectLevelFromLogLine returns the first level in the log line.
// It is zero-allocation.
func detectLevelFromLogLine(s string) string {
	for i := 0; i < len(s); i++ {
		// A level must start the line or follow a left-word delimiter.
		if i > 0 && !isLeftWordDelim(s[i-1]) {
			continue
		}
		// In ASCII, an upper-case letter and its lower-case form differ in the
		// 5th bit (0x20). For example, 'E' is 0x45 and 'e' is 0x65. If we OR an
		// ASCII uppercase letter with 0x20 it transforms it to lowercase.
		// This means s[i]|0x20 == 'e' matches both E and e without allocating or
		// calling strings.ToLower.
		c := s[i] | 0x20
		if level := levelTable[c]; level != "" {
			if hasPrefixWithDelim(s[i:], level) {
				return level
			} else if c == 'e' && hasPrefixWithDelim(s[i:], "err") {
				return constants.LogLevelError
			} else if c == 'w' && hasPrefixWithDelim(s[i:], "warning") {
				return constants.LogLevelWarn
			}
		}
	}
	return constants.LogLevelUnknown
}
