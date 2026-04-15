package stages

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
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
	pl, err := NewPipeline(util_log.Logger, loadConfig(testMatchYaml), &plName, registry)
	if err != nil {
		t.Fatal(err)
	}

	in := make(chan Entry)

	out := pl.Run(in)

	in <- newEntry(nil, nil, testMatchLogLineApp1, time.Now())

	e := <-out

	assert.Equal(t, "app1 log line", e.Line)

	// Process the second log line which should extract the output from the `msg` field
	e.Line = testMatchLogLineApp2
	e.Extracted = map[string]interface{}{}
	in <- e
	e = <-out
	assert.Equal(t, "app2 log line", e.Line)
	close(in)
}

func TestMatcher(t *testing.T) {
	t.Parallel()
	tests := []struct {
		selector string
		labels   map[string]string
		action   string

		shouldDrop bool
		shouldRun  bool
		wantErr    bool
	}{
		{`{foo="bar"} |= "foo"`, map[string]string{"foo": "bar"}, MatchActionKeep, false, true, false},
		{`{foo="bar"} |~ "foo"`, map[string]string{"foo": "bar"}, MatchActionKeep, false, true, false},
		{`{foo="bar"} |= "bar"`, map[string]string{"foo": "bar"}, MatchActionKeep, false, false, false},
		{`{foo="bar"} |~ "bar"`, map[string]string{"foo": "bar"}, MatchActionKeep, false, false, false},
		{`{foo="bar"} != "bar"`, map[string]string{"foo": "bar"}, MatchActionKeep, false, true, false},
		{`{foo="bar"} !~ "bar"`, map[string]string{"foo": "bar"}, MatchActionKeep, false, true, false},
		{`{foo="bar"} != "foo"`, map[string]string{"foo": "bar"}, MatchActionKeep, false, false, false},
		{`{foo="bar"} |= "foo"`, map[string]string{"foo": "bar"}, MatchActionDrop, true, false, false},
		{`{foo="bar"} |~ "foo"`, map[string]string{"foo": "bar"}, MatchActionDrop, true, false, false},
		{`{foo="bar"} |= "bar"`, map[string]string{"foo": "bar"}, MatchActionDrop, false, false, false},
		{`{foo="bar"} |~ "bar"`, map[string]string{"foo": "bar"}, MatchActionDrop, false, false, false},
		{`{foo="bar"} != "bar"`, map[string]string{"foo": "bar"}, MatchActionDrop, true, false, false},
		{`{foo="bar"} !~ "bar"`, map[string]string{"foo": "bar"}, MatchActionDrop, true, false, false},
		{`{foo="bar"} != "foo"`, map[string]string{"foo": "bar"}, MatchActionDrop, false, false, false},
		{`{foo="bar"} !~ "[]"`, map[string]string{"foo": "bar"}, MatchActionDrop, false, false, true},
		{"foo", map[string]string{"foo": "bar"}, MatchActionKeep, false, false, true},
		{"{}", map[string]string{"foo": "bar"}, MatchActionKeep, false, false, true},
		{"{", map[string]string{"foo": "bar"}, MatchActionKeep, false, false, true},
		{"", map[string]string{"foo": "bar"}, MatchActionKeep, false, true, true},
		{`{foo="bar"}`, map[string]string{"foo": "bar"}, MatchActionKeep, false, true, false},
		{`{foo=""}`, map[string]string{"foo": "bar"}, MatchActionKeep, false, false, false},
		{`{foo=""}`, map[string]string{}, MatchActionKeep, false, true, false},
		{`{foo!="bar"}`, map[string]string{"foo": "bar"}, MatchActionKeep, false, false, false},
		{`{foo!="bar"}`, map[string]string{"foo": "bar"}, MatchActionDrop, false, false, false},
		{`{foo="bar",bar!="test"}`, map[string]string{"foo": "bar"}, MatchActionKeep, false, true, false},
		{`{foo="bar",bar!="test"}`, map[string]string{"foo": "bar"}, MatchActionDrop, true, false, false},
		{`{foo="bar",bar!="test"}`, map[string]string{"foo": "bar", "bar": "test"}, MatchActionKeep, false, false, false},
		{`{foo="bar",bar=~"te.*"}`, map[string]string{"foo": "bar", "bar": "test"}, MatchActionDrop, true, false, false},
		{`{foo="bar",bar=~"te.*"}`, map[string]string{"foo": "bar", "bar": "test"}, MatchActionKeep, false, true, false},
		{`{foo="bar",bar!~"te.*"}`, map[string]string{"foo": "bar", "bar": "test"}, MatchActionKeep, false, false, false},
		{`{foo="bar",bar!~"te.*"}`, map[string]string{"foo": "bar", "bar": "test"}, MatchActionDrop, false, false, false},

		{`{foo=""}`, map[string]string{}, MatchActionKeep, false, true, false},
		{`{foo="bar"} |= "foo" | status >= 200`, map[string]string{"foo": "bar"}, MatchActionKeep, false, false, true},
	}

	for _, tt := range tests {
		name := fmt.Sprintf("%s/%s/%s", tt.selector, tt.labels, tt.action)

		t.Run(name, func(t *testing.T) {
			// Build a match config which has a simple label stage that when matched will add the test_label to
			// the labels in the pipeline.
			var stages PipelineStages
			if tt.action != MatchActionDrop {
				stages = PipelineStages{
					PipelineStage{
						StageTypeLabel: LabelsConfig{
							"test_label": nil,
						},
					},
				}
			}
			matchConfig := MatcherConfig{
				nil,
				tt.selector,
				stages,
				tt.action,
				nil,
			}
			s, err := newMatcherStage(util_log.Logger, nil, matchConfig, prometheus.DefaultRegisterer)
			if (err != nil) != tt.wantErr {
				t.Errorf("withMatcher() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if s != nil {

				out := processEntries(s, newEntry(map[string]interface{}{
					"test_label": "unimportant value",
				}, toLabelSet(tt.labels), "foo", time.Now()))

				if tt.shouldDrop {
					if len(out) != 0 {
						t.Errorf("stage should have been dropped but got %v", out)
					}
					return
				}
				// test_label should only be in the label set if the stage ran
				if _, ok := out[0].Labels["test_label"]; ok {
					if !tt.shouldRun {
						t.Error("stage ran but should have not")
					}
				}

			}
		})
	}
}

func Test_validateMatcherConfig(t *testing.T) {
	empty := ""
	notempty := "test"
	tests := []struct {
		name    string
		cfg     *MatcherConfig
		wantErr bool
	}{
		{"empty", nil, true},
		{"pipeline name required", &MatcherConfig{PipelineName: &empty}, true},
		{"selector required", &MatcherConfig{PipelineName: &notempty, Selector: ""}, true},
		{"nil stages without dropping", &MatcherConfig{PipelineName: &notempty, Selector: `{app="foo"}`, Action: MatchActionKeep, Stages: nil}, true},
		{"empty stages without dropping", &MatcherConfig{PipelineName: &notempty, Selector: `{app="foo"}`, Action: MatchActionKeep, Stages: []interface{}{}}, true},
		{"stages with dropping", &MatcherConfig{PipelineName: &notempty, Selector: `{app="foo"}`, Action: MatchActionDrop, Stages: []interface{}{""}}, true},
		{"empty stages dropping", &MatcherConfig{PipelineName: &notempty, Selector: `{app="foo"}`, Action: MatchActionDrop, Stages: []interface{}{}}, false},
		{"stages without dropping", &MatcherConfig{PipelineName: &notempty, Selector: `{app="foo"}`, Action: MatchActionKeep, Stages: []interface{}{""}}, false},
		{"bad selector", &MatcherConfig{PipelineName: &notempty, Selector: `{app="foo}`, Action: MatchActionKeep, Stages: []interface{}{""}}, true},
		{"bad action", &MatcherConfig{PipelineName: &notempty, Selector: `{app="foo}`, Action: "nope", Stages: []interface{}{""}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateMatcherConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateMatcherConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}
