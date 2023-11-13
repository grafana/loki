package syntax

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJSONSerializationRoundTrip(t *testing.T) {
	tests := map[string]struct {
		query string
	}{
		"simple matchers": {
			query: `{env="prod", app=~"loki.*"}`,
		},
		"simple aggregation": {
			query: `count_over_time({env="prod", app=~"loki.*"}[5m])`,
		},
		"simple aggregation with unwrap": {
			query: `sum_over_time({env="prod", app=~"loki.*"} | unwrap bytes[5m])`,
		},
		"bin op": {
			query: `(count_over_time({env="prod", app=~"loki.*"}[5m]) >= 0)`,
		},
		"label filter": {
			query: `{app="foo"} |= "bar" | json | ( latency>=250ms or ( status_code<500 , status_code>200 ) )`,
		},
		"regexp": {
			query: `{env="prod", app=~"loki.*"} |~ ".*foo.*"`,
		},
		"vector matching": {
			query: `(sum by (cluster)(rate({foo="bar"}[5m])) / ignoring (cluster)  count(rate({foo="bar"}[5m])))`,
		},
		"sum over or vector": {
			query: `(sum(count_over_time({foo="bar"}[5m])) or vector(1.000000))`,
		},
		"label replace": {
			query: `label_replace(vector(0), "foo", "bar", "", "")`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {

			expr, err := ParseExpr(test.query)
			require.NoError(t, err)

			var buf bytes.Buffer
			err = EncodeJSON(expr, &buf)
			require.NoError(t, err)

			t.Log(buf.String())

			actual, err := DecodeJSON(buf.String())
			require.NoError(t, err)

			require.Equal(t, test.query, actual.String())
		})
	}
}
func TestJSONSerializationParseTestCases(t *testing.T) {
	for _, tc := range ParseTestCases {
		if tc.err == nil {
			t.Run(tc.in, func(t *testing.T) {
				ast, err := ParseExpr(tc.in)
				require.NoError(t, err)

				var buf bytes.Buffer
				err = EncodeJSON(ast, &buf)
				require.NoError(t, err)
				actual, err := DecodeJSON(buf.String())
				require.NoError(t, err)

				require.Equal(t, tc.exp, actual)
			})
		}
	}
}
