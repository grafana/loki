package stages

import (
	"fmt"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

var testMatchYaml = `
pipeline_stages:
- json:
    expressions:
      app:
- labels:
    app:
- match:
    pipeline_name: "app1"
    selector: "{app=\"loki\"}"
    stages:
    - json:
        expressions:
          msg: message
- match:
    pipeline_name: "app2"
    selector: "{app=\"poki\"}"
    stages:
    - json:
        expressions:
          msg: msg
- output:
    source: msg
`

var testMatchLogLineApp1 = `
{
	"time":"2012-11-01T22:08:41+00:00",
	"app":"loki",
	"component": ["parser","type"],
	"level" : "WARN",
	"message" : "app1 log line"
}
`

var testMatchLogLineApp2 = `
{
	"time":"2012-11-01T22:08:41+00:00",
	"app":"poki",
	"component": ["parser","type"],
	"level" : "WARN",
	"msg" : "app2 log line"
}
`

func TestMatchPipeline(t *testing.T) {
	pl, err := NewPipeline(util.Logger, loadConfig(testMatchYaml), "test", prometheus.DefaultRegisterer)
	if err != nil {
		t.Fatal(err)
	}
	lbls := model.LabelSet{}
	ts := time.Now()
	entry := testMatchLogLineApp1
	extracted := map[string]interface{}{}
	pl.Process(lbls, extracted, &ts, &entry)
	assert.Equal(t, "app1 log line", entry)
	entry = testMatchLogLineApp2
	extracted = map[string]interface{}{}
	pl.Process(lbls, extracted, &ts, &entry)
	assert.Equal(t, "app2 log line", entry)
}

func TestMatcher(t *testing.T) {
	t.Parallel()
	tests := []struct {
		matcher string
		labels  map[string]string

		shouldRun bool
		wantErr   bool
	}{
		{"{foo=\"bar\"} |= \"foo\"", map[string]string{"foo": "bar"}, false, true},
		{"{foo=\"bar\"} |~ \"foo\"", map[string]string{"foo": "bar"}, false, true},
		{"foo", map[string]string{"foo": "bar"}, false, true},
		{"{}", map[string]string{"foo": "bar"}, false, true},
		{"{", map[string]string{"foo": "bar"}, false, true},
		{"", map[string]string{"foo": "bar"}, true, true},
		{"{foo=\"bar\"}", map[string]string{"foo": "bar"}, true, false},
		{"{foo=\"\"}", map[string]string{"foo": "bar"}, false, false},
		{"{foo=\"\"}", map[string]string{}, true, false},
		{"{foo!=\"bar\"}", map[string]string{"foo": "bar"}, false, false},
		{"{foo=\"bar\",bar!=\"test\"}", map[string]string{"foo": "bar"}, true, false},
		{"{foo=\"bar\",bar!=\"test\"}", map[string]string{"foo": "bar", "bar": "test"}, false, false},
		{"{foo=\"bar\",bar=~\"te.*\"}", map[string]string{"foo": "bar", "bar": "test"}, true, false},
		{"{foo=\"bar\",bar!~\"te.*\"}", map[string]string{"foo": "bar", "bar": "test"}, false, false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.matcher, tt.labels), func(t *testing.T) {
			// Build a match config which has a simple label stage that when matched will add the test_label to
			// the labels in the pipeline.
			matchConfig := MatcherConfig{
				"pl_name",
				tt.matcher,
				PipelineStages{
					PipelineStage{
						StageTypeLabel: LabelsConfig{
							"test_label": nil,
						},
					},
				},
			}
			s, err := newMatcherStage(util.Logger, matchConfig, prometheus.DefaultRegisterer)
			if (err != nil) != tt.wantErr {
				t.Errorf("withMatcher() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if s != nil {
				ts, entry := time.Now(), ""
				extracted := map[string]interface{}{
					"test_label": "unimportant value",
				}
				labels := toLabelSet(tt.labels)
				s.Process(labels, extracted, &ts, &entry)

				// test_label should only be in the label set if the stage ran
				if _, ok := labels["test_label"]; ok {
					if !tt.shouldRun {
						t.Error("stage ran but should have not")
					}
				}
			}
		})
	}
}
