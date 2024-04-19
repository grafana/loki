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

var testReplaceYamlSingleStageWithoutSource = `
pipeline_stages:
- replace:
    expression: "11.11.11.11 - (\\S+) .*"
    replace: "dummy"
`
var testReplaceYamlMultiStageWithSource = `
pipeline_stages:
- json:
    expressions:
      level:
      msg:
- replace:
    expression: "\\S+ - \"POST (\\S+) .*"
    source: msg
    replace: "/loki/api/v1/push/"
`

var testReplaceYamlWithNamedCaputedGroupWithTemplate = `
---
pipeline_stages:
  -
    replace:
      expression: "^(?P<ip>\\S+) (?P<identd>\\S+) (?P<user>\\S+) \\[(?P<timestamp>[\\w:/]+\\s[+\\-]\\d{4})\\] \"(?P<action>\\S+)\\s?(?P<path>\\S+)?\\s?(?P<protocol>\\S+)?\" (?P<status>\\d{3}|-) (\\d+|-)\\s?\"?(?P<referer>[^\"]*)\"?\\s?\"?(?P<useragent>[^\"]*)?\"?$"
      replace: '{{ if eq .Value "200" }}{{ Replace .Value "200" "HttpStatusOk" -1 }}{{ else }}{{ .Value | ToUpper }}{{ end }}'
`

var testReplaceYamlWithNestedCapturedGroups = `
---
pipeline_stages:
  -
    replace:
      expression: "(?P<ip_user>^(?P<ip>\\S+) (?P<identd>\\S+) (?P<user>\\S+)) \\[(?P<timestamp>[\\w:/]+\\s[+\\-]\\d{4})\\] \"(?P<action_path>(?P<action>\\S+)\\s?(?P<path>\\S+)?)\\s?(?P<protocol>\\S+)?\" (?P<status>\\d{3}|-) (\\d+|-)\\s?\"?(?P<referer>[^\"]*)\"?\\s?\"?(?P<useragent>[^\"]*)?\"?$"
      replace: '{{ if eq .Value "200" }}{{ Replace .Value "200" "HttpStatusOk" -1 }}{{ else }}{{ .Value | ToUpper }}{{ end }}'
`

var testReplaceYamlWithTemplate = `
---
pipeline_stages:
  -
    replace:
      expression: "^(\\S+) (\\S+) (\\S+) \\[([\\w:/]+\\s[+\\-]\\d{4})\\] \"(\\S+)\\s?(\\S+)?\\s?(\\S+)?\" (\\d{3}|-) (\\d+|-)\\s?\"?([^\"]*)\"?\\s?\"?([^\"]*)?\"?$"
      replace: '{{ if eq .Value "200" }}{{ Replace .Value "200" "HttpStatusOk" -1 }}{{ else }}{{ .Value | ToUpper }}{{ end }}'
`

var testReplaceYamlWithEmptyReplace = `
---
pipeline_stages:
  -
    replace:
      expression: "11.11.11.11 - (\\S+\\s)"
      replace: ''
`

var testReplaceAdjacentCaptureGroups = `
---
pipeline_stages:
  -
    replace:
      expression: '(a|b|c)'
      replace: ''
`

var testReplaceLogLine = `11.11.11.11 - frank [25/Jan/2000:14:00:01 -0500] "GET /1986.js HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"`
var testReplaceLogJSONLine = `{"time":"2019-01-01T01:00:00.000000001Z", "level": "info", "msg": "11.11.11.11 - \"POST /loki/api/push/ HTTP/1.1\" 200 932 \"-\" \"Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6\""}`
var testReplaceLogLineAdjacentCaptureGroups = `abc`

func TestPipeline_Replace(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		config        string
		entry         string
		extracted     map[string]interface{}
		expectedEntry string
	}{
		"successfully run a pipeline with 1 regex stage without source": {
			testReplaceYamlSingleStageWithoutSource,
			testReplaceLogLine,
			map[string]interface{}{},
			`11.11.11.11 - dummy [25/Jan/2000:14:00:01 -0500] "GET /1986.js HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"`,
		},
		"successfully run a pipeline with multi stage with": {
			testReplaceYamlMultiStageWithSource,
			testReplaceLogJSONLine,
			map[string]interface{}{
				"level": "info",
				"msg":   `11.11.11.11 - "POST /loki/api/v1/push/ HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"`,
			},
			`{"time":"2019-01-01T01:00:00.000000001Z", "level": "info", "msg": "11.11.11.11 - \"POST /loki/api/push/ HTTP/1.1\" 200 932 \"-\" \"Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6\""}`,
		},
		"successfully run a pipeline with 1 regex stage with named captured group and with template and without source": {
			testReplaceYamlWithNamedCaputedGroupWithTemplate,
			testReplaceLogLine,
			map[string]interface{}{
				"ip":        "11.11.11.11",
				"identd":    "-",
				"user":      "FRANK",
				"timestamp": "25/JAN/2000:14:00:01 -0500",
				"action":    "GET",
				"path":      "/1986.JS",
				"protocol":  "HTTP/1.1",
				"status":    "HttpStatusOk",
				"referer":   "-",
				"useragent": "MOZILLA/5.0 (WINDOWS; U; WINDOWS NT 5.1; DE; RV:1.9.1.7) GECKO/20091221 FIREFOX/3.5.7 GTB6",
			},
			`11.11.11.11 - FRANK [25/JAN/2000:14:00:01 -0500] "GET /1986.JS HTTP/1.1" HttpStatusOk 932 "-" "MOZILLA/5.0 (WINDOWS; U; WINDOWS NT 5.1; DE; RV:1.9.1.7) GECKO/20091221 FIREFOX/3.5.7 GTB6"`,
		},
		"successfully run a pipeline with 1 regex stage with nested captured groups and with template and without source": {
			testReplaceYamlWithNestedCapturedGroups,
			testReplaceLogLine,
			map[string]interface{}{
				"ip_user":     "11.11.11.11 - FRANK",
				"action_path": "GET /1986.JS",
				"ip":          "11.11.11.11",
				"identd":      "-",
				"user":        "FRANK",
				"timestamp":   "25/JAN/2000:14:00:01 -0500",
				"action":      "GET",
				"path":        "/1986.JS",
				"protocol":    "HTTP/1.1",
				"status":      "HttpStatusOk",
				"referer":     "-",
				"useragent":   "MOZILLA/5.0 (WINDOWS; U; WINDOWS NT 5.1; DE; RV:1.9.1.7) GECKO/20091221 FIREFOX/3.5.7 GTB6",
			},
			`11.11.11.11 - FRANK [25/JAN/2000:14:00:01 -0500] "GET /1986.JS HTTP/1.1" HttpStatusOk 932 "-" "MOZILLA/5.0 (WINDOWS; U; WINDOWS NT 5.1; DE; RV:1.9.1.7) GECKO/20091221 FIREFOX/3.5.7 GTB6"`,
		},
		"successfully run a pipeline with 1 regex stage with template and without source": {
			testReplaceYamlWithTemplate,
			testReplaceLogLine,
			map[string]interface{}{},
			`11.11.11.11 - FRANK [25/JAN/2000:14:00:01 -0500] "GET /1986.JS HTTP/1.1" HttpStatusOk 932 "-" "MOZILLA/5.0 (WINDOWS; U; WINDOWS NT 5.1; DE; RV:1.9.1.7) GECKO/20091221 FIREFOX/3.5.7 GTB6"`,
		},
		"successfully run a pipeline with empty replace value": {
			testReplaceYamlWithEmptyReplace,
			testReplaceLogLine,
			map[string]interface{}{},
			`11.11.11.11 - [25/Jan/2000:14:00:01 -0500] "GET /1986.js HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"`,
		},
		"successfully run a pipeline with adjacent capture groups": {
			testReplaceAdjacentCaptureGroups,
			testReplaceLogLineAdjacentCaptureGroups,
			map[string]interface{}{},
			``,
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			pl, err := NewPipeline(util_log.Logger, loadConfig(testData.config), nil, prometheus.DefaultRegisterer)
			if err != nil {
				t.Fatal(err)
			}
			out := processEntries(pl, newEntry(nil, nil, testData.entry, time.Now()))[0]
			assert.Equal(t, testData.expectedEntry, out.Line)
			assert.Equal(t, testData.extracted, out.Extracted)
		})
	}
}

var replaceCfg = `
replace:
  expression: "regexexpression"
  replace: "replace"`

func TestReplaceMapStructure(t *testing.T) {
	t.Parallel()

	// testing that we can use yaml data into mapstructure.
	var mapstruct map[interface{}]interface{}
	if err := yaml.Unmarshal([]byte(replaceCfg), &mapstruct); err != nil {
		t.Fatalf("error while un-marshalling config: %s", err)
	}
	p, ok := mapstruct["replace"].(map[interface{}]interface{})
	if !ok {
		t.Fatalf("could not read parser %+v", mapstruct["replace"])
	}
	got, err := parseReplaceConfig(p)
	if err != nil {
		t.Fatalf("could not create parser from yaml: %s", err)
	}
	want := &ReplaceConfig{
		Expression: "regexexpression",
		Replace:    "replace",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want: %+v got: %+v", want, got)
	}
}

func TestReplaceConfig_validate(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		config interface{}
		err    error
	}{
		"empty config": {
			nil,
			errors.New(ErrExpressionRequired),
		},
		"missing regex_expression": {
			map[string]interface{}{},
			errors.New(ErrExpressionRequired),
		},
		"invalid regex_expression": {
			map[string]interface{}{
				"expression": "(?P<ts[0-9]+).*",
				"replace":    "test",
			},
			errors.New(ErrCouldNotCompileRegex + ": error parsing regexp: invalid named capture: `(?P<ts[0-9]+).*`"),
		},
		"empty source": {
			map[string]interface{}{
				"expression": "(?P<ts>[0-9]+).*",
				"source":     "",
			},
			errors.New(ErrEmptyReplaceStageSource),
		},
		"valid without source": {
			map[string]interface{}{
				"expression": "(?P<ts>[0-9]+).*",
				"replace":    "test",
			},
			nil,
		},
		"valid with source": {
			map[string]interface{}{
				"expression": "(?P<ts>[0-9]+).*",
				"source":     "log",
				"replace":    "test",
			},
			nil,
		},
	}
	for tName, tt := range tests {
		tt := tt
		t.Run(tName, func(t *testing.T) {
			c, err := parseReplaceConfig(tt.config)
			if err != nil {
				t.Fatalf("failed to create config: %s", err)
			}
			_, err = validateReplaceConfig(c)
			if (err != nil) != (tt.err != nil) {
				t.Errorf("ReplaceConfig.validate() expected error = %v, actual error = %v", tt.err, err)
				return
			}
			if (err != nil) && (err.Error() != tt.err.Error()) {
				t.Errorf("ReplaceConfig.validate() expected error = %v, actual error = %v", tt.err, err)
				return
			}
		})
	}
}
