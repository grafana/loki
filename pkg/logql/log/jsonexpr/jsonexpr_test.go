package jsonexpr

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJSONExpressionParser(t *testing.T) {
	// {"app":"foo","field with space":"value","field with ÜFT8👌":true,"namespace":"prod","pod":{"uuid":"foo","deployment":{"ref":"foobar", "params": [{"param": true},2,3]}}}

	tests := []struct {
		name       string
		expression string
		want       []any
		error      error
	}{
		{
			"single field",
			"app",
			[]any{"app"},
			nil,
		},
		{
			"top-level field with spaces",
			`["field with space"]`,
			[]any{"field with space"},
			nil,
		},
		{
			"top-level field with UTF8",
			`["field with ÜFT8👌"]`,
			[]any{"field with ÜFT8👌"},
			nil,
		},
		{
			"top-level array access",
			`[0]`,
			[]any{0},
			nil,
		},
		{
			"nested field",
			`pod.uuid`,
			[]any{"pod", "uuid"},
			nil,
		},
		{
			"nested field alternate syntax",
			`pod["uuid"]`,
			[]any{"pod", "uuid"},
			nil,
		},
		{
			"nested field alternate syntax 2",
			`["pod"]["uuid"]`,
			[]any{"pod", "uuid"},
			nil,
		},
		{
			"array access",
			`pod.deployment.params[0]`,
			[]any{"pod", "deployment", "params", 0},
			nil,
		},
		{
			"multi-level array access",
			`pod.deployment.params[0].param`,
			[]any{"pod", "deployment", "params", 0, "param"},
			nil,
		},
		{
			"multi-level array access alternate syntax",
			`pod.deployment.params[0]["param"]`,
			[]any{"pod", "deployment", "params", 0, "param"},
			nil,
		},
		{
			"empty",
			``,
			nil,
			nil,
		},

		{
			"invalid field access",
			`field with space`,
			nil,
			fmt.Errorf("syntax error: unexpected FIELD"),
		},
		{
			"missing opening square bracket",
			`"pod"]`,
			nil,
			fmt.Errorf("syntax error: unexpected STRING, expecting LSB or FIELD"),
		},
		{
			"missing closing square bracket",
			`["pod"`,
			nil,
			fmt.Errorf("syntax error: unexpected $end, expecting RSB"),
		},
		{
			"missing closing square bracket",
			`["pod""deployment"]`,
			nil,
			fmt.Errorf("syntax error: unexpected STRING, expecting RSB"),
		},
		{
			"invalid nesting",
			`pod..uuid`,
			nil,
			fmt.Errorf("syntax error: unexpected DOT, expecting FIELD"),
		},
		{
			"syntax error on key access",
			`["key`,
			nil,
			fmt.Errorf("syntax error: unexpected $end, expecting RSB"),
		},
		{
			"identifier with number",
			`utf8`,
			[]any{"utf8"},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := Parse(tt.expression, false)

			require.Equal(t, tt.want, parsed)

			if tt.error == nil {
				return
			}

			require.NotNil(t, err)
			require.Equal(t, tt.error.Error(), err.Error())
		})
	}
}
