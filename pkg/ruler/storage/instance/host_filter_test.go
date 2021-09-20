// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package instance

import (
	"testing"

	"github.com/grafana/agent/pkg/util"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/pkg/relabel"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func makeGroup(labels []model.LabelSet) *targetgroup.Group {
	return &targetgroup.Group{
		Targets: labels,
		Labels:  model.LabelSet{},
	}
}

func TestFilterGroups(t *testing.T) {
	tt := []struct {
		name         string
		labelHost    string
		inputHost    string
		shouldRemove bool
	}{
		{
			name:         "complete match",
			labelHost:    "myhost",
			inputHost:    "myhost",
			shouldRemove: false,
		},
		{
			name:         "mismatch",
			labelHost:    "notmyhost",
			inputHost:    "myhost",
			shouldRemove: true,
		},
		{
			name:         "match with port",
			labelHost:    "myhost:12345",
			inputHost:    "myhost",
			shouldRemove: false,
		},
		{
			name:         "mismatch with port",
			labelHost:    "notmyhost:12345",
			inputHost:    "myhost",
			shouldRemove: true,
		},
	}

	// Sets of labels we want to test against.
	labels := []model.LabelName{
		model.AddressLabel,
		model.LabelName("__meta_consul_node"),
		model.LabelName("__meta_dockerswarm_node_id"),
		model.LabelName("__meta_dockerswarm_node_hostname"),
		model.LabelName("__meta_dockerswarm_node_address"),
		model.LabelName("__meta_kubernetes_pod_node_name"),
		model.LabelName("__meta_kubernetes_node_name"),
		model.LabelName("__host__"),
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			for _, label := range labels {
				t.Run(string(label), func(t *testing.T) {
					lset := model.LabelSet{
						label: model.LabelValue(tc.labelHost),
					}

					// Special case: if label is not model.AddressLabel, we need to give
					// it a fake value. model.AddressLabel is always expected to be present and
					// is considered an error if it isn't.
					if label != model.AddressLabel {
						lset[model.AddressLabel] = "fake"
					}

					group := makeGroup([]model.LabelSet{lset})

					groups := DiscoveredGroups{"test": []*targetgroup.Group{group}}
					result := FilterGroups(groups, tc.inputHost, nil)

					require.NotNil(t, result["test"])
					if tc.shouldRemove {
						require.NotEqual(t, len(result["test"][0].Targets), len(groups["test"][0].Targets))
					} else {
						require.Equal(t, len(result["test"][0].Targets), len(groups["test"][0].Targets))
					}
				})
			}
		})
	}
}

func TestFilterGroups_Relabel(t *testing.T) {
	tt := []struct {
		name         string
		labelHost    string
		inputHost    string
		shouldRemove bool
	}{
		{
			name:         "complete match",
			labelHost:    "myhost",
			inputHost:    "myhost",
			shouldRemove: false,
		},
		{
			name:         "mismatch",
			labelHost:    "notmyhost",
			inputHost:    "myhost",
			shouldRemove: true,
		},
		{
			name:         "match with port",
			labelHost:    "myhost:12345",
			inputHost:    "myhost",
			shouldRemove: false,
		},
		{
			name:         "mismatch with port",
			labelHost:    "notmyhost:12345",
			inputHost:    "myhost",
			shouldRemove: true,
		},
	}

	relabelConfig := []*relabel.Config{{
		SourceLabels: model.LabelNames{"__internal_label"},
		Action:       relabel.Replace,
		Separator:    ";",
		Regex:        relabel.MustNewRegexp("(.*)"),
		Replacement:  "$1",
		TargetLabel:  "__host__",
	}}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			lset := model.LabelSet{
				model.AddressLabel: "fake_target",
				"__internal_label": model.LabelValue(tc.labelHost),
			}

			group := makeGroup([]model.LabelSet{lset})

			groups := DiscoveredGroups{"test": []*targetgroup.Group{group}}
			result := FilterGroups(groups, tc.inputHost, relabelConfig)

			require.NotNil(t, result["test"])
			if tc.shouldRemove {
				require.NotEqual(t, len(result["test"][0].Targets), len(groups["test"][0].Targets))
			} else {
				require.Equal(t, len(result["test"][0].Targets), len(groups["test"][0].Targets))
			}
		})
	}
}

func TestHostFilter_PatchSD(t *testing.T) {
	rawInput := util.Untab(`
- job_name: default
  kubernetes_sd_configs:
	  - role: service
	  - role: pod`)

	expect := util.Untab(`
- job_name: default
	honor_timestamps: true
	metrics_path: /metrics
	scheme: http
	follow_redirects: true
	kubernetes_sd_configs:
		- role: service
		  follow_redirects: true
		- role: pod
			follow_redirects: true
			selectors:
			- role: pod
			  field: spec.nodeName=myhost
	`)

	var input []*config.ScrapeConfig
	err := yaml.Unmarshal([]byte(rawInput), &input)
	require.NoError(t, err)

	NewHostFilter("myhost", nil).PatchSD(input)

	output, err := yaml.Marshal(input)
	require.NoError(t, err)
	require.YAMLEq(t, expect, string(output))
}
