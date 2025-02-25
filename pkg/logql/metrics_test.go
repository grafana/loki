package logql

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/opentracing/opentracing-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber/jaeger-client-go"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func TestQueryType(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"limited", `{app="foo"}`, QueryTypeLimited},
		{"limited multi label", `{app="foo" ,fuzz=~"foo"}`, QueryTypeLimited},
		{"limited with parser", `{app="foo" ,fuzz=~"foo"} | logfmt`, QueryTypeLimited},
		{"filter", `{app="foo"} |= "foo"`, QueryTypeFilter},
		{"filter string extracted label", `{app="foo"} | json | foo="a"`, QueryTypeFilter},
		{"filter duration", `{app="foo"} | json | duration > 5s`, QueryTypeFilter},
		{"metrics", `rate({app="foo"} |= "foo"[5m])`, QueryTypeMetric},
		{"metrics binary", `rate({app="foo"} |= "foo"[5m]) + count_over_time({app="foo"} |= "foo"[5m]) / rate({app="foo"} |= "foo"[5m]) `, QueryTypeMetric},
		{"filters", `{app="foo"} |= "foo" |= "f" != "b"`, QueryTypeFilter},
		{"filters and labels filters", `{app="foo"} |= "foo" |= "f" != "b" | json | a > 5`, QueryTypeFilter},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := QueryType(syntax.MustParseExpr(tt.query))
			require.NoError(t, err)
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
		queryString: `{foo="bar"} |= "buzz"`,
		direction:   logproto.BACKWARD,
		end:         now,
		start:       now.Add(-1 * time.Hour),
		limit:       1000,
		step:        time.Minute,
		queryExpr:   syntax.MustParseExpr(`{foo="bar"} |= "buzz"`),
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
			`level=info org_id=foo traceID=%s sampled=true latency=slow query=".*" query_hash=.* query_type=filter range_type=range length=1h0m0s .*\n`,
			sp.Context().(jaeger.SpanContext).SpanID().String(),
		)),
		buf.String())
	util_log.Logger = log.NewNopLogger()
}

func TestLogLabelsQuery(t *testing.T) {
	buf := bytes.NewBufferString("")
	tr, c := jaeger.NewTracer("foo", jaeger.NewConstSampler(true), jaeger.NewInMemoryReporter())
	logger := log.NewLogfmtLogger(buf)
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
		Caches: stats.Caches{
			LabelResult: stats.Cache{
				EntriesRequested:  2,
				EntriesFound:      1,
				EntriesStored:     1,
				DownloadTime:      80,
				QueryLengthServed: 10,
			},
		},
	})
	require.Regexp(t,
		fmt.Sprintf(
			"level=info org_id=foo traceID=%s sampled=true latency=slow query_type=labels splits=0 start=.* end=.* start_delta=1h0m0.* end_delta=.* length=1h0m0s duration=25.25s status=200 label=foo query= query_hash=2166136261 total_entries=12 cache_label_results_req=2 cache_label_results_hit=1 cache_label_results_stored=1 cache_label_results_download_time=80ns cache_label_results_query_length_served=10ns\n",
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
	RecordSeriesQueryMetrics(ctx, logger, now.Add(-1*time.Hour), now, []string{`{container_name=~"prometheus.*", component="server"}`, `{app="loki"}`}, "200", []string{}, stats.Result{
		Summary: stats.Summary{
			BytesProcessedPerSecond: 100000,
			ExecTime:                25.25,
			TotalBytesProcessed:     100000,
			TotalEntriesReturned:    10,
		},
		Caches: stats.Caches{
			SeriesResult: stats.Cache{
				EntriesRequested:  2,
				EntriesFound:      1,
				EntriesStored:     1,
				DownloadTime:      80,
				QueryLengthServed: 10,
			},
		},
	})
	require.Regexp(t,
		fmt.Sprintf(
			"level=info org_id=foo traceID=%s sampled=true latency=slow query_type=series splits=0 start=.* end=.* start_delta=1h0m0.* end_delta=.* length=1h0m0s duration=25.25s status=200 match=\"{container_name=.*\"}:{app=.*}\" query_hash=23523089 total_entries=10 cache_series_results_req=2 cache_series_results_hit=1 cache_series_results_stored=1 cache_series_results_download_time=80ns cache_series_results_query_length_served=10ns\n",
			sp.Context().(jaeger.SpanContext).SpanID().String(),
		),
		buf.String())
	util_log.Logger = log.NewNopLogger()
}

func TestQueryHashing(t *testing.T) {
	h1 := util.HashedQuery(`{app="myapp",env="myenv"} |= "error" |= "metrics.go" |= logfmt`)
	h2 := util.HashedQuery(`{app="myapp",env="myenv"} |= "error" |= logfmt |= "metrics.go"`)
	// check that it capture differences of order.
	require.NotEqual(t, h1, h2)
	h3 := util.HashedQuery(`{app="myapp",env="myenv"} |= "error" |= "metrics.go" |= logfmt`)
	// check that it evaluate same queries as same hashes, even if evaluated at different timestamps.
	require.Equal(t, h1, h3)
}

func TestHasMatchEqualLabelFilterBeforeParser(t *testing.T) {
	cases := []struct {
		query  string
		result bool
	}{
		{
			query:  `{env="prod"} |= "id"`,
			result: false,
		},
		{
			query:  `{env="prod"} |= "id" | level="debug"`,
			result: true,
		},
		{
			query:  `{env="prod"} |= "id" | logfmt | level="debug"`,
			result: false,
		},
		{
			query:  `{env="prod"} | level="debug" or level="info"`,
			result: true,
		},
		{
			query:  `{env="prod"} | level="debug" and level!="info"`,
			result: false,
		},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("%s => %v", c.query, c.result), func(t *testing.T) {
			p := LiteralParams{
				queryExpr: syntax.MustParseExpr(c.query),
			}
			assert.Equal(t, c.result, hasMatchEqualLabelFilterBeforeParser(p))
		})
	}
}
