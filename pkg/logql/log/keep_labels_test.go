package log

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

func Test_KeepLabels(t *testing.T) {
	for _, tc := range []struct {
		Name       string
		keepLabels []KeepLabel
		lbs        labels.Labels

		want labels.Labels
	}{
		{
			"keep all",
			[]KeepLabel{},
			labels.FromStrings(
				"app", "foo",
				"namespace", "prod",
				"env", "prod",
				"pod_uuid", "foo",
			),
			labels.FromStrings(
				"app", "foo",
				"namespace", "prod",
				"env", "prod",
				"pod_uuid", "foo",
			),
		},
		{
			"keep by name",
			[]KeepLabel{
				{
					nil,
					"app",
				},
				{
					nil,
					"namespace",
				},
			},
			labels.FromStrings(
				"app", "foo",
				"namespace", "prod",
				"env", "prod",
				"pod_uuid", "foo",
			),
			labels.FromStrings(
				"app", "foo",
				"namespace", "prod",
			),
		},
		{
			"keep labels with names and matcher",
			[]KeepLabel{
				{
					labels.MustNewMatcher(labels.MatchEqual, "namespace", "prod"),
					"",
				},
				{
					nil,
					"app",
				},
				{
					nil,
					"fizz",
				},
			},
			labels.FromStrings(
				"app", "foo",
				"namespace", "prod",
				"env", "prod",
				"pod_uuid", "foo",
			),
			labels.FromStrings(
				"app", "foo",
				"namespace", "prod",
			),
		},
		{
			"preserve special labels",
			[]KeepLabel{
				{
					labels.MustNewMatcher(labels.MatchEqual, "namespace", "prod"),
					"",
				},
				{
					nil,
					"app",
				},
			},
			labels.FromStrings(
				"app", "foo",
				"namespace", "prod",
				"env", "prod",
				"pod_uuid", "foo",
				logqlmodel.PreserveErrorLabel, "true",
			),
			labels.FromStrings(
				"app", "foo",
				"namespace", "prod",
				logqlmodel.PreserveErrorLabel, "true",
			),
		},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			keepLabels := NewKeepLabels(tc.keepLabels)
			lbls := NewBaseLabelsBuilder().ForLabels(tc.lbs, tc.lbs.Hash())
			lbls.Reset()
			keepLabels.Process(0, []byte(""), lbls)
			require.Equal(t, tc.want, lbls.LabelsResult().Labels())
		})
	}
}
