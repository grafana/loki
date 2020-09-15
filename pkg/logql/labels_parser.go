package logql

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/grafana/loki/pkg/logql/logfmt"
	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/prometheus/pkg/labels"
)

const (
	jsonSpacer = "_"

	errJson    = "JSONParserError"
	errLogfmt  = "LogfmtParserError"
	errorLabel = "__error__"

	duplicateSuffix = "_extracted"
)

var (
	errMissingCapture = errors.New("at least one named capture must be supplied")
)

type LabelParser interface {
	Parse(line []byte, lbs labels.Labels) labels.Labels
}

type jsonParser struct {
	builder *labels.Builder
}

func NewJSONParser() *jsonParser {
	return &jsonParser{
		builder: labels.NewBuilder(nil),
	}
}

func (j *jsonParser) Parse(line []byte, lbs labels.Labels) labels.Labels {
	data := map[string]interface{}{}
	j.builder.Reset(lbs)
	err := jsoniter.ConfigFastest.Unmarshal(line, &data)
	if err != nil {
		j.builder.Set(errorLabel, errJson)
		return j.builder.Labels()
	}
	parseMap("", data, addLabel(j.builder, lbs))
	return j.builder.Labels()
}

func addLabel(builder *labels.Builder, lbs labels.Labels) func(key, value string) {
	return func(key, value string) {
		if lbs.Has(key) {
			key = fmt.Sprintf("%s%s", key, duplicateSuffix)
		}
		builder.Set(key, value)
	}
}

func parseMap(prefix string, data map[string]interface{}, add func(key, value string)) {
	for key, val := range data {
		switch concrete := val.(type) {
		case map[string]interface{}:
			parseMap(jsonKey(prefix, key), concrete, add)
		case string:
			add(jsonKey(prefix, key), concrete)
		case float64:
			f := strconv.FormatFloat(concrete, 'f', -1, 64)
			add(jsonKey(prefix, key), f)
		}
	}
}

func jsonKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return fmt.Sprintf("%s%s%s", prefix, jsonSpacer, key)
}

type regexpParser struct {
	regex     *regexp.Regexp
	builder   *labels.Builder
	nameIndex map[int]string
}

func NewRegexpParser(re string) (*regexpParser, error) {
	regex, err := regexp.Compile(re)
	if err != nil {
		return nil, err
	}
	if regex.NumSubexp() == 0 {
		return nil, errMissingCapture
	}
	nameIndex := map[int]string{}
	for i, n := range regex.SubexpNames() {
		if n != "" {
			nameIndex[i] = n
		}
	}
	if len(nameIndex) == 0 {
		return nil, errMissingCapture
	}
	return &regexpParser{
		regex:     regex,
		builder:   labels.NewBuilder(nil),
		nameIndex: nameIndex,
	}, nil
}

func (r *regexpParser) Parse(line []byte, lbs labels.Labels) labels.Labels {
	r.builder.Reset(lbs)
	for i, value := range r.regex.FindSubmatch(line) {
		if name, ok := r.nameIndex[i]; ok {
			addLabel(r.builder, lbs)(name, string(value))
		}
	}
	return r.builder.Labels()
}

type logfmtParser struct {
	builder *labels.Builder
	dec     *logfmt.Decoder
}

func NewLogfmtParser() *logfmtParser {
	return &logfmtParser{
		builder: labels.NewBuilder(nil),
		dec:     logfmt.NewDecoder(),
	}
}

func (l *logfmtParser) Parse(line []byte, lbs labels.Labels) labels.Labels {
	l.builder.Reset(lbs)
	l.dec.Reset(line)

	for l.dec.ScanKeyval() {
		addLabel(l.builder, lbs)(string(l.dec.Key()), string(l.dec.Value()))
	}
	if l.dec.Err() != nil {
		l.builder.Set(errorLabel, errLogfmt)
	}
	return l.builder.Labels()
}
