package log

import (
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/grafana/loki/pkg/logql/log/jsonexpr"
	"github.com/grafana/loki/pkg/logql/log/logfmt"

	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
)

const (
	jsonSpacer      = '_'
	duplicateSuffix = "_extracted"
	trueString      = "true"
	falseString     = "false"
	PackedEntryKey  = "_entry"
)

var (
	_ Stage = &JSONParser{}
	_ Stage = &RegexpParser{}
	_ Stage = &LogfmtParser{}

	errMissingCapture = errors.New("at least one named capture must be supplied")
)

type JSONParser struct {
	buf []byte // buffer used to build json keys
	lbs *LabelsBuilder
}

// NewJSONParser creates a log stage that can parse a json log line and add properties as labels.
func NewJSONParser() *JSONParser {
	return &JSONParser{
		buf: make([]byte, 0, 1024),
	}
}

func (j *JSONParser) Process(line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	if lbs.ParserLabelHints().NoLabels() {
		return line, true
	}
	it := jsoniter.ConfigFastest.BorrowIterator(line)
	defer jsoniter.ConfigFastest.ReturnIterator(it)

	// reset the state.
	j.buf = j.buf[:0]
	j.lbs = lbs

	if err := j.readObject(it); err != nil {
		lbs.SetErr(errJSON)
		return line, true
	}
	return line, true
}

func (j *JSONParser) readObject(it *jsoniter.Iterator) error {
	// we only care about object and values.
	if nextType := it.WhatIsNext(); nextType != jsoniter.ObjectValue {
		return fmt.Errorf("expecting json object(%d), got %d", jsoniter.ObjectValue, nextType)
	}
	_ = it.ReadMapCB(j.parseMap(""))
	if it.Error != nil && it.Error != io.EOF {
		return it.Error
	}
	return nil
}

func (j *JSONParser) parseMap(prefix string) func(iter *jsoniter.Iterator, field string) bool {
	return func(iter *jsoniter.Iterator, field string) bool {
		switch iter.WhatIsNext() {
		// are we looking at a value that needs to be added ?
		case jsoniter.StringValue, jsoniter.NumberValue, jsoniter.BoolValue:
			j.parseLabelValue(iter, prefix, field)
		// Or another new object based on a prefix.
		case jsoniter.ObjectValue:
			if key, ok := j.nextKeyPrefix(prefix, field); ok {
				return iter.ReadMapCB(j.parseMap(key))
			}
			// If this keys is not expected we skip the object
			iter.Skip()
		default:
			iter.Skip()
		}
		return true
	}
}

func (j *JSONParser) nextKeyPrefix(prefix, field string) (string, bool) {
	// first time we add return the field as prefix.
	if len(prefix) == 0 {
		field = sanitizeLabelKey(field, true)
		if j.lbs.ParserLabelHints().ShouldExtractPrefix(field) {
			return field, true
		}
		return "", false
	}
	// otherwise we build the prefix and check using the buffer
	j.buf = j.buf[:0]
	j.buf = append(j.buf, prefix...)
	j.buf = append(j.buf, byte(jsonSpacer))
	j.buf = append(j.buf, sanitizeLabelKey(field, false)...)
	// if matches keep going
	if j.lbs.ParserLabelHints().ShouldExtractPrefix(string(j.buf)) {
		return string(j.buf), true
	}
	return "", false
}

func (j *JSONParser) parseLabelValue(iter *jsoniter.Iterator, prefix, field string) {
	// the first time we use the field as label key.
	if len(prefix) == 0 {
		field = sanitizeLabelKey(field, true)
		if !j.lbs.ParserLabelHints().ShouldExtract(field) {
			// we can skip the value
			iter.Skip()
			return

		}
		if j.lbs.BaseHas(field) {
			field = field + duplicateSuffix
		}
		j.lbs.Set(field, readValue(iter))
		return

	}
	// otherwise we build the label key using the buffer
	j.buf = j.buf[:0]
	j.buf = append(j.buf, prefix...)
	j.buf = append(j.buf, byte(jsonSpacer))
	j.buf = append(j.buf, sanitizeLabelKey(field, false)...)
	if j.lbs.BaseHas(string(j.buf)) {
		j.buf = append(j.buf, duplicateSuffix...)
	}
	if !j.lbs.ParserLabelHints().ShouldExtract(string(j.buf)) {
		iter.Skip()
		return
	}
	j.lbs.Set(string(j.buf), readValue(iter))
}

func (j *JSONParser) RequiredLabelNames() []string { return []string{} }

func readValue(iter *jsoniter.Iterator) string {
	switch iter.WhatIsNext() {
	case jsoniter.StringValue:
		return iter.ReadString()
	case jsoniter.NumberValue:
		return iter.ReadNumber().String()
	case jsoniter.BoolValue:
		if iter.ReadBool() {
			return trueString
		}
		return falseString
	default:
		iter.Skip()
		return ""
	}
}

func addLabel(lbs *LabelsBuilder, key, value string) {
	key = sanitizeLabelKey(key, true)
	if len(key) == 0 {
		return
	}
	if lbs.BaseHas(key) {
		key = fmt.Sprintf("%s%s", key, duplicateSuffix)
	}
	lbs.Set(key, value)
}

type RegexpParser struct {
	regex     *regexp.Regexp
	nameIndex map[int]string
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
			if !model.LabelName(n).IsValid() {
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
	}, nil
}

func mustNewRegexParser(re string) *RegexpParser {
	r, err := NewRegexpParser(re)
	if err != nil {
		panic(err)
	}
	return r
}

func (r *RegexpParser) Process(line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	for i, value := range r.regex.FindSubmatch(line) {
		if name, ok := r.nameIndex[i]; ok {
			addLabel(lbs, name, string(value))
		}
	}
	return line, true
}

func (r *RegexpParser) RequiredLabelNames() []string { return []string{} }

type LogfmtParser struct {
	dec *logfmt.Decoder
}

// NewLogfmtParser creates a parser that can extract labels from a logfmt log line.
// Each keyval is extracted into a respective label.
func NewLogfmtParser() *LogfmtParser {
	return &LogfmtParser{
		dec: logfmt.NewDecoder(nil),
	}
}

func (l *LogfmtParser) Process(line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	if lbs.ParserLabelHints().NoLabels() {
		return line, true
	}
	l.dec.Reset(line)
	for l.dec.ScanKeyval() {
		if !lbs.ParserLabelHints().ShouldExtract(sanitizeLabelKey(string(l.dec.Key()), true)) {
			continue
		}
		key := string(l.dec.Key())
		val := string(l.dec.Value())
		addLabel(lbs, key, val)
	}
	if l.dec.Err() != nil {
		lbs.SetErr(errLogfmt)
		return line, true
	}
	return line, true
}

func (l *LogfmtParser) RequiredLabelNames() []string { return []string{} }

type JSONExpressionParser struct {
	expressions map[string][]interface{}
}

func NewJSONExpressionParser(expressions []JSONExpression) (*JSONExpressionParser, error) {
	paths := make(map[string][]interface{})

	for _, exp := range expressions {
		path, err := jsonexpr.Parse(exp.Expression, false)
		if err != nil {
			return nil, fmt.Errorf("cannot parse expression [%s]: %w", exp.Expression, err)
		}

		if !model.LabelName(exp.Identifier).IsValid() {
			return nil, fmt.Errorf("invalid extracted label name '%s'", exp.Identifier)
		}

		paths[exp.Identifier] = path
	}

	return &JSONExpressionParser{
		expressions: paths,
	}, nil
}

func (j *JSONExpressionParser) Process(line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	if lbs.ParserLabelHints().NoLabels() {
		return line, true
	}

	if !jsoniter.ConfigFastest.Valid(line) {
		lbs.SetErr(errJSON)
		return line, true
	}

	for identifier, paths := range j.expressions {
		result := jsoniter.ConfigFastest.Get(line, paths...).ToString()

		if lbs.BaseHas(identifier) {
			identifier = identifier + duplicateSuffix
		}

		lbs.Set(identifier, result)
	}

	return line, true
}

func (j *JSONExpressionParser) RequiredLabelNames() []string { return []string{} }

type UnpackParser struct {
	lbsBuffer []string
}

// NewUnpackParser creates a new unpack stage.
// The unpack stage will parse a json log line as map[string]string where each key will be translated into labels.
// A special key _entry will also be used to replace the original log line. This is to be used in conjunction with Promtail pack stage.
// see https://grafana.com/docs/loki/latest/clients/promtail/stages/pack/
func NewUnpackParser() *UnpackParser {
	return &UnpackParser{
		lbsBuffer: make([]string, 0, 16),
	}
}

func (UnpackParser) RequiredLabelNames() []string { return []string{} }

func (u *UnpackParser) Process(line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	if lbs.ParserLabelHints().NoLabels() {
		return line, true
	}
	u.lbsBuffer = u.lbsBuffer[:0]
	it := jsoniter.ConfigFastest.BorrowIterator(line)
	defer jsoniter.ConfigFastest.ReturnIterator(it)

	entry, err := u.unpack(it, line, lbs)
	if err != nil {
		lbs.SetErr(errJSON)
		return line, true
	}
	return entry, true
}

func (u *UnpackParser) unpack(it *jsoniter.Iterator, entry []byte, lbs *LabelsBuilder) ([]byte, error) {
	// we only care about object and values.
	if nextType := it.WhatIsNext(); nextType != jsoniter.ObjectValue {
		return nil, fmt.Errorf("expecting json object(%d), got %d", jsoniter.ObjectValue, nextType)
	}
	var isPacked bool
	_ = it.ReadMapCB(func(iter *jsoniter.Iterator, field string) bool {
		switch iter.WhatIsNext() {
		case jsoniter.StringValue:
			// we only unpack map[string]string. Anything else is skipped.
			if field == PackedEntryKey {
				// todo(ctovena): we should just reslice the original line since the property is contiguous
				// but jsoniter doesn't allow us to do this right now.
				// https://github.com/buger/jsonparser might do a better job at this.
				entry = []byte(iter.ReadString())
				isPacked = true
				return true
			}
			if !lbs.ParserLabelHints().ShouldExtract(field) {
				iter.Skip()
				return true
			}
			if lbs.BaseHas(field) {
				field = field + duplicateSuffix
			}
			// append to the buffer of labels
			u.lbsBuffer = append(u.lbsBuffer, field, iter.ReadString())
		default:
			iter.Skip()
		}
		return true
	})
	if it.Error != nil && it.Error != io.EOF {
		return nil, it.Error
	}
	// flush the buffer if we found a packed entry.
	if isPacked {
		for i := 0; i < len(u.lbsBuffer); i = i + 2 {
			lbs.Set(u.lbsBuffer[i], u.lbsBuffer[i+1])
		}
	}
	return entry, nil
}
