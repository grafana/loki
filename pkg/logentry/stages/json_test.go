package stages

import (
	"reflect"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
)

var cfg = `json:
  expressions:
    key1: expression1
    key2: expression2.expression2`

// nolint
func TestYamlMapStructure(t *testing.T) {
	t.Parallel()

	// testing that we can use yaml data into mapstructure.
	var mapstruct map[interface{}]interface{}
	if err := yaml.Unmarshal([]byte(cfg), &mapstruct); err != nil {
		t.Fatalf("error while un-marshalling config: %s", err)
	}
	p, ok := mapstruct["json"].(map[interface{}]interface{})
	if !ok {
		t.Fatalf("could not read parser %+v", mapstruct["json"])
	}
	got, err := parseJSONConfig(p)
	if err != nil {
		t.Fatalf("could not create parser from yaml: %s", err)
	}
	want := &JSONConfig{
		Expressions: map[string]string{
			"key1": "expression1",
			"key2": "expression2.expression2",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want: %+v got: %+v", want, got)
	}
}

func TestJSONConfig_validate(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		config        *JSONConfig
		wantExprCount int
		err           error
	}{
		"empty": {
			nil,
			0,
			errors.New(ErrExpressionsRequired),
		},
		"no expressions": {
			&JSONConfig{},
			0,
			errors.New(ErrExpressionsRequired),
		},
		"invalid expression": {
			&JSONConfig{
				Expressions: map[string]string{
					"extr1": "3##@$#33",
				},
			},
			0,
			errors.Wrap(errors.New("SyntaxError: Unknown char: '#'"), ErrCouldNotCompileJMES),
		},
		"valid": {
			&JSONConfig{
				Expressions: map[string]string{
					"expr1": "expr",
					"expr2": "",
					"expr3": "expr1.expr2",
				},
			},
			3,
			nil,
		},
	}
	for tName, tt := range tests {
		tt := tt
		t.Run(tName, func(t *testing.T) {
			c, err := parseJSONConfig(tt.config)
			if err != nil {
				t.Fatalf("failed to create config: %s", err)
			}
			got, err := validateJSONConfig(c)
			if (err != nil) != (tt.err != nil) {
				t.Errorf("JSONConfig.validate() expected error = %v, actual error = %v", tt.err, err)
				return
			}
			if (err != nil) && (err.Error() != tt.err.Error()) {
				t.Errorf("JSONConfig.validate() expected error = %v, actual error = %v", tt.err, err)
				return
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
		config          *JSONConfig
		entry           string
		expectedExtract map[string]interface{}
	}{
		"extract all": {
			&JSONConfig{
				Expressions: map[string]string{
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
			logFixture,
			map[string]interface{}{
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
		"invalid json": {
			&JSONConfig{
				Expressions: map[string]string{
					"expr1": "",
				},
			},
			"ts=now log=notjson",
			map[string]interface{}{},
		},
	}
	for tName, tt := range tests {
		tt := tt
		t.Run(tName, func(t *testing.T) {
			t.Parallel()
			p, err := New(util.Logger, nil, StageTypeJSON, tt.config, nil)
			if err != nil {
				t.Fatalf("failed to create json parser: %s", err)
			}
			lbs := model.LabelSet{}
			extr := map[string]interface{}{}
			ts := time.Now()
			p.Process(lbs, extr, &ts, &tt.entry)

			assert.Equal(t, tt.expectedExtract, extr)
		})
	}
}
