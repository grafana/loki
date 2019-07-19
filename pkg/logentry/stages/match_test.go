package stages

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
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
	registry := prometheus.NewRegistry()
	plName := "test_pipeline"
	pl, err := NewPipeline(util.Logger, loadConfig(testMatchYaml), &plName, registry)
	if err != nil {
		t.Fatal(err)
	}
	lbls := model.LabelSet{}
	ts := time.Now()
	// Process the first log line which should extract the output from the `message` field
	entry := testMatchLogLineApp1
	extracted := map[string]interface{}{}
	pl.Process(lbls, extracted, &ts, &entry)
	assert.Equal(t, "app1 log line", entry)

	// Process the second log line which should extract the output from the `msg` field
	entry = testMatchLogLineApp2
	extracted = map[string]interface{}{}
	pl.Process(lbls, extracted, &ts, &entry)
	assert.Equal(t, "app2 log line", entry)

	got, err := registry.Gather()
	if err != nil {
		t.Fatalf("gathering metrics failed: %s", err)
	}
	var gotBuf bytes.Buffer
	enc := expfmt.NewEncoder(&gotBuf, expfmt.FmtText)
	for _, mf := range got {
		if err := enc.Encode(mf); err != nil {
			t.Fatalf("encoding gathered metrics failed: %s", err)
		}
	}
	gotStr := gotBuf.String()
	// We should only get metrics from the main pipeline and the second match which defines the pipeline_name
	assert.Contains(t, gotStr, "logentry_pipeline_duration_seconds_bucket{job_name=\"test_pipeline\"")
	assert.Contains(t, gotStr, "logentry_pipeline_duration_seconds_bucket{job_name=\"test_pipeline_app2\"")
	assert.NotContains(t, gotStr, "logentry_pipeline_duration_seconds_bucket{job_name=\"test_pipeline_app1\"")
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
		{"{foo=\"\"}", map[string]string{}, true, false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.matcher, tt.labels), func(t *testing.T) {
			// Build a match config which has a simple label stage that when matched will add the test_label to
			// the labels in the pipeline.
			matchConfig := MatcherConfig{
				nil,
				tt.matcher,
				PipelineStages{
					PipelineStage{
						StageTypeLabel: LabelsConfig{
							"test_label": nil,
						},
					},
				},
			}
			s, err := newMatcherStage(util.Logger, nil, matchConfig, prometheus.DefaultRegisterer)
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
