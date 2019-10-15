package stages

import (
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

var (
	ct                = time.Now()
	rawTestLine       = `{"log":"11.11.11.11 - frank [25/Jan/2000:14:00:01 -0500] \"GET /1986.js HTTP/1.1\" 200 932 \"-\" \"Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6\"","stream":"stderr","time":"2019-04-30T02:12:41.8443515Z"}`
	processedTestLine = `11.11.11.11 - frank [25/Jan/2000:14:00:01 -0500] "GET /1986.js HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"`
)

var testYaml = `
pipeline_stages:
- match:
    selector: "{match=\"true\"}"
    stages:
    - docker:
    - regex:
        expression: "^(?P<ip>\\S+) (?P<identd>\\S+) (?P<user>\\S+) \\[(?P<timestamp>[\\w:/]+\\s[+\\-]\\d{4})\\] \"(?P<action>\\S+)\\s?(?P<path>\\S+)?\\s?(?P<protocol>\\S+)?\" (?P<status>\\d{3}|-) (?P<size>\\d+|-)\\s?\"?(?P<referer>[^\"]*)\"?\\s?\"?(?P<useragent>[^\"]*)?\"?$"
    - regex:
        source:     filename
        expression: "(?P<service>[^\\/]+)\\.log"
    - timestamp:
        source: timestamp
        format: "02/Jan/2006:15:04:05 -0700"
    - labels:
        action:
        service:
        status_code: "status"
`

func loadConfig(yml string) PipelineStages {
	var config map[string]interface{}
	err := yaml.Unmarshal([]byte(yml), &config)
	if err != nil {
		panic(err)
	}
	return config["pipeline_stages"].([]interface{})
}

func TestNewPipeline(t *testing.T) {

	p, err := NewPipeline(util.Logger, loadConfig(testYaml), nil, prometheus.DefaultRegisterer)
	if err != nil {
		panic(err)
	}
	require.Equal(t, 1, len(p.stages))
}

func TestPipeline_MultiStage(t *testing.T) {
	est, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal("could not parse timestamp", err)
	}

	var config map[string]interface{}
	err = yaml.Unmarshal([]byte(testYaml), &config)
	if err != nil {
		panic(err)
	}
	p, err := NewPipeline(util.Logger, config["pipeline_stages"].([]interface{}), nil, prometheus.DefaultRegisterer)
	if err != nil {
		panic(err)
	}

	tests := map[string]struct {
		entry          string
		expectedEntry  string
		t              time.Time
		expectedT      time.Time
		labels         model.LabelSet
		expectedLabels model.LabelSet
	}{
		"happy path": {
			rawTestLine,
			processedTestLine,
			time.Now(),
			time.Date(2000, 01, 25, 14, 00, 01, 0, est),
			map[model.LabelName]model.LabelValue{
				"match": "true",
			},
			map[model.LabelName]model.LabelValue{
				"match":       "true",
				"stream":      "stderr",
				"action":      "GET",
				"status_code": "200",
			},
		},
		"no match": {
			rawTestLine,
			rawTestLine,
			ct,
			ct,
			map[model.LabelName]model.LabelValue{
				"nomatch": "true",
			},
			map[model.LabelName]model.LabelValue{
				"nomatch": "true",
			},
		},
		"should initialize the extracted map with the initial labels": {
			rawTestLine,
			processedTestLine,
			time.Now(),
			time.Date(2000, 01, 25, 14, 00, 01, 0, est),
			map[model.LabelName]model.LabelValue{
				"match":    "true",
				"filename": "/var/log/nginx/frontend.log",
			},
			map[model.LabelName]model.LabelValue{
				"filename":    "/var/log/nginx/frontend.log",
				"match":       "true",
				"stream":      "stderr",
				"service":     "frontend",
				"action":      "GET",
				"status_code": "200",
			},
		},
	}

	for tName, tt := range tests {
		tt := tt
		t.Run(tName, func(t *testing.T) {
			t.Parallel()
			extracted := map[string]interface{}{}
			p.Process(tt.labels, extracted, &tt.t, &tt.entry)

			assert.Equal(t, tt.expectedLabels, tt.labels, "did not get expected labels")
			assert.Equal(t, tt.expectedEntry, tt.entry, "did not receive expected log entry")
			if tt.t.Unix() != tt.expectedT.Unix() {
				t.Fatalf("mismatch ts want: %s got:%s", tt.expectedT, tt.t)
			}
		})
	}
}

var (
	l = log.NewNopLogger()
	//w = log.NewSyncWriter(os.Stdout)
	//l = log.NewLogfmtLogger(w)
	infoLogger  = level.NewFilter(l, level.AllowInfo())
	debugLogger = level.NewFilter(l, level.AllowDebug())
)

func BenchmarkPipeline(b *testing.B) {
	benchmarks := []struct {
		name   string
		stgs   PipelineStages
		logger log.Logger
		entry  string
	}{
		{
			"two stage info level",
			loadConfig(testYaml),
			infoLogger,
			rawTestLine,
		},
		{
			"two stage debug level",
			loadConfig(testYaml),
			debugLogger,
			rawTestLine,
		},
	}
	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			pl, err := NewPipeline(bm.logger, bm.stgs, nil, prometheus.DefaultRegisterer)
			if err != nil {
				panic(err)
			}
			lb := model.LabelSet{}
			ts := time.Now()
			for i := 0; i < b.N; i++ {
				entry := bm.entry
				extracted := map[string]interface{}{}
				pl.Process(lb, extracted, &ts, &entry)
			}
		})
	}
}

type stubHandler struct {
	bool
}

func (s *stubHandler) Handle(labels model.LabelSet, time time.Time, entry string) error {
	s.bool = true
	return nil
}

func TestPipeline_Wrap(t *testing.T) {
	now := time.Now()
	var config map[string]interface{}
	err := yaml.Unmarshal([]byte(testYaml), &config)
	if err != nil {
		panic(err)
	}
	p, err := NewPipeline(util.Logger, config["pipeline_stages"].([]interface{}), nil, prometheus.DefaultRegisterer)
	if err != nil {
		panic(err)
	}

	tests := map[string]struct {
		labels     model.LabelSet
		shouldSend bool
	}{
		"should drop": {
			map[model.LabelName]model.LabelValue{
				dropLabel:     "true",
				"stream":      "stderr",
				"action":      "GET",
				"status_code": "200",
			},
			false,
		},
		"should send": {
			map[model.LabelName]model.LabelValue{
				"stream":      "stderr",
				"action":      "GET",
				"status_code": "200",
			},
			true,
		},
	}

	for tName, tt := range tests {
		tt := tt
		t.Run(tName, func(t *testing.T) {
			t.Parallel()
			extracted := map[string]interface{}{}
			p.Process(tt.labels, extracted, &now, &rawTestLine)
			stub := &stubHandler{}
			handler := p.Wrap(stub)
			if err := handler.Handle(tt.labels, now, rawTestLine); err != nil {
				t.Fatalf("failed to handle entry: %v", err)
			}
			assert.Equal(t, stub.bool, tt.shouldSend)

		})
	}
}
