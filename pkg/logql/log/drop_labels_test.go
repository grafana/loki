package log

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

func Test_DropLabels(t *testing.T) {
	tests := []struct {
		Name       string
		dropLabels []NamedLabelMatcher
		err        string
		errDetails string
		lbs        labels.Labels
		want       labels.Labels
	}{
		{
			"drop by name",
			[]NamedLabelMatcher{
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
			labels.FromStrings("app", "foo",
				"namespace", "prod",
				"pod_uuid", "foo",
			),
			labels.FromStrings("pod_uuid", "foo"),
		},
		{
			"drop by __error__",
			[]NamedLabelMatcher{
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
			labels.FromStrings("app", "foo",
				"namespace", "prod",
				"pod_uuid", "foo",
			),
			labels.FromStrings("app", "foo",
				"namespace", "prod",
				"pod_uuid", "foo",
			),
		},
		{
			"drop with wrong __error__ value",
			[]NamedLabelMatcher{
				{
					labels.MustNewMatcher(labels.MatchEqual, logqlmodel.ErrorLabel, errLogfmt),
					"",
				},
			},
			errJSON,
			"json error",
			labels.FromStrings("app", "foo",
				"namespace", "prod",
				"pod_uuid", "foo",
			),
			labels.FromStrings("app", "foo",
				"namespace", "prod",
				"pod_uuid", "foo",
				logqlmodel.ErrorLabel, errJSON,
				logqlmodel.ErrorDetailsLabel, "json error",
			),
		},
		{
			"drop by __error_details__",
			[]NamedLabelMatcher{
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
			labels.FromStrings("app", "foo",
				"namespace", "prod",
				"pod_uuid", "foo",
			),
			labels.FromStrings("app", "foo",
				"namespace", "prod",
				"pod_uuid", "foo",
			),
		},
		{
			"drop labels with names and matcher",
			[]NamedLabelMatcher{
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
			labels.FromStrings("app", "foo",
				"namespace", "prod",
				"pod_uuid", "foo",
			),
			labels.FromStrings("pod_uuid", "foo"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dropLabels := NewDropLabels(tt.dropLabels)
			lbls := NewBaseLabelsBuilder().ForLabels(tt.lbs, tt.lbs.Hash())
			lbls.Reset()
			lbls.SetErr(tt.err)
			lbls.SetErrorDetails(tt.errDetails)
			dropLabels.Process(0, []byte(""), lbls)
			require.Equal(t, tt.want, lbls.LabelsResult().Labels())
		})
	}
}
