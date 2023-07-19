package stages

import (
	"github.com/grafana/loki/pkg/push"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

var pipelineStagesNonIndexedLabelsFromLogfmt = `
pipeline_stages:
- logfmt:
    mapping:
      app:
- non_indexed_labels:
    app:
`

var pipelineStagesNonIndexedLabelsFromJson = `
pipeline_stages:
- json:
    expressions:
      app:
- non_indexed_labels:
    app:
`

var pipelineStagesNonIndexedLabelsWithRegexParser = `
pipeline_stages:
- regex:
    expression: "^(?s)(?P<time>\\S+?) (?P<stream>stdout|stderr) (?P<flags>\\S+?) (?P<content>.*)$"
- non_indexed_labels:
    stream:
`

var pipelineStagesNonIndexedLabelsFromJsonWithTemplate = `
pipeline_stages:
- json:
    expressions:
      app:
- template:
    source: app
    template: '{{ ToUpper .Value }}'
- non_indexed_labels:
    app:
`

func Test_NonIndexedLabelsStage(t *testing.T) {
	tests := map[string]struct {
		pipelineStagesYaml string
		logLine            string
		expectedLabels     push.LabelsAdapter
	}{
		"expected non-indexed labels to be extracted with logfmt parser and to be added to entry": {
			pipelineStagesYaml: pipelineStagesNonIndexedLabelsFromLogfmt,
			logLine:            "app=loki component=ingester",
			expectedLabels:     push.LabelsAdapter{push.LabelAdapter{Name: "app", Value: "loki"}},
		},
		"expected non-indexed labels to be extracted with json parser and to be added to entry": {
			pipelineStagesYaml: pipelineStagesNonIndexedLabelsFromJson,
			logLine:            `{"app":"loki" ,"component":"ingester"}`,
			expectedLabels:     push.LabelsAdapter{push.LabelAdapter{Name: "app", Value: "loki"}},
		},
		"expected non-indexed labels to be extracted with regexp parser and to be added to entry": {
			pipelineStagesYaml: pipelineStagesNonIndexedLabelsWithRegexParser,
			logLine:            `2019-01-01T01:00:00.000000001Z stderr P i'm a log message!`,
			expectedLabels:     push.LabelsAdapter{push.LabelAdapter{Name: "stream", Value: "stderr"}},
		},
		"expected non-indexed labels to be extracted with json parser and to be added to entry after rendering the template": {
			pipelineStagesYaml: pipelineStagesNonIndexedLabelsFromJsonWithTemplate,
			logLine:            `{"app":"loki" ,"component":"ingester"}`,
			expectedLabels:     push.LabelsAdapter{push.LabelAdapter{Name: "app", Value: "LOKI"}},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			pl, err := NewPipeline(util_log.Logger, loadConfig(test.pipelineStagesYaml), nil, prometheus.DefaultRegisterer)
			require.NoError(t, err)

			result := processEntries(pl, newEntry(nil, nil, test.logLine, time.Now()))[0]

			require.Equal(t, test.expectedLabels, result.NonIndexedLabels)
			require.Empty(t, result.Labels)
		})
	}
}
