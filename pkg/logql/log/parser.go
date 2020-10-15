package log

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/grafana/loki/pkg/logql/log/logfmt"

	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
)

const (
	jsonSpacer      = "_"
	duplicateSuffix = "_extracted"
)

var (
	_ Stage = &jsonParser{}
	_ Stage = &regexpParser{}
	_ Stage = &logfmtParser{}

	errMissingCapture = errors.New("at least one named capture must be supplied")

	underscore = "_"
	point      = "."
	dash       = "-"
)

func addLabel(lbs Labels) func(key, value string) {
	unique := map[string]struct{}{}
	return func(key, value string) {
		_, ok := unique[key]
		if ok {
			return
		}
		unique[key] = struct{}{}
		key = strings.ReplaceAll(strings.ReplaceAll(key, point, underscore), dash, underscore)
		if lbs.Has(key) {
			key = fmt.Sprintf("%s%s", key, duplicateSuffix)
		}
		lbs[key] = value
	}
}

type jsonParser struct{}

func NewJSONParser() *jsonParser {
	return &jsonParser{}
}

func (j *jsonParser) Process(line []byte, lbs Labels) ([]byte, bool) {
	data := map[string]interface{}{}
	err := jsoniter.ConfigFastest.Unmarshal(line, &data)
	if err != nil {
		lbs.SetError(errJSON)
		return line, true
	}
	parseMap("", data, addLabel(lbs))
	return line, true
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
	return &regexpParser{
		regex:     regex,
		nameIndex: nameIndex,
	}, nil
}

func mustNewRegexParser(re string) *regexpParser {
	r, err := NewRegexpParser(re)
	if err != nil {
		panic(err)
	}
	return r
}

func (r *regexpParser) Process(line []byte, lbs Labels) ([]byte, bool) {
	add := addLabel(lbs)
	for i, value := range r.regex.FindSubmatch(line) {
		if name, ok := r.nameIndex[i]; ok {
			add(name, string(value))
		}
	}
	return line, true
}

type logfmtParser struct{}

func NewLogfmtParser() *logfmtParser {
	return &logfmtParser{}
}

func (l *logfmtParser) Process(line []byte, lbs Labels) ([]byte, bool) {
	// todo(cyriltovena): we should be using the same decoder for the whole query.
	// However right now backward queries, because of the batch iterator that has a go loop,
	// can run this method in parallel. This causes a race e.g it will reset to a new line while scaning for keyvals.
	dec := logfmt.NewDecoder(line)
	add := addLabel(lbs)
	for dec.ScanKeyval() {
		key := string(dec.Key())
		val := string(dec.Value())
		add(key, val)
	}
	if dec.Err() != nil {
		lbs.SetError(errLogfmt)
		return line, true
	}
	return line, true
}
