package logql

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log"
	"github.com/opentracing/opentracing-go"
	"github.com/stretchr/testify/require"
	"github.com/uber/jaeger-client-go"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/stats"
)

func TestQueryType(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    string
		wantErr bool
	}{
		{"bad", "ddd", "", true},
		{"limited", `{app="foo"}`, QueryTypeLimited, false},
		{"limited multi label", `{app="foo" ,fuzz=~"foo"}`, QueryTypeLimited, false},
		{"filter", `{app="foo"} |= "foo"`, QueryTypeFilter, false},
		{"metrics", `rate({app="foo"} |= "foo"[5m])`, QueryTypeMetric, false},
		{"metrics binary", `rate({app="foo"} |= "foo"[5m]) + count_over_time({app="foo"} |= "foo"[5m]) / rate({app="foo"} |= "foo"[5m]) `, QueryTypeMetric, false},
		{"filters", `{app="foo"} |= "foo" |= "f" != "b"`, QueryTypeFilter, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := QueryType(tt.query)
			if (err != nil) != tt.wantErr {
				t.Errorf("QueryType() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("QueryType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLogSlowQuery(t *testing.T) {
	buf := bytes.NewBufferString("")
	util.Logger = log.NewLogfmtLogger(buf)
	tr, c := jaeger.NewTracer("foo", jaeger.NewConstSampler(true), jaeger.NewInMemoryReporter())
	defer c.Close()
	opentracing.SetGlobalTracer(tr)
	sp := opentracing.StartSpan("")
	ctx := opentracing.ContextWithSpan(user.InjectOrgID(context.Background(), "foo"), sp)
	now := time.Now()
	RecordMetrics(ctx, LiteralParams{
		qs:        `{foo="bar"} |= "buzz"`,
		direction: logproto.BACKWARD,
		end:       now,
		start:     now.Add(-1 * time.Hour),
		limit:     1000,
		step:      time.Minute,
	}, "200", stats.Result{
		Summary: stats.Summary{
			BytesProcessedPerSecond: 100000,
			ExecTime:                25.25,
			TotalBytesProcessed:     100000,
		},
	})
	require.Equal(t,
		fmt.Sprintf(
			"level=info org_id=foo traceID=%s latency=slow query=\"{foo=\\\"bar\\\"} |= \\\"buzz\\\"\" query_type=filter range_type=range length=1h0m0s step=1m0s duration=25.25s status=200 throughput_mb=0.1 total_bytes_mb=0.1\n",
			sp.Context().(jaeger.SpanContext).SpanID().String(),
		),
		buf.String())
	util.Logger = log.NewNopLogger()
}
