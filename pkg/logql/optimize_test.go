package logql

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_optimizeSampleExpr(t *testing.T) {

	tests := []struct {
		in, expected string
	}{
		//noop
		{`1`, `1`},
		{`1 + 1`, `2`},
		{`topk(10,sum by(name)(rate({region="us-east1"}[5m])))`, `topk(10,sum by(name)(rate({region="us-east1"}[5m])))`},
		{`sum by(name)(rate({region="us-east1"}[5m]))`, `sum by(name)(rate({region="us-east1"}[5m]))`},
		{`sum by(name)(bytes_over_time({region="us-east1"} | line_format "something else"[5m]))`, `sum by(name)(bytes_over_time({region="us-east1"} | line_format "something else"[5m]))`},
		{`sum by(name)(rate({region="us-east1"} | json | line_format "something else" |= "something"[5m]))`, `sum by(name)(rate({region="us-east1"} | json | line_format "something else" |= "something"[5m]))`},
		{`sum by(name)(rate({region="us-east1"} | json | line_format "something else" | logfmt[5m]))`, `sum by(name)(rate({region="us-east1"} | json | line_format "something else" | logfmt[5m]))`},

		// remove line_format that is not required.
		{`sum by(name)(rate({region="us-east1"} | line_format "something else"[5m]))`, `sum by(name)(rate({region="us-east1"}[5m]))`},
		{`sum by(name)(rate({region="us-east1"} | json | line_format "something else" | unwrap foo[5m]))`, `sum by(name)(rate({region="us-east1"} | json | unwrap foo[5m]))`},
		{`quantile_over_time(1,{region="us-east1"} | json | line_format "something else" | unwrap foo[5m])`, `quantile_over_time(1,{region="us-east1"} | json | unwrap foo[5m])`},
		{`sum by(name)(count_over_time({region="us-east1"} | json | line_format "something else" | label_format foo=bar | line_format "boo"[5m]))`, `sum by(name)(count_over_time({region="us-east1"} | json | label_format foo=bar[5m]))`},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			e, err := ParseSampleExpr(tt.in)
			require.NoError(t, err)
			got, err := optimizeSampleExpr(e)
			require.NoError(t, err)
			require.Equal(t, tt.expected, got.String())
		})
	}
}
