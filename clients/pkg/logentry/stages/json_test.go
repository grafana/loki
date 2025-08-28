package stages

import (
	"reflect"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var testJSONYamlSingleStageWithoutSource = `
pipeline_stages:
- json:
    expressions:
      out:  message
      app:
      nested:
      duration:
      unknown:
`

var testJSONYamlMultiStageWithSource = `
pipeline_stages:
- json:
    expressions:
      extra:
- json:
    expressions:
      user:
    source: extra
`

var testJSONLogLine = `
{
	"time":"2012-11-01T22:08:41+00:00",
	"app":"loki",
	"component": ["parser","type"],
	"level" : "WARN",
	"nested" : {"child":"value"},
    "duration" : 125,
	"message" : "this is a log line",
	"extra": "{\"user\":\"marco\"}"
}
`

func TestPipeline_JSON(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		config          string
		entry           string
		expectedExtract map[string]any
	}{
		"successfully run a pipeline with 1 json stage without source": {
			testJSONYamlSingleStageWithoutSource,
			testJSONLogLine,
			map[string]any{
				"out":      "this is a log line",
				"app":      "loki",
				"nested":   "{\"child\":\"value\"}",
				"duration": float64(125),
				"unknown":  nil,
			},
		},
		"successfully run a pipeline with 2 json stages with source": {
			testJSONYamlMultiStageWithSource,
			testJSONLogLine,
			map[string]any{
				"extra": "{\"user\":\"marco\"}",
				"user":  "marco",
			},
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			pl, err := NewPipeline(util_log.Logger, loadConfig(testData.config), nil, prometheus.DefaultRegisterer)
			assert.NoError(t, err, "Expected pipeline creation to not result in error")
			out := processEntries(pl, newEntry(nil, nil, testData.entry, time.Now()))[0]
			assert.Equal(t, testData.expectedExtract, out.Extracted)
		})
	}
}

var cfg = `json:
  expressions:
    key1: expression1
    key2: expression2.expression2`

// nolint
func TestYamlMapStructure(t *testing.T) {
	t.Parallel()

	// testing that we can use yaml data into mapstructure.
	var mapstruct map[any]any
	err := yaml.Unmarshal([]byte(cfg), &mapstruct)
	assert.NoError(t, err, "error while un-marshalling config: %s", err)
	p, ok := mapstruct["json"].(map[any]any)
	assert.True(t, ok, "could not read parser %+v", mapstruct["json"])
	got, err := parseJSONConfig(p)
	assert.NoError(t, err, "could not create parser from yaml: %s", err)
	want := &JSONConfig{
		Expressions: map[string]string{
			"key1": "expression1",
			"key2": "expression2.expression2",
		},
	}
	assert.True(t, reflect.DeepEqual(got, want), "want: %+v got: %+v", want, got)
}

func TestJSONConfig_validate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		config        any
		wantExprCount int
		err           error
	}{
		"empty config": {
			nil,
			0,
			errors.New(ErrExpressionsRequired),
		},
		"no expressions": {
			map[string]any{},
			0,
			errors.New(ErrExpressionsRequired),
		},
		"invalid expression": {
			map[string]any{
				"expressions": map[string]any{
					"extr1": "3##@$#33",
				},
			},
			0,
			errors.Wrap(errors.New("SyntaxError: Unknown char: '#'"), ErrCouldNotCompileJMES),
		},
		"empty source": {
			map[string]any{
				"expressions": map[string]any{
					"extr1": "expr",
				},
				"source": "",
			},
			0,
			errors.New(ErrEmptyJSONStageSource),
		},
		"valid without source": {
			map[string]any{
				"expressions": map[string]string{
					"expr1": "expr",
					"expr2": "",
					"expr3": "expr1.expr2",
				},
			},
			3,
			nil,
		},
		"valid with source": {
			map[string]any{
				"expressions": map[string]string{
					"expr1": "expr",
					"expr2": "",
					"expr3": "expr1.expr2",
				},
				"source": "log",
			},
			3,
			nil,
		},
	}
	for tName, tt := range tests {
		t.Run(tName, func(t *testing.T) {
			c, err := parseJSONConfig(tt.config)
			assert.NoError(t, err, "failed to create config: %s", err)
			got, err := validateJSONConfig(c)
			if tt.err != nil {
				assert.NotNil(t, err, "JSONConfig.validate() expected error = %v, but got nil", tt.err)
			}
			if err != nil {
				assert.Equal(t, tt.err.Error(), err.Error(), "JSONConfig.validate() expected error = %v, actual error = %v", tt.err, err)
			}
			assert.Equal(t, tt.wantExprCount, len(got))
		})
	}
}

var logFixture = `
{
	"time":"2012-11-01T22:08:41+00:00",
	"app":"loki",
	"component": ["parser","type"],
	"level" : "WARN",
	"numeric": {
		"float": 12.34,
		"integer": 123,
		"string": "123"
	},
	"nested" : {"child":"value"},
	"message" : "this is a log line",
	"complex" : {
		"log" : {"array":[{"test1":"test2"},{"test3":"test4"}],"prop":"value","prop2":"val2"}
	}
}
`

func TestJSONParser_Parse(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		config          any
		extracted       map[string]any
		entry           string
		expectedExtract map[string]any
	}{
		"successfully decode json on entry": {
			map[string]any{
				"expressions": map[string]string{
					"time":      "",
					"app":       "",
					"component": "",
					"level":     "",
					"float":     "numeric.float",
					"integer":   "numeric.integer",
					"string":    "numeric.string",
					"nested":    "",
					"message":   "",
					"complex":   "complex.log.array[1].test3",
				},
			},
			map[string]any{},
			logFixture,
			map[string]any{
				"time":      "2012-11-01T22:08:41+00:00",
				"app":       "loki",
				"component": "[\"parser\",\"type\"]",
				"level":     "WARN",
				"float":     12.34,
				"integer":   123.0,
				"string":    "123",
				"nested":    "{\"child\":\"value\"}",
				"message":   "this is a log line",
				"complex":   "test4",
			},
		},
		"successfully decode json on extracted[source]": {
			map[string]any{
				"expressions": map[string]string{
					"time":      "",
					"app":       "",
					"component": "",
					"level":     "",
					"float":     "numeric.float",
					"integer":   "numeric.integer",
					"string":    "numeric.string",
					"nested":    "",
					"message":   "",
					"complex":   "complex.log.array[1].test3",
				},
				"source": "log",
			},
			map[string]any{
				"log": logFixture,
			},
			"{}",
			map[string]any{
				"time":      "2012-11-01T22:08:41+00:00",
				"app":       "loki",
				"component": "[\"parser\",\"type\"]",
				"level":     "WARN",
				"float":     12.34,
				"integer":   123.0,
				"string":    "123",
				"nested":    "{\"child\":\"value\"}",
				"message":   "this is a log line",
				"complex":   "test4",
				"log":       logFixture,
			},
		},
		"missing extracted[source]": {
			map[string]any{
				"expressions": map[string]string{
					"app": "",
				},
				"source": "log",
			},
			map[string]any{},
			logFixture,
			map[string]any{},
		},
		"invalid json on entry": {
			map[string]any{
				"expressions": map[string]string{
					"expr1": "",
				},
			},
			map[string]any{},
			"ts=now log=notjson",
			map[string]any{},
		},
		"invalid json on extracted[source]": {
			map[string]any{
				"expressions": map[string]string{
					"app": "",
				},
				"source": "log",
			},
			map[string]any{
				"log": "not a json",
			},
			logFixture,
			map[string]any{
				"log": "not a json",
			},
		},
		"nil source": {
			map[string]any{
				"expressions": map[string]string{
					"app": "",
				},
				"source": "log",
			},
			map[string]any{
				"log": nil,
			},
			logFixture,
			map[string]any{
				"log": nil,
			},
		},
	}
	for tName, tt := range tests {
		t.Run(tName, func(t *testing.T) {
			t.Parallel()
			p, err := New(util_log.Logger, nil, StageTypeJSON, tt.config, nil)
			assert.NoError(t, err, "failed to create json parser: %s", err)
			out := processEntries(p, newEntry(tt.extracted, nil, tt.entry, time.Now()))[0]

			assert.Equal(t, tt.expectedExtract, out.Extracted)
		})
	}
}

func TestValidateJSONDrop(t *testing.T) {
	labels := map[string]string{"foo": "bar"}
	matchConfig := JSONConfig{
		DropMalformed: true,
		Expressions:   map[string]string{"page": "page"},
	}
	s, err := newJSONStage(util_log.Logger, matchConfig)
	assert.NoError(t, err, "withMatcher() error = %v", err)
	assert.NotNil(t, s, "newJSONStage failed to create the pipeline stage and was nil")
	out := processEntries(s, newEntry(map[string]any{
		"test_label": "unimportant value",
	}, toLabelSet(labels), `{"page": 1, "fruits": ["apple", "peach"]}`, time.Now()))
	assert.Equal(t, 1, len(out), "stage should have kept one valid json line but got %v", out)

	out = processEntries(s, newEntry(map[string]any{
		"test_label": "unimportant value",
	}, toLabelSet(labels), `{"page": 1, fruits": ["apple", "peach"]}`, time.Now()))
	assert.Equal(t, 0, len(out), "stage should have kept zero valid json line but got %v", out)
}
