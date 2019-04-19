package stages

import (
	"reflect"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"
)

var cfg = `json: 
  timestamp:
    source: time
    format: RFC3339
  labels:
    stream:
      source: json_key_name.json_sub_key_name
  output:
    source: log`

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
	got, err := newJSONConfig(p)
	if err != nil {
		t.Fatalf("could not create parser from yaml: %s", err)
	}
	want := &JSONConfig{
		Labels: map[string]*JSONLabel{
			"stream": &JSONLabel{
				Source: String("json_key_name.json_sub_key_name"),
			},
		},
		Output: &JSONOutput{Source: String("log")},
		Timestamp: &JSONTimestamp{
			Format: "RFC3339",
			Source: String("time"),
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want: %+v got: %+v", want, got)
	}
}

func String(s string) *string {
	return &s
}

func TestJSONConfig_validate(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		config        interface{}
		wantExprCount int
		wantErr       bool
	}{
		"empty": {
			map[string]interface{}{},
			0,
			true,
		},
		"missing output info": {
			map[string]interface{}{
				"output": map[string]interface{}{},
			},
			0,
			true,
		},
		"missing output source": {
			map[string]interface{}{
				"output": map[string]interface{}{
					"source": "",
				},
			},
			0,
			true,
		},
		"invalid output source": {
			map[string]interface{}{
				"output": map[string]interface{}{
					"source": "[",
				},
			},
			0,
			true,
		},
		"missing timestamp source": {
			map[string]interface{}{
				"timestamp": map[string]interface{}{
					"source": "",
					"format": "ANSIC",
				},
			},
			0,
			true,
		},
		"invalid timestamp source": {
			map[string]interface{}{
				"timestamp": map[string]interface{}{
					"source": "[",
					"format": "ANSIC",
				},
			},
			0,
			true,
		},
		"missing timestamp format": {
			map[string]interface{}{
				"timestamp": map[string]interface{}{
					"source": "test",
					"format": "",
				},
			},
			0,
			true,
		},
		"invalid label name": {
			map[string]interface{}{
				"labels": map[string]interface{}{
					"": map[string]interface{}{},
				},
			},
			0,
			true,
		},
		"invalid label source": {
			map[string]interface{}{
				"labels": map[string]interface{}{
					"stream": map[string]interface{}{
						"source": "]",
					},
				},
			},
			0,
			true,
		},
		"valid": {
			map[string]interface{}{
				"output": map[string]interface{}{
					"source": "log.msg[0]",
				},
				"timestamp": map[string]interface{}{
					"source": "log.ts",
					"format": "RFC3339",
				},
				"labels": map[string]interface{}{
					"stream": map[string]interface{}{
						"source": "test",
					},
					"app": map[string]interface{}{
						"source": "component.app",
					},
					"level": nil,
				},
			},
			4,
			false,
		},
	}
	for tName, tt := range tests {
		tt := tt
		t.Run(tName, func(t *testing.T) {
			c, err := newJSONConfig(tt.config)
			if err != nil {
				t.Fatalf("failed to create config: %s", err)
			}
			got, err := c.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("JSONConfig.validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != tt.wantExprCount {
				t.Errorf("expressions count = %v, want %v", len(got), tt.wantExprCount)
			}
		})
	}
}

var logFixture = `
{
	"time":"2012-11-01T22:08:41+00:00",
	"app":"loki",
	"component": ["parser","type"],
	"level" : "WARN",
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
		config         interface{}
		entry          string
		expectedEntry  string
		t              time.Time
		expectedT      time.Time
		labels         map[string]string
		expectedLabels map[string]string
	}{
		"replace all": {
			map[string]interface{}{
				"labels": map[string]interface{}{
					"level": nil,
					"component": map[string]interface{}{
						"source": "component[0]",
					},
					"module": map[string]interface{}{
						"source": "app",
					},
				},
				"output": map[string]interface{}{
					"source": "complex.log",
				},
				"timestamp": map[string]interface{}{
					"source": "time",
					"format": "RFC3339",
				},
			},
			logFixture,
			`{"array":[{"test1":"test2"},{"test3":"test4"}],"prop":"value","prop2":"val2"}`,
			mustParseTime(time.RFC3339, "2019-03-28T11:29:10+07:00"),
			mustParseTime(time.RFC3339, "2012-11-01T22:08:41+00:00"),
			map[string]string{"stream": "stdout"},
			map[string]string{"stream": "stdout", "level": "WARN", "component": "parser", "module": "loki"},
		},
		"invalid json": {
			map[string]interface{}{
				"labels": map[string]interface{}{
					"level": nil,
				},
			},
			"ts=now log=notjson",
			"ts=now log=notjson",
			mustParseTime(time.RFC3339, "2019-03-28T11:29:10+07:00"),
			mustParseTime(time.RFC3339, "2019-03-28T11:29:10+07:00"),
			map[string]string{"stream": "stdout"},
			map[string]string{"stream": "stdout"},
		},
		"invalid timestamp skipped": {
			map[string]interface{}{
				"labels": map[string]interface{}{
					"level": nil,
				},
				"timestamp": map[string]interface{}{
					"source": "time",
					"format": "invalid",
				},
			},
			logFixture,
			logFixture,
			mustParseTime(time.RFC3339, "2019-03-28T11:29:10+07:00"),
			mustParseTime(time.RFC3339, "2019-03-28T11:29:10+07:00"),
			map[string]string{"stream": "stdout"},
			map[string]string{"stream": "stdout", "level": "WARN"},
		},
		"invalid labels skipped": {
			map[string]interface{}{
				"labels": map[string]interface{}{
					"notexisting": nil,
					"level": map[string]interface{}{
						"source": "doesnotexist",
					},
				},
			},
			logFixture,
			logFixture,
			mustParseTime(time.RFC3339, "2019-03-28T11:29:10+07:00"),
			mustParseTime(time.RFC3339, "2019-03-28T11:29:10+07:00"),
			map[string]string{"stream": "stdout"},
			map[string]string{"stream": "stdout"},
		},
		"invalid output skipped": {
			map[string]interface{}{
				"output": map[string]interface{}{
					"source": "doesnotexist",
				},
			},
			logFixture,
			logFixture,
			mustParseTime(time.RFC3339, "2019-03-28T11:29:10+07:00"),
			mustParseTime(time.RFC3339, "2019-03-28T11:29:10+07:00"),
			map[string]string{"stream": "stdout"},
			map[string]string{"stream": "stdout"},
		},
		"string output": {
			map[string]interface{}{
				"output": map[string]interface{}{
					"source": "message",
				},
			},
			logFixture,
			"this is a log line",
			mustParseTime(time.RFC3339, "2019-03-28T11:29:10+07:00"),
			mustParseTime(time.RFC3339, "2019-03-28T11:29:10+07:00"),
			map[string]string{"stream": "stdout"},
			map[string]string{"stream": "stdout"},
		},
	}
	for tName, tt := range tests {
		tt := tt
		t.Run(tName, func(t *testing.T) {
			p, err := NewJSON(util.Logger, tt.config)
			if err != nil {
				t.Fatalf("failed to create json parser: %s", err)
			}
			lbs := toLabelSet(tt.labels)
			p.Process(lbs, &tt.t, &tt.entry)

			assertLabels(t, tt.expectedLabels, lbs)
			if tt.entry != tt.expectedEntry {
				t.Fatalf("mismatch entry want: %s got:%s", tt.expectedEntry, tt.entry)
			}
			if tt.t.Unix() != tt.expectedT.Unix() {
				t.Fatalf("mismatch ts want: %s got:%s", tt.expectedT, tt.t)
			}
		})
	}
}

func mustParseTime(layout, value string) time.Time {
	t, err := time.Parse(layout, value)
	if err != nil {
		panic(err)
	}
	return t
}

func toLabelSet(lbs map[string]string) model.LabelSet {
	res := model.LabelSet{}
	for k, v := range lbs {
		res[model.LabelName(k)] = model.LabelValue(v)
	}
	return res
}

func assertLabels(t *testing.T, expect map[string]string, got model.LabelSet) {
	if len(expect) != len(got) {
		t.Fatalf("labels are not equal in size want: %s got: %s", expect, got)
	}
	for k, v := range expect {
		gotV, ok := got[model.LabelName(k)]
		if !ok {
			t.Fatalf("missing expected label key: %s", k)
		}
		if gotV != model.LabelValue(v) {
			t.Fatalf("mismatch label value got: %s/%s want %s/%s", k, gotV, k, model.LabelValue(v))
		}
	}
}
