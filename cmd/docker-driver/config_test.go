package main

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
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
