package log

import (
	"sort"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logqlmodel"
)

func Test_DropLabels(t *testing.T) {
	tests := []struct {
		Name       string
		dropLabels []DropLabel
		err        string
		errDetails string
		lbs        labels.Labels
		want       labels.Labels
	}{
		{
			"filter by name",
			[]DropLabel{
				{
					nil,
					"app",
				},
				{
					nil,
					"namespace",
				},
			},
			"",
			"",
			labels.Labels{
				{Name: "app", Value: "foo"},
				{Name: "namespace", Value: "prod"},
				{Name: "pod_uuid", Value: "foo"},
			},
			labels.Labels{
				{Name: "pod_uuid", Value: "foo"},
			},
		},
		{
			"filter by __error__",
			[]DropLabel{
				{
					&labels.Matcher{
						Type:  labels.MatchEqual,
						Name:  logqlmodel.ErrorLabel,
						Value: errJSON,
					},
					"",
				},
			},
			errJSON,
			"json error",
			labels.Labels{
				{Name: "app", Value: "foo"},
				{Name: "namespace", Value: "prod"},
				{Name: "pod_uuid", Value: "foo"},
			},
			labels.Labels{
				{Name: "app", Value: "foo"},
				{Name: "namespace", Value: "prod"},
				{Name: "pod_uuid", Value: "foo"},
			},
		},
		{
			// we don't support dropping labels by __error_details__
			"filter by __error_details__",
			[]DropLabel{
				{
					&labels.Matcher{
						Type:  labels.MatchEqual,
						Name:  logqlmodel.ErrorDetailsLabel,
						Value: "json error",
					},
					"",
				},
			},
			errJSON,
			"json error",
			labels.Labels{
				{Name: "app", Value: "foo"},
				{Name: "namespace", Value: "prod"},
				{Name: "pod_uuid", Value: "foo"},
			},
			labels.Labels{
				{Name: "app", Value: "foo"},
				{Name: "namespace", Value: "prod"},
				{Name: "pod_uuid", Value: "foo"},
				{Name: logqlmodel.ErrorLabel, Value: errJSON},
				{Name: logqlmodel.ErrorDetailsLabel, Value: "json error"},
			},
		},
		{
			"drop labels with names and matcher",
			[]DropLabel{
				{
					&labels.Matcher{
						Type:  labels.MatchEqual,
						Name:  logqlmodel.ErrorLabel,
						Value: errJSON,
					},
					"",
				},
				{
					nil,
					"app",
				},
				{
					nil,
					"namespace",
				},
			},
			errJSON,
			"json error",
			labels.Labels{
				{Name: "app", Value: "foo"},
				{Name: "namespace", Value: "prod"},
				{Name: "pod_uuid", Value: "foo"},
			},
			labels.Labels{
				{Name: "pod_uuid", Value: "foo"},
			},
		},
	}
	for _, tt := range tests {
		dropLabels := NewDropLabels(tt.dropLabels)
		lbls := NewBaseLabelsBuilder().ForLabels(tt.lbs, tt.lbs.Hash())
		lbls.Reset()
		lbls.SetErr(tt.err)
		lbls.SetErrorDetails(tt.errDetails)
		dropLabels.Process(0, []byte(""), lbls)
		sort.Sort(tt.want)
		require.Equal(t, tt.want, lbls.LabelsResult().Labels())
	}
}
