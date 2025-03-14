package main

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/docker/docker/daemon/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/logentry/stages"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var jobRename = `
- regex: (.*)
  source_labels: [swarm_stack]
  target_label: job
- regex: ^swarm_stack$
  action: labeldrop`

func Test_relabelConfig(t *testing.T) {

	tests := []struct {
		name    string
		config  string
		in      model.LabelSet
		out     model.LabelSet
		wantErr bool
	}{
		{
			"config err",
			"foo",
			nil,
			nil,
			true,
		},
		{
			"config err",
			jobRename,
			model.LabelSet{"swarm_stack": "foo", "bar": "buzz"},
			model.LabelSet{"job": "foo", "bar": "buzz"},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := relabelConfig(tt.config, tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("relabelConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			require.True(t, got.Equal(tt.out))
		})
	}
}

var pipelineString = `
- regex:
     expression: '(level|lvl|severity)=(?P<level>\w+)'
- labels:
    level:
`
var pipeline = PipelineConfig{
	PipelineStages: []interface{}{
		map[interface{}]interface{}{
			"regex": map[interface{}]interface{}{
				"expression": "(level|lvl|severity)=(?P<level>\\w+)",
			},
		},
		map[interface{}]interface{}{
			"labels": map[interface{}]interface{}{
				"level": nil,
			},
		},
	},
}

func Test_parsePipeline(t *testing.T) {
	f, err := os.CreateTemp("", "Test_parsePipeline")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	_, err = f.Write([]byte(fmt.Sprintf("pipeline_stages:\n%s", pipelineString)))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		logCtx  logger.Info
		want    PipelineConfig
		wantErr bool
	}{
		{"no config", logger.Info{Config: map[string]string{}}, PipelineConfig{}, false},
		{"double config", logger.Info{Config: map[string]string{cfgPipelineStagesFileKey: "", cfgPipelineStagesKey: ""}}, PipelineConfig{}, true},
		{"string config", logger.Info{Config: map[string]string{cfgPipelineStagesKey: pipelineString}}, pipeline, false},
		{"string wrong", logger.Info{Config: map[string]string{cfgPipelineStagesKey: "pipelineString"}}, PipelineConfig{}, true},
		{"file config", logger.Info{Config: map[string]string{cfgPipelineStagesFileKey: f.Name()}}, pipeline, false},
		{"file wrong", logger.Info{Config: map[string]string{cfgPipelineStagesFileKey: "foo"}}, PipelineConfig{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePipeline(tt.logCtx)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePipeline() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parsePipeline() = %v, want %v", got, tt.want)
			}

			// all configs are supposed to be valid
			name := "foo"
			_, err = stages.NewPipeline(util_log.Logger, got.PipelineStages, &name, prometheus.DefaultRegisterer)
			if err != nil {
				t.Error(err)
			}

		})
	}
}
