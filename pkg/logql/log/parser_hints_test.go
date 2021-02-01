// uses log_test package to avoid circular dependency between log and logql package.
package log_test

import (
	"testing"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql"
)

var jsonLine = []byte(`{
	"remote_user": "foo",
	"upstream_addr": "10.0.0.1:80",
	"protocol": "HTTP/2.0",
	"request": {
		"time": "30.001",
		"method": "POST",
		"host": "foo.grafana.net",
		"uri": "/rpc/v2/stage",
		"size": "101"
	},
	"response": {
		"status": 204,
		"latency_seconds": "30.001"
	}
}`)

func Test_ParserHints(t *testing.T) {
	lbs := labels.Labels{{Name: "app", Value: "nginx"}}

	t.Parallel()
	for _, tt := range []struct {
		expr      string
		expectOk  bool
		expectVal float64
		expectLbs string
	}{
		{
			`rate({app="nginx"} | json | response_status = 204 [1m])`,
			true,
			1.0,
			`{app="nginx", protocol="HTTP/2.0", remote_user="foo", request_host="foo.grafana.net", request_method="POST", request_size="101", request_time="30.001", request_uri="/rpc/v2/stage", response_latency_seconds="30.001", response_status="204", upstream_addr="10.0.0.1:80"}`,
		},
		{
			`sum without (request_host,app) (rate({app="nginx"} | json | __error__="" | response_status = 204 [1m]))`,
			true,
			1.0,
			`{protocol="HTTP/2.0", remote_user="foo", request_method="POST", request_size="101", request_time="30.001", request_uri="/rpc/v2/stage", response_latency_seconds="30.001", response_status="204", upstream_addr="10.0.0.1:80"}`,
		},
		{
			`sum by (request_host,app) (rate({app="nginx"} | json | __error__="" | response_status = 204 [1m]))`,
			true,
			1.0,
			`{app="nginx", request_host="foo.grafana.net"}`,
		},
		{
			`sum(rate({app="nginx"} | json | __error__="" | response_status = 204 [1m]))`,
			true,
			1.0,
			`{}`,
		},
		{
			`sum(rate({app="nginx"} | json [1m]))`,
			true,
			1.0,
			`{}`,
		},
		{
			`sum(rate({app="nginx"} | json | unwrap response_latency_seconds [1m]))`,
			true,
			30.001,
			`{}`,
		},
		{
			`sum(rate({app="nginx"} | json | response_status = 204 | unwrap response_latency_seconds [1m]))`,
			true,
			30.001,
			`{}`,
		},
		{
			`sum by (request_host,app)(rate({app="nginx"} | json | response_status = 204 and  remote_user = "foo" | unwrap response_latency_seconds [1m]))`,
			true,
			30.001,
			`{app="nginx", request_host="foo.grafana.net"}`,
		},
		{
			`rate({app="nginx"} | json | response_status = 204 | unwrap response_latency_seconds [1m])`,
			true,
			30.001,
			`{app="nginx", protocol="HTTP/2.0", remote_user="foo", request_host="foo.grafana.net", request_method="POST", request_size="101", request_time="30.001", request_uri="/rpc/v2/stage", response_status="204", upstream_addr="10.0.0.1:80"}`,
		},
		{
			`sum without (request_host,app)(rate({app="nginx"} | json | response_status = 204 | unwrap response_latency_seconds [1m]))`,
			true,
			30.001,
			`{protocol="HTTP/2.0", remote_user="foo", request_method="POST", request_size="101", request_time="30.001", request_uri="/rpc/v2/stage", response_status="204", upstream_addr="10.0.0.1:80"}`,
		},
	} {
		tt := tt
		t.Run(tt.expr, func(t *testing.T) {
			t.Parallel()
			expr, err := logql.ParseSampleExpr(tt.expr)
			require.NoError(t, err)
			ex, err := expr.Extractor()
			require.NoError(t, err)
			v, lbsRes, ok := ex.ForStream(lbs).Process(jsonLine)
			var lbsResString string
			if lbsRes != nil {
				lbsResString = lbsRes.String()
			}
			require.Equal(t, tt.expectOk, ok)
			require.Equal(t, tt.expectVal, v)
			require.Equal(t, tt.expectLbs, lbsResString)
		})
	}
}
