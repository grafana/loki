// uses log_test package to avoid circular dependency between log and logql package.
package log_test

import (
	"testing"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql"
)

var (
	jsonLine = []byte(`{
	"remote_user": "foo",
	"upstream_addr": "10.0.0.1:80",
	"protocol": "HTTP/2.0",
    "cluster": "us-east-west",
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

	packedLine = []byte(`{
		"remote_user": "foo",
		"upstream_addr": "10.0.0.1:80",
		"protocol": "HTTP/2.0",
		"cluster": "us-east-west",
		"_entry":"foo"
	}`)

	logfmtLine = []byte(`ts=2021-02-02T14:35:05.983992774Z caller=spanlogger.go:79 org_id=3677 traceID=2e5c7234b8640997 Ingester.TotalReached=15 Ingester.TotalChunksMatched=0 Ingester.TotalBatches=0`)
)

func Test_ParserHints(t *testing.T) {
	lbs := labels.Labels{{Name: "app", Value: "nginx"}, {Name: "cluster", Value: "us-central-west"}}

	t.Parallel()
	for _, tt := range []struct {
		expr      string
		line      []byte
		expectOk  bool
		expectVal float64
		expectLbs string
	}{
		{
			`rate({app="nginx"} | json | response_status = 204 [1m])`,
			jsonLine,
			true,
			1.0,
			`{app="nginx", cluster="us-central-west", cluster_extracted="us-east-west", protocol="HTTP/2.0", remote_user="foo", request_host="foo.grafana.net", request_method="POST", request_size="101", request_time="30.001", request_uri="/rpc/v2/stage", response_latency_seconds="30.001", response_status="204", upstream_addr="10.0.0.1:80"}`,
		},
		{
			`sum without (request_host,app,cluster) (rate({app="nginx"} | json | __error__="" | response_status = 204 [1m]))`,
			jsonLine,
			true,
			1.0,
			`{cluster_extracted="us-east-west", protocol="HTTP/2.0", remote_user="foo", request_method="POST", request_size="101", request_time="30.001", request_uri="/rpc/v2/stage", response_latency_seconds="30.001", response_status="204", upstream_addr="10.0.0.1:80"}`,
		},
		{
			`sum by (request_host,app) (rate({app="nginx"} | json | __error__="" | response_status = 204 [1m]))`,
			jsonLine,
			true,
			1.0,
			`{app="nginx", request_host="foo.grafana.net"}`,
		},
		{
			`sum(rate({app="nginx"} | json | __error__="" | response_status = 204 [1m]))`,
			jsonLine,
			true,
			1.0,
			`{}`,
		},
		{
			`sum(rate({app="nginx"} | json [1m]))`,
			jsonLine,
			true,
			1.0,
			`{}`,
		},
		{
			`sum(rate({app="nginx"} | json | unwrap response_latency_seconds [1m]))`,
			jsonLine,
			true,
			30.001,
			`{}`,
		},
		{
			`sum(rate({app="nginx"} | json | response_status = 204 | unwrap response_latency_seconds [1m]))`,
			jsonLine,
			true,
			30.001,
			`{}`,
		},
		{
			`sum by (request_host,app)(rate({app="nginx"} | json | response_status = 204 and  remote_user = "foo" | unwrap response_latency_seconds [1m]))`,
			jsonLine,
			true,
			30.001,
			`{app="nginx", request_host="foo.grafana.net"}`,
		},
		{
			`rate({app="nginx"} | json | response_status = 204 | unwrap response_latency_seconds [1m])`,
			jsonLine,
			true,
			30.001,
			`{app="nginx", cluster="us-central-west", cluster_extracted="us-east-west", protocol="HTTP/2.0", remote_user="foo", request_host="foo.grafana.net", request_method="POST", request_size="101", request_time="30.001", request_uri="/rpc/v2/stage", response_status="204", upstream_addr="10.0.0.1:80"}`,
		},
		{
			`sum without (request_host,app,cluster)(rate({app="nginx"} | json | response_status = 204 | unwrap response_latency_seconds [1m]))`,
			jsonLine,
			true,
			30.001,
			`{cluster_extracted="us-east-west", protocol="HTTP/2.0", remote_user="foo", request_method="POST", request_size="101", request_time="30.001", request_uri="/rpc/v2/stage", response_status="204", upstream_addr="10.0.0.1:80"}`,
		},
		{
			`sum(rate({app="nginx"} | logfmt | org_id=3677 | unwrap Ingester_TotalReached[1m]))`,
			logfmtLine,
			true,
			15.0,
			`{}`,
		},
		{
			`sum by (org_id,app) (rate({app="nginx"} | logfmt | org_id=3677 | unwrap Ingester_TotalReached[1m]))`,
			logfmtLine,
			true,
			15.0,
			`{app="nginx", org_id="3677"}`,
		},
		{
			`rate({app="nginx"} | logfmt | org_id=3677 | unwrap Ingester_TotalReached[1m])`,
			logfmtLine,
			true,
			15.0,
			`{Ingester_TotalBatches="0", Ingester_TotalChunksMatched="0", app="nginx", caller="spanlogger.go:79", cluster="us-central-west", org_id="3677", traceID="2e5c7234b8640997", ts="2021-02-02T14:35:05.983992774Z"}`,
		},
		{
			`sum without (org_id,app,cluster)(rate({app="nginx"} | logfmt | org_id=3677 | unwrap Ingester_TotalReached[1m]))`,
			logfmtLine,
			true,
			15.0,
			`{Ingester_TotalBatches="0", Ingester_TotalChunksMatched="0", caller="spanlogger.go:79", traceID="2e5c7234b8640997", ts="2021-02-02T14:35:05.983992774Z"}`,
		},
		{
			`sum(rate({app="nginx"} | json | remote_user="foo" [1m]))`,
			jsonLine,
			true,
			1.0,
			`{}`,
		},
		{
			`sum(rate({app="nginx"} | json | nonexistant_field="foo" [1m]))`,
			jsonLine,
			false,
			0,
			``,
		},
		{
			`absent_over_time({app="nginx"} | json [1m])`,
			jsonLine,
			true,
			1.0,
			`{}`,
		},
		{
			`absent_over_time({app="nginx"} | json | nonexistant_field="foo" [1m])`,
			jsonLine,
			false,
			0,
			``,
		},
		{
			`absent_over_time({app="nginx"} | json | remote_user="foo" [1m])`,
			jsonLine,
			true,
			1.0,
			`{}`,
		},
		{
			`sum by (cluster_extracted)(count_over_time({app="nginx"} | json | cluster_extracted="us-east-west" [1m]))`,
			jsonLine,
			true,
			1.0,
			`{cluster_extracted="us-east-west"}`,
		},
		{
			`sum by (cluster_extracted)(count_over_time({app="nginx"} | unpack | cluster_extracted="us-east-west" [1m]))`,
			packedLine,
			true,
			1.0,
			`{cluster_extracted="us-east-west"}`,
		},
		{
			`sum(rate({app="nginx"} | unpack | nonexistant_field="foo" [1m]))`,
			packedLine,
			false,
			0,
			``,
		},
	} {
		tt := tt
		t.Run(tt.expr, func(t *testing.T) {
			t.Parallel()
			expr, err := logql.ParseSampleExpr(tt.expr)
			require.NoError(t, err)

			ex, err := expr.Extractor()
			require.NoError(t, err)
			v, lbsRes, ok := ex.ForStream(lbs).Process(append([]byte{}, tt.line...))
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
