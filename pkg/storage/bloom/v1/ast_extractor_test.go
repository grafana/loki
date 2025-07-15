package v1_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
)

func TestExtractLabelMatchers(t *testing.T) {
	tt := []struct {
		name   string
		input  string
		expect []v1.LabelMatcher
	}{
		{
			name:  "basic label matcher",
			input: `{app="foo"} | key="value"`,
			expect: []v1.LabelMatcher{
				v1.KeyValueMatcher{Key: "key", Value: "value"},
			},
		},

		{
			name:  "or label matcher",
			input: `{app="foo"} | key1="value1" or key2="value2"`,
			expect: []v1.LabelMatcher{
				v1.OrLabelMatcher{
					Left:  v1.KeyValueMatcher{Key: "key1", Value: "value1"},
					Right: v1.KeyValueMatcher{Key: "key2", Value: "value2"},
				},
			},
		},

		{
			name:  "and label matcher",
			input: `{app="foo"} | key1="value1" and key2="value2"`,
			expect: []v1.LabelMatcher{
				v1.AndLabelMatcher{
					Left:  v1.KeyValueMatcher{Key: "key1", Value: "value1"},
					Right: v1.KeyValueMatcher{Key: "key2", Value: "value2"},
				},
			},
		},

		{
			name:  "multiple label matchers",
			input: `{app="foo"} | key1="value1" | key2="value2"`,
			expect: []v1.LabelMatcher{
				v1.KeyValueMatcher{Key: "key1", Value: "value1"},
				v1.KeyValueMatcher{Key: "key2", Value: "value2"},
			},
		},

		{
			name:  "basic regex matcher",
			input: `{app="foo"} | key1=~"value1"`,
			expect: []v1.LabelMatcher{
				v1.KeyValueMatcher{Key: "key1", Value: "value1"},
			},
		},

		{
			name:  "regex matcher short", // simplifies to value[15].
			input: `{app="foo"} | key1=~"value1|value5"`,
			expect: []v1.LabelMatcher{
				v1.OrLabelMatcher{
					v1.KeyValueMatcher{Key: "key1", Value: "value1"},
					v1.KeyValueMatcher{Key: "key1", Value: "value5"},
				},
			},
		},

		{
			name:  "regex matcher range",
			input: `{app="foo"} | key1=~"value[0-9]"`,
			expect: []v1.LabelMatcher{
				buildOrMatchers(
					v1.KeyValueMatcher{Key: "key1", Value: "value0"},
					v1.KeyValueMatcher{Key: "key1", Value: "value1"},
					v1.KeyValueMatcher{Key: "key1", Value: "value2"},
					v1.KeyValueMatcher{Key: "key1", Value: "value3"},
					v1.KeyValueMatcher{Key: "key1", Value: "value4"},
					v1.KeyValueMatcher{Key: "key1", Value: "value5"},
					v1.KeyValueMatcher{Key: "key1", Value: "value6"},
					v1.KeyValueMatcher{Key: "key1", Value: "value7"},
					v1.KeyValueMatcher{Key: "key1", Value: "value8"},
					v1.KeyValueMatcher{Key: "key1", Value: "value9"},
				),
			},
		},

		{
			name:  "regex matcher ignore high cardinality",
			input: `{app="foo"} | key1=~"value[0-9][0-9][0-9]"`, // This would expand to 1000 matchers. Too many!
			expect: []v1.LabelMatcher{
				v1.UnsupportedLabelMatcher{},
			},
		},

		{
			name:  "regex matcher",
			input: `{app="foo"} | key1=~"value123|value456"`,
			expect: []v1.LabelMatcher{
				v1.OrLabelMatcher{
					v1.KeyValueMatcher{Key: "key1", Value: "value123"},
					v1.KeyValueMatcher{Key: "key1", Value: "value456"},
				},
			},
		},

		{
			name:  "regex multiple expands",
			input: `{app="foo"} | detected_level=~"debug|info|warn|error"`,
			expect: []v1.LabelMatcher{
				buildOrMatchers(
					v1.KeyValueMatcher{Key: "detected_level", Value: "debug"},
					v1.KeyValueMatcher{Key: "detected_level", Value: "info"},
					v1.KeyValueMatcher{Key: "detected_level", Value: "warn"},
					v1.KeyValueMatcher{Key: "detected_level", Value: "error"},
				),
			},
		},

		{
			name:  "regex matcher with ignored capture groups",
			input: `{app="foo"} | key1=~"value1|(value2)"`,
			expect: []v1.LabelMatcher{
				v1.OrLabelMatcher{
					v1.KeyValueMatcher{Key: "key1", Value: "value1"},
					v1.KeyValueMatcher{Key: "key1", Value: "value2"},
				},
			},
		},

		{
			name:  "advanced regex matcher",
			input: `{app="foo"} | key1=~"(warn|info[0-3])"`,
			expect: []v1.LabelMatcher{
				v1.OrLabelMatcher{
					v1.KeyValueMatcher{Key: "key1", Value: "warn"},
					buildOrMatchers(
						v1.KeyValueMatcher{Key: "key1", Value: "info0"},
						v1.KeyValueMatcher{Key: "key1", Value: "info1"},
						v1.KeyValueMatcher{Key: "key1", Value: "info2"},
						v1.KeyValueMatcher{Key: "key1", Value: "info3"},
					),
				},
			},
		},

		{
			name:  "regex .+ matcher",
			input: `{app="foo"} | key1=~".+"`,
			expect: []v1.LabelMatcher{
				v1.KeyMatcher{Key: "key1"},
			},
		},

		{
			// This should also be unsupported for suffix or substring regexes.
			name:  "regex .+ prefix matcher",
			input: `{app="foo"} | key1=~".+foo"`,
			expect: []v1.LabelMatcher{
				v1.UnsupportedLabelMatcher{},
			},
		},

		{
			name:  "regex .* matcher",
			input: `{app="foo"} | key1=~".*"`,
			expect: []v1.LabelMatcher{
				v1.UnsupportedLabelMatcher{},
			},
		},

		{
			name:  "unsupported label matchers",
			input: `{app="foo"} | key1!="value1"`,
			expect: []v1.LabelMatcher{
				v1.UnsupportedLabelMatcher{},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := syntax.ParseExpr(tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.expect, v1.ExtractTestableLabelMatchers(expr))
		})
	}
}

func buildOrMatchers(matchers ...v1.LabelMatcher) v1.LabelMatcher {
	if len(matchers) == 1 {
		return matchers[0]
	}

	left := matchers[0]

	for _, right := range matchers[1:] {
		left = v1.OrLabelMatcher{
			Left:  left,
			Right: right,
		}
	}

	return left
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
				v1.KeyValueMatcher{Key: "key1", Value: "value1"},
				// key2="value2" should be ignored following tc.expr
			}

			expr, err := syntax.ParseExpr(fullInput)
			require.NoError(t, err)

			require.Equal(t, expect, v1.ExtractTestableLabelMatchers(expr), "key2=value2 should be ignored with query %s", fullInput)
		})
	}
}
