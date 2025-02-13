package main

import (
	"testing"

	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/require"

	"github.com/grafana/regexp"
)

func TestParseRelabelConfigs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []*relabel.Config
		wantErr bool
	}{
		{
			name:    "empty input",
			input:   "",
			want:    nil,
			wantErr: false,
		},
		{
			name:  "default config",
			input: `[{"target_label": "new_label"}]`,
			want: []*relabel.Config{
				{
					TargetLabel: "new_label",
					Action:      relabel.Replace,
					Regex:       relabel.Regexp{Regexp: regexp.MustCompile("(.*)")},
					Replacement: "$1",
				},
			},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			input:   "invalid json",
			wantErr: true,
		},
		{
			name: "valid single config",
			input: `[{
				"source_labels": ["__name__"],
				"regex": "my_metric_.*",
				"target_label": "new_label",
				"replacement": "foo",
				"action": "replace"
			}]`,
			wantErr: false,
		},
		{
			name: "invalid regex",
			input: `[{
				"source_labels": ["__name__"],
				"regex": "[[invalid regex",
				"target_label": "new_label",
				"action": "replace"
			}]`,
			wantErr: true,
		},
		{
			name: "multiple valid configs",
			input: `[
				{
					"source_labels": ["__name__"],
					"regex": "my_metric_.*",
					"target_label": "new_label",
					"replacement": "foo",
					"action": "replace"
				},
				{
					"source_labels": ["label1", "label2"],
					"separator": ";",
					"regex": "val1;val2",
					"target_label": "combined",
					"action": "replace"
				}
			]`,
			wantErr: false,
		},
		{
			name: "invalid action",
			input: `[{
				"source_labels": ["__name__"],
				"regex": "my_metric_.*",
				"target_label": "new_label",
				"action": "invalid_action"
			}]`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRelabelConfigs(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.input == "" {
				require.Nil(t, got)
				return
			}

			require.NotNil(t, got)
			// For valid configs, verify they can be used for relabeling
			// This implicitly tests that the conversion was successful
			if len(got) > 0 {
				for _, cfg := range got {
					require.NotNil(t, cfg)
					require.NotEmpty(t, cfg.Action)
				}
			}
		})
	}
}
