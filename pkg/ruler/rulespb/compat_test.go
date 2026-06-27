package rulespb

import (
	"testing"

	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestRuleGroupRoundTripIncludesGroupLabels(t *testing.T) {
	group := rulefmt.RuleGroup{
		Name: "group",
		Labels: map[string]string{
			"foo":    "bar",
			"shared": "group",
		},
		Rules: []rulefmt.Rule{{
			Alert: "AlertName",
			Expr:  "vector(1)",
			Labels: map[string]string{
				"shared": "rule",
			},
		}},
	}

	proto := ToProto("user", "namespace", group)
	require.Equal(t, group.Labels, logproto.FromLabelAdaptersToLabels(proto.Labels).Map())
	require.Equal(t, group.Rules[0].Labels, logproto.FromLabelAdaptersToLabels(proto.Rules[0].Labels).Map())

	roundTrip := FromProto(proto)
	require.Equal(t, group.Labels, roundTrip.Labels)
	require.Equal(t, group.Rules[0].Labels, roundTrip.Rules[0].Labels)
}
