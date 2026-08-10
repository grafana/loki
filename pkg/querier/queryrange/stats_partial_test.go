package queryrange

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/server"
)

// wrapErr mirrors how pkg/querier/queryrange/limits.go attaches a sentinel to
// the httpgrpc limit error.
func wrapErr(sentinel, err error) error {
	return fmt.Errorf("%w: %w", sentinel, err)
}

// failedQueryUsageFields is the complete field set of the failed-query usage
// line; the regular stats line's fields must stay off it.
var failedQueryUsageFields = []string{
	"msg", "query", "query_hash", "query_type", "range_type", "length",
	"status", "duration", "total_bytes", "failure_category", "failure_reason",
}

// failedQueryUsageCapture collects the failed-query usage lines as key/value
// pairs, so tests can assert the exact field set rather than a formatting of it.
type failedQueryUsageCapture struct {
	mu    sync.Mutex
	lines []map[string]string
}

func (c *failedQueryUsageCapture) Log(keyvals ...interface{}) error {
	line := make(map[string]string, len(keyvals)/2)
	for i := 0; i+1 < len(keyvals); i += 2 {
		line[fmt.Sprint(keyvals[i])] = fmt.Sprint(keyvals[i+1])
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, line)
	return nil
}

func (c *failedQueryUsageCapture) all() []map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]map[string]string(nil), c.lines...)
}

func (c *failedQueryUsageCapture) only(t *testing.T) map[string]string {
	t.Helper()
	lines := c.all()
	require.Len(t, lines, 1)
	return lines[0]
}

// captureFailedQueryUsage redirects the failed-query usage logger for the
// duration of the test. Callers must not use t.Parallel: the swapped package
// var is read unsynchronized on the production path.
func captureFailedQueryUsage(t *testing.T) *failedQueryUsageCapture {
	t.Helper()

	c := &failedQueryUsageCapture{}
	original := failedQueryUsageLogger
	failedQueryUsageLogger = func(ctx context.Context) log.Logger {
		return util_log.WithContext(ctx, c)
	}
	t.Cleanup(func() { failedQueryUsageLogger = original })
	return c
}

// requireFailedQueryUsageShape asserts the line carries exactly the agreed
// fields: nothing leaked in and nothing went missing.
func requireFailedQueryUsageShape(t *testing.T, line map[string]string) {
	t.Helper()

	fields := make([]string, 0, len(line))
	for k := range line {
		// Added by the logging wrappers, not by the line itself.
		if k == "level" || k == "org_id" {
			continue
		}
		fields = append(fields, k)
	}
	require.ElementsMatch(t, failedQueryUsageFields, fields)

	// Spelled out as well: these were deliberately left off the line.
	for _, absent := range []string{
		"total_lines", "total_bytes_structured_metadata", "partial",
		"throughput", "lines_per_second", "returned_lines",
	} {
		require.NotContains(t, line, absent)
	}

	require.Equal(t, "failed query usage", line["msg"])
	_, err := time.ParseDuration(line["duration"])
	require.NoError(t, err)
}

func failedQueryUsageCount(t *testing.T, category string) float64 {
	t.Helper()
	return testutil.ToFloat64(failedQueryUsageRecordedTotal.WithLabelValues(category))
}

// TestStatsCollectorMiddleware_LogsFailedQueryUsage simulates a sub-query that
// completes before the overall query fails. The middleware must emit the
// failed-query usage line, record no regular stats line, and still propagate the
// original error.
func TestStatsCollectorMiddleware_LogsFailedQueryUsage(t *testing.T) {
	// A byte-limit failure sentinel-wrapped like queryrange/limits.go builds it.
	limitErr := wrapErr(logqlmodel.ErrMaxQueryBytesRead,
		httpgrpc.Errorf(http.StatusBadRequest, "the query would read too many bytes (query: 5GB, limit: 1GB)"))

	for _, tc := range []struct {
		name          string
		query         string
		partialBytes  int64
		err           error
		wantQueryType string
		wantStatus    string
		wantBytes     string
		wantCategory  string
		wantReason    string
	}{
		{
			name:  "log query rejected by a byte limit",
			query: `{app="foo"} |= "bar"`,
			// The usage of the sub-query that completed before the failure is
			// what the line exists to report.
			partialBytes:  4096,
			err:           limitErr,
			wantQueryType: logql.QueryTypeFilter,
			wantStatus:    "400",
			wantBytes:     "4.1kB",
			wantCategory:  server.FailureLimit,
			wantReason:    "max_query_bytes_read",
		},
		{
			// The query type is derived from the request expression, there being
			// no response to switch on.
			name:          "metric query canceled by the client",
			query:         `sum(rate({app="foo"}[5m]))`,
			err:           context.Canceled,
			wantQueryType: logql.QueryTypeMetric,
			wantStatus:    fmt.Sprint(server.StatusClientClosedRequest),
			// No sub-query completed, so the line reports zero usage.
			wantBytes:    "0B",
			wantCategory: server.FailureCanceled,
			wantReason:   "client_canceled",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			data := &queryData{}
			ctx := user.InjectOrgID(context.WithValue(context.Background(), ctxKey, data), "fake")

			lines := captureFailedQueryUsage(t)
			countBefore := failedQueryUsageCount(t, tc.wantCategory)

			_, err := StatsCollectorMiddleware().Wrap(queryrangebase.HandlerFunc(func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				if tc.partialBytes > 0 {
					// Simulate a shard/interval sub-query that succeeded before
					// the failure.
					stats.JoinPartial(ctx, stats.Result{
						Querier: stats.Querier{
							Store: stats.Store{
								Chunk: stats.Chunk{DecompressedBytes: tc.partialBytes, DecompressedLines: 20},
							},
						},
					})
				}
				return nil, tc.err
			})).Do(ctx, &LokiRequest{Query: tc.query, StartTs: now, EndTs: now.Add(time.Hour)})

			require.ErrorIs(t, err, tc.err) // original error is still returned

			line := lines.only(t)
			requireFailedQueryUsageShape(t, line)
			require.Equal(t, tc.query, line["query"])
			require.Equal(t, fmt.Sprint(util.HashedQuery(tc.query)), line["query_hash"])
			require.Equal(t, tc.wantQueryType, line["query_type"])
			require.Equal(t, string(logql.RangeType), line["range_type"])
			require.Equal(t, time.Hour.String(), line["length"])
			// The status is the one the client is served.
			require.Equal(t, tc.wantStatus, line["status"])
			require.Equal(t, tc.wantBytes, line["total_bytes"])
			require.Equal(t, tc.wantCategory, line["failure_category"])
			require.Equal(t, tc.wantReason, line["failure_reason"])
			require.Equal(t, "fake", line["org_id"])

			// Exactly one increment per emitted line.
			require.Equal(t, countBefore+1, failedQueryUsageCount(t, tc.wantCategory))

			// A failed query gets this line and nothing else: no regular stats line.
			require.False(t, data.recorded)
			require.Nil(t, data.statistics)
		})
	}
}

// TestStatsCollectorMiddleware_SuccessEmitsNoFailedQueryLine pins the other
// side: a successful query records the regular stats line and no failed-query one.
func TestStatsCollectorMiddleware_SuccessEmitsNoFailedQueryLine(t *testing.T) {
	data := &queryData{}
	ctx := context.WithValue(context.Background(), ctxKey, data)

	lines := captureFailedQueryUsage(t)
	countBefore := failedQueryUsageCount(t, server.FailureLimit)

	now := time.Now()
	_, err := StatsCollectorMiddleware().Wrap(queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return &LokiResponse{
			Status:  loghttp.QueryStatusSuccess,
			Version: uint32(loghttp.VersionV1),
			Statistics: stats.Result{
				Querier: stats.Querier{Store: stats.Store{Chunk: stats.Chunk{DecompressedBytes: 4096}}},
			},
			Data: LokiData{ResultType: loghttp.ResultTypeStream},
		}, nil
	})).Do(ctx, &LokiRequest{Query: `{app="foo"}`, StartTs: now, EndTs: now.Add(time.Hour)})

	require.NoError(t, err)
	require.Empty(t, lines.all())
	require.Equal(t, countBefore, failedQueryUsageCount(t, server.FailureLimit))

	// The regular stats line is recorded, unchanged.
	require.True(t, data.recorded)
	require.Equal(t, queryTypeLog, data.queryType)
	require.Equal(t, int64(4096), data.statistics.Summary.TotalBytesProcessed)
}

// TestSplitByInterval_PartialStatsOnError drives the real split middleware with
// one interval failing after an earlier one succeeded, and asserts the
// successful interval's usage reaches the partial-stats collector.
func TestSplitByInterval_PartialStatsOnError(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "1")
	partial, ctx := stats.NewPartialContext(ctx)

	var calls atomic.Int64
	next := queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
		// Second interval fails; the first succeeds with some usage.
		if calls.Add(1) == 2 {
			return nil, context.DeadlineExceeded
		}
		return &LokiResponse{
			Status:    loghttp.QueryStatusSuccess,
			Direction: r.(*LokiRequest).Direction,
			Limit:     r.(*LokiRequest).Limit,
			Version:   uint32(loghttp.VersionV1),
			Statistics: stats.Result{
				Querier: stats.Querier{Store: stats.Store{Chunk: stats.Chunk{DecompressedBytes: 1000}}},
			},
			Data: LokiData{ResultType: loghttp.ResultTypeStream},
		}, nil
	})

	l := WithSplitByLimits(fakeLimits{maxQueryParallelism: 1}, time.Hour)
	split := SplitByIntervalMiddleware(testSchemas, l, DefaultCodec, newDefaultSplitter(fakeLimits{}, nil), nilMetrics).Wrap(next)

	_, err := split.Do(ctx, &LokiRequest{
		StartTs:   time.Unix(0, 0),
		EndTs:     time.Unix(0, (2 * time.Hour).Nanoseconds()),
		Query:     `{app="foo"}`,
		Limit:     1000,
		Step:      1,
		Direction: logproto.FORWARD,
		Path:      "/loki/api/v1/query_range",
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	// The interval that completed before the failure contributed its usage.
	require.Equal(t, int64(1000), partial.Result().Querier.Store.Chunk.DecompressedBytes)
}

func TestStatisticsFromResponse(t *testing.T) {
	s := stats.Result{Ingester: stats.Ingester{TotalReached: 7}}

	for _, tc := range []struct {
		name   string
		resp   queryrangebase.Response
		wantOK bool
	}{
		{"log", &LokiResponse{Statistics: s}, true},
		{"metric", &LokiPromResponse{Statistics: s}, true},
		{"index_stats", &IndexStatsResponse{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := statisticsFromResponse(tc.resp)
			require.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				require.Equal(t, int32(7), got.Ingester.TotalReached)
			}
		})
	}
}

// TestStatsCollectorMiddleware_ConcurrentFanOutPartialStats fails one interval
// while its siblings are still executing and writing into the stats context.
// Emitting the line must report the completed intervals' usage without racing
// with the siblings still in flight (run this with -race).
func TestStatsCollectorMiddleware_ConcurrentFanOutPartialStats(t *testing.T) {
	const (
		intervals       = 8
		parallelism     = 4
		bytesPerSubQuer = 1000
		// The interval that fails: the first four run concurrently, so failing
		// the fifth guarantees the first four have been collected while the
		// remaining ones are still in flight.
		failAfterInterval = 4
	)

	data := &queryData{}
	ctx := user.InjectOrgID(context.WithValue(context.Background(), ctxKey, data), "1")

	lines := captureFailedQueryUsage(t)
	countBefore := failedQueryUsageCount(t, server.FailureLimit)

	limitErr := wrapErr(logqlmodel.ErrQuerierTooManyBytes, httpgrpc.Errorf(http.StatusBadRequest, "query too large to execute on a single querier: (query: 5GB, limit: 1GB)"))

	next := queryrangebase.HandlerFunc(func(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
		interval := int(r.GetStart().Sub(time.Unix(0, 0)) / time.Hour)
		if interval == failAfterInterval {
			return nil, limitErr
		}

		// Keep writing into the shared stats context while the failure path emits
		// the accumulated usage.
		sctx := stats.FromContext(ctx)
		for i := 0; i < 500; i++ {
			sctx.AddCacheEntriesFound(stats.ChunkCache, 1)
			stats.JoinResults(ctx, stats.Result{
				Querier: stats.Querier{Store: stats.Store{Chunk: stats.Chunk{HeadChunkBytes: 1}}},
			})
		}

		return &LokiResponse{
			Status:    loghttp.QueryStatusSuccess,
			Direction: r.(*LokiRequest).Direction,
			Limit:     r.(*LokiRequest).Limit,
			Version:   uint32(loghttp.VersionV1),
			Statistics: stats.Result{
				Querier: stats.Querier{Store: stats.Store{Chunk: stats.Chunk{DecompressedBytes: bytesPerSubQuer}}},
			},
			Data: LokiData{ResultType: loghttp.ResultTypeStream},
		}, nil
	})

	l := WithSplitByLimits(fakeLimits{maxQueryParallelism: parallelism}, time.Hour)
	handler := queryrangebase.MergeMiddlewares(
		StatsCollectorMiddleware(),
		SplitByIntervalMiddleware(testSchemas, l, DefaultCodec, newDefaultSplitter(fakeLimits{}, nil), nilMetrics),
	).Wrap(next)

	_, err := handler.Do(ctx, &LokiRequest{
		StartTs:   time.Unix(0, 0),
		EndTs:     time.Unix(0, (intervals * time.Hour).Nanoseconds()),
		Query:     `{app="foo"}`,
		Limit:     1000,
		Step:      1,
		Direction: logproto.FORWARD,
		Path:      "/loki/api/v1/query_range",
	})

	require.ErrorIs(t, err, logqlmodel.ErrQuerierTooManyBytes)

	line := lines.only(t)
	requireFailedQueryUsageShape(t, line)
	require.Equal(t, server.FailureLimit, line["failure_category"])
	require.Equal(t, "querier_too_large", line["failure_reason"])
	// The intervals collected before the failure contributed their usage.
	require.Equal(t, util.HumanizeBytes(failAfterInterval*bytesPerSubQuer), line["total_bytes"])
	require.Equal(t, countBefore+1, failedQueryUsageCount(t, server.FailureLimit))
	require.False(t, data.recorded)
}

// TestQueryTypeFromRequest covers the request types the failure path has to
// attribute without a response to switch on: only the two that report usage
// bytes get a type, everything else must fall outside the positive list.
func TestQueryTypeFromRequest(t *testing.T) {
	for _, tc := range []struct {
		name       string
		req        queryrangebase.Request
		expected   string
		usageBytes bool
	}{
		{"log", &LokiRequest{Query: `{app="foo"}`}, queryTypeLog, true},
		{"metric", &LokiRequest{Query: `sum(rate({app="foo"}[5m]))`}, queryTypeMetric, true},
		{"instant_log", &LokiInstantRequest{Query: `{app="foo"}`}, queryTypeLog, true},
		{"instant_metric", &LokiInstantRequest{Query: `sum(rate({app="foo"}[5m]))`}, queryTypeMetric, true},
		// None of these carry usage bytes, so they must not be attributed at all.
		{"series", &LokiSeriesRequest{Match: []string{`{app="foo"}`}}, "", false},
		{"label", &LabelRequest{}, "", false},
		{"index_stats", &logproto.IndexStatsRequest{}, "", false},
		{"volume", &logproto.VolumeRequest{}, "", false},
		{"detected_fields", &DetectedFieldsRequest{}, "", false},
		{"detected_labels", &DetectedLabelsRequest{}, "", false},
		{"shards", &logproto.ShardsRequest{}, "", false},
		{"patterns", &logproto.QueryPatternsRequest{Query: `{app="foo"}`}, "", false},
		// A request type this function does not know about, e.g. one added
		// later, must not be mis-attributed as a log query.
		{"unknown", &queryrangebase.PrometheusRequest{Query: `{app="foo"}`}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queryType := queryTypeFromRequest(tc.req)
			require.Equal(t, tc.expected, queryType)
			require.Equal(t, tc.usageBytes, recordsQueryUsageBytes(queryType))
		})
	}
}

// TestLoggedQueryType pins that the query_type field speaks the same taxonomy as
// the regular stats line, and degrades to an empty value on an unparseable query.
func TestLoggedQueryType(t *testing.T) {
	for _, tc := range []struct {
		query     string
		queryTags string
		expected  string
	}{
		{`{app="foo"}`, "", logql.QueryTypeLimited},
		{`{app="foo"} |= "bar"`, "", logql.QueryTypeFilter},
		{`sum(rate({app="foo"}[5m]))`, "", logql.QueryTypeMetric},
		{`not a query`, "", ""},
		// Datasample queries are remapped to "limited", as on the regular line.
		{`{app="foo"} |= "bar"`, "source=datasample", logql.QueryTypeLimited},
	} {
		t.Run(tc.query+"/"+tc.queryTags, func(t *testing.T) {
			ctx := context.Background()
			if tc.queryTags != "" {
				ctx = context.WithValue(ctx, httpreq.QueryTagsHTTPHeader, tc.queryTags)
			}
			require.Equal(t, tc.expected, loggedQueryType(ctx, &LokiRequest{Query: tc.query}))
		})
	}
}

// TestStatsCollectorMiddleware_SkipsQueryTypesWithoutUsageBytes makes sure a
// failed query whose type carries no usage bytes gets no failed-query usage
// line, even if usage somehow reached the partial collector.
func TestStatsCollectorMiddleware_SkipsQueryTypesWithoutUsageBytes(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  queryrangebase.Request
	}{
		{"volume", &logproto.VolumeRequest{
			From:     model.Time(0),
			Through:  model.Time(time.Hour.Milliseconds()),
			Matchers: `{app="foo"}`,
			Limit:    100,
		}},
		{"series", &LokiSeriesRequest{
			StartTs: time.Unix(0, 0),
			EndTs:   time.Unix(0, time.Hour.Nanoseconds()),
			Match:   []string{`{app="foo"}`},
			Path:    "/loki/api/v1/series",
		}},
		{"index_stats", &logproto.IndexStatsRequest{
			From:     model.Time(0),
			Through:  model.Time(time.Hour.Milliseconds()),
			Matchers: `{app="foo"}`,
		}},
		{"patterns", &logproto.QueryPatternsRequest{
			Query: `{app="foo"}`,
			Start: time.Unix(0, 0),
			End:   time.Unix(0, time.Hour.Nanoseconds()),
			Step:  time.Minute.Milliseconds(),
		}},
		// A request type the failure path does not recognise gets no line either.
		{"unknown_request_type", &queryrangebase.PrometheusRequest{
			Query: `{app="foo"}`,
			Start: time.Unix(0, 0),
			End:   time.Unix(0, time.Hour.Nanoseconds()),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &queryData{}
			ctx := context.WithValue(context.Background(), ctxKey, data)

			lines := captureFailedQueryUsage(t)
			countBefore := failedQueryUsageCount(t, server.FailureTimeout)

			_, err := StatsCollectorMiddleware().Wrap(queryrangebase.HandlerFunc(func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				stats.JoinPartial(ctx, stats.Result{
					Querier: stats.Querier{Store: stats.Store{Chunk: stats.Chunk{DecompressedBytes: 2048}}},
				})
				return nil, context.DeadlineExceeded
			})).Do(ctx, tc.req)

			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Empty(t, lines.all())
			require.Equal(t, countBefore, failedQueryUsageCount(t, server.FailureTimeout))
			require.False(t, data.recorded)
			require.Nil(t, data.statistics)
		})
	}
}
