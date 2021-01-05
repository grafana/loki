package log

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/grafana/loki/pkg/logql/log/logfmt"

	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
)

const (
	jsonSpacer      = '_'
	duplicateSuffix = "_extracted"
	trueString      = "true"
	falseString     = "false"
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
		if isValidKeyPrefix(field, j.lbs.ParserLabelHints()) {
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
	if isValidKeyPrefix(string(j.buf), j.lbs.ParserLabelHints()) {
		return string(j.buf), true
	}
	return "", false

}

// isValidKeyPrefix extract an object if the prefix is valid
func isValidKeyPrefix(objectprefix string, hints []string) bool {
	if len(hints) == 0 {
		return true
	}
	for _, k := range hints {
		if strings.HasPrefix(k, objectprefix) {
			return true
		}
	}
	return false
}

func (j *JSONParser) parseLabelValue(iter *jsoniter.Iterator, prefix, field string) {
	// the first time we use the field as label key.
	if len(prefix) == 0 {
		field = sanitizeLabelKey(field, true)
		if !shouldExtractKey(field, j.lbs.ParserLabelHints()) {
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
	if !shouldExtractKey(string(j.buf), j.lbs.ParserLabelHints()) {
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

func shouldExtractKey(key string, hints []string) bool {
	if len(hints) == 0 {
		return true
	}
	for _, k := range hints {
		if k == key {
			return true
		}
	}
	return false
}

func addLabel(lbs *LabelsBuilder, key, value string) {
	key = sanitizeLabelKey(key, true)
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
	l.dec.Reset(line)
	for l.dec.ScanKeyval() {
		if !shouldExtractKey(string(l.dec.Key()), lbs.ParserLabelHints()) {
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
