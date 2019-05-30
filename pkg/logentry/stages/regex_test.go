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

var regexCfg = `regex: 
  expression: "regexexpression"`

// nolint
func TestRegexMapStructure(t *testing.T) {
	t.Parallel()

	// testing that we can use yaml data into mapstructure.
	var mapstruct map[interface{}]interface{}
	if err := yaml.Unmarshal([]byte(regexCfg), &mapstruct); err != nil {
		t.Fatalf("error while un-marshalling config: %s", err)
	}
	p, ok := mapstruct["regex"].(map[interface{}]interface{})
	if !ok {
		t.Fatalf("could not read parser %+v", mapstruct["regex"])
	}
	got, err := parseRegexConfig(p)
	if err != nil {
		t.Fatalf("could not create parser from yaml: %s", err)
	}
	want := &RegexConfig{
		Expression: "regexexpression",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want: %+v got: %+v", want, got)
	}
}

func TestRegexConfig_validate(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		config interface{}
		err    error
	}{
		"empty": {
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
			},
			errors.New(ErrCouldNotCompileRegex + ": error parsing regexp: invalid named capture: `(?P<ts[0-9]+).*`"),
		},
		"valid": {
			map[string]interface{}{
				"expression": "(?P<ts>[0-9]+).*",
			},
			nil,
		},
	}
	for tName, tt := range tests {
		tt := tt
		t.Run(tName, func(t *testing.T) {
			c, err := parseRegexConfig(tt.config)
			if err != nil {
				t.Fatalf("failed to create config: %s", err)
			}
			_, err = validateRegexConfig(c)
			if (err != nil) != (tt.err != nil) {
				t.Errorf("RegexConfig.validate() expected error = %v, actual error = %v", tt.err, err)
				return
			}
			if (err != nil) && (err.Error() != tt.err.Error()) {
				t.Errorf("RegexConfig.validate() expected error = %v, actual error = %v", tt.err, err)
				return
			}
		})
	}
}

var (
	regexLogFixture = `11.11.11.11 - frank [25/Jan/2000:14:00:01 -0500] "GET /1986.js HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"`
)

func TestRegexParser_Parse(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		config          interface{}
		entry           string
		expectedExtract map[string]interface{}
	}{
		"happy path": {
			map[string]interface{}{
				"expression": "^(?P<ip>\\S+) (?P<identd>\\S+) (?P<user>\\S+) \\[(?P<timestamp>[\\w:/]+\\s[+\\-]\\d{4})\\] \"(?P<action>\\S+)\\s?(?P<path>\\S+)?\\s?(?P<protocol>\\S+)?\" (?P<status>\\d{3}|-) (?P<size>\\d+|-)\\s?\"?(?P<referer>[^\"]*)\"?\\s?\"?(?P<useragent>[^\"]*)?\"?$",
			},
			regexLogFixture,
			map[string]interface{}{
				"ip":        "11.11.11.11",
				"identd":    "-",
				"user":      "frank",
				"timestamp": "25/Jan/2000:14:00:01 -0500",
				"action":    "GET",
				"path":      "/1986.js",
				"protocol":  "HTTP/1.1",
				"status":    "200",
				"size":      "932",
				"referer":   "-",
				"useragent": "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6",
			},
		},
		"match failed": {
			map[string]interface{}{
				"expression": "^(?s)(?P<time>\\S+?) (?P<stream>stdout|stderr) (?P<flags>\\S+?) (?P<message>.*)$",
			},
			"blahblahblah",
			map[string]interface{}{},
		},
	}
	for tName, tt := range tests {
		tt := tt
		t.Run(tName, func(t *testing.T) {
			t.Parallel()
			p, err := New(util.Logger, nil, StageTypeRegex, tt.config, nil)
			if err != nil {
				t.Fatalf("failed to create regex parser: %s", err)
			}
			lbs := model.LabelSet{}
			extr := map[string]interface{}{}
			ts := time.Now()
			p.Process(lbs, extr, &ts, &tt.entry)
			assert.Equal(t, tt.expectedExtract, extr)

		})
	}
}

func BenchmarkRegexStage(b *testing.B) {
	benchmarks := []struct {
		name   string
		config map[string]interface{}
		entry  string
	}{
		{
			"apache common log",
			map[string]interface{}{
				"expression": "^(?P<ip>\\S+) (?P<identd>\\S+) (?P<user>\\S+) \\[(?P<timestamp>[\\w:/]+\\s[+\\-]\\d{4})\\] \"(?P<action>\\S+)\\s?(?P<path>\\S+)?\\s?(?P<protocol>\\S+)?\" (?P<status>\\d{3}|-) (?P<size>\\d+|-)\\s?\"?(?P<referer>[^\"]*)\"?\\s?\"?(?P<useragent>[^\"]*)?\"?$",
				"timestamp": map[string]interface{}{
					"source": "timestamp",
					"format": "02/Jan/2006:15:04:05 -0700",
				},
				"labels": map[string]interface{}{
					"action": map[string]interface{}{
						"source": "action",
					},
					"status_code": map[string]interface{}{
						"source": "status",
					},
				},
			},
			regexLogFixture,
		},
	}
	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			stage, err := New(util.Logger, nil, StageTypeRegex, bm.config, nil)
			if err != nil {
				panic(err)
			}
			labels := model.LabelSet{}
			ts := time.Now()
			extr := map[string]interface{}{}
			for i := 0; i < b.N; i++ {
				entry := bm.entry
				stage.Process(labels, extr, &ts, &entry)
			}
		})
	}
}
