package stages

import (
	"fmt"
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
  timestamp:
    source: time
    format: RFC3339
  labels:
    stream:
      source: stream
  output:
    source: log`

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
	got, err := newRegexConfig(p)
	if err != nil {
		t.Fatalf("could not create parser from yaml: %s", err)
	}
	want := &RegexConfig{
		Labels: map[string]*RegexLabel{
			"stream": {
				Source: String("stream"),
			},
		},
		Output: &RegexOutput{Source: String("log")},
		Timestamp: &RegexTimestamp{
			Format: "RFC3339",
			Source: String("time"),
		},
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
			map[string]interface{}{},
			errors.New(ErrEmptyRegexStageConfig),
		},
		"missing output info": {
			map[string]interface{}{
				"expression": ".*",
				"output":     map[string]interface{}{},
			},
			errors.New(ErrOutputSourceRequired),
		},
		"missing regex_expression": {
			map[string]interface{}{
				"output": map[string]interface{}{},
			},
			errors.New(ErrExpressionRequired),
		},
		"invalid regex_expression": {
			map[string]interface{}{
				"expression": "(?P<ts[0-9]+).*",
				"output":     map[string]interface{}{},
			},
			errors.New(ErrCouldNotCompileRegex + ": error parsing regexp: invalid named capture: `(?P<ts[0-9]+).*`"),
		},
		"missing output source": {
			map[string]interface{}{
				"expression": ".*",
				"output": map[string]interface{}{
					"source": "",
				},
			},
			errors.New(ErrOutputSourceRequired),
		},
		"invalid output source": {
			map[string]interface{}{
				"expression": ".*",
				"output": map[string]interface{}{
					"source": "[",
				},
			},
			nil,
		},
		"missing timestamp source": {
			map[string]interface{}{
				"expression": ".*",
				"timestamp": map[string]interface{}{
					"source": "",
					"format": "ANSIC",
				},
			},
			errors.New(ErrTimestampSourceRequired),
		},
		"missing timestamp format": {
			map[string]interface{}{
				"expression": ".*",
				"timestamp": map[string]interface{}{
					"source": "test",
					"format": "",
				},
			},
			errors.New(ErrTimestampFormatRequired),
		},
		"invalid label name": {
			map[string]interface{}{
				"expression": ".*",
				"labels": map[string]interface{}{
					"": map[string]interface{}{},
				},
			},
			fmt.Errorf(ErrInvalidLabelName, ""),
		},
		"invalid label source": {
			map[string]interface{}{
				"expression": ".*",
				"labels": map[string]interface{}{
					"stream": map[string]interface{}{
						"source": "]",
					},
				},
			},
			nil,
		},
		"missing_timestamp_group": {
			map[string]interface{}{
				"expression": ".*",
				"timestamp": map[string]interface{}{
					"source": "ts",
					"format": "RFC3339",
				},
			},
			errors.Errorf(ErrTimestampGroupRequired, "ts"),
		},
		"valid": {
			map[string]interface{}{
				"expression": "(?P<ts>[0-9]+).*",
				"output": map[string]interface{}{
					"source": "log",
				},
				"timestamp": map[string]interface{}{
					"source": "ts",
					"format": "RFC3339",
				},
				"labels": map[string]interface{}{
					"stream": map[string]interface{}{
						"source": "test",
					},
					"app": map[string]interface{}{
						"source": "app",
					},
					"level": nil,
				},
			},
			nil,
		},
	}
	for tName, tt := range tests {
		tt := tt
		t.Run(tName, func(t *testing.T) {
			c, err := newRegexConfig(tt.config)
			if err != nil {
				t.Fatalf("failed to create config: %s", err)
			}
			_, err = c.validate()
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
	regexLogFixture                 = `11.11.11.11 - frank [25/Jan/2000:14:00:01 -0500] "GET /1986.js HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"`
	regexLogFixtureMissingLabel     = `2016-10-06T00:17:09.669794202Z stdout k `
	regexLogFixtureInvalidTimestamp = `2016-10-06sfsT00:17:09.669794202Z stdout k `
)

func TestRegexParser_Parse(t *testing.T) {
	t.Parallel()

	est, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal("could not parse timestamp", err)
	}

	utc, err := time.LoadLocation("UTC")
	if err != nil {
		t.Fatal("could not parse timestamp", err)
	}

	tests := map[string]struct {
		config         interface{}
		entry          string
		expectedEntry  string
		t              time.Time
		expectedT      time.Time
		labels         map[string]string
		expectedLabels map[string]string
	}{
		"happy path": {
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
			regexLogFixture,
			time.Now(),
			time.Date(2000, 01, 25, 14, 00, 01, 0, est),
			nil,
			map[string]string{
				"action":      "GET",
				"status_code": "200",
			},
		},
		"modify output": {
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
				"output": map[string]interface{}{
					"source": "path",
				},
			},
			regexLogFixture,
			"/1986.js",
			time.Now(),
			time.Date(2000, 01, 25, 14, 00, 01, 0, est),
			nil,
			map[string]string{
				"action":      "GET",
				"status_code": "200",
			},
		},
		"missing label": {
			map[string]interface{}{
				"expression": "^(?s)(?P<time>\\S+?) (?P<stream>stdout|stderr) (?P<flags>\\S+?) (?P<missing_label>.*)$",
				"timestamp": map[string]interface{}{
					"source": "time",
					"format": "RFC3339",
				},
				"labels": map[string]interface{}{
					"stream": map[string]interface{}{
						"source": "stream",
					},
					"missing_label": map[string]interface{}{
						"source": "missing_label",
					},
				},
			},
			regexLogFixtureMissingLabel,
			regexLogFixtureMissingLabel,
			time.Now(),
			time.Date(2016, 10, 06, 00, 17, 9, 669794202, utc),
			nil,
			map[string]string{
				"stream":        "stdout",
				"missing_label": "",
			},
		},
		"invalid timestamp skipped": {
			map[string]interface{}{
				"expression": "^(?s)(?P<time>\\S+?) (?P<stream>stdout|stderr) (?P<flags>\\S+?) (?P<message>.*)$",
				"timestamp": map[string]interface{}{
					"source": "time",
					"format": "UnixDate",
				},
				"labels": map[string]interface{}{
					"stream": map[string]interface{}{
						"source": "stream",
					},
				},
			},
			regexLogFixtureInvalidTimestamp,
			regexLogFixtureInvalidTimestamp,
			time.Date(2019, 10, 06, 00, 17, 9, 0, utc),
			time.Date(2019, 10, 06, 00, 17, 9, 0, utc),
			nil,
			map[string]string{
				"stream": "stdout",
			},
		},
		"match failed": {
			map[string]interface{}{
				"expression": "^(?s)(?P<time>\\S+?) (?P<stream>stdout|stderr) (?P<flags>\\S+?) (?P<message>.*)$",
				"timestamp": map[string]interface{}{
					"source": "time",
					"format": "UnixDate",
				},
				"labels": map[string]interface{}{
					"stream": map[string]interface{}{
						"source": "stream",
					},
				},
			},
			"blahblahblah",
			"blahblahblah",
			time.Date(2019, 10, 06, 00, 17, 9, 0, utc),
			time.Date(2019, 10, 06, 00, 17, 9, 0, utc),
			map[string]string{
				"stream": "stdout",
			},
			map[string]string{
				"stream": "stdout",
			},
		},
	}
	for tName, tt := range tests {
		tt := tt
		t.Run(tName, func(t *testing.T) {
			t.Parallel()
			p, err := NewRegex(util.Logger, tt.config)
			if err != nil {
				t.Fatalf("failed to create regex parser: %s", err)
			}
			lbs := toLabelSet(tt.labels)
			p.Process(lbs, &tt.t, &tt.entry)

			assertLabels(t, tt.expectedLabels, lbs)
			assert.Equal(t, tt.expectedEntry, tt.entry, "did not receive expected log entry")
			if tt.t.Unix() != tt.expectedT.Unix() {
				t.Fatalf("mismatch ts want: %s got:%s", tt.expectedT, tt.t)
			}
		})
	}
}

func Benchmark(b *testing.B) {
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
			stage, err := NewRegex(util.Logger, bm.config)
			if err != nil {
				panic(err)
			}
			labels := model.LabelSet{}
			ts := time.Now()
			for i := 0; i < b.N; i++ {
				entry := bm.entry
				stage.Process(labels, &ts, &entry)
			}
		})
	}
}
