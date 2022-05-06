package log

import (
	"bytes"
	"fmt"
	"unicode/utf8"

	"github.com/buger/jsonparser"
	jsoniter "github.com/json-iterator/go"
)

const (
	jsonSpacer      = '_'
	duplicateSuffix = "_extracted"
	trueString      = "true"
	falseString     = "false"
)

var (
	_                       Stage = &JSONParser{}
	errUnexpectedJSONObject       = fmt.Errorf("expecting json object(%d), but it is not", jsoniter.ObjectValue)
)

type JSONParser struct {
	buf    []byte // buffer used to build json keys
	prefix []byte
	lbs    *LabelsBuilder

	keys internedStringSet
}

// NewJSONParser creates a log stage that can parse a json log line and add properties as labels.
func NewJSONParser() *JSONParser {
	return &JSONParser{
		buf:    make([]byte, 0, 1024),
		prefix: make([]byte, 0, 1024),
		keys:   internedStringSet{},
	}
}

func (j *JSONParser) Process(line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	if lbs.ParserLabelHints().NoLabels() {
		return line, true
	}

	j.lbs = lbs
	j.prefix = j.prefix[:0]
	j.buf = j.buf[:0]
	if err := jsonparser.ObjectEach(line, j.objectCallback); err != nil {
		lbs.SetErr(errJSON)
		return line, true
	}
	return line, true
}

func (j *JSONParser) objectCallback(key, value []byte, dataType jsonparser.ValueType, offset int) error {
	switch dataType {
	case jsonparser.String, jsonparser.Number, jsonparser.Boolean, jsonparser.Null:
		j.handleValue(key, value, dataType)
		return nil
	case jsonparser.Object:
		var ok bool
		old := j.prefix
		j.prefix, ok = j.appendPrefix(j.prefix, key)
		if !ok {
			j.prefix = old
			return nil
		}
		err := jsonparser.ObjectEach(value, j.objectCallback)
		j.prefix = old
		return err
	}
	return nil
}

func (j *JSONParser) handleValue(field []byte, value []byte, dataType jsonparser.ValueType) {
	// the first time we use the field as label key.
	if len(j.prefix) == 0 {
		key, ok := j.keys.Get(field, func() (string, bool) {
			j.buf = sanitizeLabelTo(field, j.buf, true)
			if j.lbs.BaseHas(unsafeGetString(j.buf)) {
				j.buf = append(j.buf, duplicateSuffix...)
			}
			if !j.lbs.ParserLabelHints().ShouldExtract(unsafeGetString(j.buf)) {
				return "", false
			}

			return string(j.buf), true
		})
		if !ok {
			return
		}
		j.lbs.Set(key, j.valueString(value, dataType))
		return

	}
	// otherwise we build the label key using the buffer
	old := j.prefix
	defer func() { j.prefix = old }()
	j.prefix = append(j.prefix, byte(jsonSpacer))
	j.buf = sanitizeLabelTo(field, j.buf, false)
	j.prefix = append(j.prefix, j.buf...)
	key, ok := j.keys.Get(j.prefix, func() (string, bool) {
		if j.lbs.BaseHas(unsafeGetString(j.prefix)) {
			j.prefix = append(j.prefix, duplicateSuffix...)
		}
		if !j.lbs.ParserLabelHints().ShouldExtract(unsafeGetString(j.prefix)) {
			return "", false
		}
		return string(j.prefix), true
	})
	if !ok {
		return
	}
	j.lbs.Set(key, j.valueString(value, dataType))
}

func (j *JSONParser) valueString(value []byte, dataType jsonparser.ValueType) string {
	switch dataType {
	case jsonparser.String:
		if bytes.ContainsRune(value, utf8.RuneError) {
			return ""
		}
		// todo if j.buf has grown it won't be reused.
		// unlikely to happen.
		v, err := jsonparser.Unescape(value, j.buf)
		if err != nil {
			return ""
		}
		return string(j.buf)
	case jsonparser.Number:
		return string(value)
	case jsonparser.Boolean:
		if value[0] == 't' || value[0] == 'T' {
			return trueString
		}
		return falseString
	case jsonparser.Null:
		return ""
	default:
		return ""
	}
}

func (j *JSONParser) appendPrefix(prefix, field []byte) ([]byte, bool) {
	// first time we add return the field as prefix.
	if len(prefix) == 0 {
		prefix = sanitizeLabelTo(field, j.prefix, true)
		if j.lbs.ParserLabelHints().ShouldExtractPrefix(unsafeGetString(prefix)) {
			return prefix, true
		}
		return nil, false
	}
	// otherwise we build the prefix and check using the buffer
	prefix = append(prefix, byte(jsonSpacer))
	j.buf = sanitizeLabelTo(field, j.buf, false)
	prefix = append(prefix, j.buf...)
	// if matches keep going
	if j.lbs.ParserLabelHints().ShouldExtractPrefix(unsafeGetString(prefix)) {
		return prefix, true
	}
	return nil, false
}

func (j *JSONParser) RequiredLabelNames() []string { return []string{} }
