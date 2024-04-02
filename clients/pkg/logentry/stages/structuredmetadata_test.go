package stages

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var pipelineStagesStructuredMetadataUsingMatch = `
pipeline_stages:
- match:
    selector: '{source="test"}'
    stages:
      - logfmt:
          mapping:
            app:
      - structured_metadata:
          app:
`

var pipelineStagesStructuredMetadataFromLogfmt = `
pipeline_stages:
- logfmt:
    mapping:
      app:
- structured_metadata:
    app:
`

var pipelineStagesStructuredMetadataFromJSON = `
pipeline_stages:
- json:
    expressions:
      app:
- structured_metadata:
    app:
`

var pipelineStagesStructuredMetadataWithRegexParser = `
pipeline_stages:
- regex:
    expression: "^(?s)(?P<time>\\S+?) (?P<stream>stdout|stderr) (?P<flags>\\S+?) (?P<content>.*)$"
- structured_metadata:
    stream:
`

var pipelineStagesStructuredMetadataFromJSONWithTemplate = `
pipeline_stages:
- json:
    expressions:
      app:
- template:
    source: app
    template: '{{ ToUpper .Value }}'
- structured_metadata:
    app:
`

var pipelineStagesStructuredMetadataAndRegularLabelsFromJSON = `
pipeline_stages:
- json:
    expressions:
      app:
      component:
- structured_metadata:
    app:
- labels:
    component: 
`

var deprecatedPipelineStagesStructuredMetadataFromJSON = `
pipeline_stages:
- json:
    expressions:
      app:
- non_indexed_labels:
    app:
`

var pipelineStagesStructuredMetadataFromStaticLabels = `
pipeline_stages:
- static_labels:
    component: querier
    pod: loki-querier-664f97db8d-qhnwg
- structured_metadata:
    pod:
`

var pipelineStagesStructuredMetadataFromStaticLabelsDifferentKey = `
pipeline_stages:
- static_labels:
    component: querier
    pod: loki-querier-664f97db8d-qhnwg
- structured_metadata:
    pod_name: pod
`

func Test_StructuredMetadataStage(t *testing.T) {
	tests := map[string]struct {
		pipelineStagesYaml         string
		logLine                    string
		streamLabels               model.LabelSet
		expectedStructuredMetadata push.LabelsAdapter
		expectedLabels             model.LabelSet
	}{
		"expected structured metadata to be extracted with logfmt parser and to be added to entry": {
			pipelineStagesYaml:         pipelineStagesStructuredMetadataFromLogfmt,
			logLine:                    "app=loki component=ingester",
			expectedStructuredMetadata: push.LabelsAdapter{push.LabelAdapter{Name: "app", Value: "loki"}},
		},
		"expected structured metadata to be extracted with json parser and to be added to entry": {
			pipelineStagesYaml:         pipelineStagesStructuredMetadataFromJSON,
			logLine:                    `{"app":"loki" ,"component":"ingester"}`,
			expectedStructuredMetadata: push.LabelsAdapter{push.LabelAdapter{Name: "app", Value: "loki"}},
		},
		"expected structured metadata to be extracted with json parser and to be added to entry even if deprecated stage name is used": {
			pipelineStagesYaml:         deprecatedPipelineStagesStructuredMetadataFromJSON,
			logLine:                    `{"app":"loki" ,"component":"ingester"}`,
			expectedStructuredMetadata: push.LabelsAdapter{push.LabelAdapter{Name: "app", Value: "loki"}},
		},
		"expected structured metadata to be extracted with regexp parser and to be added to entry": {
			pipelineStagesYaml:         pipelineStagesStructuredMetadataWithRegexParser,
			logLine:                    `2019-01-01T01:00:00.000000001Z stderr P i'm a log message!`,
			expectedStructuredMetadata: push.LabelsAdapter{push.LabelAdapter{Name: "stream", Value: "stderr"}},
		},
		"expected structured metadata to be extracted with json parser and to be added to entry after rendering the template": {
			pipelineStagesYaml:         pipelineStagesStructuredMetadataFromJSONWithTemplate,
			logLine:                    `{"app":"loki" ,"component":"ingester"}`,
			expectedStructuredMetadata: push.LabelsAdapter{push.LabelAdapter{Name: "app", Value: "LOKI"}},
		},
		"expected structured metadata and regular labels to be extracted with json parser and to be added to entry": {
			pipelineStagesYaml:         pipelineStagesStructuredMetadataAndRegularLabelsFromJSON,
			logLine:                    `{"app":"loki" ,"component":"ingester"}`,
			expectedStructuredMetadata: push.LabelsAdapter{push.LabelAdapter{Name: "app", Value: "loki"}},
			expectedLabels:             model.LabelSet{model.LabelName("component"): model.LabelValue("ingester")},
		},
		"expected structured metadata to be extracted using match stage": {
			pipelineStagesYaml:         pipelineStagesStructuredMetadataUsingMatch,
			logLine:                    `app=loki component=ingester`,
			expectedStructuredMetadata: push.LabelsAdapter{push.LabelAdapter{Name: "app", Value: "loki"}},
			expectedLabels:             model.LabelSet{model.LabelName("source"): model.LabelValue("test")},
			streamLabels:               model.LabelSet{model.LabelName("source"): model.LabelValue("test")},
		},
		"expected structured metadata and regular labels to be extracted with static labels stage and to be added to entry": {
			pipelineStagesYaml:         pipelineStagesStructuredMetadataFromStaticLabels,
			logLine:                    `sample log line`,
			expectedStructuredMetadata: push.LabelsAdapter{push.LabelAdapter{Name: "pod", Value: "loki-querier-664f97db8d-qhnwg"}},
			expectedLabels:             model.LabelSet{model.LabelName("component"): model.LabelValue("querier")},
		},
		"expected structured metadata and regular labels to be extracted with static labels stage using different structured key": {
			pipelineStagesYaml:         pipelineStagesStructuredMetadataFromStaticLabelsDifferentKey,
			logLine:                    `sample log line`,
			expectedStructuredMetadata: push.LabelsAdapter{push.LabelAdapter{Name: "pod_name", Value: "loki-querier-664f97db8d-qhnwg"}},
			expectedLabels:             model.LabelSet{model.LabelName("component"): model.LabelValue("querier")},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			pl, err := NewPipeline(util_log.Logger, loadConfig(test.pipelineStagesYaml), nil, prometheus.DefaultRegisterer)
			require.NoError(t, err)
			result := processEntries(pl, newEntry(nil, test.streamLabels, test.logLine, time.Now()))[0]
			require.Equal(t, test.expectedStructuredMetadata, result.StructuredMetadata)
			if test.expectedLabels != nil {
				require.Equal(t, test.expectedLabels, result.Labels)
			} else {
				require.Empty(t, result.Labels)
			}
		})
	}
}
