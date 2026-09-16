package rulespb

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/stretchr/testify/require"
)

const (
	indentedExpr = "\n   sum by (level) (\n     count_over_time({job=~\".+\"}[1m])\n   )\n  "
	trimmedExpr  = "sum by (level) (\n     count_over_time({job=~\".+\"}[1m])\n   )"
)

func TestToProtoTrimsWhitespaceSurroundingExpr(t *testing.T) {
	group := rulefmt.RuleGroup{
		Name:     "group",
		Interval: model.Duration(15 * time.Second),
		Rules: []rulefmt.Rule{
			{Record: "recording", Expr: indentedExpr},
			{
				Alert:       "alerting",
				Expr:        indentedExpr,
				For:         model.Duration(30 * time.Second),
				Labels:      map[string]string{"severity": "page"},
				Annotations: map[string]string{"summary": "too many logs"},
			},
		},
	}

	desc := ToProto("user1", "namespace", group)

	require.Equal(t, trimmedExpr, desc.Rules[0].Expr)
	require.Equal(t, trimmedExpr, desc.Rules[1].Expr)

	require.Equal(t, "recording", desc.Rules[0].Record)
	require.Equal(t, "alerting", desc.Rules[1].Alert)
	require.Equal(t, 30*time.Second, desc.Rules[1].For)
	require.Equal(t, "severity", desc.Rules[1].Labels[0].Name)
	require.Equal(t, "summary", desc.Rules[1].Annotations[0].Name)
}

// Groups stored before expressions were trimmed on write must still read back cleanly,
// otherwise the namespace stays unreadable for as long as the group exists.
func TestFromProtoTrimsWhitespaceSurroundingExpr(t *testing.T) {
	desc := &RuleGroupDesc{
		Name:      "group",
		Namespace: "namespace",
		User:      "user1",
		Interval:  15 * time.Second,
		Rules:     []*RuleDesc{{Record: "recording", Expr: indentedExpr}},
	}

	group := FromProto(desc)

	require.Equal(t, trimmedExpr, group.Rules[0].Expr)
	require.Equal(t, "recording", group.Rules[0].Record)
}
