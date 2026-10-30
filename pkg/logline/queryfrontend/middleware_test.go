package queryfrontend

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v4/pkg/loghttp"
	logline_query_limits "github.com/grafana/loki/v4/pkg/logline/queryfrontend/limits"
	"github.com/grafana/loki/v4/pkg/logproto"
	"github.com/grafana/loki/v4/pkg/logql/syntax"
	"github.com/grafana/loki/v4/pkg/querier/queryrange"
	"github.com/grafana/loki/v4/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v4/pkg/util/httpreq"

	"github.com/grafana/loki/v4/pkg/logline/hintprovider"
)

// ---------------------------------------------------------------------------
// Test mocks
// ---------------------------------------------------------------------------

type mockHintProvider struct {
	mu           sync.RWMutex
	hints        *hintprovider.Hints
	err          error
	calls        int
	from         model.Time
	through      model.Time
	sawSkipCache bool
	sawCancel    bool
	delay        time.Duration
	release      chan struct{} // when set, ProvideHints blocks until it is closed
	stats        *hintprovider.QueryStats
	minDate      time.Time
}

// mockLimits is the limits_config view the middleware reads. A tenant without
// an entry gets the defaults, like a tenant without runtime overrides.
type mockLimits struct {
	modes                 map[string]Mode
	minQueryBytesForIndex map[string]int64
	defaultMinQueryBytes  int64
}

func (m mockLimits) LoglineQueryMode(tenant string) string {
	switch m.modes[tenant] {
	case ModeOff:
		return "off"
	case ModeDryRun:
		return "dry_run"
	case ModeLive:
		return "live"
	default:
		return ""
	}
}

func (m mockLimits) LoglineQueryMinQueryBytesForIndex(tenant string) int64 {
	if v, ok := m.minQueryBytesForIndex[tenant]; ok {
		return v
	}
	return m.defaultMinQueryBytes
}

func (m *mockHintProvider) ProvideHints(
	ctx context.Context,
	_ string,
	_ syntax.Expr,
	from, through model.Time,
) (*hintprovider.Hints, *hintprovider.QueryStats, error) {
	stats := hintprovider.NewQueryStats()

	m.mu.Lock()
	m.calls++
	m.from = from
	m.through = through
	hints := m.hints
	err := m.err
	m.sawSkipCache = hintprovider.SkipCache(ctx)
	delay := m.delay
	release := m.release
	if m.stats != nil {
		stats = m.stats
	}
	m.mu.Unlock()

	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			m.mu.Lock()
			m.sawCancel = true
			m.mu.Unlock()
			return nil, stats, ctx.Err()
		}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			m.mu.Lock()
			m.sawCancel = true
			m.mu.Unlock()
			return nil, stats, ctx.Err()
		}
	}
	return hints, stats, err
}

func waitingSubRequests(r *hintPrefetchResult) int {
	r.wait.mu.Lock()
	defer r.wait.mu.Unlock()
	return r.wait.waiters
}

// releaseWhenWaiting closes release once n sub-requests wait for the lookup,
// or after a deadline so a broken tracker fails the test instead of hanging it.
func releaseWhenWaiting(r *hintPrefetchResult, n int, release chan struct{}) {
	deadline := time.Now().Add(5 * time.Second)
	for waitingSubRequests(r) < n && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(release)
}

func (m *mockHintProvider) Calls() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.calls
}

func (m *mockHintProvider) Window() (model.Time, model.Time) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.from, m.through
}

func (m *mockHintProvider) SawSkipCache() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sawSkipCache
}

func (m *mockHintProvider) SawCancel() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sawCancel
}

func (m *mockHintProvider) MinDate() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.minDate
}

func newTestMetrics() *Metrics {
	return NewMetrics(prometheus.NewRegistry())
}

func hintWaitObservations(t *testing.T, metrics *Metrics, status string) (count uint64, sum float64) {
	t.Helper()
	var m dto.Metric
	require.NoError(t, metrics.hintWaitDuration.WithLabelValues(status).(prometheus.Histogram).Write(&m))
	return m.GetHistogram().GetSampleCount(), m.GetHistogram().GetSampleSum()
}

func newTestLokiRequest(query string, from, through time.Time) *queryrange.LokiRequest {
	return &queryrange.LokiRequest{
		Query:   query,
		StartTs: from,
		EndTs:   through,
	}
}

func newTestLokiInstantRequest(query string, ts time.Time) *queryrange.LokiInstantRequest {
	return &queryrange.LokiInstantRequest{
		Query:  query,
		TimeTs: ts,
		Path:   "/loki/api/v1/query",
	}
}

func emptyStreamResponse() *queryrange.LokiResponse {
	return &queryrange.LokiResponse{
		Status:    "success",
		Direction: logproto.BACKWARD,
		Data:      queryrange.LokiData{ResultType: "streams", Result: []logproto.Stream{}},
	}
}

func streamResponseWithEntries(entries ...logproto.Entry) *queryrange.LokiResponse {
	return &queryrange.LokiResponse{
		Status:    "success",
		Direction: logproto.BACKWARD,
		Data: queryrange.LokiData{
			ResultType: "streams",
			Result: []logproto.Stream{
				{
					Labels:  `{job="test"}`,
					Entries: entries,
				},
			},
		},
	}
}

// prefetchMiddlewareForTest injects a QueryHintProvider directly. Production
// NewLoglinePrefetchMiddleware wraps the concrete provider with the remote
// adapter and the hint cache, which these tests replace with a mock.
func prefetchMiddlewareForTest(
	hp hintprovider.QueryHintProvider,
	cfg Config,
	limits logline_query_limits.Limits,
	metrics *Metrics,
	logger log.Logger,
) queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return newLoglinePrefetchHandler(next, hp, cfg, limits, metrics, logger)
	})
}

// buildStack wires prefetch + filter middleware the same way integration.go does,
// except the "Loki tripperware" is replaced by a transparent pass-through.
// This lets us test the two-layer interaction in isolation.
func buildStack(
	hp *mockHintProvider,
	cfg Config,
	metrics *Metrics,
	querier queryrangebase.Handler,
	limits ...logline_query_limits.Limits,
) queryrangebase.Handler {
	filterMW := NewLoglineFilterMiddleware(10*time.Second, metrics, nil)
	var settings logline_query_limits.Limits = mockLimits{}
	if len(limits) > 0 {
		settings = limits[0]
	}
	prefetchMW := prefetchMiddlewareForTest(hp, cfg, settings, metrics, nil)
	return prefetchMW.Wrap(filterMW.Wrap(querier))
}

func buildStackWithLogger(
	hp *mockHintProvider,
	cfg Config,
	metrics *Metrics,
	logger log.Logger,
	querier queryrangebase.Handler,
	limits ...logline_query_limits.Limits,
) queryrangebase.Handler {
	filterMW := NewLoglineFilterMiddleware(10*time.Second, metrics, logger)
	var settings logline_query_limits.Limits = mockLimits{}
	if len(limits) > 0 {
		settings = limits[0]
	}
	prefetchMW := prefetchMiddlewareForTest(hp, cfg, settings, metrics, logger)
	return prefetchMW.Wrap(filterMW.Wrap(querier))
}

func buildStatsAwareQuerier(
	statsResp *logproto.IndexStatsResponse,
	statsErr error,
	statsCalls *int,
	queryHandler queryrangebase.Handler,
) queryrangebase.Handler {
	return queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		switch req.(type) {
		case *logproto.IndexStatsRequest:
			if statsCalls != nil {
				*statsCalls = *statsCalls + 1
			}
			if statsErr != nil {
				return nil, statsErr
			}
			if statsResp == nil {
				statsResp = &logproto.IndexStatsResponse{}
			}
			return &queryrange.IndexStatsResponse{Response: statsResp}, nil
		default:
			return queryHandler.Do(ctx, req)
		}
	})
}

// detectedFieldsLimits are the limits the detected fields handler reads before
// it sends its log request. They allow any line limit and matchers.
type detectedFieldsLimits struct{ queryrange.Limits }

func (detectedFieldsLimits) MaxEntriesLimitPerQuery(context.Context, string) int { return 0 }
func (detectedFieldsLimits) RequiredLabels(context.Context, string) []string     { return nil }
func (detectedFieldsLimits) RequiredNumberLabels(context.Context, string) int    { return 0 }

// buildRoutedStack wires prefetch and filter like buildStack, with a router in
// place of the Loki tripperware. The router answers index stats requests with
// statsBytes and sends detected fields requests through the real detected
// fields tripperware, which calls the filter with its own log request.
func buildRoutedStack(
	t *testing.T,
	hp *mockHintProvider,
	cfg Config,
	metrics *Metrics,
	limits logline_query_limits.Limits,
	statsBytes uint64,
	statsCalls *int,
	querier queryrangebase.Handler,
) queryrangebase.Handler {
	t.Helper()
	filtered := NewLoglineFilterMiddleware(10*time.Second, metrics, nil).Wrap(querier)
	detectedFieldsMW, err := queryrange.NewDetectedFieldsTripperware(detectedFieldsLimits{}, identityMiddleware{}, identityMiddleware{})
	require.NoError(t, err)
	detectedFields := detectedFieldsMW.Wrap(filtered)
	router := queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		if _, ok := req.(*queryrange.DetectedFieldsRequest); ok {
			return detectedFields.Do(ctx, req)
		}
		return filtered.Do(ctx, req)
	})
	stats := buildStatsAwareQuerier(&logproto.IndexStatsResponse{Bytes: statsBytes}, nil, statsCalls, router)
	return prefetchMiddlewareForTest(hp, cfg, limits, metrics, nil).Wrap(stats)
}

// newTestDetectedFieldsRequest builds a detected fields request, or a field
// values request when field is set, with a line limit of 1000.
func newTestDetectedFieldsRequest(query string, from, through time.Time, field string) *queryrange.DetectedFieldsRequest {
	path := "/loki/api/v1/detected_fields"
	if field != "" {
		path = "/loki/api/v1/detected_field/" + field + "/values"
	}
	req := queryrange.NewDetectedFieldsRequest(from, through, 1000, 100, 0, query, path)
	if field != "" {
		req.AsProto().Values = true
		req.AsProto().Name = field
	}
	return req
}

// logfmtQuerier records the log requests it receives and answers each with one
// logfmt line, so a detected fields response has fields to report. A metric
// request gets one sample instead.
type logfmtQuerier struct {
	reqs          []*queryrange.LokiRequest
	encodingFlags []httpreq.EncodingFlags
}

func (q *logfmtQuerier) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	lokiReq := req.(*queryrange.LokiRequest)
	q.reqs = append(q.reqs, lokiReq)
	q.encodingFlags = append(q.encodingFlags, httpreq.ExtractEncodingFlagsFromCtx(ctx))
	if _, err := syntax.ParseSampleExpr(lokiReq.Query); err == nil {
		return metricMatrixResponse(1, lokiReq.EndTs), nil
	}
	return streamResponseWithEntries(logproto.Entry{
		Timestamp: lokiReq.EndTs.Add(-time.Minute),
		Line:      `level=error msg=timeout`,
	}), nil
}

func detectedFieldLabels(resp *queryrange.DetectedFieldsResponse) []string {
	var names []string
	for _, f := range resp.Response.Fields {
		names = append(names, f.Label)
	}
	return names
}

func findLogLine(logs, contains string) string {
	for line := range strings.SplitSeq(logs, "\n") {
		if strings.Contains(line, contains) {
			return line
		}
	}
	return ""
}

func testTenantContext() context.Context {
	return user.InjectOrgID(context.Background(), "test")
}

func testTenantContextWithDryRun() context.Context {
	ctx := testTenantContext()
	return httpreq.InjectHeader(ctx, LoglineIndexHeader, string(LoglineIndexDryRun))
}

func testTenantContextWithLive() context.Context {
	ctx := testTenantContext()
	return httpreq.InjectHeader(ctx, LoglineIndexHeader, string(LoglineIndexLive))
}

func testTenantContextWithForceLive() context.Context {
	ctx := testTenantContext()
	return httpreq.InjectHeader(ctx, LoglineIndexHeader, string(LoglineIndexLive))
}

func testTenantContextWithForceDisable() context.Context {
	ctx := testTenantContext()
	return httpreq.InjectHeader(ctx, LoglineIndexHeader, string(LoglineIndexOff))
}

// ---------------------------------------------------------------------------
// Prefetch + Filter integration tests
// ---------------------------------------------------------------------------

func TestPrefetchFilter_NonLokiRequest(t *testing.T) {
	hp := &mockHintProvider{}
	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		return &queryrange.LokiSeriesResponse{}, nil
	})

	handler := buildStack(hp, Config{RequireOptInHeader: true}, newTestMetrics(), next)
	ctx := testTenantContextWithLive()
	_, err := handler.Do(ctx, &queryrange.LokiSeriesRequest{})
	require.NoError(t, err)
	require.Equal(t, 1, nextCalled)
	require.Equal(t, 0, hp.Calls(), "hint provider should not be called for non-LokiRequest")
}

func TestPrefetchFilter_DetectedFieldsLiveHints(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	from := now.Add(-time.Hour)
	hintStart := now.Add(-45 * time.Minute)
	hintEnd := now.Add(-30 * time.Minute)

	for _, tc := range []struct {
		name       string
		field      string
		wantFields []string
		wantValues []string
	}{
		{name: "fields", wantFields: []string{"level", "msg"}},
		{name: "field values", field: "level", wantValues: []string{"error"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hp := &mockHintProvider{hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			}}}
			querier := &logfmtQuerier{}
			handler := buildRoutedStack(t, hp, Config{RequireOptInHeader: true, NgramLength: 6}, newTestMetrics(), mockLimits{}, 0, nil, querier)

			req := newTestDetectedFieldsRequest(`{job="test"} |= "timeout"`, from, now, tc.field)
			resp, err := handler.Do(testTenantContextWithLive(), req)
			require.NoError(t, err)

			require.Equal(t, 1, hp.Calls())
			gotFrom, gotThrough := hp.Window()
			require.Equal(t, model.TimeFromUnixNano(from.UnixNano()), gotFrom)
			require.Equal(t, model.TimeFromUnixNano(now.UnixNano()), gotThrough)

			require.Len(t, querier.reqs, 1, "detected fields sends one log request")
			got := querier.reqs[0]
			require.Equal(t, req.GetQuery(), got.Query)
			require.Equal(t, from, got.StartTs)
			require.Equal(t, now, got.EndTs)
			require.Equal(t, logproto.BACKWARD, got.Direction)
			require.Equal(t, uint32(1000), got.Limit)
			require.Equal(t, []logproto.HintTimeRange{{Start: hintStart, End: hintEnd}}, got.HintRanges)
			require.True(t, querier.encodingFlags[0].Has(httpreq.FlagCategorizeLabels),
				"the filter keeps the categorize-labels flag the detected fields handler sets")

			dfResp, ok := resp.(*queryrange.DetectedFieldsResponse)
			require.True(t, ok, "got %T", resp)
			require.ElementsMatch(t, tc.wantFields, detectedFieldLabels(dfResp))
			require.ElementsMatch(t, tc.wantValues, dfResp.Response.Values)
		})
	}
}

func TestPrefetchFilter_DetectedFieldsNoMatchingHintsSkipsQuerier(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{hints: &hintprovider.Hints{}}
	querier := &logfmtQuerier{}
	handler := buildRoutedStack(t, hp, Config{RequireOptInHeader: true, NgramLength: 6}, newTestMetrics(), mockLimits{}, 0, nil, querier)

	resp, err := handler.Do(testTenantContextWithLive(), newTestDetectedFieldsRequest(`{job="test"} |= "timeout"`, now.Add(-time.Hour), now, ""))
	require.NoError(t, err)

	require.Equal(t, 1, hp.Calls())
	require.Empty(t, querier.reqs, "no hint range overlaps the request, so the filter skips it")
	dfResp, ok := resp.(*queryrange.DetectedFieldsResponse)
	require.True(t, ok, "got %T", resp)
	require.Empty(t, dfResp.Response.Fields)
	require.Empty(t, dfResp.Response.Values)
}

// Dry-run verifies hints against log lines or samples, and a detected fields
// response has neither, so detected fields only look up hints in live mode.
func TestPrefetchFilter_DetectedFieldsSkipsLookup(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	const supported = `{job="test"} |= "timeout"`
	const metric = `sum(count_over_time({job="test"} |= "timeout" [5m]))`
	optIn := Config{RequireOptInHeader: true, NgramLength: 6}

	for _, tc := range []struct {
		name   string
		ctx    context.Context
		cfg    Config
		limits mockLimits
		query  string
		// A metric query is rejected by the detected fields handler.
		wantStatus     int32
		wantStatsCalls int
	}{
		{name: "dry run header", ctx: testTenantContextWithDryRun(), cfg: optIn, query: supported},
		{name: "dry run tenant mode", ctx: testTenantContext(), cfg: optIn, limits: mockLimits{modes: map[string]Mode{"test": ModeDryRun}}, query: supported},
		{name: "dry run default", ctx: testTenantContext(), cfg: Config{DryRun: true, NgramLength: 6}, query: supported},
		{name: "dry run metric query", ctx: testTenantContextWithDryRun(), cfg: optIn, query: metric, wantStatus: http.StatusBadRequest},
		{name: "live metric query", ctx: testTenantContextWithLive(), cfg: optIn, query: metric, wantStatus: http.StatusBadRequest},
		{name: "query without a line filter", ctx: testTenantContextWithLive(), cfg: optIn, query: `{job="test"}`},
		{name: "search term shorter than the n-gram length", ctx: testTenantContextWithLive(), cfg: optIn, query: `{job="test"} |= "err"`},
		{name: "pattern filter", ctx: testTenantContextWithLive(), cfg: optIn, query: `{job="test"} |> "<_> timeout <_>"`},
		{name: "below the byte threshold", ctx: testTenantContextWithLive(), cfg: optIn, limits: mockLimits{defaultMinQueryBytes: 1}, query: supported, wantStatsCalls: 1},
		{name: "logline off header", ctx: testTenantContextWithForceDisable(), cfg: optIn, query: supported},
		{name: "tenant not opted in", ctx: testTenantContext(), cfg: optIn, query: supported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hp := &mockHintProvider{hints: &hintprovider.Hints{}}
			querier := &logfmtQuerier{}
			metrics := newTestMetrics()
			statsCalls := 0
			handler := buildRoutedStack(t, hp, tc.cfg, metrics, tc.limits, 0, &statsCalls, querier)

			resp, err := handler.Do(tc.ctx, newTestDetectedFieldsRequest(tc.query, now.Add(-time.Hour), now, ""))
			// Dry-run counts the lookup before it starts it in the background.
			require.Zero(t, testutil.ToFloat64(metrics.dryRunTotal))
			require.Equal(t, 0, hp.Calls())
			require.Equal(t, tc.wantStatsCalls, statsCalls)

			if tc.wantStatus != 0 {
				httpResp, ok := httpgrpc.HTTPResponseFromError(err)
				require.True(t, ok, "got %v", err)
				require.Equal(t, tc.wantStatus, httpResp.Code)
				require.Empty(t, querier.reqs)
				return
			}
			require.NoError(t, err)
			require.Len(t, querier.reqs, 1)
			require.Empty(t, querier.reqs[0].HintRanges)
			dfResp, ok := resp.(*queryrange.DetectedFieldsResponse)
			require.True(t, ok, "got %T", resp)
			require.ElementsMatch(t, []string{"level", "msg"}, detectedFieldLabels(dfResp))
		})
	}
}

func TestPrefetchFilter_ParseError(t *testing.T) {
	hp := &mockHintProvider{}
	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		return emptyStreamResponse(), nil
	})

	handler := buildStack(hp, Config{RequireOptInHeader: true}, newTestMetrics(), next)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`not valid logql!!!`, time.Now().Add(-1*time.Hour), time.Now())
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, nextCalled)
	require.Equal(t, 0, hp.Calls())
}

func TestPrefetchFilter_OptInHeader_Missing(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-45 * time.Minute), End: now.Add(-30 * time.Minute)},
			},
		},
	}

	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		return emptyStreamResponse(), nil
	})

	handler := buildStack(hp, Config{RequireOptInHeader: true}, newTestMetrics(), next)
	ctx := testTenantContext() // header intentionally omitted
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, nextCalled, "should pass through when opt-in header is missing")
	require.Equal(t, 0, hp.Calls(), "should not call ProvideHints without opt-in header")
}

func TestPrefetchFilter_OptInHeader_Present(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintStart := now.Add(-45 * time.Minute)
	hintEnd := now.Add(-30 * time.Minute)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			},
		},
	}

	var gotStart, gotEnd time.Time
	var gotHints []logproto.HintTimeRange
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		gotHints = lokiReq.HintRanges
		return emptyStreamResponse(), nil
	})

	handler := buildStack(hp, Config{RequireOptInHeader: true}, newTestMetrics(), next)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, hp.Calls(), "should call ProvideHints when opt-in header is present")
	require.Equal(t, req.StartTs, gotStart)
	require.Equal(t, req.EndTs, gotEnd)
	require.Equal(t, []logproto.HintTimeRange{{Start: hintStart, End: hintEnd}}, gotHints)
}

func TestPrefetchFilter_RequireOptInHeaderFalse_UsesHintsWithoutHeader(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintStart := now.Add(-45 * time.Minute)
	hintEnd := now.Add(-30 * time.Minute)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			},
		},
	}

	var gotStart, gotEnd time.Time
	var gotHints []logproto.HintTimeRange
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		gotHints = lokiReq.HintRanges
		return emptyStreamResponse(), nil
	})

	cfg := Config{RequireOptInHeader: false}
	handler := buildStack(hp, cfg, newTestMetrics(), next)
	ctx := testTenantContext() // header intentionally omitted
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, hp.Calls(), "should call ProvideHints when header requirement is disabled")
	require.Equal(t, req.StartTs, gotStart)
	require.Equal(t, req.EndTs, gotEnd)
	require.Equal(t, []logproto.HintTimeRange{{Start: hintStart, End: hintEnd}}, gotHints)
}

func TestDryRun_QueryUnmodified(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintStart := now.Add(-45 * time.Minute)
	hintEnd := now.Add(-30 * time.Minute)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			},
		},
	}

	var gotStart, gotEnd time.Time
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		time.Sleep(25 * time.Millisecond)
		return streamResponseWithEntries(logproto.Entry{Timestamp: hintStart, Line: "failure"}), nil
	})

	cfg := Config{DryRun: true, NgramLength: 3}
	handler := buildStack(hp, cfg, newTestMetrics(), next)
	ctx := testTenantContext()
	req := newTestLokiRequest(`{job="test"} |= "failure"`, now.Add(-1*time.Hour), now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, req.StartTs, gotStart, "dry-run must not narrow query start")
	require.Equal(t, req.EndTs, gotEnd, "dry-run must not narrow query end")
	require.Equal(t, 1, hp.Calls(), "dry-run should still perform a hint lookup")
}

func TestDryRun_VerifiesHintCoverage(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintStart := now.Add(-45 * time.Minute)
	hintEnd := now.Add(-30 * time.Minute)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			},
		},
	}

	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		time.Sleep(25 * time.Millisecond)
		return streamResponseWithEntries(logproto.Entry{Timestamp: now.Add(-35 * time.Minute), Line: "failure"}), nil
	})

	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	cfg := Config{DryRun: true, NgramLength: 3}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next)
	ctx := testTenantContext()
	req := newTestLokiRequest(`{job="test"} |= "failure"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)

	logLine := findLogLine(logs.String(), "logline dry-run verification")
	require.NotEmpty(t, logLine)
	require.Contains(t, logLine, "correct=true")
	require.Contains(t, logLine, "query_type=filter")
	require.Contains(t, logLine, "false_negatives=0")
	require.Contains(t, logLine, "total_entries=1")
	require.Contains(t, logLine, "query_timeout=false")
	require.Contains(t, logLine, "query_hash=")
}

func TestDryRun_LogsHintSummaryFields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	queryStart := now.Add(-1 * time.Hour)
	queryEnd := now
	ranges := []hintprovider.HintTimeRange{
		{Start: now.Add(-50 * time.Minute), End: now.Add(-49 * time.Minute)},
		{Start: now.Add(-40 * time.Minute), End: now.Add(-40*time.Minute + 500*time.Millisecond)},
	}

	cases := []struct {
		name      string
		direction logproto.Direction
	}{
		{name: "forward", direction: logproto.FORWARD},
		{name: "backward", direction: logproto.BACKWARD},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hp := &mockHintProvider{
				hints: &hintprovider.Hints{TimeRanges: ranges},
			}
			next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				time.Sleep(25 * time.Millisecond)
				return streamResponseWithEntries(logproto.Entry{Timestamp: ranges[0].Start.Add(10 * time.Second), Line: "failure"}), nil
			})

			var logs bytes.Buffer
			logger := log.NewLogfmtLogger(&logs)
			cfg := Config{DryRun: true, NgramLength: 3}
			handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next)

			req := newTestLokiRequest(`{job="test"} |= "failure"`, queryStart, queryEnd)
			req.Direction = tc.direction

			_, err := handler.Do(testTenantContext(), req)
			require.NoError(t, err)

			logLine := findLogLine(logs.String(), "logline dry-run verification")
			require.NotEmpty(t, logLine)

			summary := summarizeDryRunHints(ranges, queryStart, queryEnd, queryEnd, tc.direction)
			require.Contains(t, logLine, "hint_total_seconds="+summary.hintTotalSeconds)
			require.Contains(t, logLine, "earliest_hint_time="+summary.earliestHintTime)
			require.Contains(t, logLine, "latest_hint_time="+summary.latestHintTime)
			require.Contains(t, logLine, "initial_skip_seconds="+summary.initialSkipSeconds)
			require.Contains(t, logLine, "hint_ranges_checksum="+summary.rangesChecksum)
		})
	}
}

func TestDryRun_EmptyHintRanges_LogsSummaryFields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	queryStart := now.Add(-1 * time.Hour)
	queryEnd := now
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: nil},
	}

	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		time.Sleep(25 * time.Millisecond)
		return emptyStreamResponse(), nil
	})

	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	cfg := Config{DryRun: true, NgramLength: 3}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next)

	req := newTestLokiRequest(`{job="test"} |= "failure"`, queryStart, queryEnd)
	req.Direction = logproto.FORWARD

	_, err := handler.Do(testTenantContext(), req)
	require.NoError(t, err)

	logLine := findLogLine(logs.String(), "logline dry-run verification")
	require.NotEmpty(t, logLine)

	summary := summarizeDryRunHints(nil, queryStart, queryEnd, queryEnd, req.Direction)
	require.Contains(t, logLine, "hint_total_seconds="+summary.hintTotalSeconds)
	require.Contains(t, logLine, "earliest_hint_time="+summary.earliestHintTime)
	require.Contains(t, logLine, "latest_hint_time="+summary.latestHintTime)
	require.Contains(t, logLine, "initial_skip_seconds="+summary.initialSkipSeconds)
	require.Contains(t, logLine, "hint_ranges_checksum="+summary.rangesChecksum)
}

func TestDryRun_FalseNegative(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-45 * time.Minute), End: now.Add(-40 * time.Minute)},
			},
		},
	}

	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		time.Sleep(25 * time.Millisecond)
		return streamResponseWithEntries(logproto.Entry{Timestamp: now.Add(-20 * time.Minute), Line: "failure"}), nil
	})

	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	cfg := Config{DryRun: true, NgramLength: 3}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next)
	ctx := testTenantContext()
	req := newTestLokiRequest(`{job="test"} |= "failure"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)

	logLine := findLogLine(logs.String(), "logline dry-run verification")
	require.NotEmpty(t, logLine)
	require.Contains(t, logLine, "correct=false")
	require.Contains(t, logLine, "false_negatives=1")
	require.Contains(t, logLine, "total_entries=1")
}

func TestDryRun_EmptyResults_CorrectTrue(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-45 * time.Minute), End: now.Add(-30 * time.Minute)},
			},
		},
	}

	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		time.Sleep(25 * time.Millisecond)
		return emptyStreamResponse(), nil
	})

	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	cfg := Config{DryRun: true, NgramLength: 3}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next)
	ctx := testTenantContext()
	req := newTestLokiRequest(`{job="test"} |= "failure"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)

	logLine := findLogLine(logs.String(), "logline dry-run verification")
	require.NotEmpty(t, logLine, "zero-entry queries should still produce a verification log")
	require.Contains(t, logLine, "correct=true")
	require.Contains(t, logLine, "total_entries=0")
}

func TestDryRun_IngesterWindowOnly_SkipsDryRun(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-20 * time.Minute), End: now.Add(-10 * time.Minute)},
			},
		},
	}

	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		return emptyStreamResponse(), nil
	})

	cfg := Config{DryRun: true, NgramLength: 3, QueryIngestersWithin: 3 * time.Hour}
	handler := buildStack(hp, cfg, newTestMetrics(), next)
	ctx := testTenantContext()
	req := newTestLokiRequest(`{job="test"} |= "failure"`, now.Add(-30*time.Minute), now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, nextCalled, "query should pass through")
	require.Equal(t, 0, hp.Calls(), "should not call ProvideHints when entirely in ingester window")
}

func TestDryRun_IngesterWindowEntries_ExcludedFromVerification(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintStart := now.Add(-5 * time.Hour)
	hintEnd := now.Add(-4 * time.Hour)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			},
		},
	}

	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		time.Sleep(25 * time.Millisecond)
		return streamResponseWithEntries(
			logproto.Entry{Timestamp: now.Add(-4*time.Hour - 30*time.Minute), Line: "covered"},
			logproto.Entry{Timestamp: now.Add(-1 * time.Hour), Line: "in ingester window"},
		), nil
	})

	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	cfg := Config{DryRun: true, NgramLength: 3, QueryIngestersWithin: 3 * time.Hour}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next)
	ctx := testTenantContext()
	req := newTestLokiRequest(`{job="test"} |= "covered"`, now.Add(-6*time.Hour), now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)

	logLine := findLogLine(logs.String(), "logline dry-run verification")
	require.NotEmpty(t, logLine)
	require.Contains(t, logLine, "correct=true")
	require.Contains(t, logLine, "total_entries=1", "ingester-window entry should be excluded")
	require.Contains(t, logLine, "false_negatives=0")
}

func TestDryRun_QueryFinishesBeforeHints(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-45 * time.Minute), End: now.Add(-40 * time.Minute)},
			},
		},
		delay: 200 * time.Millisecond,
	}

	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return emptyStreamResponse(), nil
	})

	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	cfg := Config{DryRun: true, NgramLength: 3, HintTimeout: 5 * time.Second}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next)
	ctx := testTenantContext()
	req := newTestLokiRequest(`{job="test"} |= "failure"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return hp.SawCancel() }, time.Second, 10*time.Millisecond)

	logLine := findLogLine(logs.String(), "logline dry-run hint lookup incomplete")
	require.NotEmpty(t, logLine)
	require.Contains(t, logLine, "query_timeout=false")
	require.Contains(t, logLine, "query_hash=")
}

func TestDryRun_ClientTimeout_HintLookupCompleted(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintStart := now.Add(-45 * time.Minute)
	hintEnd := now.Add(-30 * time.Minute)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			},
		},
	}

	// Simulate a client timeout by cancelling the context while the query
	// is in flight. The hint lookup (no delay) will finish first.
	queryCtx, cancelQuery := context.WithCancel(testTenantContext())
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		time.Sleep(25 * time.Millisecond)
		cancelQuery()
		return nil, context.Canceled
	})

	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	cfg := Config{DryRun: true, NgramLength: 3, HintTimeout: 5 * time.Second}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next)
	req := newTestLokiRequest(`{job="test"} |= "failure"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(queryCtx, req)
	require.Error(t, err)

	logLine := findLogLine(logs.String(), "logline dry-run verification")
	require.NotEmpty(t, logLine, "should log verification even when query times out")
	require.Contains(t, logLine, "correct=true")
	require.Contains(t, logLine, "query_timeout=true")
	require.Contains(t, logLine, "query_hash=")
	require.Contains(t, logLine, "hint_ranges=1")
	summary := summarizeDryRunHints([]hintprovider.HintTimeRange{{Start: hintStart, End: hintEnd}}, req.StartTs, req.EndTs, req.EndTs, req.Direction)
	require.Contains(t, logLine, "hint_total_seconds="+summary.hintTotalSeconds)
	require.Contains(t, logLine, "earliest_hint_time="+summary.earliestHintTime)
	require.Contains(t, logLine, "latest_hint_time="+summary.latestHintTime)
	require.Contains(t, logLine, "initial_skip_seconds="+summary.initialSkipSeconds)
	require.Contains(t, logLine, "hint_ranges_checksum="+summary.rangesChecksum)
}

func TestDryRun_ClientTimeout_HintLookupIncomplete(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-45 * time.Minute), End: now.Add(-30 * time.Minute)},
			},
		},
		delay: 500 * time.Millisecond,
	}

	// Simulate a client timeout: cancel the context, and the hint lookup
	// is slow enough that it won't finish in time.
	queryCtx, cancelQuery := context.WithCancel(testTenantContext())
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		cancelQuery()
		return nil, context.Canceled
	})

	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	cfg := Config{DryRun: true, NgramLength: 3, HintTimeout: 5 * time.Second}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next)
	req := newTestLokiRequest(`{job="test"} |= "failure"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(queryCtx, req)
	require.Error(t, err)

	logLine := findLogLine(logs.String(), "logline dry-run hint lookup incomplete")
	require.NotEmpty(t, logLine, "should log hint lookup incomplete on client timeout")
	require.Contains(t, logLine, "query_timeout=true")
	require.Contains(t, logLine, "query_hash=")
}

func TestDryRun_ClientTimeout_HintLookupCancelledByContext(t *testing.T) {
	// The hint lookup goroutine finishes (done channel closes) but with a
	// context.Canceled error because the parent context was cancelled.
	// This should be logged as "incomplete" with query_timeout=true.
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-45 * time.Minute), End: now.Add(-30 * time.Minute)},
			},
		},
		delay: 50 * time.Millisecond,
	}

	queryCtx, cancelQuery := context.WithCancel(testTenantContext())
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		cancelQuery()
		time.Sleep(100 * time.Millisecond)
		return nil, context.Canceled
	})

	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	cfg := Config{DryRun: true, NgramLength: 3, HintTimeout: 5 * time.Second}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next)
	req := newTestLokiRequest(`{job="test"} |= "failure"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(queryCtx, req)
	require.Error(t, err)

	logLine := findLogLine(logs.String(), "logline dry-run hint lookup incomplete")
	require.NotEmpty(t, logLine, "context-cancelled hint lookup should be treated as incomplete")
	require.Contains(t, logLine, "query_timeout=true")
	require.Contains(t, logLine, "query_hash=")
}

func TestDryRun_RateLimitSkipsSecondDryRun(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-45 * time.Minute), End: now.Add(-40 * time.Minute)},
			},
		},
	}

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	callNum := 0
	var callMu sync.Mutex
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		callMu.Lock()
		callNum++
		current := callNum
		callMu.Unlock()
		if current == 1 {
			close(firstStarted)
			<-releaseFirst
		}
		return streamResponseWithEntries(logproto.Entry{Timestamp: now.Add(-43 * time.Minute), Line: "failure"}), nil
	})

	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	cfg := Config{DryRun: true, NgramLength: 3}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next)
	ctx := testTenantContext()

	firstDone := make(chan error, 1)
	go func() {
		req := newTestLokiRequest(`{job="test"} |= "failure"`, now.Add(-1*time.Hour), now)
		_, err := handler.Do(ctx, req)
		firstDone <- err
	}()

	<-firstStarted
	secondReq := newTestLokiRequest(`{job="test"} |= "failure"`, now.Add(-1*time.Hour), now)
	_, err := handler.Do(ctx, secondReq)
	require.NoError(t, err)

	close(releaseFirst)
	require.NoError(t, <-firstDone)
	require.Eventually(t, func() bool { return hp.Calls() == 1 }, time.Second, 10*time.Millisecond,
		"second dry-run should skip hint lookup due to inflight rate limit")

	logLine := findLogLine(logs.String(), "dry-run hint lookup skipped due to inflight limit")
	require.NotEmpty(t, logLine)
	require.Contains(t, logLine, "query_hash=")
}

func TestDryRun_UnsupportedQuerySkipped(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{{Start: now.Add(-45 * time.Minute), End: now.Add(-30 * time.Minute)}}},
	}

	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		return emptyStreamResponse(), nil
	})

	cfg := Config{DryRun: true, NgramLength: 3}
	handler := buildStack(hp, cfg, newTestMetrics(), next)
	ctx := testTenantContext()
	req := newTestLokiRequest(`{job="test"}`, now.Add(-1*time.Hour), now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, nextCalled)
	require.Equal(t, 0, hp.Calls(), "unsupported query should not invoke dry-run hint lookup")
}

func TestDryRun_StatsBelowThresholdSkipped(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-45 * time.Minute), End: now.Add(-30 * time.Minute)},
			},
		},
	}

	var gotStart, gotEnd time.Time
	queryCalls := 0
	queryHandler := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		queryCalls++
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		return emptyStreamResponse(), nil
	})

	statsCalls := 0
	querier := buildStatsAwareQuerier(
		&logproto.IndexStatsResponse{Bytes: 100},
		nil,
		&statsCalls,
		queryHandler,
	)

	cfg := Config{DryRun: true, NgramLength: 3}
	handler := buildStack(hp, cfg, newTestMetrics(), querier, mockLimits{defaultMinQueryBytes: 500})
	ctx := testTenantContext()
	req := newTestLokiRequest(`{job="test"} |= "failure"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, statsCalls, "should call index stats once for dry-run gating")
	require.Equal(t, 0, hp.Calls(), "should skip dry-run hint lookup when query bytes are below threshold")
	require.Equal(t, 1, queryCalls, "should execute query via passthrough path")
	require.Equal(t, req.StartTs, gotStart, "query should keep original start when dry-run is skipped")
	require.Equal(t, req.EndTs, gotEnd, "query should keep original end when dry-run is skipped")
}

func TestDryRun_HeaderGated_WithHeader(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintStart := now.Add(-45 * time.Minute)
	hintEnd := now.Add(-30 * time.Minute)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			},
		},
	}

	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		time.Sleep(25 * time.Millisecond)
		return streamResponseWithEntries(logproto.Entry{Timestamp: now.Add(-35 * time.Minute), Line: "failure"}), nil
	})

	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	cfg := Config{DryRun: true, RequireOptInHeader: true, NgramLength: 3}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next)
	ctx := testTenantContextWithDryRun()
	req := newTestLokiRequest(`{job="test"} |= "failure"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, hp.Calls(), "header-gated dry-run with header should perform hint lookup")

	logLine := findLogLine(logs.String(), "logline dry-run verification")
	require.NotEmpty(t, logLine, "should produce verification log")
}

func TestDryRun_HeaderGated_WithoutHeader(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-45 * time.Minute), End: now.Add(-30 * time.Minute)},
			},
		},
	}

	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		return emptyStreamResponse(), nil
	})

	cfg := Config{DryRun: true, RequireOptInHeader: true, NgramLength: 3}
	handler := buildStack(hp, cfg, newTestMetrics(), next)
	ctx := testTenantContext() // no header
	req := newTestLokiRequest(`{job="test"} |= "failure"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, nextCalled, "header-gated dry-run without header should passthrough")
	require.Equal(t, 0, hp.Calls(), "should not perform hint lookup without opt-in header")
}

func TestWrapMiddleware_Disabled(t *testing.T) {
	wrapped, storeSvc, cleanup, err := WrapMiddleware(
		Config{Enabled: false},
		Deps{},
		nil,
		nil,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)
	require.Nil(t, storeSvc)
	require.NotNil(t, cleanup)

	calls := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		calls++
		return emptyStreamResponse(), nil
	})

	handler := wrapped.Wrap(next)
	req := newTestLokiRequest(`{job="test"} |= "failure"`, time.Now().Add(-1*time.Hour), time.Now())
	_, err = handler.Do(testTenantContext(), req)
	require.NoError(t, err)
	require.Equal(t, 1, calls, "disabled config should leave middleware as identity")
	cleanup()
}

func TestPrefetchFilter_QueryBytesBelowThreshold_SkipsHintPrefetch(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-45 * time.Minute), End: now.Add(-30 * time.Minute)},
			},
		},
	}

	var gotStart, gotEnd time.Time
	queryCalls := 0
	queryHandler := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		queryCalls++
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		return emptyStreamResponse(), nil
	})

	statsCalls := 0
	querier := buildStatsAwareQuerier(
		&logproto.IndexStatsResponse{Bytes: 100},
		nil,
		&statsCalls,
		queryHandler,
	)

	cfg := Config{}
	handler := buildStack(hp, cfg, newTestMetrics(), querier, mockLimits{defaultMinQueryBytes: 500})
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, statsCalls, "should call index stats once for gating")
	require.Equal(t, 0, hp.Calls(), "should skip hint prefetch when query bytes are below threshold")
	require.Equal(t, 1, queryCalls, "should execute query via passthrough path")
	require.Equal(t, req.StartTs, gotStart, "query should keep original start when hints are skipped")
	require.Equal(t, req.EndTs, gotEnd, "query should keep original end when hints are skipped")
}

func TestPrefetchFilter_QueryBytesAboveThreshold_UsesHints(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintStart := now.Add(-45 * time.Minute)
	hintEnd := now.Add(-30 * time.Minute)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			},
		},
	}

	var gotStart, gotEnd time.Time
	var gotHints []logproto.HintTimeRange
	queryCalls := 0
	queryHandler := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		queryCalls++
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		gotHints = lokiReq.HintRanges
		return emptyStreamResponse(), nil
	})

	statsCalls := 0
	querier := buildStatsAwareQuerier(
		&logproto.IndexStatsResponse{Bytes: 1000},
		nil,
		&statsCalls,
		queryHandler,
	)

	cfg := Config{}
	handler := buildStack(hp, cfg, newTestMetrics(), querier, mockLimits{defaultMinQueryBytes: 500})
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, statsCalls, "should call index stats once for gating")
	require.Equal(t, 1, hp.Calls(), "should prefetch hints when query bytes exceed threshold")
	require.Equal(t, 1, queryCalls)
	require.Equal(t, req.StartTs, gotStart)
	require.Equal(t, req.EndTs, gotEnd)
	require.Equal(t, []logproto.HintTimeRange{{Start: hintStart, End: hintEnd}}, gotHints)
}

func TestPrefetchFilter_TenantMinQueryBytesOverridesGlobalThreshold(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintStart := now.Add(-45 * time.Minute)
	hintEnd := now.Add(-30 * time.Minute)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			},
		},
	}

	var gotStart, gotEnd time.Time
	var gotHints []logproto.HintTimeRange
	queryHandler := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		gotHints = lokiReq.HintRanges
		return emptyStreamResponse(), nil
	})

	statsCalls := 0
	querier := buildStatsAwareQuerier(
		&logproto.IndexStatsResponse{Bytes: 1000},
		nil,
		&statsCalls,
		queryHandler,
	)
	settings := mockLimits{
		defaultMinQueryBytes:  5000,
		minQueryBytesForIndex: map[string]int64{"test": 500},
	}

	cfg := Config{}
	handler := buildStack(hp, cfg, newTestMetrics(), querier, settings)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, statsCalls, "should call index stats once for tenant gating")
	require.Equal(t, 1, hp.Calls(), "tenant threshold should allow hint prefetch")
	require.Equal(t, req.StartTs, gotStart)
	require.Equal(t, req.EndTs, gotEnd)
	require.Equal(t, []logproto.HintTimeRange{{Start: hintStart, End: hintEnd}}, gotHints)
}

func TestPrefetchFilter_QueryStatsError_SkipsHintPrefetch(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-45 * time.Minute), End: now.Add(-30 * time.Minute)},
			},
		},
	}

	var gotStart, gotEnd time.Time
	queryCalls := 0
	queryHandler := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		queryCalls++
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		return emptyStreamResponse(), nil
	})

	statsCalls := 0
	querier := buildStatsAwareQuerier(
		nil,
		errors.New("stats backend unavailable"),
		&statsCalls,
		queryHandler,
	)

	cfg := Config{}
	handler := buildStack(hp, cfg, newTestMetrics(), querier, mockLimits{defaultMinQueryBytes: 500})
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, statsCalls, "should attempt index stats once for gating")
	require.Equal(t, 0, hp.Calls(), "should skip hint prefetch when stats call fails")
	require.Equal(t, 1, queryCalls, "should execute query via passthrough path")
	require.Equal(t, req.StartTs, gotStart, "query should keep original start when stats fail")
	require.Equal(t, req.EndTs, gotEnd, "query should keep original end when stats fail")
}

func TestPrefetchFilter_MinQueryBytesZero_DisablesStatsGating(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintStart := now.Add(-45 * time.Minute)
	hintEnd := now.Add(-30 * time.Minute)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			},
		},
	}

	var gotStart, gotEnd time.Time
	var gotHints []logproto.HintTimeRange
	queryHandler := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		gotHints = lokiReq.HintRanges
		return emptyStreamResponse(), nil
	})

	statsCalls := 0
	querier := buildStatsAwareQuerier(
		&logproto.IndexStatsResponse{Bytes: 1000},
		nil,
		&statsCalls,
		queryHandler,
	)

	cfg := Config{}
	handler := buildStack(hp, cfg, newTestMetrics(), querier, mockLimits{defaultMinQueryBytes: 0})
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 0, statsCalls, "stats gating should be disabled when threshold is zero")
	require.Equal(t, 1, hp.Calls(), "hint prefetch should run when stats gating is disabled")
	require.Equal(t, req.StartTs, gotStart)
	require.Equal(t, req.EndTs, gotEnd)
	require.Equal(t, []logproto.HintTimeRange{{Start: hintStart, End: hintEnd}}, gotHints)
}

func TestPrefetchFilter_TenantMinQueryBytesZero_DisablesStatsGating(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintStart := now.Add(-45 * time.Minute)
	hintEnd := now.Add(-30 * time.Minute)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			},
		},
	}

	var gotStart, gotEnd time.Time
	var gotHints []logproto.HintTimeRange
	queryHandler := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		gotHints = lokiReq.HintRanges
		return emptyStreamResponse(), nil
	})

	statsCalls := 0
	querier := buildStatsAwareQuerier(
		&logproto.IndexStatsResponse{Bytes: 1000},
		nil,
		&statsCalls,
		queryHandler,
	)
	settings := mockLimits{
		defaultMinQueryBytes:  500,
		minQueryBytesForIndex: map[string]int64{"test": 0},
	}

	cfg := Config{}
	handler := buildStack(hp, cfg, newTestMetrics(), querier, settings)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 0, statsCalls, "tenant zero threshold should disable stats gating")
	require.Equal(t, 1, hp.Calls(), "hint prefetch should run when tenant disables stats gating")
	require.Equal(t, req.StartTs, gotStart)
	require.Equal(t, req.EndTs, gotEnd)
	require.Equal(t, []logproto.HintTimeRange{{Start: hintStart, End: hintEnd}}, gotHints)
}

func TestPrefetchFilter_HintPrefetchCompletedLogIncludesQueryBytes(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-45 * time.Minute), End: now.Add(-30 * time.Minute)},
			},
		},
	}

	queryHandler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return emptyStreamResponse(), nil
	})

	statsCalls := 0
	querier := buildStatsAwareQuerier(
		&logproto.IndexStatsResponse{Bytes: 700},
		nil,
		&statsCalls,
		queryHandler,
	)

	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	cfg := Config{}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, querier, mockLimits{defaultMinQueryBytes: 500})
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, statsCalls)

	logLine := findLogLine(logs.String(), "hint prefetch completed")
	require.NotEmpty(t, logLine)
	require.Contains(t, logLine, "level=debug")
	require.Contains(t, logLine, "query_bytes=700")
	require.Contains(t, logLine, "hint_ranges_detail=")
	require.Contains(t, logLine, " +15m0s]")
	require.NotContains(t, logLine, "hint_total_seconds=", "the range duration moved to the metrics.go line")
}

func TestPrefetchFilter_SkipCacheHeaderSetsHintContext(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-45 * time.Minute), End: now.Add(-30 * time.Minute)},
			},
		},
	}

	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return emptyStreamResponse(), nil
	})

	handler := buildStack(hp, Config{RequireOptInHeader: true}, newTestMetrics(), next)
	ctx := testTenantContextWithLive()
	ctx = httpreq.InjectHeader(ctx, LoglineSkipCacheHeader, "true")
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, hp.Calls())
	require.True(t, hp.SawSkipCache(), "prefetch middleware should inject skip-cache context")
}

func TestPrefetchFilter_Unsupported(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		err: hintprovider.ErrUnsupported,
	}

	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		return emptyStreamResponse(), nil
	})

	handler := buildStack(hp, Config{RequireOptInHeader: true}, newTestMetrics(), next)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, nextCalled, "should pass through on ErrUnsupported")
}

func TestPrefetchFilter_ProviderError(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		err: errors.New("index unavailable"),
	}

	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		return emptyStreamResponse(), nil
	})

	handler := buildStack(hp, Config{RequireOptInHeader: true}, newTestMetrics(), next)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, nextCalled, "should pass through on provider error")
}

func TestPrefetchFilter_EmptyHints_SkipsQuerier(t *testing.T) {
	// No matching documents → empty response, no querier call.
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: nil},
	}

	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		return emptyStreamResponse(), nil
	})

	handler := buildStack(hp, Config{RequireOptInHeader: true}, newTestMetrics(), next)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)
	resp, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 0, nextCalled, "querier should NOT be called when hints are empty")

	lokiResp, ok := resp.(*queryrange.LokiResponse)
	require.True(t, ok)
	require.Equal(t, "success", lokiResp.Status)
}

func TestPrefetchFilter_AttachesHintRanges(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintStart := now.Add(-45 * time.Minute)
	hintEnd := now.Add(-30 * time.Minute)
	reqStart := now.Add(-1 * time.Hour)

	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			},
		},
	}

	var got *queryrange.LokiRequest
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		got = req.(*queryrange.LokiRequest)
		return emptyStreamResponse(), nil
	})

	handler := buildStack(hp, Config{RequireOptInHeader: true}, newTestMetrics(), next)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, reqStart, now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, req.StartTs, got.StartTs)
	require.Equal(t, req.EndTs, got.EndTs)
	require.Equal(t, []logproto.HintTimeRange{{Start: hintStart, End: hintEnd}}, got.HintRanges)
	require.Empty(t, req.HintRanges)
}

func TestPrefetchFilter_PreMinDateHintSource_RecordsPassthrough(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	reqStart := now.Add(-90 * time.Minute)
	reqEnd := now.Add(-75 * time.Minute)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{
					Start:  time.Time{},
					End:    now.Add(-30 * time.Minute),
					Source: hintprovider.HintSourcePreMinDate,
				},
			},
		},
	}

	var gotStart, gotEnd time.Time
	var gotHints []logproto.HintTimeRange
	var prefetchResult *hintPrefetchResult
	next := queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		gotHints = lokiReq.HintRanges
		prefetchResult = hintPrefetchFromContext(ctx)
		return emptyStreamResponse(), nil
	})

	handler := buildStack(hp, Config{RequireOptInHeader: true}, newTestMetrics(), next)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, reqStart, reqEnd)
	resp, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, prefetchResult)

	require.Equal(t, reqStart, gotStart, "pre-min-date hint should pass through original start")
	require.Equal(t, reqEnd, gotEnd, "pre-min-date hint should pass through original end")
	require.Empty(t, gotHints)

	loglineStats := resp.(*queryrange.LokiResponse).Statistics.Logline
	require.Zero(t, loglineStats.SkippedRequests)
	require.Zero(t, loglineStats.NarrowedRequests)
	require.Equal(t, int64(1), loglineStats.HintRanges)
	require.Zero(t, loglineStats.HintRangesDuration, "the pre-min-date range has no index coverage")
}

func TestPrefetchFilter_QueryStatsAttached(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-30 * time.Minute), End: now.Add(-20 * time.Minute)},
			},
		},
	}

	var prefetchResult *hintPrefetchResult
	next := queryrangebase.HandlerFunc(func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		prefetchResult = hintPrefetchFromContext(ctx)
		return emptyStreamResponse(), nil
	})

	handler := buildStack(hp, Config{RequireOptInHeader: true}, newTestMetrics(), next)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, prefetchResult)
	stats := prefetchResult.stats
	require.NotNil(t, stats)

	snap := stats.Snapshot()
	require.Equal(t, int32(1), snap.PrefetchCalls)
	require.Equal(t, int32(0), snap.PrefetchTimeouts)
}

func TestPrefetchFilter_QueryStatsCountsTimeout(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: nil},
		delay: 100 * time.Millisecond,
	}

	metrics := newTestMetrics()
	prefetchMW := prefetchMiddlewareForTest(hp, Config{RequireOptInHeader: true}, mockLimits{}, metrics, nil)
	filterMW := NewLoglineFilterMiddleware(10*time.Millisecond, metrics, nil)
	var prefetchResult *hintPrefetchResult
	next := queryrangebase.HandlerFunc(func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		prefetchResult = hintPrefetchFromContext(ctx)
		return emptyStreamResponse(), nil
	})
	handler := prefetchMW.Wrap(filterMW.Wrap(next))

	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)
	resp, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, prefetchResult)
	stats := prefetchResult.stats
	require.NotNil(t, stats)
	snap := stats.Snapshot()
	require.Equal(t, int32(1), snap.PrefetchCalls)
	require.Equal(t, int32(1), snap.PrefetchTimeouts)

	loglineStats := resp.(*queryrange.LokiResponse).Statistics.Logline
	require.Equal(t, hintStatusIncomplete, loglineStats.HintStatus, "the lookup was still running when the query returned")
	require.Equal(t, outcomeLookupTimeout, loglineStats.Outcome)
	require.Zero(t, loglineStats.SkippedRequests)
	require.Zero(t, loglineStats.NarrowedRequests)
}

func TestPrefetchFilter_MultipleHintRanges(t *testing.T) {
	intervalStart := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	intervalEnd := time.Date(2026, 9, 8, 12, 59, 0, 0, time.UTC)
	hints := []hintprovider.HintTimeRange{
		{Start: time.Date(2026, 9, 8, 12, 5, 0, 0, time.UTC), End: time.Date(2026, 9, 8, 12, 6, 0, 0, time.UTC)},
		{Start: time.Date(2026, 9, 8, 12, 21, 0, 0, time.UTC), End: time.Date(2026, 9, 8, 12, 22, 0, 0, time.UTC)},
		{Start: time.Date(2026, 9, 8, 12, 35, 0, 0, time.UTC), End: time.Date(2026, 9, 8, 12, 36, 0, 0, time.UTC)},
		{Start: time.Date(2026, 9, 8, 12, 41, 0, 0, time.UTC), End: time.Date(2026, 9, 8, 12, 42, 0, 0, time.UTC)},
		{Start: time.Date(2026, 9, 8, 12, 51, 0, 0, time.UTC), End: time.Date(2026, 9, 8, 12, 52, 0, 0, time.UTC)},
	}

	hp := &mockHintProvider{hints: &hintprovider.Hints{TimeRanges: hints}}

	var got *queryrange.LokiRequest
	calls := 0
	var prefetchResult *hintPrefetchResult
	next := queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		calls++
		got = req.(*queryrange.LokiRequest)
		prefetchResult = hintPrefetchFromContext(ctx)
		return emptyStreamResponse(), nil
	})

	metrics := newTestMetrics()
	handler := buildStack(hp, Config{RequireOptInHeader: true}, metrics, next)
	req := newTestLokiRequest(`{job="test"} |= "error"`, intervalStart, intervalEnd)
	resp, err := handler.Do(testTenantContextWithLive(), req)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, intervalStart, got.StartTs)
	require.Equal(t, intervalEnd, got.EndTs)
	require.Equal(t, []logproto.HintTimeRange{
		{Start: hints[0].Start, End: hints[0].End},
		{Start: hints[1].Start, End: hints[1].End},
		{Start: hints[2].Start, End: hints[2].End},
		{Start: hints[3].Start, End: hints[3].End},
		{Start: hints[4].Start, End: hints[4].End},
	}, got.HintRanges)

	require.NotNil(t, prefetchResult)
	loglineStats := resp.(*queryrange.LokiResponse).Statistics.Logline
	require.Equal(t, int64(1), loglineStats.NarrowedRequests)
	require.Equal(t, (5 * time.Minute).Nanoseconds(), loglineStats.HintRangesDuration, "range duration is the sum of raw hint ranges")
	require.Equal(t, 1.0, testutil.ToFloat64(metrics.hintSubRequests.WithLabelValues("narrowed")))
	require.Equal(t, 0.0, testutil.ToFloat64(metrics.hintSubRequests.WithLabelValues("skipped")))
}

func TestPrefetchFilter_ClipsHintsToInterval(t *testing.T) {
	intervalStart := time.Date(2026, 9, 8, 12, 10, 0, 0, time.UTC)
	intervalEnd := time.Date(2026, 9, 8, 12, 40, 0, 0, time.UTC)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{
			{Start: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 8, 12, 20, 0, 0, time.UTC)},
			{Start: time.Date(2026, 9, 8, 12, 30, 0, 0, time.UTC), End: time.Date(2026, 9, 8, 12, 50, 0, 0, time.UTC)},
		}},
	}

	var got *queryrange.LokiRequest
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		got = req.(*queryrange.LokiRequest)
		return emptyStreamResponse(), nil
	})

	handler := buildStack(hp, Config{RequireOptInHeader: true}, newTestMetrics(), next)
	req := newTestLokiRequest(`{job="test"} |= "error"`, intervalStart, intervalEnd)
	_, err := handler.Do(testTenantContextWithLive(), req)
	require.NoError(t, err)
	require.Equal(t, intervalStart, got.StartTs)
	require.Equal(t, intervalEnd, got.EndTs)
	require.Equal(t, []logproto.HintTimeRange{
		{Start: intervalStart, End: time.Date(2026, 9, 8, 12, 20, 0, 0, time.UTC)},
		{Start: time.Date(2026, 9, 8, 12, 30, 0, 0, time.UTC), End: intervalEnd},
	}, got.HintRanges)
}

func TestPrefetchFilter_HintsOutsideIntervalIgnored(t *testing.T) {
	intervalStart := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	intervalEnd := time.Date(2026, 9, 8, 13, 0, 0, 0, time.UTC)
	inside := hintprovider.HintTimeRange{Start: time.Date(2026, 9, 8, 12, 20, 0, 0, time.UTC), End: time.Date(2026, 9, 8, 12, 21, 0, 0, time.UTC)}
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{
			{Start: time.Date(2026, 9, 8, 11, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 8, 11, 5, 0, 0, time.UTC)},
			inside,
			{Start: time.Date(2026, 9, 8, 13, 10, 0, 0, time.UTC), End: time.Date(2026, 9, 8, 13, 15, 0, 0, time.UTC)},
		}},
	}

	var got *queryrange.LokiRequest
	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		got = req.(*queryrange.LokiRequest)
		return emptyStreamResponse(), nil
	})

	handler := buildStack(hp, Config{RequireOptInHeader: true}, newTestMetrics(), next)
	req := newTestLokiRequest(`{job="test"} |= "error"`, intervalStart, intervalEnd)
	_, err := handler.Do(testTenantContextWithLive(), req)
	require.NoError(t, err)
	require.Equal(t, 1, nextCalled)
	require.Equal(t, intervalStart, got.StartTs)
	require.Equal(t, intervalEnd, got.EndTs)
	require.Equal(t, []logproto.HintTimeRange{{Start: inside.Start, End: inside.End}}, got.HintRanges)
}

func TestPrefetchFilter_DownstreamErrorPropagated(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{
			{Start: now.Add(-45 * time.Minute), End: now.Add(-40 * time.Minute)},
			{Start: now.Add(-20 * time.Minute), End: now.Add(-15 * time.Minute)},
		}},
	}

	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		return nil, errors.New("querier boom")
	})

	handler := buildStack(hp, Config{RequireOptInHeader: true}, newTestMetrics(), next)
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)
	_, err := handler.Do(testTenantContextWithLive(), req)
	require.EqualError(t, err, "querier boom")
	require.Equal(t, 1, nextCalled)
}

func TestPrefetchFilter_PreservesRequestFields(t *testing.T) {
	intervalStart := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	intervalEnd := time.Date(2026, 9, 8, 13, 0, 0, 0, time.UTC)
	storeChunks := &logproto.ChunkRefGroup{}
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{
			{Start: time.Date(2026, 9, 8, 12, 5, 0, 0, time.UTC), End: time.Date(2026, 9, 8, 12, 6, 0, 0, time.UTC)},
			{Start: time.Date(2026, 9, 8, 12, 35, 0, 0, time.UTC), End: time.Date(2026, 9, 8, 12, 36, 0, 0, time.UTC)},
		}},
	}

	var got []*queryrange.LokiRequest
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		got = append(got, req.(*queryrange.LokiRequest))
		return emptyStreamResponse(), nil
	})

	handler := buildStack(hp, Config{RequireOptInHeader: true}, newTestMetrics(), next)
	req := &queryrange.LokiRequest{
		Query:       `{job="test"} |= "error"`,
		Limit:       42,
		Step:        1000,
		Interval:    2000,
		StartTs:     intervalStart,
		EndTs:       intervalEnd,
		Direction:   logproto.FORWARD,
		Path:        "/loki/api/v1/query_range",
		Shards:      []string{"0_of_4"},
		StoreChunks: storeChunks,
	}
	_, err := handler.Do(testTenantContextWithLive(), req)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, intervalStart, got[0].StartTs)
	require.Equal(t, intervalEnd, got[0].EndTs)
	require.Equal(t, []logproto.HintTimeRange{
		{Start: time.Date(2026, 9, 8, 12, 5, 0, 0, time.UTC), End: time.Date(2026, 9, 8, 12, 6, 0, 0, time.UTC)},
		{Start: time.Date(2026, 9, 8, 12, 35, 0, 0, time.UTC), End: time.Date(2026, 9, 8, 12, 36, 0, 0, time.UTC)},
	}, got[0].HintRanges)
	require.Equal(t, intervalStart, req.StartTs)
	require.Equal(t, intervalEnd, req.EndTs)
	require.Empty(t, req.HintRanges)
	g := got[0]
	require.Equal(t, req.Query, g.Query)
	require.Equal(t, req.Limit, g.Limit)
	require.Equal(t, req.Step, g.Step)
	require.Equal(t, req.Interval, g.Interval)
	require.Equal(t, req.Direction, g.Direction)
	require.Equal(t, req.Path, g.Path)
	require.Equal(t, req.Shards, g.Shards)
	require.Equal(t, req.StoreChunks, g.StoreChunks)
}

// --- Ingester window tests ---

func TestPrefetchFilter_IngesterWindowOnly(t *testing.T) {
	// Entire query within ingester window → pass through, no hint lookup.
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{}

	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		return emptyStreamResponse(), nil
	})

	cfg := Config{QueryIngestersWithin: 3 * time.Hour}
	handler := buildStack(hp, cfg, newTestMetrics(), next)
	ctx := testTenantContextWithLive()
	// Query last 2h — entirely within 3h ingester window.
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-2*time.Hour), now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, nextCalled, "should pass through entire query")
	require.Equal(t, 0, hp.Calls(), "should not call ProvideHints")
}

func TestPrefetchFilter_IngesterWindowPassthrough(t *testing.T) {
	// Query spans covered + ingester window. The filter should pass through
	// intervals in the ingester window but narrow covered intervals.
	now := time.Now().Truncate(time.Millisecond)
	hintRange := hintprovider.HintTimeRange{
		Start: now.Add(-5 * time.Hour),
		End:   now.Add(-4 * time.Hour),
	}

	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{hintRange},
		},
	}

	// Simulate SplitByInterval dispatching two intervals:
	// 1. Covered interval: [-6h, -3h]
	// 2. Ingester interval: [-3h, now]
	cfg := Config{QueryIngestersWithin: 3 * time.Hour}
	metrics := newTestMetrics()

	prefetchMW := prefetchMiddlewareForTest(hp, cfg, mockLimits{}, metrics, nil)

	var mu sync.Mutex
	type subReqInfo struct {
		start, end time.Time
		hints      []logproto.HintTimeRange
	}
	var gotReqs []subReqInfo

	// The filter middleware + querier for both intervals.
	filterMW := NewLoglineFilterMiddleware(10*time.Second, metrics, nil)
	querier := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		mu.Lock()
		gotReqs = append(gotReqs, subReqInfo{start: lokiReq.StartTs, end: lokiReq.EndTs, hints: lokiReq.HintRanges})
		mu.Unlock()
		return emptyStreamResponse(), nil
	})

	// Fake "SplitByInterval" that dispatches two intervals.
	splitSimulator := queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		filteredQuerier := filterMW.Wrap(querier)

		// Interval 1: covered
		covered := lokiReq.WithStartEnd(now.Add(-6*time.Hour), now.Add(-3*time.Hour))
		resp1, err := filteredQuerier.Do(ctx, covered)
		if err != nil {
			return nil, err
		}

		// Interval 2: ingester window
		ingester := lokiReq.WithStartEnd(now.Add(-3*time.Hour), now)
		resp2, err := filteredQuerier.Do(ctx, ingester)
		if err != nil {
			return nil, err
		}

		return queryrange.DefaultCodec.MergeResponse(resp1, resp2)
	})

	handler := prefetchMW.Wrap(splitSimulator)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-6*time.Hour), now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)

	// Covered interval keeps its bounds and carries the hint. The ingester
	// interval passes through with no hints.
	require.Len(t, gotReqs, 2, "should have hinted + ingester sub-requests")

	hasHinted := false
	hasIngester := false
	for _, r := range gotReqs {
		if r.end.Equal(now.Add(-3 * time.Hour)) {
			hasHinted = true
			require.Equal(t, []logproto.HintTimeRange{{Start: hintRange.Start, End: hintRange.End}}, r.hints)
		}
		if r.end.Equal(now) {
			hasIngester = true
			require.Empty(t, r.hints)
		}
	}
	require.True(t, hasHinted, "should have a hinted sub-request")
	require.True(t, hasIngester, "should have an ingester passthrough sub-request")
}

func TestPrefetchFilter_24hQueryWith3hIngesterWindow(t *testing.T) {
	// Past-24h query, 3h ingester window, full coverage.
	// Covered interval should narrow to hint range.
	// Ingester interval should pass through.
	now := time.Now().Truncate(time.Millisecond)
	hintRange := hintprovider.HintTimeRange{
		Start: now.Add(-12 * time.Hour),
		End:   now.Add(-10 * time.Hour),
	}

	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{hintRange},
		},
	}

	cfg := Config{QueryIngestersWithin: 3 * time.Hour}
	metrics := newTestMetrics()

	prefetchMW := prefetchMiddlewareForTest(hp, cfg, mockLimits{}, metrics, nil)
	filterMW := NewLoglineFilterMiddleware(10*time.Second, metrics, nil)

	var mu sync.Mutex
	type subReqInfo struct {
		start, end time.Time
		hints      []logproto.HintTimeRange
	}
	var gotReqs []subReqInfo
	querier := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		mu.Lock()
		gotReqs = append(gotReqs, subReqInfo{start: lokiReq.StartTs, end: lokiReq.EndTs, hints: lokiReq.HintRanges})
		mu.Unlock()
		return emptyStreamResponse(), nil
	})

	// Fake split: [-24h, -3h] and [-3h, now]
	splitSim := queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		fq := filterMW.Wrap(querier)
		resp1, err := fq.Do(ctx, req.WithStartEnd(now.Add(-24*time.Hour), now.Add(-3*time.Hour)))
		if err != nil {
			return nil, err
		}
		resp2, err := fq.Do(ctx, req.WithStartEnd(now.Add(-3*time.Hour), now))
		if err != nil {
			return nil, err
		}
		return queryrange.DefaultCodec.MergeResponse(resp1, resp2)
	})

	handler := prefetchMW.Wrap(splitSim)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-24*time.Hour), now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)

	// Covered interval [-24h, -3h] keeps its bounds and carries the hint.
	// Ingester interval [-3h, now] should pass through.
	require.Equal(t, 2, len(gotReqs), "expected hinted + ingester sub-requests, got %d", len(gotReqs))

	hasHinted := false
	hasIngester := false
	for _, r := range gotReqs {
		if r.end.Equal(now.Add(-3 * time.Hour)) {
			hasHinted = true
			require.Equal(t, []logproto.HintTimeRange{{Start: hintRange.Start, End: hintRange.End}}, r.hints)
		}
		if r.end.Equal(now) {
			hasIngester = true
			dur := r.end.Sub(r.start)
			require.InDelta(t, (3 * time.Hour).Seconds(), dur.Seconds(), 5)
			require.Empty(t, r.hints)
		}
	}
	require.True(t, hasHinted, "should have hinted sub-request")
	require.True(t, hasIngester, "should have ingester passthrough")
}

func TestPrefetchFilter_PassthroughIntervalsDoNotWaitForLookup(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	minDate := now.Add(-20 * time.Hour)

	for _, tc := range []struct {
		name       string
		start, end time.Time
		reason     string
	}{
		{name: "ingester window", start: now.Add(-1 * time.Hour), end: now, reason: "ingester_window"},
		{name: "straddles the ingester cutoff", start: now.Add(-4 * time.Hour), end: now.Add(-2 * time.Hour), reason: "ingester_window"},
		{name: "before min date", start: now.Add(-30 * time.Hour), end: minDate, reason: "pre_min_date"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hp := &mockHintProvider{hints: &hintprovider.Hints{}, delay: time.Minute, minDate: minDate}
			metrics := newTestMetrics()
			prefetchMW := prefetchMiddlewareForTest(hp, Config{QueryIngestersWithin: 3 * time.Hour}, mockLimits{}, metrics, nil)
			filterMW := NewLoglineFilterMiddleware(time.Minute, metrics, nil)

			var gotHints []logproto.HintTimeRange
			querier := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
				gotHints = req.(*queryrange.LokiRequest).HintRanges
				return emptyStreamResponse(), nil
			})
			// Stands in for SplitByInterval sending a single interval.
			splitSim := queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
				return filterMW.Wrap(querier).Do(ctx, req.WithStartEnd(tc.start, tc.end))
			})
			handler := prefetchMW.Wrap(splitSim)
			req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-30*time.Hour), now)

			type result struct {
				resp queryrangebase.Response
				err  error
			}
			done := make(chan result, 1)
			go func() {
				resp, err := handler.Do(testTenantContextWithLive(), req)
				done <- result{resp: resp, err: err}
			}()
			var res result
			select {
			case res = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the sub-request waited for the hint lookup")
			}
			require.NoError(t, res.err)
			require.Empty(t, gotHints)
			require.Equal(t, 1.0, testutil.ToFloat64(metrics.passthroughSubRequests.WithLabelValues(tc.reason)))

			loglineStats := res.resp.(*queryrange.LokiResponse).Statistics.Logline
			require.Equal(t, hintStatusIncomplete, loglineStats.HintStatus)
			require.Equal(t, outcomeAnsweredBeforeLookup, loglineStats.Outcome)
			require.Eventually(t, hp.SawCancel, 5*time.Second, 10*time.Millisecond, "the lookup should stop once the query returns")
		})
	}
}

func TestPrefetchFilter_LimitReachedInPassthroughInterval(t *testing.T) {
	// A query whose first interval fills the limit returns without waiting for
	// the lookup when that interval passes through, and the interval that was
	// waiting for the lookup is canceled, as SplitByInterval does.
	now := time.Now().Truncate(time.Millisecond)
	minDate := now.Add(-20 * time.Hour)

	for _, tc := range []struct {
		name                 string
		direction            logproto.Direction
		answered, waiting    [2]time.Time
		passthroughReason    string
		queryStart, queryEnd time.Time
	}{
		{
			name:              "backward query answered from the ingester window",
			direction:         logproto.BACKWARD,
			answered:          [2]time.Time{now.Add(-1 * time.Hour), now},
			waiting:           [2]time.Time{now.Add(-6 * time.Hour), now.Add(-4 * time.Hour)},
			passthroughReason: "ingester_window",
			queryStart:        now.Add(-6 * time.Hour),
			queryEnd:          now,
		},
		{
			name:              "forward query answered before the index min date",
			direction:         logproto.FORWARD,
			answered:          [2]time.Time{now.Add(-30 * time.Hour), now.Add(-24 * time.Hour)},
			waiting:           [2]time.Time{now.Add(-12 * time.Hour), now.Add(-10 * time.Hour)},
			passthroughReason: "pre_min_date",
			queryStart:        now.Add(-30 * time.Hour),
			queryEnd:          now.Add(-10 * time.Hour),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hp := &mockHintProvider{hints: &hintprovider.Hints{}, delay: time.Minute, minDate: minDate}
			metrics := newTestMetrics()
			prefetchMW := prefetchMiddlewareForTest(hp, Config{QueryIngestersWithin: 3 * time.Hour}, mockLimits{}, metrics, nil)
			filterMW := NewLoglineFilterMiddleware(5*time.Second, metrics, nil)

			var querierCalls atomic.Int64
			querier := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
				querierCalls.Add(1)
				return streamResponseWithEntries(logproto.Entry{Timestamp: req.GetEnd().Add(-time.Minute), Line: "needle"}), nil
			})
			var waitingErr error
			splitSim := queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				filtered := filterMW.Wrap(querier)
				waiting := make(chan error, 1)
				go func() {
					_, err := filtered.Do(ctx, req.WithStartEnd(tc.waiting[0], tc.waiting[1]))
					waiting <- err
				}()
				resp, err := filtered.Do(ctx, req.WithStartEnd(tc.answered[0], tc.answered[1]))
				cancel()
				waitingErr = <-waiting
				return resp, err
			})
			handler := prefetchMW.Wrap(splitSim)

			req := newTestLokiRequest(`{job="test"} |= "needle"`, tc.queryStart, tc.queryEnd)
			req.Direction = tc.direction
			resp, err := handler.Do(testTenantContextWithLive(), req)
			require.NoError(t, err)
			require.Equal(t, int64(1), resp.(*queryrange.LokiResponse).Count())
			require.ErrorIs(t, waitingErr, context.Canceled)
			require.Equal(t, int64(1), querierCalls.Load(), "only the pass-through interval reaches a querier")
			require.Equal(t, 1.0, testutil.ToFloat64(metrics.passthroughSubRequests.WithLabelValues(tc.passthroughReason)))
			require.Equal(t, outcomeAnsweredBeforeLookup, resp.(*queryrange.LokiResponse).Statistics.Logline.Outcome)

			require.Eventually(t, func() bool {
				return testutil.ToFloat64(metrics.hintLookupsCanceled) == 1
			}, 5*time.Second, 10*time.Millisecond, "the lookup should stop once the query returns")
			var m dto.Metric
			require.NoError(t, metrics.hintProviderDuration.Write(&m))
			require.Zero(t, m.GetHistogram().GetSampleCount(), "a canceled lookup has no lookup duration")
		})
	}
}

func TestPrefetchFilter_LookupErrorDoesNotRelabelPassthroughIntervals(t *testing.T) {
	// The ingester-window interval doesn't wait for the lookup, so it counts as
	// an ingester-window pass-through even when the lookup fails. Only the
	// interval that waited counts as an error pass-through.
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{err: errors.New("index unavailable")}
	metrics := newTestMetrics()
	prefetchMW := prefetchMiddlewareForTest(hp, Config{QueryIngestersWithin: 3 * time.Hour}, mockLimits{}, metrics, nil)
	filterMW := NewLoglineFilterMiddleware(5*time.Second, metrics, nil)

	var querierCalls atomic.Int64
	var gotHints atomic.Int64
	querier := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		querierCalls.Inc()
		gotHints.Add(int64(len(req.(*queryrange.LokiRequest).HintRanges)))
		return emptyStreamResponse(), nil
	})
	splitSim := queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		filtered := filterMW.Wrap(querier)
		if _, err := filtered.Do(ctx, req.WithStartEnd(now.Add(-6*time.Hour), now.Add(-4*time.Hour))); err != nil {
			return nil, err
		}
		return filtered.Do(ctx, req.WithStartEnd(now.Add(-1*time.Hour), now))
	})
	handler := prefetchMW.Wrap(splitSim)

	resp, err := handler.Do(testTenantContextWithLive(), newTestLokiRequest(`{job="test"} |= "needle"`, now.Add(-6*time.Hour), now))
	require.NoError(t, err)
	require.Equal(t, int64(2), querierCalls.Load())
	require.Zero(t, gotHints.Load())
	require.Equal(t, 1.0, testutil.ToFloat64(metrics.passthroughSubRequests.WithLabelValues("ingester_window")))
	require.Equal(t, 1.0, testutil.ToFloat64(metrics.hintPassthrough.WithLabelValues("error")))

	loglineStats := resp.(*queryrange.LokiResponse).Statistics.Logline
	require.Equal(t, hintStatusError, loglineStats.HintStatus)
	require.Equal(t, outcomeLookupFailed, loglineStats.Outcome)
}

func TestPrefetchFilter_PreMinDateOnly_SkipsLookup(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{minDate: now.Add(-24 * time.Hour)}

	var nextCalled atomic.Int64
	var gotHints []logproto.HintTimeRange
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled.Inc()
		gotHints = req.(*queryrange.LokiRequest).HintRanges
		resp := emptyStreamResponse()
		resp.Statistics.Index.ShardPlannedChunks = 40
		resp.Statistics.Index.ShardPlannedChunksScanned = 40
		return resp, nil
	})
	metrics := newTestMetrics()
	handler := buildStack(hp, Config{RequireOptInHeader: true}, metrics, next)
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-48*time.Hour), now.Add(-30*time.Hour))
	resp, err := handler.Do(testTenantContextWithLive(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1), nextCalled.Load())
	require.Empty(t, gotHints)
	require.Equal(t, 0, hp.Calls(), "nothing in the query is indexed")
	require.Equal(t, 1.0, testutil.ToFloat64(metrics.passthroughSubRequests.WithLabelValues("pre_min_date")))

	loglineStats := resp.(*queryrange.LokiResponse).Statistics.Logline
	require.Equal(t, hintStatusOK, loglineStats.HintStatus)
	require.Equal(t, int64(1), loglineStats.HintRanges, "the pre-min-date range the lookup would have returned")
	require.Zero(t, loglineStats.HintRangesDuration)
	require.Equal(t, outcomeTimeRangeNotIndexed, loglineStats.Outcome)
}

func TestPrefetchFilter_LoglineStatsAcrossSkipNarrowPassthrough(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintRange := hintprovider.HintTimeRange{
		Start: now.Add(-6 * time.Hour),
		End:   now.Add(-5 * time.Hour),
	}

	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{hintRange}},
	}

	cfg := Config{QueryIngestersWithin: 2 * time.Hour}
	metrics := newTestMetrics()
	prefetchMW := prefetchMiddlewareForTest(hp, cfg, mockLimits{}, metrics, nil)
	filterMW := NewLoglineFilterMiddleware(10*time.Second, metrics, nil)

	var prefetchResult *hintPrefetchResult
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return emptyStreamResponse(), nil
	})

	// Simulate SplitByInterval producing three intervals:
	// 1) covered but no overlap with hints => skipped
	// 2) covered overlap => narrowed
	// 3) ingester window => passthrough
	splitSim := queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		prefetchResult = hintPrefetchFromContext(ctx)
		require.NotNil(t, prefetchResult)

		filtered := filterMW.Wrap(next)
		resp1, err := filtered.Do(ctx, req.WithStartEnd(now.Add(-8*time.Hour), now.Add(-7*time.Hour)))
		require.NoError(t, err)
		resp2, err := filtered.Do(ctx, req.WithStartEnd(now.Add(-7*time.Hour), now.Add(-4*time.Hour)))
		require.NoError(t, err)
		resp3, err := filtered.Do(ctx, req.WithStartEnd(now.Add(-1*time.Hour), now))
		require.NoError(t, err)
		return queryrange.DefaultCodec.MergeResponse(resp1, resp2, resp3)
	})

	handler := prefetchMW.Wrap(splitSim)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-8*time.Hour), now)
	resp, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, prefetchResult)

	loglineStats := resp.(*queryrange.LokiResponse).Statistics.Logline
	require.Equal(t, hintStatusOK, loglineStats.HintStatus)
	require.Equal(t, int64(1), loglineStats.HintRanges)
	require.Equal(t, time.Hour.Nanoseconds(), loglineStats.HintRangesDuration)
	require.Equal(t, int64(1), loglineStats.SkippedRequests)
	require.Equal(t, int64(1), loglineStats.NarrowedRequests, "the ingester-window passthrough is neither skipped nor narrowed")
}

func TestPrefetchFilter_AttachesLoglineStatsInsteadOfImpactLine(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{
					Start: now.Add(-45 * time.Minute),
					End:   now.Add(-15 * time.Minute),
				},
			},
		},
	}

	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return emptyStreamResponse(), nil
	})

	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	handler := buildStackWithLogger(hp, Config{RequireOptInHeader: true}, newTestMetrics(), logger, next)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)
	resp, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Empty(t, findLogLine(logs.String(), "query hint impact"))

	loglineStats := resp.(*queryrange.LokiResponse).Statistics.Logline
	require.Equal(t, hintStatusOK, loglineStats.HintStatus)
	require.Equal(t, int64(1), loglineStats.HintRanges)
	require.Equal(t, (30 * time.Minute).Nanoseconds(), loglineStats.HintRangesDuration)
	require.Zero(t, loglineStats.SkippedRequests)
	require.Equal(t, int64(1), loglineStats.NarrowedRequests)
}

func TestPrefetchFilter_LoglineHintStatus(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hints := &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{
		{Start: now.Add(-45 * time.Minute), End: now.Add(-15 * time.Minute)},
	}}

	for _, tc := range []struct {
		name                 string
		err                  error
		queryIngestersWithin time.Duration
		want                 string
	}{
		{name: "ok", want: hintStatusOK},
		{name: "unsupported", err: hintprovider.ErrUnsupported, want: hintStatusUnsupported},
		{name: "error", err: errors.New("index unavailable"), want: hintStatusError},
		{name: "canceled", err: context.Canceled, want: hintStatusCanceled},
		{name: "whole query in ingester window", queryIngestersWithin: 2 * time.Hour, want: hintStatusIngesterWindow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hp := &mockHintProvider{hints: hints, err: tc.err}
			next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				return emptyStreamResponse(), nil
			})
			cfg := Config{RequireOptInHeader: true, QueryIngestersWithin: tc.queryIngestersWithin}
			handler := buildStack(hp, cfg, newTestMetrics(), next)
			req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

			resp, err := handler.Do(testTenantContextWithLive(), req)
			require.NoError(t, err)
			loglineStats := resp.(*queryrange.LokiResponse).Statistics.Logline
			require.Equal(t, tc.want, loglineStats.HintStatus)
		})
	}
}

func TestPrefetchFilter_LoglineOutcome(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	indexedHints := &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{
		{Start: now.Add(-45 * time.Minute), End: now.Add(-15 * time.Minute)},
	}}
	preMinDateHints := &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{
		{End: now.Add(30 * time.Minute), Source: hintprovider.HintSourcePreMinDate},
	}}

	for _, tc := range []struct {
		name                 string
		ctx                  context.Context
		query                string
		ngramLength          int
		minQueryBytes        int64
		indexStatsErr        error
		hints                *hintprovider.Hints
		hintErr              error
		queryIngestersWithin time.Duration
		plannedChunks        int64
		scannedChunks        int64
		wantOutcome          string // empty when no logline stats are expected
		wantRatio            float64
	}{
		{name: "data skipped", plannedChunks: 40, scannedChunks: 4, wantOutcome: outcomeDataSkipped, wantRatio: 0.9},
		{name: "exactly at the tiny threshold", plannedChunks: 40, scannedChunks: 30, wantOutcome: outcomeDataSkipped, wantRatio: 0.25},
		{name: "tiny data skipped", plannedChunks: 40, scannedChunks: 35, wantOutcome: outcomeTinyDataSkipped, wantRatio: 0.125},
		{name: "no shard planning", wantOutcome: outcomeSkipRatioUnavailable},
		{name: "every sub-request passed through", hints: preMinDateHints, plannedChunks: 40, scannedChunks: 40, wantOutcome: outcomeTimeRangeNotIndexed},
		{name: "whole query in ingester window", queryIngestersWithin: 2 * time.Hour, plannedChunks: 40, scannedChunks: 40, wantOutcome: outcomeRecentDataOnly},
		{name: "lookup reports unsupported query", hintErr: hintprovider.ErrUnsupported, wantOutcome: outcomeUnsupportedQuery},
		{name: "lookup failed", hintErr: errors.New("index unavailable"), wantOutcome: outcomeLookupFailed},
		{name: "lookup canceled", hintErr: context.Canceled, wantOutcome: outcomeCanceled},
		{name: "search term shorter than the n-gram length", query: `{job="test"} |= "err"`, ngramLength: 6, plannedChunks: 40, scannedChunks: 40, wantOutcome: outcomeUnsupportedQuery},
		{name: "query below the byte threshold", minQueryBytes: 500, plannedChunks: 40, scannedChunks: 40, wantOutcome: outcomeQueryTooSmall},
		{name: "query size estimate failed", minQueryBytes: 500, indexStatsErr: errors.New("stats unavailable"), wantOutcome: outcomeSizeEstimateFailed},
		{name: "dry run skip records nothing", ctx: testTenantContextWithDryRun(), minQueryBytes: 500},
		{name: "logline not enabled", ctx: testTenantContext()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hints := tc.hints
			if hints == nil {
				hints = indexedHints
			}
			hp := &mockHintProvider{hints: hints, err: tc.hintErr}
			next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				resp := emptyStreamResponse()
				resp.Statistics.Index.ShardPlannedChunks = tc.plannedChunks
				resp.Statistics.Index.ShardPlannedChunksScanned = tc.scannedChunks
				return resp, nil
			})
			querier := buildStatsAwareQuerier(&logproto.IndexStatsResponse{Bytes: 100}, tc.indexStatsErr, nil, next)
			cfg := Config{RequireOptInHeader: true, NgramLength: tc.ngramLength, QueryIngestersWithin: tc.queryIngestersWithin}
			handler := buildStack(hp, cfg, newTestMetrics(), querier, mockLimits{defaultMinQueryBytes: tc.minQueryBytes})
			ctx := tc.ctx
			if ctx == nil {
				ctx = testTenantContextWithLive()
			}
			query := tc.query
			if query == "" {
				query = `{job="test"} |= "error"`
			}

			resp, err := handler.Do(ctx, newTestLokiRequest(query, now.Add(-1*time.Hour), now))
			require.NoError(t, err)

			loglineStats := resp.(*queryrange.LokiResponse).Statistics.Logline
			if tc.wantOutcome == "" {
				require.Nil(t, loglineStats)
				return
			}
			require.NotNil(t, loglineStats)
			require.Equal(t, tc.wantOutcome, loglineStats.Outcome)
			require.Equal(t, tc.plannedChunks, loglineStats.PlannedChunks)
			require.Equal(t, tc.scannedChunks, loglineStats.ScannedChunks)
			require.InDelta(t, tc.wantRatio, loglineStats.ChunkFilterRatio, 1e-9)
		})
	}
}

func TestPrefetchFilter_LoglineLookupIOStats(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{
			{Start: now.Add(-45 * time.Minute), End: now.Add(-15 * time.Minute)},
		}},
		stats: hintprovider.FromProtoStats(&logproto.HintQueryStats{
			HeaderReads:   5,
			MetadataReads: 7,
			TotalIOBytes:  2048,
			TotalIOWait:   40 * time.Millisecond,
		}),
	}
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return emptyStreamResponse(), nil
	})
	handler := buildStack(hp, Config{RequireOptInHeader: true}, newTestMetrics(), next)

	resp, err := handler.Do(testTenantContextWithLive(), newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now))
	require.NoError(t, err)

	loglineStats := resp.(*queryrange.LokiResponse).Statistics.Logline
	require.Equal(t, int64(12), loglineStats.ObjectRequests, "header plus metadata reads")
	require.Equal(t, int64(2048), loglineStats.IOBytes)
	require.Equal(t, (40 * time.Millisecond).Nanoseconds(), loglineStats.IOWait)
}

func TestWaitTracker(t *testing.T) {
	at := func(ms int64) time.Time { return time.UnixMilli(ms) }
	type event struct {
		begin bool
		ms    int64
	}
	begin := func(ms int64) event { return event{begin: true, ms: ms} }
	end := func(ms int64) event { return event{ms: ms} }

	for _, tc := range []struct {
		name   string
		events []event
		now    int64
		want   time.Duration
	}{
		{name: "no wait", now: 100},
		{name: "overlapping waits count once", events: []event{begin(0), begin(10), end(30), end(40)}, now: 100, want: 40 * time.Millisecond},
		{name: "waits in sequence add up", events: []event{begin(0), end(15), begin(20), end(35)}, now: 100, want: 30 * time.Millisecond},
		{name: "a wait in progress counts up to now", events: []event{begin(0), end(10), begin(50)}, now: 70, want: 30 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var w waitTracker
			for _, e := range tc.events {
				if e.begin {
					w.begin(at(e.ms))
				} else {
					w.end(at(e.ms))
				}
			}
			require.Equal(t, tc.want, w.duration(at(tc.now)))
		})
	}
}

// hintWaitStack wires prefetch, split, filter and querier, where split decides
// how the query's sub-requests reach the filter.
func hintWaitStack(
	hp *mockHintProvider,
	metrics *Metrics,
	filterTimeout time.Duration,
	split func(ctx context.Context, req queryrangebase.Request, filter queryrangebase.Handler) (queryrangebase.Response, error),
) queryrangebase.Handler {
	querier := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return emptyStreamResponse(), nil
	})
	filter := NewLoglineFilterMiddleware(filterTimeout, metrics, nil).Wrap(querier)
	cfg := Config{RequireOptInHeader: true}
	prefetch := prefetchMiddlewareForTest(hp, cfg, mockLimits{}, metrics, nil)
	return prefetch.Wrap(queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		return split(ctx, req, filter)
	}))
}

func TestPrefetchFilter_HintWait(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hints := &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{
		{Start: now.Add(-45 * time.Minute), End: now.Add(-15 * time.Minute)},
	}}
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)
	waitForHints := func(ctx context.Context) { <-hintPrefetchFromContext(ctx).done }

	t.Run("hints ready before any sub-request record a zero wait", func(t *testing.T) {
		metrics := newTestMetrics()
		handler := hintWaitStack(&mockHintProvider{hints: hints}, metrics, 10*time.Second,
			func(ctx context.Context, req queryrangebase.Request, filter queryrangebase.Handler) (queryrangebase.Response, error) {
				waitForHints(ctx)
				return filter.Do(ctx, req)
			})

		resp, err := handler.Do(testTenantContextWithLive(), req)
		require.NoError(t, err)
		require.Zero(t, resp.(*queryrange.LokiResponse).Statistics.Logline.HintWaitTime)
		count, sum := hintWaitObservations(t, metrics, hintStatusOK)
		require.Equal(t, uint64(1), count, "a query that never waited still records a zero")
		require.Zero(t, sum)
	})

	t.Run("parallel sub-requests count once", func(t *testing.T) {
		metrics := newTestMetrics()
		release := make(chan struct{})
		hp := &mockHintProvider{hints: hints, release: release}
		handler := hintWaitStack(hp, metrics, 10*time.Second,
			func(ctx context.Context, req queryrangebase.Request, filter queryrangebase.Handler) (queryrangebase.Response, error) {
				resps := make([]queryrangebase.Response, 4)
				errs := make([]error, len(resps))
				var wg sync.WaitGroup
				for i := range resps {
					wg.Go(func() { resps[i], errs[i] = filter.Do(ctx, req) })
				}
				releaseWhenWaiting(hintPrefetchFromContext(ctx), len(resps), release)
				wg.Wait()
				if err := errors.Join(errs...); err != nil {
					return nil, err
				}
				return queryrange.DefaultCodec.MergeResponse(resps...)
			})

		start := time.Now()
		resp, err := handler.Do(testTenantContextWithLive(), req)
		elapsed := time.Since(start)
		require.NoError(t, err)

		wait := time.Duration(resp.(*queryrange.LokiResponse).Statistics.Logline.HintWaitTime)
		require.Positive(t, wait)
		require.LessOrEqual(t, wait, elapsed, "overlapping waits must not add up")
		count, sum := hintWaitObservations(t, metrics, hintStatusOK)
		require.Equal(t, uint64(1), count)
		require.Equal(t, wait.Seconds(), sum)
	})

	t.Run("each wave of sub-requests pays the timeout", func(t *testing.T) {
		const timeout = 20 * time.Millisecond
		metrics := newTestMetrics()
		hp := &mockHintProvider{hints: hints, delay: time.Minute}
		handler := hintWaitStack(hp, metrics, timeout,
			func(ctx context.Context, req queryrangebase.Request, filter queryrangebase.Handler) (queryrangebase.Response, error) {
				first, err := filter.Do(ctx, req)
				if err != nil {
					return nil, err
				}
				second, err := filter.Do(ctx, req)
				if err != nil {
					return nil, err
				}
				return queryrange.DefaultCodec.MergeResponse(first, second)
			})
		ctx, cancel := context.WithCancel(testTenantContextWithLive())
		defer cancel()

		resp, err := handler.Do(ctx, req)
		require.NoError(t, err)

		loglineStats := resp.(*queryrange.LokiResponse).Statistics.Logline
		require.Equal(t, hintStatusIncomplete, loglineStats.HintStatus)
		require.GreaterOrEqual(t, time.Duration(loglineStats.HintWaitTime), 2*timeout)
		count, _ := hintWaitObservations(t, metrics, hintStatusIncomplete)
		require.Equal(t, uint64(1), count)
	})

	t.Run("a failed query is still observed", func(t *testing.T) {
		metrics := newTestMetrics()
		handler := hintWaitStack(&mockHintProvider{hints: hints}, metrics, 10*time.Second,
			func(ctx context.Context, _ queryrangebase.Request, _ queryrangebase.Handler) (queryrangebase.Response, error) {
				waitForHints(ctx)
				return nil, errors.New("querier unavailable")
			})

		_, err := handler.Do(testTenantContextWithLive(), req)
		require.Error(t, err)
		count, _ := hintWaitObservations(t, metrics, hintStatusOK)
		require.Equal(t, uint64(1), count)
	})

	t.Run("dry run is not observed", func(t *testing.T) {
		metrics := newTestMetrics()
		handler := hintWaitStack(&mockHintProvider{hints: hints}, metrics, 10*time.Second,
			func(ctx context.Context, req queryrangebase.Request, filter queryrangebase.Handler) (queryrangebase.Response, error) {
				return filter.Do(ctx, req)
			})

		_, err := handler.Do(testTenantContextWithDryRun(), req)
		require.NoError(t, err)
		require.Zero(t, testutil.CollectAndCount(metrics.hintWaitDuration))
	})
}

// --- rangesOverlapping tests ---

func TestRangesOverlapping(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ranges := []hintprovider.HintTimeRange{
		{Start: base.Add(10 * time.Minute), End: base.Add(20 * time.Minute)},
		{Start: base.Add(30 * time.Minute), End: base.Add(40 * time.Minute)},
		{Start: base.Add(50 * time.Minute), End: base.Add(60 * time.Minute)},
	}

	tests := []struct {
		name  string
		start time.Time
		end   time.Time
		want  int
	}{
		{"before all", base, base.Add(5 * time.Minute), 0},
		{"after all", base.Add(70 * time.Minute), base.Add(80 * time.Minute), 0},
		{"overlaps first", base.Add(15 * time.Minute), base.Add(25 * time.Minute), 1},
		{"overlaps all", base, base.Add(70 * time.Minute), 3},
		{"between ranges", base.Add(21 * time.Minute), base.Add(29 * time.Minute), 0},
		{"exact match", base.Add(30 * time.Minute), base.Add(40 * time.Minute), 1},
		{"overlaps two", base.Add(15 * time.Minute), base.Add(35 * time.Minute), 2},
		{"empty ranges", base, base.Add(70 * time.Minute), 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rangesOverlapping(ranges, tc.start, tc.end)
			require.Len(t, got, tc.want)
			require.Equal(t, tc.want > 0, rangesOverlap(ranges, tc.start, tc.end))
		})
	}

	// Empty input.
	require.Nil(t, rangesOverlapping(nil, base, base.Add(time.Hour)))
	require.False(t, rangesOverlap(nil, base, base.Add(time.Hour)))
}

func TestRangesOverlap_ExclusiveHintEnd(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	hint := []hintprovider.HintTimeRange{{
		Start: base,
		End:   base.Add(5 * time.Minute),
	}}

	// Lookback (5m, 10m] shares nothing with [0, 5m).
	require.False(t, rangesOverlap(hint, base.Add(5*time.Minute), base.Add(10*time.Minute)))
	// A hint that starts at the lookback end includes that instant.
	require.True(t, rangesOverlap(hint, base.Add(-time.Minute), base))
}

func testTime(n int) time.Time {
	return time.Date(2025, 1, 1, 0, 0, n, 0, time.UTC)
}

func TestEmptyLokiResponse(t *testing.T) {
	tests := []struct {
		name      string
		req       *queryrange.LokiRequest
		wantDir   logproto.Direction
		wantLimit uint32
		wantVer   uint32
	}{
		{
			name: "forward query with limit",
			req: &queryrange.LokiRequest{
				Direction: logproto.FORWARD,
				Limit:     1000,
				Path:      "/loki/api/v1/query_range",
			},
			wantDir:   logproto.FORWARD,
			wantLimit: 1000,
			wantVer:   uint32(loghttp.VersionV1),
		},
		{
			name: "backward query with limit",
			req: &queryrange.LokiRequest{
				Direction: logproto.BACKWARD,
				Limit:     500,
				Path:      "/loki/api/v1/query_range",
			},
			wantDir:   logproto.BACKWARD,
			wantLimit: 500,
			wantVer:   uint32(loghttp.VersionV1),
		},
		{
			name: "legacy path",
			req: &queryrange.LokiRequest{
				Direction: logproto.FORWARD,
				Limit:     42,
				Path:      "/api/prom/query",
			},
			wantDir:   logproto.FORWARD,
			wantLimit: 42,
			wantVer:   uint32(loghttp.VersionLegacy),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := emptyLokiResponse(tc.req)

			require.Equal(t, "success", resp.Status)
			require.Equal(t, tc.wantDir, resp.Direction)
			require.Equal(t, tc.wantLimit, resp.Limit)
			require.Equal(t, tc.wantVer, resp.Version)
			require.Equal(t, loghttp.ResultTypeStream, resp.Data.ResultType)
			require.Empty(t, resp.Data.Result)
		})
	}
}

func TestEmptyLokiResponseMergesSafely(t *testing.T) {
	req := &queryrange.LokiRequest{
		Direction: logproto.FORWARD,
		Limit:     100,
		Path:      "/loki/api/v1/query_range",
	}

	empty := emptyLokiResponse(req)
	realResp := &queryrange.LokiResponse{
		Status:    "success",
		Direction: logproto.FORWARD,
		Limit:     100,
		Version:   uint32(loghttp.VersionV1),
		Data: queryrange.LokiData{
			ResultType: loghttp.ResultTypeStream,
			Result: []logproto.Stream{
				{
					Labels: `{app="test"}`,
					Entries: []logproto.Entry{
						{Timestamp: testTime(1), Line: "line1"},
						{Timestamp: testTime(2), Line: "line2"},
					},
				},
			},
		},
	}

	merged, err := queryrange.DefaultCodec.MergeResponse(empty, realResp)
	require.NoError(t, err)

	lokiMerged := merged.(*queryrange.LokiResponse)
	require.Equal(t, logproto.FORWARD, lokiMerged.Direction, "direction must match the original request")
	require.Equal(t, uint32(100), lokiMerged.Limit, "limit must match the original request")
	require.Equal(t, uint32(loghttp.VersionV1), lokiMerged.Version, "version must match the original request")
	require.Len(t, lokiMerged.Data.Result, 1, "real stream entries must survive the merge")
	require.Len(t, lokiMerged.Data.Result[0].Entries, 2)
}

// Metric range queries are verified in dry-run from the matrix already returned
// to the user. Hint lookup still uses the request bounds widened by the range
// interval and offset, matching getQueryBytes. A positive sample is correct
// when its lookback window overlaps a hint. Live mode and instant queries are unchanged.
func TestDryRun_MetricRange_PositiveStepOverlapsWidenedHint(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		interval time.Duration
		offset   time.Duration
	}{
		{
			name:     "sum of count_over_time",
			query:    `sum(count_over_time({app="api"} |= "timeout" [5m]))`,
			interval: 5 * time.Minute,
		},
		{
			name:     "offset widens the start and shifts the end",
			query:    `sum(count_over_time({app="api"} |= "timeout" [5m] offset 10m))`,
			interval: 5 * time.Minute,
			offset:   10 * time.Minute,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().Truncate(time.Millisecond)
			reqStart := now.Add(-1 * time.Hour)
			lookupFrom := reqStart.Add(-(tc.interval + tc.offset))
			lookupThrough := now.Add(-tc.offset)

			hp := &mockHintProvider{
				hints: &hintprovider.Hints{
					TimeRanges: []hintprovider.HintTimeRange{
						{Start: lookupFrom, End: lookupThrough},
					},
				},
			}

			userResp := metricMatrixResponse(1, now)
			var userReq *queryrange.LokiRequest
			nextCalled := 0
			next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
				nextCalled++
				time.Sleep(25 * time.Millisecond)
				userReq = req.(*queryrange.LokiRequest)
				return userResp, nil
			})

			var logs bytes.Buffer
			handler := buildStackWithLogger(hp, Config{DryRun: true, NgramLength: 6}, newTestMetrics(), log.NewLogfmtLogger(&logs), next)
			req := newTestLokiRequest(tc.query, reqStart, now)
			resp, err := handler.Do(testTenantContext(), req)
			require.NoError(t, err)

			require.Same(t, userResp, resp, "dry-run returns the unhinted result")
			require.Equal(t, 1, nextCalled, "dry-run runs the query once")
			require.NotNil(t, userReq)
			require.Equal(t, reqStart, userReq.StartTs)
			require.Equal(t, now, userReq.EndTs)
			require.Empty(t, userReq.HintRanges)
			require.Empty(t, req.HintRanges)

			require.Equal(t, 1, hp.Calls())
			gotFrom, gotThrough := hp.Window()
			require.Equal(t, model.TimeFromUnixNano(lookupFrom.UnixNano()), gotFrom)
			require.Equal(t, model.TimeFromUnixNano(lookupThrough.UnixNano()), gotThrough)

			logLine := findLogLine(logs.String(), "logline dry-run verification")
			require.NotEmpty(t, logLine)
			require.Contains(t, logLine, "correct=true")
			require.Contains(t, logLine, "query_type=metric")
			require.Contains(t, logLine, "total_entries=1")
			require.Contains(t, logLine, "covered_entries=1")
			require.Contains(t, logLine, "false_negatives=0")
			require.NotContains(t, logLine, "false_negative_ranges=")
			summary := summarizeDryRunHints([]hintprovider.HintTimeRange{{Start: lookupFrom, End: lookupThrough}}, lookupFrom, lookupThrough, lookupThrough, logproto.FORWARD)
			require.Contains(t, logLine, "hint_total_seconds="+summary.hintTotalSeconds, "the summary covers the widened lookup, including lookback before the request start")
		})
	}
}

func TestDryRun_MetricRange_StepExtendsLookup(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	reqStart := now.Add(-1 * time.Hour)
	step := time.Minute
	interval := 5 * time.Minute
	alignedStart, alignedEnd := alignMetricStep(reqStart, now, step.Milliseconds())
	lookupFrom := alignedStart.Add(-interval)
	lookupThrough := alignedEnd

	var statsFrom, statsThrough model.Time
	querier := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		switch r := req.(type) {
		case *logproto.IndexStatsRequest:
			statsFrom, statsThrough = r.From, r.Through
			return &queryrange.IndexStatsResponse{Response: &logproto.IndexStatsResponse{Bytes: 1000}}, nil
		default:
			time.Sleep(25 * time.Millisecond)
			return metricMatrixResponse(1, now), nil
		}
	})

	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{{Start: lookupFrom, End: lookupThrough}},
		},
	}
	handler := buildStack(hp, Config{DryRun: true, NgramLength: 6}, newTestMetrics(), querier, mockLimits{defaultMinQueryBytes: 1})
	req := newTestLokiRequest(`sum(count_over_time({app="api"} |= "timeout" [5m]))`, reqStart, now)
	req.Step = step.Milliseconds()
	_, err := handler.Do(testTenantContext(), req)
	require.NoError(t, err)

	require.Equal(t, model.Time(lookupFrom.UnixMilli()), statsFrom)
	require.Equal(t, model.Time(lookupThrough.UnixMilli()), statsThrough)
	gotFrom, gotThrough := hp.Window()
	require.Equal(t, model.TimeFromUnixNano(lookupFrom.UnixNano()), gotFrom)
	require.Equal(t, model.TimeFromUnixNano(lookupThrough.UnixNano()), gotThrough)
}

func TestDryRun_LogQuery_StepDoesNotExtendLookupStart(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	reqStart := now.Add(-1 * time.Hour)
	hp := &mockHintProvider{hints: &hintprovider.Hints{}}
	querier := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		if _, ok := req.(*logproto.IndexStatsRequest); ok {
			return &queryrange.IndexStatsResponse{Response: &logproto.IndexStatsResponse{Bytes: 1000}}, nil
		}
		time.Sleep(25 * time.Millisecond)
		return emptyStreamResponse(), nil
	})
	handler := buildStack(hp, Config{DryRun: true, NgramLength: 6}, newTestMetrics(), querier, mockLimits{defaultMinQueryBytes: 1})
	req := newTestLokiRequest(`{app="api"} |= "timeout"`, reqStart, now)
	req.Step = time.Minute.Milliseconds()
	_, err := handler.Do(testTenantContext(), req)
	require.NoError(t, err)

	gotFrom, gotThrough := hp.Window()
	require.Equal(t, model.TimeFromUnixNano(reqStart.UnixNano()), gotFrom)
	require.Equal(t, model.TimeFromUnixNano(now.UnixNano()), gotThrough)
}

func TestDryRun_MetricRange_EmptyHintsMissPositiveStep(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	reqStart := now.Add(-1 * time.Hour)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: nil},
	}

	userResp := metricMatrixResponse(1, now)
	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		time.Sleep(25 * time.Millisecond)
		lokiReq := req.(*queryrange.LokiRequest)
		require.Empty(t, lokiReq.HintRanges)
		return userResp, nil
	})

	var logs bytes.Buffer
	handler := buildStackWithLogger(hp, Config{DryRun: true, NgramLength: 6}, newTestMetrics(), log.NewLogfmtLogger(&logs), next)
	resp, err := handler.Do(testTenantContext(), newTestLokiRequest(`sum(count_over_time({app="api"} |= "timeout" [5m]))`, reqStart, now))
	require.NoError(t, err)

	require.Same(t, userResp, resp, "dry-run returns the unhinted result")
	require.Equal(t, 1, nextCalled, "dry-run runs the query once")
	require.Equal(t, 1, hp.Calls())
	gotFrom, gotThrough := hp.Window()
	require.Equal(t, model.TimeFromUnixNano(reqStart.Add(-5*time.Minute).UnixNano()), gotFrom)
	require.Equal(t, model.TimeFromUnixNano(now.UnixNano()), gotThrough)

	logLine := findLogLine(logs.String(), "logline dry-run verification")
	require.NotEmpty(t, logLine)
	require.Contains(t, logLine, "correct=false", "a positive sample whose lookback is uncovered is a miss")
	require.Contains(t, logLine, "query_type=metric")
	require.Contains(t, logLine, "total_entries=1")
	require.Contains(t, logLine, "covered_entries=0")
	require.Contains(t, logLine, "false_negatives=1")
	require.Contains(t, logLine, "false_negative_ranges=")
	require.Contains(t, logLine, hintprovider.FormatHintRanges([]hintprovider.HintTimeRange{{
		Start: now.Add(-5 * time.Minute),
		End:   now,
	}}))
	require.NotContains(t, logLine, "non-Loki response")
}

func TestDryRun_MetricRange_PartialHintCoversLookback(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	reqStart := now.Add(-1 * time.Hour)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-5 * time.Minute), End: now.Add(-4 * time.Minute)},
			},
		},
	}

	userResp := metricMatrixResponse(1, now)
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		time.Sleep(25 * time.Millisecond)
		return userResp, nil
	})

	var logs bytes.Buffer
	handler := buildStackWithLogger(hp, Config{DryRun: true, NgramLength: 6}, newTestMetrics(), log.NewLogfmtLogger(&logs), next)
	resp, err := handler.Do(testTenantContext(), newTestLokiRequest(`sum(count_over_time({app="api"} |= "timeout" [5m]))`, reqStart, now))
	require.NoError(t, err)
	require.Same(t, userResp, resp)

	logLine := findLogLine(logs.String(), "logline dry-run verification")
	require.NotEmpty(t, logLine)
	require.Contains(t, logLine, "correct=true", "any hint in the lookback counts as covered")
	require.Contains(t, logLine, "total_entries=1")
	require.Contains(t, logLine, "covered_entries=1")
	require.Contains(t, logLine, "false_negatives=0")
	require.NotContains(t, logLine, "false_negative_ranges=")
}

func TestDryRun_MetricRange_HintOutsideLookbackIsMiss(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	reqStart := now.Add(-1 * time.Hour)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-10 * time.Minute), End: now.Add(-6 * time.Minute)},
			},
		},
	}

	userResp := metricMatrixResponse(1, now)
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		time.Sleep(25 * time.Millisecond)
		return userResp, nil
	})

	var logs bytes.Buffer
	handler := buildStackWithLogger(hp, Config{DryRun: true, NgramLength: 6}, newTestMetrics(), log.NewLogfmtLogger(&logs), next)
	resp, err := handler.Do(testTenantContext(), newTestLokiRequest(`sum(count_over_time({app="api"} |= "timeout" [5m]))`, reqStart, now))
	require.NoError(t, err)
	require.Same(t, userResp, resp)

	logLine := findLogLine(logs.String(), "logline dry-run verification")
	require.NotEmpty(t, logLine)
	require.Contains(t, logLine, "correct=false", "a hint outside the lookback leaves the sample uncovered")
	require.Contains(t, logLine, "total_entries=1")
	require.Contains(t, logLine, "covered_entries=0")
	require.Contains(t, logLine, "false_negatives=1")
	require.Contains(t, logLine, hintprovider.FormatHintRanges([]hintprovider.HintTimeRange{{
		Start: now.Add(-5 * time.Minute),
		End:   now,
	}}))
}

func TestDryRun_MetricRange_IngesterWindowSamplesExcluded(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	reqStart := now.Add(-6 * time.Hour)
	// Lookback ends before the 3h cutoff, so this step is checked.
	storeStep := now.Add(-4 * time.Hour)
	// Lookback ends after the cutoff, so this step is left out.
	ingesterStep := now.Add(-1 * time.Hour)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: nil},
	}

	userResp := &queryrange.LokiPromResponse{
		Response: &queryrangebase.PrometheusResponse{
			Status: loghttp.QueryStatusSuccess,
			Data: queryrangebase.PrometheusData{
				ResultType: loghttp.ResultTypeMatrix,
				Result: []queryrangebase.SampleStream{{
					Labels: []logproto.LabelAdapter{{Name: "app", Value: "api"}},
					Samples: []logproto.LegacySample{
						{TimestampMs: storeStep.UnixMilli(), Value: 1},
						{TimestampMs: ingesterStep.UnixMilli(), Value: 1},
					},
				}},
			},
		},
	}
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		time.Sleep(25 * time.Millisecond)
		return userResp, nil
	})

	var logs bytes.Buffer
	handler := buildStackWithLogger(hp, Config{
		DryRun:               true,
		NgramLength:          6,
		QueryIngestersWithin: 3 * time.Hour,
	}, newTestMetrics(), log.NewLogfmtLogger(&logs), next)
	resp, err := handler.Do(testTenantContext(), newTestLokiRequest(`sum(count_over_time({app="api"} |= "timeout" [5m]))`, reqStart, now))
	require.NoError(t, err)
	require.Same(t, userResp, resp)

	_, gotThrough := hp.Window()
	require.WithinDuration(t, now.Add(-3*time.Hour), gotThrough.Time(), 2*time.Second)

	logLine := findLogLine(logs.String(), "logline dry-run verification")
	require.NotEmpty(t, logLine)
	require.Contains(t, logLine, "correct=false")
	require.Contains(t, logLine, "total_entries=1", "the ingester-window step is left out")
	require.Contains(t, logLine, "covered_entries=0")
	require.Contains(t, logLine, "false_negatives=1")
	require.Contains(t, logLine, hintprovider.FormatHintRanges([]hintprovider.HintTimeRange{{
		Start: storeStep.Add(-5 * time.Minute),
		End:   storeStep,
	}}))
	require.NotContains(t, logLine, hintprovider.FormatHintRanges([]hintprovider.HintTimeRange{{
		Start: ingesterStep.Add(-5 * time.Minute),
		End:   ingesterStep,
	}}))
}

func TestDryRun_MetricRange_OldestLookbackPastCutoffSkipsLookup(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	// A 2h query with [1d] widens a day back, but the oldest sample's lookback
	// still ends inside the 3h ingester window.
	reqStart := now.Add(-2 * time.Hour)
	hp := &mockHintProvider{hints: &hintprovider.Hints{TimeRanges: nil}}
	userResp := metricMatrixResponse(1, now)
	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		return userResp, nil
	})

	var logs bytes.Buffer
	handler := buildStackWithLogger(hp, Config{
		DryRun:               true,
		NgramLength:          6,
		QueryIngestersWithin: 3 * time.Hour,
	}, newTestMetrics(), log.NewLogfmtLogger(&logs), next)
	resp, err := handler.Do(testTenantContext(), newTestLokiRequest(`sum(count_over_time({app="api"} |= "timeout" [1d]))`, reqStart, now))
	require.NoError(t, err)
	require.Same(t, userResp, resp)
	require.Equal(t, 1, nextCalled)
	require.Equal(t, 0, hp.Calls())
	require.Empty(t, findLogLine(logs.String(), "logline dry-run verification"))
}

func TestDryRun_MetricRange_AbsentOverTimeSkipsOverlap(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	reqStart := now.Add(-1 * time.Hour)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: nil},
	}

	userResp := metricMatrixResponse(1, now)
	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		time.Sleep(25 * time.Millisecond)
		return userResp, nil
	})

	var logs bytes.Buffer
	handler := buildStackWithLogger(hp, Config{DryRun: true, NgramLength: 6}, newTestMetrics(), log.NewLogfmtLogger(&logs), next)
	resp, err := handler.Do(testTenantContext(), newTestLokiRequest(`sum(absent_over_time({app="api"} |= "timeout" [5m]))`, reqStart, now))
	require.NoError(t, err)

	require.Same(t, userResp, resp)
	require.Equal(t, 1, nextCalled)
	logLine := findLogLine(logs.String(), "logline dry-run verification")
	require.NotEmpty(t, logLine)
	require.Contains(t, logLine, "correct=true", "a positive absent_over_time sample means the window was empty")
	require.Contains(t, logLine, "total_entries=0")
	require.Contains(t, logLine, "false_negatives=0")
}

func TestDryRun_MetricRange_UnsupportedSkipsHintLookup(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-30 * time.Minute), End: now.Add(-20 * time.Minute)},
			},
		},
	}

	userResp := metricMatrixResponse(1, now)
	var got *queryrange.LokiRequest
	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		got = req.(*queryrange.LokiRequest)
		return userResp, nil
	})

	handler := buildStack(hp, Config{DryRun: true, NgramLength: 6}, newTestMetrics(), next)
	req := newTestLokiRequest(
		`sum(count_over_time({app="api"} |= "timeout" [5m])) / sum(count_over_time({app="api"} |= "database" [5m]))`,
		now.Add(-1*time.Hour),
		now,
	)
	resp, err := handler.Do(testTenantContext(), req)
	require.NoError(t, err)
	require.Same(t, userResp, resp)
	require.Equal(t, 0, hp.Calls())
	require.Equal(t, 1, nextCalled)
	require.Empty(t, got.HintRanges)
}

func TestDryRun_MetricRange_LiveModeSkipsHintLookup(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-30 * time.Minute), End: now.Add(-20 * time.Minute)},
			},
		},
	}

	userResp := metricMatrixResponse(1, now)
	var got *queryrange.LokiRequest
	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		got = req.(*queryrange.LokiRequest)
		return userResp, nil
	})

	handler := buildStack(hp, Config{DryRun: true, NgramLength: 6}, newTestMetrics(), next)
	req := newTestLokiRequest(`sum(count_over_time({app="api"} |= "timeout" [5m]))`, now.Add(-1*time.Hour), now)
	resp, err := handler.Do(testTenantContextWithLive(), req)
	require.NoError(t, err)
	require.Same(t, userResp, resp)
	require.Equal(t, 0, hp.Calls())
	require.Equal(t, 1, nextCalled)
	require.Empty(t, got.HintRanges)
}

func TestDryRun_MetricInstant_PositiveSampleOverlapsWidenedHint(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		interval time.Duration
		offset   time.Duration
	}{
		{
			name:     "sum of count_over_time",
			query:    `sum(count_over_time({app="api"} |= "timeout" [5m]))`,
			interval: 5 * time.Minute,
		},
		{
			name:     "offset widens the start and shifts the end",
			query:    `sum(count_over_time({app="api"} |= "timeout" [5m] offset 10m))`,
			interval: 5 * time.Minute,
			offset:   10 * time.Minute,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().Truncate(time.Millisecond)
			lookupFrom := now.Add(-(tc.interval + tc.offset))
			lookupThrough := now.Add(-tc.offset)

			hp := &mockHintProvider{
				hints: &hintprovider.Hints{
					TimeRanges: []hintprovider.HintTimeRange{
						{Start: lookupFrom, End: lookupThrough},
					},
				},
			}

			userResp := metricInstantResponse(1, now)
			var userReq *queryrange.LokiInstantRequest
			nextCalled := 0
			next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
				nextCalled++
				time.Sleep(25 * time.Millisecond)
				userReq = req.(*queryrange.LokiInstantRequest)
				return userResp, nil
			})

			var logs bytes.Buffer
			handler := buildStackWithLogger(hp, Config{DryRun: true, NgramLength: 6}, newTestMetrics(), log.NewLogfmtLogger(&logs), next)
			req := newTestLokiInstantRequest(tc.query, now)
			resp, err := handler.Do(testTenantContext(), req)
			require.NoError(t, err)

			require.Same(t, userResp, resp, "dry-run returns the unhinted result")
			require.Equal(t, 1, nextCalled, "dry-run runs the query once")
			require.Equal(t, now, userReq.TimeTs)

			require.Equal(t, 1, hp.Calls())
			gotFrom, gotThrough := hp.Window()
			require.Equal(t, model.TimeFromUnixNano(lookupFrom.UnixNano()), gotFrom)
			require.Equal(t, model.TimeFromUnixNano(lookupThrough.UnixNano()), gotThrough)

			logLine := findLogLine(logs.String(), "logline dry-run verification")
			require.NotEmpty(t, logLine)
			require.Contains(t, logLine, "correct=true")
			require.Contains(t, logLine, "query_type=metric")
			require.Contains(t, logLine, "total_entries=1")
			require.Contains(t, logLine, "covered_entries=1")
			require.Contains(t, logLine, "false_negatives=0")
			require.NotContains(t, logLine, "false_negative_ranges=")
		})
	}
}

func TestDryRun_MetricInstant_EmptyHintsMissPositiveSample(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{hints: &hintprovider.Hints{TimeRanges: nil}}
	userResp := metricInstantResponse(1, now)
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		time.Sleep(25 * time.Millisecond)
		require.IsType(t, &queryrange.LokiInstantRequest{}, req)
		return userResp, nil
	})

	var logs bytes.Buffer
	handler := buildStackWithLogger(hp, Config{DryRun: true, NgramLength: 6}, newTestMetrics(), log.NewLogfmtLogger(&logs), next)
	resp, err := handler.Do(testTenantContext(), newTestLokiInstantRequest(`sum(count_over_time({app="api"} |= "timeout" [5m]))`, now))
	require.NoError(t, err)
	require.Same(t, userResp, resp)

	logLine := findLogLine(logs.String(), "logline dry-run verification")
	require.NotEmpty(t, logLine)
	require.Contains(t, logLine, "correct=false", "a positive sample whose lookback is uncovered is a miss")
	require.Contains(t, logLine, "total_entries=1")
	require.Contains(t, logLine, "covered_entries=0")
	require.Contains(t, logLine, "false_negatives=1")
	require.Contains(t, logLine, hintprovider.FormatHintRanges([]hintprovider.HintTimeRange{{
		Start: now.Add(-5 * time.Minute),
		End:   now,
	}}))
}

func TestDryRun_MetricInstant_PartialHintCoversLookback(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-5 * time.Minute), End: now.Add(-4 * time.Minute)},
			},
		},
	}
	userResp := metricInstantResponse(1, now)
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		time.Sleep(25 * time.Millisecond)
		return userResp, nil
	})

	var logs bytes.Buffer
	handler := buildStackWithLogger(hp, Config{DryRun: true, NgramLength: 6}, newTestMetrics(), log.NewLogfmtLogger(&logs), next)
	resp, err := handler.Do(testTenantContext(), newTestLokiInstantRequest(`sum(count_over_time({app="api"} |= "timeout" [5m]))`, now))
	require.NoError(t, err)
	require.Same(t, userResp, resp)

	logLine := findLogLine(logs.String(), "logline dry-run verification")
	require.NotEmpty(t, logLine)
	require.Contains(t, logLine, "correct=true", "any hint in the lookback counts as covered")
	require.Contains(t, logLine, "total_entries=1")
	require.Contains(t, logLine, "covered_entries=1")
	require.NotContains(t, logLine, "false_negative_ranges=")
}

func TestDryRun_MetricInstant_HintOutsideLookbackIsMiss(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-10 * time.Minute), End: now.Add(-6 * time.Minute)},
			},
		},
	}
	userResp := metricInstantResponse(1, now)
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		time.Sleep(25 * time.Millisecond)
		return userResp, nil
	})

	var logs bytes.Buffer
	handler := buildStackWithLogger(hp, Config{DryRun: true, NgramLength: 6}, newTestMetrics(), log.NewLogfmtLogger(&logs), next)
	resp, err := handler.Do(testTenantContext(), newTestLokiInstantRequest(`sum(count_over_time({app="api"} |= "timeout" [5m]))`, now))
	require.NoError(t, err)
	require.Same(t, userResp, resp)

	logLine := findLogLine(logs.String(), "logline dry-run verification")
	require.NotEmpty(t, logLine)
	require.Contains(t, logLine, "correct=false", "a hint outside the lookback leaves the sample uncovered")
	require.Contains(t, logLine, "total_entries=1")
	require.Contains(t, logLine, "false_negatives=1")
	require.Contains(t, logLine, hintprovider.FormatHintRanges([]hintprovider.HintTimeRange{{
		Start: now.Add(-5 * time.Minute),
		End:   now,
	}}))
}

func TestDryRun_MetricInstant_IngesterWindowOverlapSkipsLookup(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	// The 5m lookback crosses the 3h cutoff, so the only sample would be left out.
	eval := now.Add(-3*time.Hour + 2*time.Minute)
	hp := &mockHintProvider{hints: &hintprovider.Hints{TimeRanges: nil}}
	userResp := metricInstantResponse(1, eval)
	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		return userResp, nil
	})

	var logs bytes.Buffer
	handler := buildStackWithLogger(hp, Config{
		DryRun:               true,
		NgramLength:          6,
		QueryIngestersWithin: 3 * time.Hour,
	}, newTestMetrics(), log.NewLogfmtLogger(&logs), next)
	resp, err := handler.Do(testTenantContext(), newTestLokiInstantRequest(`sum(count_over_time({app="api"} |= "timeout" [5m]))`, eval))
	require.NoError(t, err)
	require.Same(t, userResp, resp)
	require.Equal(t, 1, nextCalled)
	require.Equal(t, 0, hp.Calls())
	require.Empty(t, findLogLine(logs.String(), "logline dry-run verification"))
}

func TestDryRun_MetricInstant_AbsentOverTimeSkipsOverlap(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{hints: &hintprovider.Hints{TimeRanges: nil}}
	userResp := metricInstantResponse(1, now)
	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		time.Sleep(25 * time.Millisecond)
		return userResp, nil
	})

	var logs bytes.Buffer
	handler := buildStackWithLogger(hp, Config{DryRun: true, NgramLength: 6}, newTestMetrics(), log.NewLogfmtLogger(&logs), next)
	resp, err := handler.Do(testTenantContext(), newTestLokiInstantRequest(`sum(absent_over_time({app="api"} |= "timeout" [5m]))`, now))
	require.NoError(t, err)
	require.Same(t, userResp, resp)
	require.Equal(t, 1, nextCalled)
	require.Equal(t, 1, hp.Calls())

	logLine := findLogLine(logs.String(), "logline dry-run verification")
	require.NotEmpty(t, logLine)
	require.Contains(t, logLine, "correct=true", "a positive absent_over_time sample means the window was empty")
	require.Contains(t, logLine, "total_entries=0")
	require.Contains(t, logLine, "false_negatives=0")
}

func TestDryRun_MetricInstant_SkipsHintLookup(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	tests := []struct {
		name  string
		ctx   context.Context
		query string
	}{
		{
			name:  "arithmetic",
			ctx:   testTenantContext(),
			query: `sum(count_over_time({app="api"} |= "timeout" [5m])) / sum(count_over_time({app="api"} |= "database" [5m]))`,
		},
		{
			name:  "live header",
			ctx:   testTenantContextWithLive(),
			query: `sum(count_over_time({app="api"} |= "timeout" [5m]))`,
		},
		{
			name:  "log instant query",
			ctx:   testTenantContext(),
			query: `{app="api"} |= "timeout"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hp := &mockHintProvider{
				hints: &hintprovider.Hints{
					TimeRanges: []hintprovider.HintTimeRange{
						{Start: now.Add(-30 * time.Minute), End: now.Add(-20 * time.Minute)},
					},
				},
			}
			userResp := metricInstantResponse(1, now)
			nextCalled := 0
			next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
				nextCalled++
				require.IsType(t, &queryrange.LokiInstantRequest{}, req)
				return userResp, nil
			})

			handler := buildStack(hp, Config{DryRun: true, NgramLength: 6}, newTestMetrics(), next)
			resp, err := handler.Do(tc.ctx, newTestLokiInstantRequest(tc.query, now))
			require.NoError(t, err)
			require.Same(t, userResp, resp)
			require.Equal(t, 0, hp.Calls())
			require.Equal(t, 1, nextCalled)
		})
	}
}

func metricInstantResponse(value float64, ts time.Time) *queryrange.LokiPromResponse {
	resp := metricMatrixResponse(value, ts)
	resp.Response.Data.ResultType = loghttp.ResultTypeVector
	return resp
}

func metricMatrixResponse(value float64, ts time.Time) *queryrange.LokiPromResponse {
	return &queryrange.LokiPromResponse{
		Response: &queryrangebase.PrometheusResponse{
			Status: loghttp.QueryStatusSuccess,
			Data: queryrangebase.PrometheusData{
				ResultType: loghttp.ResultTypeMatrix,
				Result: []queryrangebase.SampleStream{
					{
						Labels:  []logproto.LabelAdapter{{Name: "app", Value: "api"}},
						Samples: []logproto.LegacySample{{TimestampMs: ts.UnixMilli(), Value: value}},
					},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Header override tests
// ---------------------------------------------------------------------------

func TestResolveMode_HeaderOffOverridesAllConfig(t *testing.T) {
	cases := []struct {
		name               string
		tenantMode         Mode
		defaultMode        Mode
		requireOptInHeader bool
	}{
		{name: "tenant unset global live", tenantMode: ModeUnset, defaultMode: ModeLive},
		{name: "tenant unset global dry-run", tenantMode: ModeUnset, defaultMode: ModeDryRun},
		{name: "tenant live", tenantMode: ModeLive, defaultMode: ModeDryRun},
		{name: "tenant dry-run", tenantMode: ModeDryRun, defaultMode: ModeLive},
		{name: "tenant off with opt-in required", tenantMode: ModeOff, defaultMode: ModeLive, requireOptInHeader: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode, passthrough := resolveMode(string(LoglineIndexOff), tc.tenantMode, tc.defaultMode, tc.requireOptInHeader)
			require.Equal(t, ModeOff, mode)
			require.True(t, passthrough)
		})
	}
}

func TestHeaderOff_OverridesEnabledConfigAndTenantLive(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-45 * time.Minute), End: now.Add(-30 * time.Minute)},
			},
		},
	}
	settings := mockLimits{
		modes: map[string]Mode{"test": ModeLive},
	}

	var gotStart, gotEnd time.Time
	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		return emptyStreamResponse(), nil
	})

	cfg := Config{DryRun: true, RequireOptInHeader: false, NgramLength: 3}
	handler := buildStack(hp, cfg, newTestMetrics(), next, settings)
	ctx := testTenantContextWithForceDisable()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, nextCalled, "force-disable should pass through to Loki")
	require.Equal(t, 0, hp.Calls(), "force-disable should skip hint lookups")
	require.Equal(t, req.StartTs, gotStart, "force-disable should keep original start")
	require.Equal(t, req.EndTs, gotEnd, "force-disable should keep original end")
}

func TestTenantMode_Off_OnlyExplicitModeHeaderOverridesTenantOff(t *testing.T) {
	mode, passthrough := resolveMode("unexpected", ModeOff, ModeLive, false)
	require.Equal(t, ModeOff, mode)
	require.True(t, passthrough)

	mode, passthrough = resolveMode(string(LoglineIndexLive), ModeOff, ModeDryRun, false)
	require.Equal(t, ModeLive, mode)
	require.False(t, passthrough)

	mode, passthrough = resolveMode(string(LoglineIndexDryRun), ModeOff, ModeLive, false)
	require.Equal(t, ModeDryRun, mode)
	require.False(t, passthrough)
}

func TestDryRun_ForceLiveHeader_OverridesToLivePath(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintStart := now.Add(-45 * time.Minute)
	hintEnd := now.Add(-30 * time.Minute)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			},
		},
	}

	var gotStart, gotEnd time.Time
	var gotHints []logproto.HintTimeRange
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		gotHints = lokiReq.HintRanges
		return emptyStreamResponse(), nil
	})

	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	cfg := Config{DryRun: true, RequireOptInHeader: false, NgramLength: 3}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next)
	ctx := testTenantContextWithForceLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, hp.Calls(), "force-live should still call ProvideHints")
	require.Equal(t, req.StartTs, gotStart)
	require.Equal(t, req.EndTs, gotEnd)
	require.Equal(t, []logproto.HintTimeRange{{Start: hintStart, End: hintEnd}}, gotHints)

	require.NotContains(t, logs.String(), "dry-run verification", "should NOT take dry-run path")
}

func TestDryRun_ForceLiveHeader_WithOptInRequired(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintStart := now.Add(-45 * time.Minute)
	hintEnd := now.Add(-30 * time.Minute)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			},
		},
	}

	var gotStart, gotEnd time.Time
	var gotHints []logproto.HintTimeRange
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		gotHints = lokiReq.HintRanges
		return emptyStreamResponse(), nil
	})

	cfg := Config{DryRun: true, RequireOptInHeader: true, NgramLength: 3}
	handler := buildStack(hp, cfg, newTestMetrics(), next)
	ctx := testTenantContextWithForceLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, hp.Calls(), "force-live with requireOptInHeader should still activate (header is present)")
	require.Equal(t, req.StartTs, gotStart)
	require.Equal(t, req.EndTs, gotEnd)
	require.Equal(t, []logproto.HintTimeRange{{Start: hintStart, End: hintEnd}}, gotHints)
}

func TestDryRun_DryRunHeader_StaysInDryRun(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-45 * time.Minute), End: now.Add(-30 * time.Minute)},
			},
		},
	}

	var gotStart, gotEnd time.Time
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		time.Sleep(25 * time.Millisecond)
		return streamResponseWithEntries(logproto.Entry{Timestamp: now.Add(-35 * time.Minute), Line: "failure"}), nil
	})

	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	cfg := Config{DryRun: true, RequireOptInHeader: true, NgramLength: 3}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next)
	ctx := testTenantContextWithDryRun()
	req := newTestLokiRequest(`{job="test"} |= "failure"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, hp.Calls(), "dry_run header should still call ProvideHints for verification")
	require.Equal(t, req.StartTs, gotStart, "dry-run should NOT narrow query start")
	require.Equal(t, req.EndTs, gotEnd, "dry-run should NOT narrow query end")
	require.Contains(t, logs.String(), "dry-run verification", "should take dry-run path")
}

func TestTenantMode_Off_Passthrough(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-45 * time.Minute), End: now.Add(-30 * time.Minute)},
			},
		},
	}
	settings := mockLimits{
		modes: map[string]Mode{"test": ModeOff},
	}

	var gotStart, gotEnd time.Time
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		return emptyStreamResponse(), nil
	})

	cfg := Config{RequireOptInHeader: false}
	handler := buildStack(hp, cfg, newTestMetrics(), next, settings)
	ctx := testTenantContext()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 0, hp.Calls(), "off tenant mode should skip hint lookups")
	require.Equal(t, req.StartTs, gotStart, "off tenant mode should keep original start")
	require.Equal(t, req.EndTs, gotEnd, "off tenant mode should keep original end")
}

func TestTenantMode_Off_HeaderLive_ForcesLive(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintStart := now.Add(-45 * time.Minute)
	hintEnd := now.Add(-30 * time.Minute)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			},
		},
	}
	settings := mockLimits{
		modes: map[string]Mode{"test": ModeOff},
	}

	var gotStart, gotEnd time.Time
	var gotHints []logproto.HintTimeRange
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		gotHints = lokiReq.HintRanges
		return emptyStreamResponse(), nil
	})

	cfg := Config{DryRun: false, RequireOptInHeader: false}
	handler := buildStack(hp, cfg, newTestMetrics(), next, settings)
	ctx := testTenantContextWithForceLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, hp.Calls(), "force-live should override tenant off mode")
	require.Equal(t, req.StartTs, gotStart)
	require.Equal(t, req.EndTs, gotEnd)
	require.Equal(t, []logproto.HintTimeRange{{Start: hintStart, End: hintEnd}}, gotHints)
}

func TestTenantMode_Off_HeaderDryRun_ForcesDryRun(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintStart := now.Add(-45 * time.Minute)
	hintEnd := now.Add(-30 * time.Minute)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			},
		},
	}
	settings := mockLimits{
		modes: map[string]Mode{"test": ModeOff},
	}

	var gotStart, gotEnd time.Time
	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		time.Sleep(25 * time.Millisecond)
		return streamResponseWithEntries(logproto.Entry{Timestamp: hintStart, Line: "failure"}), nil
	})

	cfg := Config{DryRun: false, RequireOptInHeader: false, NgramLength: 3}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next, settings)
	ctx := testTenantContextWithDryRun()
	req := newTestLokiRequest(`{job="test"} |= "failure"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, hp.Calls(), "dry_run should override tenant off mode")
	require.Equal(t, req.StartTs, gotStart, "dry_run should keep original start")
	require.Equal(t, req.EndTs, gotEnd, "dry_run should keep original end")
	require.Contains(t, logs.String(), "dry-run verification", "should take dry-run path")
}

func TestTenantMode_Live_OverridesGlobalDryRun(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintStart := now.Add(-45 * time.Minute)
	hintEnd := now.Add(-30 * time.Minute)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			},
		},
	}
	settings := mockLimits{
		modes: map[string]Mode{"test": ModeLive},
	}

	var gotStart, gotEnd time.Time
	var gotHints []logproto.HintTimeRange
	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		gotHints = lokiReq.HintRanges
		return emptyStreamResponse(), nil
	})

	cfg := Config{DryRun: true, RequireOptInHeader: false}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next, settings)
	ctx := testTenantContext()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, hp.Calls())
	require.Equal(t, req.StartTs, gotStart)
	require.Equal(t, req.EndTs, gotEnd)
	require.Equal(t, []logproto.HintTimeRange{{Start: hintStart, End: hintEnd}}, gotHints)
	require.NotContains(t, logs.String(), "dry-run verification", "tenant live mode should not execute dry-run path")
}

func TestTenantMode_DryRun_OverridesGlobalLive(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintStart := now.Add(-45 * time.Minute)
	hintEnd := now.Add(-30 * time.Minute)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			},
		},
	}
	settings := mockLimits{
		modes: map[string]Mode{"test": ModeDryRun},
	}

	var gotStart, gotEnd time.Time
	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		time.Sleep(25 * time.Millisecond)
		return streamResponseWithEntries(logproto.Entry{Timestamp: hintStart, Line: "failure"}), nil
	})

	cfg := Config{DryRun: false, RequireOptInHeader: false, NgramLength: 3}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next, settings)
	ctx := testTenantContext()
	req := newTestLokiRequest(`{job="test"} |= "failure"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, hp.Calls())
	require.Equal(t, req.StartTs, gotStart, "tenant dry-run mode should keep original start")
	require.Equal(t, req.EndTs, gotEnd, "tenant dry-run mode should keep original end")
	require.Contains(t, logs.String(), "dry-run verification", "tenant dry-run mode should execute dry-run path")
}

func TestTenantMode_Live_BypassesRequireOptInHeader(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintStart := now.Add(-45 * time.Minute)
	hintEnd := now.Add(-30 * time.Minute)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: hintStart, End: hintEnd},
			},
		},
	}
	settings := mockLimits{
		modes: map[string]Mode{"test": ModeLive},
	}

	var gotStart, gotEnd time.Time
	var gotHints []logproto.HintTimeRange
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		gotHints = lokiReq.HintRanges
		return emptyStreamResponse(), nil
	})

	cfg := Config{DryRun: false, RequireOptInHeader: true}
	handler := buildStack(hp, cfg, newTestMetrics(), next, settings)
	ctx := testTenantContext()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, hp.Calls(), "tenant live mode should bypass opt-in-header gating")
	require.Equal(t, req.StartTs, gotStart)
	require.Equal(t, req.EndTs, gotEnd)
	require.Equal(t, []logproto.HintTimeRange{{Start: hintStart, End: hintEnd}}, gotHints)
}
