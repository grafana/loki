package log

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
	"unsafe"

	"github.com/grafana/jsonparser"

	"github.com/grafana/loki/v3/pkg/logql/log/jsonexpr"
	"github.com/grafana/loki/v3/pkg/logql/log/logfmt"
	"github.com/grafana/loki/v3/pkg/logql/log/pattern"
	"github.com/grafana/loki/v3/pkg/logqlmodel"

	"github.com/grafana/regexp"
	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
)

const (
	jsonSpacer = '_'

	// DuplicateSuffix is appended to a label whose name is already taken, so both reach the
	// output. A stream label always takes its name. The parsers also count a structured-metadata
	// key as taking its name, for the labels they extract.
	//
	// A rename appends the suffix once, so one trim recovers the original name. A key that
	// genuinely ends with it is indistinguishable, so a reader must consider both names.
	DuplicateSuffix = "_extracted"
	trueString      = "true"
	falseString     = "false"
	// How much stack space to allocate for unescaping JSON strings; if a string longer
	// than this needs to be escaped, it will result in a heap allocation
	unescapeStackBufSize = 64
)

var (
	_ Stage = &JSONParser{}
	_ Stage = &RegexpParser{}
	_ Stage = &LogfmtParser{}

	trueBytes = []byte("true")

	errUnexpectedJSONObject = fmt.Errorf("expecting json object(%d), but it is not", jsoniter.ObjectValue)
	errMissingCapture       = errors.New("at least one named capture must be supplied")
	errFoundAllLabels       = errors.New("found all required labels")
	errLabelDoesNotMatch    = errors.New("found a label with a matcher that didn't match")

	// the rune error replacement is rejected by Prometheus hence replacing them with space.
	removeInvalidUtf = func(r rune) rune {
		if r == utf8.RuneError {
			return 32 // rune value for space
		}
		return r
	}
)

// stageRenames tracks the final label names that the current parser stage
// produced by renaming a key that collided with a stream label or structured
// metadata (see DuplicateSuffix). It resolves ties between a renamed key and a
// literal key targeting the same final name (e.g. a line containing both "foo"
// and "foo_extracted" while "foo" is a stream label) without relying on the
// order of keys in the line — JSON does not define key order, so the result
// must not depend on it. Within a stage the literal key always wins; keys
// extracted by earlier parser stages take precedence over both.
type stageRenames struct {
	m map[string]struct{}
}

func (s *stageRenames) reset() { clear(s.m) }

// shouldSet reports whether an extracted key may be written, given whether the
// key was renamed due to a collision. It is checked in addition to
// ShouldExtract and replaces the plain hints.Extracted(key) bypass. A renamed
// key also yields to a stream label or structured metadata key that already
// has its final name: a parsed value never replaces such a label.
func (s *stageRenames) shouldSet(lbs *LabelsBuilder, key string, renamed bool) bool {
	if renamed && (lbs.BaseHas(key) || lbs.HasInCategory(key, StructuredMetadataLabel)) {
		return false
	}
	hints := lbs.ParserLabelHints()
	if !hints.Extracted(key) {
		return true
	}
	if renamed {
		// A renamed key never overwrites an already extracted value.
		return false
	}
	// A literal key overwrites a value that this same stage created by
	// renaming a colliding key, but nothing extracted by earlier stages.
	_, renamedHere := s.m[key]
	return renamedHere
}

// record remembers whether the value stored under key came from a renamed key.
func (s *stageRenames) record(key string, renamed bool) {
	if renamed {
		if s.m == nil {
			s.m = make(map[string]struct{})
		}
		s.m[key] = struct{}{}
	} else if s.m != nil {
		delete(s.m, key)
	}
}

// continueParsing reports whether a parser may go on with the line after it wrote key. It asks the
// label filter hints, unless the key was renamed. A literal key later in the same line can replace
// the value of a renamed key (see stageRenames), so a filter must not judge that value. The filter
// stage after the parser sees the final one.
func continueParsing(hints LabelFilterHints, key string, renamed bool, lbs *LabelsBuilder) bool {
	return renamed || hints.ShouldContinueParsingLine(key, lbs)
}

type JSONParser struct {
	prefixBuffer    [][]byte // buffer used to build json keys
	lbs             *LabelsBuilder
	captureJSONPath bool

	keys                  internedStringSet
	parserHints           ParserHint
	sanitizedPrefixBuffer []byte
	renames               stageRenames
}

// NewJSONParser creates a log stage that can parse a json log line and add properties as labels.
func NewJSONParser(captureJSONPath bool) *JSONParser {
	return &JSONParser{
		prefixBuffer:          [][]byte{},
		keys:                  internedStringSet{},
		captureJSONPath:       captureJSONPath,
		sanitizedPrefixBuffer: make([]byte, 0, 64),
	}
}

func (j *JSONParser) Process(_ int64, line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	parserHints := lbs.ParserLabelHints()
	if parserHints.NoLabels() {
		return line, true
	}

	// reset the state.
	j.prefixBuffer = j.prefixBuffer[:0]
	j.lbs = lbs
	j.parserHints = parserHints
	j.renames.reset()

	if err := jsonparser.ObjectEach(line, j.parseObject); err != nil {
		if errors.Is(err, errFoundAllLabels) {
			// Short-circuited
			return line, true
		}

		if errors.Is(err, errLabelDoesNotMatch) {
			// one of the label matchers does not match. The whole line can be thrown away
			return line, false
		}

		lbs.SetErr(errJSON, err)

		return line, true
	}
	return line, true
}

func (j *JSONParser) parseObject(key, value []byte, dataType jsonparser.ValueType, _ int) error {
	var err error
	switch dataType {
	case jsonparser.String, jsonparser.Number, jsonparser.Boolean:
		err = j.parseLabelValue(key, value, dataType)
	case jsonparser.Object:
		prefixLen := len(j.prefixBuffer)
		if ok := j.nextKeyPrefix(key); ok {
			err = jsonparser.ObjectEach(value, j.parseObject)
		}
		// rollback the prefix as we exit the current object.
		j.prefixBuffer = j.prefixBuffer[:prefixLen]
		return err
	}

	if j.parserHints.AllRequiredExtracted() {
		// Not actually an error. Parsing can be short-circuited
		// and this tells jsonparser to stop parsing
		return errFoundAllLabels
	}

	return err
}

// nextKeyPrefix load the next prefix in the buffer and tells if it should be processed based on hints.
func (j *JSONParser) nextKeyPrefix(key []byte) bool {
	j.prefixBuffer = append(j.prefixBuffer, key)

	sanitized := j.buildSanitizedPrefixFromBuffer()
	return j.lbs.ParserLabelHints().ShouldExtractPrefix(
		string(sanitized),
	)
}

func (j *JSONParser) parseLabelValue(key, value []byte, dataType jsonparser.ValueType) error {
	// the first time we use the field as label key.
	if len(j.prefixBuffer) == 0 {
		sanitizedKey, ok := j.keys.Get(key, func() (string, bool) {
			field := sanitizeLabelKey(string(key), true)
			if len(field) == 0 {
				return "", false
			}

			return field, true
		})
		if !ok {
			return nil
		}

		renamed := false
		if j.lbs.BaseHas(sanitizedKey) || j.lbs.HasInCategory(sanitizedKey, StructuredMetadataLabel) {
			sanitizedKey = sanitizedKey + DuplicateSuffix
			renamed = true
		}

		if !j.lbs.ParserLabelHints().ShouldExtract(sanitizedKey) || !j.renames.shouldSet(j.lbs, sanitizedKey, renamed) {
			return nil
		}

		j.lbs.Set(ParsedLabel, sanitizedKey, readValue(value, dataType))
		j.renames.record(sanitizedKey, renamed)
		if j.captureJSONPath {
			j.lbs.SetJSONPath(sanitizedKey, []string{string(key)})
		}

		if !continueParsing(j.lbs.LabelFilterHints(), sanitizedKey, renamed, j.lbs) {
			return errLabelDoesNotMatch
		}
		return nil

	}
	// otherwise we build the label key using the buffer

	// snapshot the current prefix position
	prefixLen := len(j.prefixBuffer)
	j.prefixBuffer = append(j.prefixBuffer, key)

	sanitized := j.buildSanitizedPrefixFromBuffer()
	keyString, _ := j.keys.Get(sanitized, func() (string, bool) {
		return string(sanitized), true
	})

	renamed := false
	if j.lbs.BaseHas(keyString) || j.lbs.HasInCategory(keyString, StructuredMetadataLabel) {
		j.prefixBuffer[prefixLen] = make([]byte, 0, len(key)+len(DuplicateSuffix))
		j.prefixBuffer[prefixLen] = append(j.prefixBuffer[prefixLen], key...)
		j.prefixBuffer[prefixLen] = append(j.prefixBuffer[prefixLen], DuplicateSuffix...)

		keyString = string(j.buildSanitizedPrefixFromBuffer())
		renamed = true
	}

	if !j.parserHints.ShouldExtract(keyString) || !j.renames.shouldSet(j.lbs, keyString, renamed) {
		// reset the prefix position
		j.prefixBuffer = j.prefixBuffer[:prefixLen]
		return nil
	}

	if j.captureJSONPath {
		if jsonPath := j.buildJSONPathFromPrefixBuffer(); len(jsonPath) > 0 {
			j.lbs.SetJSONPath(keyString, jsonPath)
		}
	}

	// reset the prefix position
	j.prefixBuffer = j.prefixBuffer[:prefixLen]

	j.lbs.Set(ParsedLabel, keyString, readValue(value, dataType))
	j.renames.record(keyString, renamed)

	if !continueParsing(j.lbs.LabelFilterHints(), keyString, renamed, j.lbs) {
		return errLabelDoesNotMatch
	}
	return nil
}

func (j *JSONParser) buildSanitizedPrefixFromBuffer() []byte {
	j.sanitizedPrefixBuffer = j.sanitizedPrefixBuffer[:0]

	for i, part := range j.prefixBuffer {
		if len(bytes.TrimSpace(part)) == 0 {
			continue
		}

		if i > 0 && len(j.sanitizedPrefixBuffer) > 0 {
			j.sanitizedPrefixBuffer = append(j.sanitizedPrefixBuffer, byte(jsonSpacer))
		}
		j.sanitizedPrefixBuffer = appendSanitized(j.sanitizedPrefixBuffer, part)
	}

	return j.sanitizedPrefixBuffer
}

// buildJSONPathFromPrefixBuffer constructs the JSON path from the accumulated prefix buffer.
// It returns a slice of strings representing each segment of the JSON path.
// If the prefix buffer is empty, it returns nil.
// The function also removes any "_extracted" suffix from "duplicate" fields.
func (j *JSONParser) buildJSONPathFromPrefixBuffer() []string {
	if len(j.prefixBuffer) == 0 {
		return nil
	}

	jsonPath := make([]string, 0, len(j.prefixBuffer))
	for _, part := range j.prefixBuffer {
		partStr := unsafe.String(unsafe.SliceData(part), len(part)) // #nosec G103 -- we know the string is not mutated -- nosemgrep: use-of-unsafe-block
		partStr = strings.TrimSuffix(partStr, DuplicateSuffix)
		jsonPath = append(jsonPath, partStr)
	}

	return jsonPath
}

// Hints implements Stage.
func (j *JSONParser) Hints() StageHints {
	return StageHints{CanModifyLabels: true, CanModifyLine: false}
}

func (j *JSONParser) RequiredLabelNames() []string { return []string{} }

func readValue(v []byte, dataType jsonparser.ValueType) string {
	switch dataType {
	case jsonparser.String:
		return unescapeJSONString(v)
	case jsonparser.Null:
		return ""
	case jsonparser.Number:
		return string(v)
	case jsonparser.Boolean:
		if bytes.Equal(v, trueBytes) {
			return trueString
		}
		return falseString
	default:
		return ""
	}
}

func unescapeJSONString(b []byte) string {
	var stackbuf [unescapeStackBufSize]byte // stack-allocated array for allocation-free unescaping of small strings
	bU, err := jsonparser.Unescape(b, stackbuf[:])
	if err != nil {
		return ""
	}
	res := string(bU)

	if strings.ContainsRune(res, utf8.RuneError) {
		res = strings.Map(removeInvalidUtf, res)
	}

	return res
}

type RegexpParser struct {
	regex     *regexp.Regexp
	nameIndex map[int]string

	keys    internedStringSet
	renames stageRenames
}

// NewRegexpParser creates a new log stage that can extract labels from a log line using a regex expression.
// The regex expression must contains at least one named match. If the regex doesn't match the line is not filtered out.
func NewRegexpParser(re string) (*RegexpParser, error) {
	regex, err := regexp.Compile(re)
	if err != nil {
		return nil, err
	}
	if regex.NumSubexp() == 0 {
		return nil, errMissingCapture
	}
	nameIndex := map[int]string{}
	uniqueNames := map[string]struct{}{}
	for i, n := range regex.SubexpNames() {
		if n != "" {
			if !model.UTF8Validation.IsValidLabelName(n) {
				return nil, fmt.Errorf("invalid extracted label name '%s'", n)
			}
			if _, ok := uniqueNames[n]; ok {
				return nil, fmt.Errorf("duplicate extracted label name '%s'", n)
			}
			nameIndex[i] = n
			uniqueNames[n] = struct{}{}
		}
	}
	if len(nameIndex) == 0 {
		return nil, errMissingCapture
	}
	return &RegexpParser{
		regex:     regex,
		nameIndex: nameIndex,
		keys:      internedStringSet{},
	}, nil
}

func (r *RegexpParser) Process(_ int64, line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	parserHints := lbs.ParserLabelHints()
	labelFilterHints := lbs.LabelFilterHints()
	if parserHints.NoLabels() {
		return line, true
	}

	r.renames.reset()
	for i, value := range r.regex.FindSubmatch(line) {
		if name, ok := r.nameIndex[i]; ok {
			key, ok := r.keys.Get(unsafeGetBytes(name), func() (string, bool) {
				sanitize := sanitizeLabelKey(name, true)
				if len(sanitize) == 0 {
					return "", false
				}

				return sanitize, true
			})
			if !ok {
				continue
			}

			renamed := false
			if lbs.BaseHas(key) || lbs.HasInCategory(key, StructuredMetadataLabel) {
				key = fmt.Sprintf("%s%s", key, DuplicateSuffix)
				renamed = true
			}

			if !parserHints.ShouldExtract(key) || !r.renames.shouldSet(lbs, key, renamed) {
				continue
			}

			lbs.Set(ParsedLabel, key, string(value))
			r.renames.record(key, renamed)
			if !continueParsing(labelFilterHints, key, renamed, lbs) {
				return line, false
			}
		}
	}
	return line, true
}

// Hints implements Stage.
func (r *RegexpParser) Hints() StageHints {
	return StageHints{CanModifyLabels: true, CanModifyLine: false}
}

func (r *RegexpParser) RequiredLabelNames() []string { return []string{} }

type LogfmtParser struct {
	strict    bool
	keepEmpty bool
	dec       *logfmt.Decoder
	keys      internedStringSet
	renames   stageRenames
}

// NewLogfmtParser creates a parser that can extract labels from a logfmt log line.
// Each keyval is extracted into a respective label.
func NewLogfmtParser(strict, keepEmpty bool) *LogfmtParser {
	return &LogfmtParser{
		strict:    strict,
		keepEmpty: keepEmpty,
		dec:       logfmt.NewDecoder(nil),
		keys:      internedStringSet{},
	}
}

func (l *LogfmtParser) Process(_ int64, line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	parserHints := lbs.ParserLabelHints()
	labelFilterHints := lbs.LabelFilterHints()
	if parserHints.NoLabels() {
		return line, true
	}

	l.dec.Reset(line)
	l.renames.reset()
	for !l.dec.EOL() {
		ok := l.dec.ScanKeyval()
		if !ok {
			// for strict parsing, do not continue on errs
			if l.strict {
				break
			}

			continue
		}

		key, ok := l.keys.Get(l.dec.Key(), func() (string, bool) {
			sanitized := sanitizeLabelKey(string(l.dec.Key()), true)
			if len(sanitized) == 0 {
				return "", false
			}

			return sanitized, true
		})
		if !ok {
			continue
		}

		renamed := false
		if lbs.BaseHas(key) || lbs.HasInCategory(key, StructuredMetadataLabel) {
			key = key + DuplicateSuffix
			renamed = true
		}

		if !parserHints.ShouldExtract(key) || !l.renames.shouldSet(lbs, key, renamed) {
			continue
		}

		val := l.dec.Value()

		if bytes.ContainsRune(val, utf8.RuneError) {
			val = bytes.Map(removeInvalidUtf, val)
		}

		if !l.keepEmpty && len(val) == 0 {
			continue
		}

		lbs.Set(ParsedLabel, key, string(val))
		l.renames.record(key, renamed)
		if !continueParsing(labelFilterHints, key, renamed, lbs) {
			return line, false
		}

		if parserHints.AllRequiredExtracted() {
			break
		}
	}

	if l.strict && l.dec.Err() != nil {
		lbs.SetErr(errLogfmt, l.dec.Err())

		if !labelFilterHints.ShouldContinueParsingLine(logqlmodel.ErrorLabel, lbs) {
			return line, false
		}
		return line, true
	}

	return line, true
}

// Hints implements Stage.
func (l *LogfmtParser) Hints() StageHints {
	return StageHints{CanModifyLabels: true, CanModifyLine: false}
}

func (l *LogfmtParser) RequiredLabelNames() []string { return []string{} }

type PatternParser struct {
	matcher *pattern.Matcher
	names   []string
	renames stageRenames
}

func NewPatternParser(pn string) (*PatternParser, error) {
	m, err := pattern.New(pn)
	if err != nil {
		return nil, err
	}
	for _, name := range m.Names() {
		if !model.UTF8Validation.IsValidLabelName(name) {
			return nil, fmt.Errorf("invalid capture label name '%s'", name)
		}
	}
	return &PatternParser{
		matcher: m,
		names:   m.Names(),
	}, nil
}

func (l *PatternParser) Process(_ int64, line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	parserHints := lbs.ParserLabelHints()
	labelFilterHints := lbs.LabelFilterHints()
	if parserHints.NoLabels() {
		return line, true
	}
	matches := l.matcher.Matches(line)
	names := l.names[:len(matches)]
	l.renames.reset()
	for i, m := range matches {
		name := names[i]
		renamed := false
		if lbs.BaseHas(name) || lbs.HasInCategory(name, StructuredMetadataLabel) {
			name = name + DuplicateSuffix
			renamed = true
		}

		if !parserHints.ShouldExtract(name) || !l.renames.shouldSet(lbs, name, renamed) {
			continue
		}

		lbs.Set(ParsedLabel, name, string(m))
		l.renames.record(name, renamed)
		if !continueParsing(labelFilterHints, name, renamed, lbs) {
			return line, false
		}
	}
	return line, true
}

// Hints implements Stage.
func (l *PatternParser) Hints() StageHints {
	return StageHints{CanModifyLabels: true, CanModifyLine: false}
}

func (l *PatternParser) RequiredLabelNames() []string { return []string{} }

type LogfmtExpressionParser struct {
	expressions map[string][]interface{}
	dec         *logfmt.Decoder
	keys        internedStringSet
	strict      bool

	// origKeys maps the identifier of each expression to the key that it reads from the line.
	origKeys map[string]string
	// written holds the label names that the current line wrote.
	written map[string]struct{}
}

func NewLogfmtExpressionParser(expressions []LabelExtractionExpr, strict bool) (*LogfmtExpressionParser, error) {
	if len(expressions) == 0 {
		return nil, fmt.Errorf("no logfmt expression provided")
	}
	paths := make(map[string][]interface{}, len(expressions))

	for _, exp := range expressions {
		path, err := logfmt.Parse(exp.Expression, false)
		if err != nil {
			return nil, fmt.Errorf("cannot parse expression [%s]: %w", exp.Expression, err)
		}

		if !model.UTF8Validation.IsValidLabelName(exp.Identifier) {
			return nil, fmt.Errorf("invalid extracted label name '%s'", exp.Identifier)
		}
		paths[exp.Identifier] = path
	}

	// The keys depend only on the expressions, so build them once instead of for each line.
	origKeys := make(map[string]string, len(paths))
	for id, path := range paths {
		origKeys[id] = fmt.Sprintf("%v", path...)
	}
	return &LogfmtExpressionParser{
		expressions: paths,
		origKeys:    origKeys,
		dec:         logfmt.NewDecoder(nil),
		keys:        internedStringSet{},
		strict:      strict,
		written:     make(map[string]struct{}, len(paths)),
	}, nil
}

// wantsOtherName reports whether a name of an expression other than current that the line has not
// written yet may still be wanted: the name, or its renamed form when a stream label or structured
// metadata holds it, passes the hints and no label has the renamed name. The parser stops reading a
// line when this is false.
func (l *LogfmtExpressionParser) wantsOtherName(lbs *LabelsBuilder, current string) bool {
	// The common query has one expression, so there is no other name.
	if len(l.expressions) == 1 {
		return false
	}
	hints := lbs.ParserLabelHints()
	for id := range l.expressions {
		if id == current {
			continue
		}
		name := id
		renamed := lbs.BaseHas(id) || lbs.HasInCategory(id, StructuredMetadataLabel)
		if renamed {
			name = id + DuplicateSuffix
		}
		if _, ok := l.written[name]; ok {
			continue
		}
		if renamed && (lbs.BaseHas(name) || lbs.HasInCategory(name, StructuredMetadataLabel) || hints.Extracted(name)) {
			continue
		}
		if hints.ShouldExtract(name) {
			return true
		}
	}
	return false
}

func (l *LogfmtExpressionParser) Process(_ int64, line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	// If there are no expressions, extract common labels
	// and add the suffix "_extracted"
	if len(l.expressions) == 0 {
		return line, false
	}

	if lbs.ParserLabelHints().NoLabels() {
		return line, true
	}

	clear(l.written)

	// alwaysExtract checks whether a key should be extracted regardless of other
	// conditions.
	alwaysExtract := func(key string) bool {
		// Any key in the expression list should always be extracted.
		_, ok := l.origKeys[key]
		return ok
	}

	l.dec.Reset(line)
	var current []byte
	for !l.dec.EOL() {
		ok := l.dec.ScanKeyval()
		if !ok {
			// for strict parsing, do not continue on errs
			if l.strict {
				break
			}

			continue
		}

		current = l.dec.Key()
		key, ok := l.keys.Get(current, func() (string, bool) {
			sanitized := sanitizeLabelKey(string(current), true)
			if len(sanitized) == 0 {
				return "", false
			}

			if !alwaysExtract(sanitized) && !lbs.ParserLabelHints().ShouldExtract(sanitized) {
				return "", false
			}
			return sanitized, true
		})
		if !ok || (!alwaysExtract(key) && lbs.ParserLabelHints().Extracted(key)) {
			continue
		}

		val := l.dec.Value()
		if bytes.ContainsRune(val, utf8.RuneError) {
			val = nil
		}

		for id, orig := range l.origKeys {
			if key == orig {
				key = id
				break
			}
		}

		if _, ok := l.expressions[key]; ok {
			if lbs.BaseHas(key) || lbs.HasInCategory(key, StructuredMetadataLabel) {
				id := key
				key = key + DuplicateSuffix
				if lbs.BaseHas(key) || lbs.HasInCategory(key, StructuredMetadataLabel) || lbs.ParserLabelHints().Extracted(key) || !lbs.ParserLabelHints().ShouldExtract(key) {
					// A label has the final name, or another key of this line or an earlier stage
					// wrote it, or the query does not need it. The next key of the line may still be
					// needed, so stop only when no other name is left to read.
					if !l.wantsOtherName(lbs, id) {
						break
					}
					continue
				}
			}

			lbs.Set(ParsedLabel, key, string(val))
			l.written[key] = struct{}{}

			if lbs.ParserLabelHints().AllRequiredExtracted() {
				break
			}
		}
	}

	// Every name of an expression gets a label, empty when the line has no such key. This happens
	// after the loop: an empty label that the loop had set in advance would count as extracted, so
	// it would block a renamed key and make AllRequiredExtracted stop the loop too early. A name
	// that a stream label or structured metadata holds gets no label.
	for id := range l.expressions {
		if _, ok := l.written[id]; ok {
			continue
		}
		if !lbs.BaseHas(id) && !lbs.HasInCategory(id, StructuredMetadataLabel) {
			lbs.Set(ParsedLabel, id, "")
		}
	}

	if l.strict && l.dec.Err() != nil {
		lbs.SetErr(errLogfmt, l.dec.Err())
		return line, true
	}

	return line, true
}

// Hints implements Stage.
func (l *LogfmtExpressionParser) Hints() StageHints {
	return StageHints{CanModifyLabels: true, CanModifyLine: false}
}

func (l *LogfmtExpressionParser) RequiredLabelNames() []string { return []string{} }

type JSONExpressionParser struct {
	ids   []string
	paths [][]string
	keys  internedStringSet
}

func NewJSONExpressionParser(expressions []LabelExtractionExpr) (*JSONExpressionParser, error) {
	var ids []string
	var paths [][]string
	for _, exp := range expressions {
		path, err := jsonexpr.Parse(exp.Expression, false)
		if err != nil {
			return nil, fmt.Errorf("cannot parse expression [%s]: %w", exp.Expression, err)
		}

		if !model.UTF8Validation.IsValidLabelName(exp.Identifier) {
			return nil, fmt.Errorf("invalid extracted label name '%s'", exp.Identifier)
		}

		ids = append(ids, exp.Identifier)
		paths = append(paths, JSONPathToStrings(path))
	}

	return &JSONExpressionParser{
		ids:   ids,
		paths: paths,
		keys:  internedStringSet{},
	}, nil
}

func JSONPathToStrings(paths []interface{}) []string {
	stringPaths := make([]string, 0, len(paths))
	for _, p := range paths {
		switch v := p.(type) {
		case int:
			stringPaths = append(stringPaths, fmt.Sprintf("[%d]", v))
		case string:
			stringPaths = append(stringPaths, v)
		}
	}
	return stringPaths
}

func (j *JSONExpressionParser) Process(_ int64, line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	if len(line) == 0 || lbs.ParserLabelHints().NoLabels() {
		return line, true
	}

	// Check that the line starts correctly
	// the parser will pass an error if other
	// parts of the line are malformed
	if !isValidJSONStart(line) {
		lbs.SetErr(errJSON, nil)
		return line, true
	}

	var matches int
	jsonparser.EachKey(line, func(idx int, data []byte, typ jsonparser.ValueType, err error) {
		if err != nil {
			lbs.SetErr(errJSON, err)
			return
		}

		identifier := j.ids[idx]
		key, _ := j.keys.Get(unsafeGetBytes(identifier), func() (string, bool) {
			return identifier, true
		})

		if lbs.BaseHas(key) || lbs.HasInCategory(key, StructuredMetadataLabel) {
			key = key + DuplicateSuffix
			// A renamed key must not overwrite a label that has the final name, or a value that
			// is already extracted under it, e.g. by another expression that asks for the literal
			// key (mirrors LogfmtExpressionParser).
			if lbs.BaseHas(key) || lbs.HasInCategory(key, StructuredMetadataLabel) || lbs.ParserLabelHints().Extracted(key) || !lbs.ParserLabelHints().ShouldExtract(key) {
				return
			}
		}

		switch typ {
		case jsonparser.Null:
			lbs.Set(ParsedLabel, key, "")
		case jsonparser.Object:
			lbs.Set(ParsedLabel, key, string(data))
		default:
			lbs.Set(ParsedLabel, key, unescapeJSONString(data))
		}

		matches++
	}, j.paths...)

	// Ensure there's a label for every value
	if matches < len(j.ids) {
		for _, id := range j.ids {
			if _, ok := lbs.Get(id); !ok {
				lbs.Set(ParsedLabel, id, "")
			}
		}
	}

	return line, true
}

func isValidJSONStart(data []byte) bool {
	switch data[0] {
	case '"', '{', '[':
		return true
	default:
		return false
	}
}

// Hints implements Stage.
func (j *JSONExpressionParser) Hints() StageHints {
	return StageHints{CanModifyLabels: true, CanModifyLine: false}
}

func (j *JSONExpressionParser) RequiredLabelNames() []string { return []string{} }

type UnpackParser struct {
	lbsBuffer []string
	// renamedBuffer holds, for each key/value pair in lbsBuffer, whether the
	// key was renamed with DuplicateSuffix due to a collision.
	renamedBuffer []bool
	keys          internedStringSet
	renames       stageRenames
}

// NewUnpackParser creates a new unpack stage.
// The unpack stage will parse a json log line as map[string]string where each key will be translated into labels.
// A special key _entry will also be used to replace the original log line. This is to be used in conjunction with Promtail pack stage.
// see https://grafana.com/docs/loki/latest/clients/promtail/stages/pack/
func NewUnpackParser() *UnpackParser {
	return &UnpackParser{
		lbsBuffer: make([]string, 0, 16),
		keys:      internedStringSet{},
	}
}

// Hints implements Stage.
func (u *UnpackParser) Hints() StageHints {
	return StageHints{
		// It turns the string-valued fields of a packed object into labels. It sets __error__ when
		// the line is not a JSON object.
		CanModifyLabels: true,
		// It replaces the line with the _entry value when the line carries one. It leaves the line
		// alone when the query needs no parsed label.
		CanModifyLine: true,
	}
}

func (UnpackParser) RequiredLabelNames() []string { return []string{} }

func (u *UnpackParser) Process(_ int64, line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	if len(line) == 0 || lbs.ParserLabelHints().NoLabels() {
		return line, true
	}

	// we only care about object and values.
	if line[0] != '{' {
		lbs.SetErr(errJSON, errUnexpectedJSONObject)
		return line, true
	}

	u.lbsBuffer = u.lbsBuffer[:0]
	u.renamedBuffer = u.renamedBuffer[:0]
	u.renames.reset()
	entry, err := u.unpack(line, lbs)
	if err != nil {
		if errors.Is(err, errLabelDoesNotMatch) {
			return entry, false
		}
		lbs.SetErr(errJSON, err)
		return line, true
	}

	return entry, true
}

func (u *UnpackParser) unpack(entry []byte, lbs *LabelsBuilder) ([]byte, error) {
	var isPacked bool
	err := jsonparser.ObjectEach(entry, func(key, value []byte, typ jsonparser.ValueType, _ int) error {
		switch typ {
		case jsonparser.String:
			if unsafeGetString(key) == logqlmodel.PackedEntryKey {
				// Inlined bytes escape to save allocs
				var stackbuf [unescapeStackBufSize]byte // stack-allocated array for allocation-free unescaping of small strings
				bU, err := jsonparser.Unescape(value, stackbuf[:])
				if err != nil {
					return err
				}

				entry = bU
				isPacked = true
				return nil
			}

			key, _ := u.keys.Get(key, func() (string, bool) {
				return string(key), true
			})

			renamed := false
			if lbs.BaseHas(key) || lbs.HasInCategory(key, StructuredMetadataLabel) {
				key = key + DuplicateSuffix
				renamed = true
			}

			if !lbs.ParserLabelHints().ShouldExtract(key) || lbs.ParserLabelHints().Extracted(key) {
				return nil
			}

			// append to the buffer of labels
			u.lbsBuffer = append(u.lbsBuffer, sanitizeLabelKey(key, true), unescapeJSONString(value))
			u.renamedBuffer = append(u.renamedBuffer, renamed)
		default:
			return nil
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// flush the buffer if we found a packed entry. Ties between a renamed key
	// and a literal key with the same final name are resolved here, once the
	// full line has been read, so the outcome does not depend on key order.
	if isPacked {
		for i := 0; i < len(u.lbsBuffer); i = i + 2 {
			key, renamed := u.lbsBuffer[i], u.renamedBuffer[i/2]
			if !u.renames.shouldSet(lbs, key, renamed) {
				continue
			}
			lbs.Set(ParsedLabel, key, u.lbsBuffer[i+1])
			u.renames.record(key, renamed)
			if !continueParsing(lbs.LabelFilterHints(), key, renamed, lbs) {
				return entry, errLabelDoesNotMatch
			}
		}
	}
	return entry, nil
}
