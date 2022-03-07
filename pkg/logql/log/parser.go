package log

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/buger/jsonparser"
	"github.com/grafana/regexp"
	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logql/log/jsonexpr"
	"github.com/grafana/loki/pkg/logql/log/logfmt"
	"github.com/grafana/loki/pkg/logql/log/pattern"
	"github.com/grafana/loki/pkg/logqlmodel"
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

	errUnexpectedJSONObject = fmt.Errorf("expecting json object(%d), but it is not", jsoniter.ObjectValue)
	errMissingCapture       = errors.New("at least one named capture must be supplied")
)

type JSONParser struct {
	buf []byte // buffer used to build json keys
	lbs *LabelsBuilder

	keys                  internedStringSet
	bugerJSONParserEnable bool
	jsonKeyCache          map[string][][]string
}

// NewJSONParser creates a log stage that can parse a json log line and add properties as labels.
func NewJSONParser() *JSONParser {
	return &JSONParser{
		buf:                   make([]byte, 0, 1024),
		keys:                  internedStringSet{},
		bugerJSONParserEnable: true,
		jsonKeyCache:          make(map[string][][]string),
	}
}

func (j *JSONParser) Process(line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	if lbs.ParserLabelHints().NoLabels() {
		return line, true
	}
	j.lbs = lbs
	if j.bugerJSONParserEnable {
		requiredLabels := j.lbs.ParserLabelHints().RequiredLabels()
		if len(requiredLabels) > 0 {
			err := j.parseJSONKeyVal(line, requiredLabels)
			if err != nil {
				lbs.SetErr(errJSON)
			}
			return line, true
		}
	}

	err := j.handleJson("", line)
	if err != nil {
		lbs.SetErr(errJSON)
		return line, true
	}

	return line, true
}

func (j *JSONParser) RequiredLabelNames() []string { return []string{} }

func (j *JSONParser) handleJson(preKey string, line []byte) error {
	err := jsonparser.ObjectEach(line, func(key []byte, val []byte, valueType jsonparser.ValueType, offset int) error {
		jsonKey := preKey + sanitizeLabelKey(string(key), true)

		if valueType == jsonparser.Array {
			return nil
		}
		if valueType == jsonparser.Object {
			return j.handleJson(jsonKey+"_", val)
		}
		jsonErr := j.setJSONVal(jsonKey, val, valueType)
		if jsonErr != nil {
			return jsonErr
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func (j *JSONParser) parseJSONKeyVal(line []byte, requiredLabels []string) error {
	//check for "jsonParser-not json line" benchmark
	if strings.Index(string(line), "{") != 0 {
		return errors.New("illegal format")
	}
	for _, field := range requiredLabels {
		field = sanitizeLabelKey(field, true)
		keys := make([]string, 0)
		keys = append(keys, field)

		cacheKeys, ok := j.jsonKeyCache[field]
		if ok {
			isMatchKey := false
			for _, key := range cacheKeys {
				v, t, _, e := jsonparser.Get(line, key...)
				if e != nil {
					continue
				}
				err := j.setJSONVal(field, v, t)
				if err != nil {
					return err
				}
				isMatchKey = true
				break
			}
			if isMatchKey {
				continue
			}
		}

		keyArray := make([]string, 0)
		jsonSpacerIndex := 0
		for {
			v, t, _, e := jsonparser.Get(line, keys...)
			if e != nil {
				if e != jsonparser.KeyPathNotFoundError {
					return e
				}
				if !strings.ContainsRune(field, jsonSpacer) {
					break
				}
				if jsonSpacerIndex == 0 {
					keyArray = strings.Split(field, "_")
				}
				if jsonSpacerIndex > len(keyArray) {
					break
				}
				keys = keys[0:0]
				subKey := ""
				for idx, key := range keyArray {
					if idx <= jsonSpacerIndex {
						keys = append(keys, key)
						continue
					}
					if idx > 1 {
						subKey += "_" + key
					} else {
						subKey += key
					}
				}
				keys = append(keys, subKey)
				jsonSpacerIndex++
				continue
			}
			err := j.setJSONVal(field, v, t)
			if err != nil {
				return err
			}
			if len(keys) > 1 { //just cache key >1
				cacheKeys, ok := j.jsonKeyCache[field]
				if !ok {
					j.jsonKeyCache[field] = make([][]string, 0)
					cacheKeys = j.jsonKeyCache[field]
				}
				cacheKeys = append(cacheKeys, keys)
				j.jsonKeyCache[field] = cacheKeys
			}
			break
		}
	}
	return nil
}

func (j *JSONParser) setJSONVal(field string, v []byte, t jsonparser.ValueType) error {
	val, err := parseStringVal(v, t)
	if err != nil {
		return err
	}
	if j.lbs.BaseHas(field) {
		field = field + duplicateSuffix
	}
	j.lbs.Set(field, val)
	return nil
}

func parseStringVal(v []byte, t jsonparser.ValueType) (string, error) {
	if 1 == 1 {
		return string(v), nil
	}
	var val string
	var err error
	if t == jsonparser.String {
		if bytes.IndexByte(v, '\\') == -1 {
			val = string(v)
		} else {
			val, err = jsonparser.ParseString(v)
			if err != nil {
				return val, err
			}
		}
	} else if t == jsonparser.Number {
		intVal, err := jsonparser.ParseInt(v)
		if err != nil {
			return val, err
		}
		val = strconv.FormatInt(intVal, 10)
	} else if t == jsonparser.Boolean {
		boolVal, err := jsonparser.ParseBoolean(v)
		if err != nil {
			return val, err
		}
		if boolVal {
			val = trueString
		} else {
			val = falseString
		}
	} else {
		return val, errors.New("unsupported type")
	}
	return val, nil
}

type RegexpParser struct {
	regex     *regexp.Regexp
	nameIndex map[int]string

	keys internedStringSet
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
		keys:      internedStringSet{},
	}, nil
}

func (r *RegexpParser) Process(line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	for i, value := range r.regex.FindSubmatch(line) {
		if name, ok := r.nameIndex[i]; ok {
			key, ok := r.keys.Get(unsafeGetBytes(name), func() (string, bool) {
				sanitize := sanitizeLabelKey(name, true)
				if len(sanitize) == 0 {
					return "", false
				}
				if lbs.BaseHas(sanitize) {
					sanitize = fmt.Sprintf("%s%s", sanitize, duplicateSuffix)
				}
				return sanitize, true
			})
			if !ok {
				continue
			}
			lbs.Set(key, string(value))
		}
	}
	return line, true
}

func (r *RegexpParser) RequiredLabelNames() []string { return []string{} }

type LogfmtParser struct {
	dec  *logfmt.Decoder
	keys internedStringSet
}

// NewLogfmtParser creates a parser that can extract labels from a logfmt log line.
// Each keyval is extracted into a respective label.
func NewLogfmtParser() *LogfmtParser {
	return &LogfmtParser{
		dec:  logfmt.NewDecoder(nil),
		keys: internedStringSet{},
	}
}

func (l *LogfmtParser) Process(line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	if lbs.ParserLabelHints().NoLabels() {
		return line, true
	}
	l.dec.Reset(line)
	for l.dec.ScanKeyval() {
		key, ok := l.keys.Get(l.dec.Key(), func() (string, bool) {
			sanitized := sanitizeLabelKey(string(l.dec.Key()), true)
			if !lbs.ParserLabelHints().ShouldExtract(sanitized) {
				return "", false
			}
			if len(sanitized) == 0 {
				return "", false
			}
			if lbs.BaseHas(sanitized) {
				sanitized = fmt.Sprintf("%s%s", sanitized, duplicateSuffix)
			}
			return sanitized, true
		})
		if !ok {
			continue
		}
		val := l.dec.Value()
		// the rune error replacement is rejected by Prometheus, so we skip it.
		if bytes.ContainsRune(val, utf8.RuneError) {
			val = nil
		}
		lbs.Set(key, string(val))
	}
	if l.dec.Err() != nil {
		lbs.SetErr(errLogfmt)
		return line, true
	}
	return line, true
}

func (l *LogfmtParser) RequiredLabelNames() []string { return []string{} }

type PatternParser struct {
	matcher pattern.Matcher
	names   []string
}

func NewPatternParser(pn string) (*PatternParser, error) {
	m, err := pattern.New(pn)
	if err != nil {
		return nil, err
	}
	for _, name := range m.Names() {
		if !model.LabelName(name).IsValid() {
			return nil, fmt.Errorf("invalid capture label name '%s'", name)
		}
	}
	return &PatternParser{
		matcher: m,
		names:   m.Names(),
	}, nil
}

func (l *PatternParser) Process(line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	if lbs.ParserLabelHints().NoLabels() {
		return line, true
	}
	matches := l.matcher.Matches(line)
	names := l.names[:len(matches)]
	for i, m := range matches {
		name := names[i]
		if !lbs.parserKeyHints.ShouldExtract(name) {
			continue
		}
		if lbs.BaseHas(name) {
			name = name + duplicateSuffix
		}

		lbs.Set(name, string(m))
	}
	return line, true
}

func (l *PatternParser) RequiredLabelNames() []string { return []string{} }

type JSONExpressionParser struct {
	expressions map[string][]interface{}

	keys internedStringSet
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
		keys:        internedStringSet{},
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
		key, _ := j.keys.Get(unsafeGetBytes(identifier), func() (string, bool) {
			if lbs.BaseHas(identifier) {
				identifier = identifier + duplicateSuffix
			}
			return identifier, true
		})

		lbs.Set(key, result)
	}

	return line, true
}

func (j *JSONExpressionParser) RequiredLabelNames() []string { return []string{} }

type UnpackParser struct {
	lbsBuffer []string

	keys internedStringSet
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
		return nil, errUnexpectedJSONObject
	}
	var isPacked bool
	_ = it.ReadMapCB(func(iter *jsoniter.Iterator, field string) bool {
		switch iter.WhatIsNext() {
		case jsoniter.StringValue:
			// we only unpack map[string]string. Anything else is skipped.
			if field == logqlmodel.PackedEntryKey {
				// todo(ctovena): we should just reslice the original line since the property is contiguous
				// but jsoniter doesn't allow us to do this right now.
				// https://github.com/buger/jsonparser might do a better job at this.
				entry = []byte(iter.ReadString())
				isPacked = true
				return true
			}
			key, ok := u.keys.Get(unsafeGetBytes(field), func() (string, bool) {
				if !lbs.ParserLabelHints().ShouldExtract(field) {
					return "", false
				}
				if lbs.BaseHas(field) {
					field = field + duplicateSuffix
				}
				return field, true
			})
			if !ok {
				iter.Skip()
				return true
			}

			// append to the buffer of labels
			u.lbsBuffer = append(u.lbsBuffer, key, iter.ReadString())
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
