package logql

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/opentracing/opentracing-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber/jaeger-client-go"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/util/httpreq"
	util_log "github.com/grafana/loki/pkg/util/log"
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
		{"limited with parser", `{app="foo" ,fuzz=~"foo"} | logfmt`, QueryTypeLimited, false},
		{"filter", `{app="foo"} |= "foo"`, QueryTypeFilter, false},
		{"filter string extracted label", `{app="foo"} | json | foo="a"`, QueryTypeFilter, false},
		{"filter duration", `{app="foo"} | json | duration > 5s`, QueryTypeFilter, false},
		{"metrics", `rate({app="foo"} |= "foo"[5m])`, QueryTypeMetric, false},
		{"metrics binary", `rate({app="foo"} |= "foo"[5m]) + count_over_time({app="foo"} |= "foo"[5m]) / rate({app="foo"} |= "foo"[5m]) `, QueryTypeMetric, false},
		{"filters", `{app="foo"} |= "foo" |= "f" != "b"`, QueryTypeFilter, false},
		{"filters and labels filters", `{app="foo"} |= "foo" |= "f" != "b" | json | a > 5`, QueryTypeFilter, false},
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
	util_log.Logger = log.NewLogfmtLogger(buf)
	tr, c := jaeger.NewTracer("foo", jaeger.NewConstSampler(true), jaeger.NewInMemoryReporter())
	defer c.Close()
	opentracing.SetGlobalTracer(tr)
	sp := opentracing.StartSpan("")
	ctx := opentracing.ContextWithSpan(user.InjectOrgID(context.Background(), "foo"), sp)
	now := time.Now()

	ctx = context.WithValue(ctx, httpreq.QueryTagsHTTPHeader, "Source=logvolhist,Feature=Beta")

	RecordRangeAndInstantQueryMetrics(ctx, util_log.Logger, LiteralParams{
		qs:        `{foo="bar"} |= "buzz"`,
		direction: logproto.BACKWARD,
		end:       now,
		start:     now.Add(-1 * time.Hour),
		limit:     1000,
		step:      time.Minute,
	}, "200", stats.Result{
		Summary: stats.Summary{
			BytesProcessedPerSecond: 100000,
			QueueTime:               0.000000002,
			ExecTime:                25.25,
			TotalBytesProcessed:     100000,
			TotalEntriesReturned:    10,
		},
	}, logqlmodel.Streams{logproto.Stream{Entries: make([]logproto.Entry, 10)}})
	require.Regexp(t,
		regexp.MustCompile(fmt.Sprintf(
			`level=info org_id=foo traceID=%s latency=slow query=".*" query_hash=.* query_type=filter range_type=range length=1h0m0s .*\n`,
			sp.Context().(jaeger.SpanContext).SpanID().String(),
		)),
		buf.String())
	util_log.Logger = log.NewNopLogger()
}

func TestLogLabelsQuery(t *testing.T) {
	buf := bytes.NewBufferString("")
	logger := log.NewLogfmtLogger(buf)
	tr, c := jaeger.NewTracer("foo", jaeger.NewConstSampler(true), jaeger.NewInMemoryReporter())
	defer c.Close()
	opentracing.SetGlobalTracer(tr)
	sp := opentracing.StartSpan("")
	ctx := opentracing.ContextWithSpan(user.InjectOrgID(context.Background(), "foo"), sp)
	now := time.Now()
	RecordLabelQueryMetrics(ctx, logger, now.Add(-1*time.Hour), now, "foo", "", "200", stats.Result{
		Summary: stats.Summary{
			BytesProcessedPerSecond: 100000,
			ExecTime:                25.25,
			TotalBytesProcessed:     100000,
			TotalEntriesReturned:    12,
		},
	})
	require.Equal(t,
		fmt.Sprintf(
			"level=info org_id=foo traceID=%s latency=slow query_type=labels length=1h0m0s duration=25.25s status=200 label=foo query= splits=0 throughput=100kB total_bytes=100kB total_entries=12\n",
			sp.Context().(jaeger.SpanContext).SpanID().String(),
		),
		buf.String())
	util_log.Logger = log.NewNopLogger()
}

func TestLogSeriesQuery(t *testing.T) {
	buf := bytes.NewBufferString("")
	logger := log.NewLogfmtLogger(buf)
	tr, c := jaeger.NewTracer("foo", jaeger.NewConstSampler(true), jaeger.NewInMemoryReporter())
	defer c.Close()
	opentracing.SetGlobalTracer(tr)
	sp := opentracing.StartSpan("")
	ctx := opentracing.ContextWithSpan(user.InjectOrgID(context.Background(), "foo"), sp)
	now := time.Now()
	RecordSeriesQueryMetrics(ctx, logger, now.Add(-1*time.Hour), now, []string{`{container_name=~"prometheus.*", component="server"}`, `{app="loki"}`}, "200", stats.Result{
		Summary: stats.Summary{
			BytesProcessedPerSecond: 100000,
			ExecTime:                25.25,
			TotalBytesProcessed:     100000,
			TotalEntriesReturned:    10,
		},
	})
	require.Equal(t,
		fmt.Sprintf(
			"level=info org_id=foo traceID=%s latency=slow query_type=series length=1h0m0s duration=25.25s status=200 match=\"{container_name=~\\\"prometheus.*\\\", component=\\\"server\\\"}:{app=\\\"loki\\\"}\" splits=0 throughput=100kB total_bytes=100kB total_entries=10\n",
			sp.Context().(jaeger.SpanContext).SpanID().String(),
		),
		buf.String())
	util_log.Logger = log.NewNopLogger()
}

func Test_testToKeyValues(t *testing.T) {
	cases := []struct {
		name string
		in   string
		exp  []interface{}
	}{
		{
			name: "canonical-form",
			in:   "Source=logvolhist",
			exp: []interface{}{
				"source",
				"logvolhist",
			},
		},
		{
			name: "canonical-form-multiple-values",
			in:   "Source=logvolhist,Feature=beta",
			exp: []interface{}{
				"source",
				"logvolhist",
				"feature",
				"beta",
			},
		},
		{
			name: "empty",
			in:   "",
			exp:  []interface{}{},
		},
		{
			name: "non-canonical form",
			in:   "abc",
			exp:  []interface{}{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := tagsToKeyValues(c.in)
			assert.Equal(t, c.exp, got)
		})
	}
}

func TestQueryHashing(t *testing.T) {
	h1 := HashedQuery(`{app="myapp",env="myenv"} |= "error" |= "metrics.go" |= logfmt`)
	h2 := HashedQuery(`{app="myapp",env="myenv"} |= "error" |= logfmt |= "metrics.go"`)
	// check that it capture differences of order.
	require.NotEqual(t, h1, h2)
	h3 := HashedQuery(`{app="myapp",env="myenv"} |= "error" |= "metrics.go" |= logfmt`)
	// check that it evaluate same queries as same hashes, even if evaluated at different timestamps.
	require.Equal(t, h1, h3)
}
