package logql

import (
	"context"
	"testing"

	"github.com/grafana/loki/pkg/logql/log"
	"github.com/stretchr/testify/require"
)

func Test_optimizeSampleExpr(t *testing.T) {
	tests := []struct {
		in, expected string
	}{
		// noop
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

func Test_optimizeLogSelectorExpr(t *testing.T) {
	tests := []struct {
		expectedHintCounts    int
		name, logql, expected string
	}{
		// noop
		{
			1,
			"my slow json parser case ",

			`{log_type="service_metrics",module="api_server",operation="InvokeFunction",accountID="212068714932184585",serviceName="taojimu-fc-prod",functionName="feedflow"}  | json   | durationMs > 2003`,
			`{log_type="service_metrics", module="api_server", operation="InvokeFunction", accountID="212068714932184585", serviceName="taojimu-fc-prod", functionName="feedflow"} | json | durationMs>2003 | json`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.logql, func(t *testing.T) {
			expr, err := ParseExpr(tt.logql)
			require.NoError(t, err)

			switch e := expr.(type) {
			case LogSelectorExpr:
				got, err := optimizeLogSelectorExpr(context.Background(), e)
				require.NoError(t, err)
				require.Equal(t, tt.expected, got.String(), "append json fail")

				hintCounts := 0
				got.Walk(func(e interface{}) {
					switch expr := e.(type) {
					case *LabelParserExpr:
						switch expr.Op {
						case OpParserTypeJSON:
							stage, err := expr.Stage()
							require.NoError(t, err, "replace simple json fail")
							simpleJSONParser, ok := stage.(*log.JSONParser)
							if !ok {
								t.Fatal("replace simple json fail")
							}
							hintCounts += len(simpleJSONParser.RequiredJSONLabels)
						}
					}
				})
				require.Equal(t, tt.expectedHintCounts, hintCounts, "replace simple json fail")

			}

		})
	}
}
