package logql

import (
	"bytes"
	"fmt"
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
		"label filterer": {
			query: `bytes >= 0`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {

			expr, err := syntax.ParseExpr(test.query)
			require.NoError(t, err)

			var buf bytes.Buffer
			err = EncodeJSON(expr, &buf)
			require.NoError(t, err)

			actual, err := DecodeJSON(buf.String())
			require.NoError(t, err)

			fmt.Println(buf.String())

			require.Equal(t, test.query, actual.String())
		})
	}
}
