package plan

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestMarshalTo(t *testing.T) {
	plan := QueryPlan{
		AST: syntax.MustParseExpr(`sum by (foo) (bytes_over_time({app="loki"} [1m]))`),
	}

	data := make([]byte, plan.Size())
	_, err := plan.MarshalTo(data)
	require.NoError(t, err)

	var buf bytes.Buffer
	err = syntax.EncodeJSON(plan.AST, &buf)
	require.NoError(t, err)

	require.JSONEq(t, buf.String(), string(data))
}

func TestQueryPlanRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		// Log selectors
		{"simple matchers", `{app="loki"}`},
		{"multiple matchers", `{app="loki", env="prod"}`},
		{"regex matcher", `{app=~"loki.*"}`},
		{"negation matcher", `{env="prod", app!="loki"}`},

		// Log selectors with pipeline
		{"line filter", `{app="loki"} |= "error"`},
		{"multiple line filters", `{app="loki"} |= "error" |~ "timeout"`},
		{"json parser", `{app="loki"} | json`},
		{"logfmt parser", `{app="loki"} | logfmt`},
		{"label filter", `{app="loki"} | json | status >= 400`},
		{"line format", `{app="loki"} | line_format "{{.message}}"`},
		{"label format", `{app="loki"} | label_format foo=bar`},

		// Range aggregations
		{"count_over_time", `count_over_time({app="loki"}[5m])`},
		{"rate", `rate({app="loki"}[5m])`},
		{"bytes_over_time", `bytes_over_time({app="loki"}[5m])`},
		{"bytes_rate", `bytes_rate({app="loki"}[5m])`},
		{"sum_over_time with unwrap", `sum_over_time({app="loki"} | unwrap bytes[5m])`},
		{"quantile_over_time", `quantile_over_time(0.99, {app="loki"} | unwrap latency[5m])`},

		// Vector aggregations
		{"sum", `sum(rate({app="loki"}[5m]))`},
		{"sum by", `sum by (host) (rate({app="loki"}[5m]))`},
		{"sum without", `sum without (host) (rate({app="loki"}[5m]))`},
		{"avg", `avg(rate({app="loki"}[5m]))`},
		{"count", `count(rate({app="loki"}[5m]))`},
		{"max", `max(rate({app="loki"}[5m]))`},
		{"min", `min(rate({app="loki"}[5m]))`},
		{"topk", `topk(10, rate({app="loki"}[5m]))`},
		{"bottomk", `bottomk(10, rate({app="loki"}[5m]))`},

		// Binary operations
		{"bin op add", `sum(rate({app="loki"}[5m])) + sum(rate({app="loki"}[5m]))`},
		{"bin op div", `sum(rate({app="loki"}[5m])) / sum(rate({app="loki"}[5m]))`},
		{"bin op comparison", `sum(rate({app="loki"}[5m])) > 100`},
		{"bin op with vector matching", `sum by (host) (rate({app="loki"}[5m])) / ignoring (host) sum(rate({app="loki"}[5m]))`},

		// Special expressions
		{"vector", `vector(1)`},
		{"label_replace", `label_replace(rate({app="loki"}[5m]), "dst", "$1", "src", "(.*)")`},

		// Complex queries
		{"complex pipeline", `{app="loki"} |= "error" | json | status >= 400 | line_format "{{.message}}"`},
		{"nested aggregation", `sum(sum by (host) (rate({app="loki"}[5m])))`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			original := QueryPlan{
				AST: syntax.MustParseExpr(tc.query),
			}

			// Marshal
			data, err := original.MarshalJSON()
			require.NoError(t, err)

			// Unmarshal
			var restored QueryPlan
			err = restored.UnmarshalJSON(data)
			require.NoError(t, err)

			// Verify AST equality via string representation
			require.Equal(t, original.AST.String(), restored.AST.String())

			// Verify re-marshaling produces identical JSON
			redata, err := restored.MarshalJSON()
			require.NoError(t, err)
			require.JSONEq(t, string(data), string(redata))
		})
	}
}

func TestQueryPlanUnmarshalEmpty(t *testing.T) {
	// Empty input should be tolerated for backward compatibility
	var p QueryPlan
	err := p.UnmarshalJSON([]byte{})
	require.NoError(t, err)
	require.Nil(t, p.AST)

	// nil input should also work
	err = p.UnmarshalJSON(nil)
	require.NoError(t, err)
	require.Nil(t, p.AST)
}

func TestQueryPlanNilAST(t *testing.T) {
	p := QueryPlan{AST: nil}

	// Nil AST marshals to empty string
	data, err := p.MarshalJSON()
	require.NoError(t, err)
	require.Equal(t, "", string(data))
	require.Equal(t, 0, p.Size())

	// String is empty for nil AST
	require.Equal(t, "", p.String())

	// Hash is 0 for nil AST
	require.Equal(t, uint32(0), p.Hash())

	// MarshalTo writes empty string
	buf := make([]byte, p.Size())
	n, err := p.MarshalTo(buf)
	require.NoError(t, err)
	require.Equal(t, 0, n)
	require.Equal(t, "", string(buf))

	// Round-trip: nil AST -> "" -> nil AST
	var restored QueryPlan
	err = restored.UnmarshalJSON(data)
	require.NoError(t, err)
	require.Nil(t, restored.AST)
}
