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
			"drop by name",
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
			"drop by __error__",
			[]DropLabel{
				{
					labels.MustNewMatcher(labels.MatchEqual, logqlmodel.ErrorLabel, errJSON),
					"",
				},
				{
					nil,
					"__error_details__",
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
			"drop with wrong __error__ value",
			[]DropLabel{
				{
					labels.MustNewMatcher(labels.MatchEqual, logqlmodel.ErrorLabel, errLogfmt),
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
			"drop by __error_details__",
			[]DropLabel{
				{
					labels.MustNewMatcher(labels.MatchRegexp, logqlmodel.ErrorDetailsLabel, "expecting json.*"),
					"",
				},
				{
					nil,
					"__error__",
				},
			},
			errJSON,
			"expecting json object but it is not",
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
			"drop labels with names and matcher",
			[]DropLabel{
				{
					labels.MustNewMatcher(labels.MatchEqual, logqlmodel.ErrorLabel, errJSON),
					"",
				},
				{
					nil,
					"__error_details__",
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
