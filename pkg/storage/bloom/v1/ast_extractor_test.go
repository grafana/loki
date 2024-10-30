package v1_test

import (
	"fmt"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
)

func TestExtractLabelMatchers(t *testing.T) {
	tt := []struct {
		name   string
		input  string
		series []labels.Labels
		expect []v1.LabelMatcher
	}{
		{
			name:  "basic label matcher",
			input: `{app="foo"} | key="value"`,
			expect: []v1.LabelMatcher{
				v1.PlainLabelMatcher{Key: "key", Value: "value"},
			},
		},

		{
			name:  "basic label matcher in series",
			input: `{app="foo"} | key="value"`,
			series: []labels.Labels{
				labels.FromStrings("app", "foo", "bar", "baz"),
				labels.FromStrings("app", "foo", "key", "other"),
			},
			expect: []v1.LabelMatcher{
				v1.UnsupportedLabelMatcher{},
			},
		},

		{
			name:  "basic label matcher not in series",
			input: `{app="foo"} | key="value"`,
			series: []labels.Labels{
				labels.FromStrings("app", "foo", "bar", "baz"),
				labels.FromStrings("app", "foo", "env", "prod"),
			},
			expect: []v1.LabelMatcher{
				v1.PlainLabelMatcher{Key: "key", Value: "value"},
			},
		},

		{
			name:  "or label matcher",
			input: `{app="foo"} | key1="value1" or key2="value2"`,
			expect: []v1.LabelMatcher{
				v1.OrLabelMatcher{
					Left:  v1.PlainLabelMatcher{Key: "key1", Value: "value1"},
					Right: v1.PlainLabelMatcher{Key: "key2", Value: "value2"},
				},
			},
		},

		{
			name:  "and label matcher",
			input: `{app="foo"} | key1="value1" and key2="value2"`,
			expect: []v1.LabelMatcher{
				v1.AndLabelMatcher{
					Left:  v1.PlainLabelMatcher{Key: "key1", Value: "value1"},
					Right: v1.PlainLabelMatcher{Key: "key2", Value: "value2"},
				},
			},
		},

		{
			name:  "multiple label matchers",
			input: `{app="foo"} | key1="value1" | key2="value2"`,
			expect: []v1.LabelMatcher{
				v1.PlainLabelMatcher{Key: "key1", Value: "value1"},
				v1.PlainLabelMatcher{Key: "key2", Value: "value2"},
			},
		},

		{
			name:  "unsupported label matchers",
			input: `{app="foo"} | key1=~"value1"`,
			expect: []v1.LabelMatcher{
				v1.UnsupportedLabelMatcher{},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := syntax.ParseExpr(tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.expect, v1.ExtractTestableLabelMatchers(expr, tc.series...))
		})
	}
}

func TestExtractLabelMatchers_IgnoreAfterParse(t *testing.T) {
	tt := []struct {
		name string
		expr string
	}{
		{"after json parser", `json`},
		{"after logfmt parser", `logfmt`},
		{"after pattern parser", `pattern "<msg>"`},
		{"after regexp parser", `regexp "(?P<message>.*)"`},
		{"after unpack parser", `unpack`},
		{"after label_format", `label_format foo="bar"`},
		{"after drop labels stage", `drop foo`},
		{"after keep labels stage", `keep foo`},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			fullInput := fmt.Sprintf(`{app="foo"} | key1="value1" | %s | key2="value2"`, tc.expr)
			expect := []v1.LabelMatcher{
				v1.PlainLabelMatcher{Key: "key1", Value: "value1"},
				// key2="value2" should be ignored following tc.expr
			}

			expr, err := syntax.ParseExpr(fullInput)
			require.NoError(t, err)

			require.Equal(t, expect, v1.ExtractTestableLabelMatchers(expr), "key2=value2 should be ignored with query %s", fullInput)
		})
	}
}
