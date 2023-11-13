package logql

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql/syntax"
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
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {

			expr, err := syntax.ParseExpr(test.query)
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
