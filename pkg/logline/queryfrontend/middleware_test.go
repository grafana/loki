package queryfrontend

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/loki"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/httpreq"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
)

// ---------------------------------------------------------------------------
// Test mocks
// ---------------------------------------------------------------------------

type mockHintProvider struct {
	mu           sync.RWMutex
	hints        *hintprovider.Hints
	err          error
	calls        int
	sawSkipCache bool
	sawCancel    bool
	delay        time.Duration
}

type mockTenantSettings struct {
	modes                 map[string]Mode
	minQueryBytesForIndex map[string]int64
}

func (m mockTenantSettings) Mode(tenant string) Mode {
	if m.modes == nil {
		return ModeUnset
	}
	mode, ok := m.modes[tenant]
	if !ok {
		return ModeUnset
	}
	return mode
}

func (m mockTenantSettings) MinQueryBytesForIndex(tenant string) (int64, bool) {
	if m.minQueryBytesForIndex == nil {
		return 0, false
	}
	minQueryBytes, ok := m.minQueryBytesForIndex[tenant]
	return minQueryBytes, ok
}

func (m *mockHintProvider) ProvideHints(
	ctx context.Context,
	_ string,
	_ syntax.Expr,
	_,
	_ model.Time,
) (*hintprovider.Hints, *hintprovider.QueryStats, error) {
	stats := hintprovider.NewQueryStats()

	m.mu.Lock()
	m.calls++
	hints := m.hints
	err := m.err
	m.sawSkipCache = hintprovider.SkipCache(ctx)
	delay := m.delay
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
	return hints, stats, err
}

func (m *mockHintProvider) Calls() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.calls
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

func newTestMetrics() *Metrics {
	return NewMetrics(prometheus.NewRegistry())
}

func newTestLokiRequest(query string, from, through time.Time) *queryrange.LokiRequest {
	return &queryrange.LokiRequest{
		Query:   query,
		StartTs: from,
		EndTs:   through,
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

// buildStack wires prefetch + filter middleware the same way service.go does,
// except the "Loki tripperware" is replaced by a transparent pass-through.
// This lets us test the two-layer interaction in isolation.
func buildStack(
	hp *mockHintProvider,
	cfg MiddlewareConfig,
	metrics *Metrics,
	querier queryrangebase.Handler,
	tenantSettings ...TenantSettings,
) queryrangebase.Handler {
	if cfg.ShardPlanning == (ShardPlanningConfig{}) {
		cfg.ShardPlanning.Enabled = false
		cfg.ShardPlanning.MinTimeReductionRatio = defaultShardPlanningMinReductionRatio
	}
	filterMW := NewLoglineFilterMiddleware(10*time.Second, metrics, nil)
	var settings TenantSettings
	if len(tenantSettings) > 0 {
		settings = tenantSettings[0]
	}
	prefetchMW := NewLoglinePrefetchMiddleware(hp, cfg, settings, metrics, nil)
	return prefetchMW.Wrap(filterMW.Wrap(querier))
}

func buildStackWithLogger(
	hp *mockHintProvider,
	cfg MiddlewareConfig,
	metrics *Metrics,
	logger log.Logger,
	querier queryrangebase.Handler,
	tenantSettings ...TenantSettings,
) queryrangebase.Handler {
	if cfg.ShardPlanning == (ShardPlanningConfig{}) {
		cfg.ShardPlanning.Enabled = false
		cfg.ShardPlanning.MinTimeReductionRatio = defaultShardPlanningMinReductionRatio
	}
	filterMW := NewLoglineFilterMiddleware(10*time.Second, metrics, logger)
	var settings TenantSettings
	if len(tenantSettings) > 0 {
		settings = tenantSettings[0]
	}
	prefetchMW := NewLoglinePrefetchMiddleware(hp, cfg, settings, metrics, logger)
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

	handler := buildStack(hp, MiddlewareConfig{RequireOptInHeader: true}, newTestMetrics(), next)
	ctx := testTenantContextWithLive()
	_, err := handler.Do(ctx, &queryrange.LokiSeriesRequest{})
	require.NoError(t, err)
	require.Equal(t, 1, nextCalled)
	require.Equal(t, 0, hp.Calls(), "hint provider should not be called for non-LokiRequest")
}

func TestPrefetchFilter_ParseError(t *testing.T) {
	hp := &mockHintProvider{}
	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		return emptyStreamResponse(), nil
	})

	handler := buildStack(hp, MiddlewareConfig{RequireOptInHeader: true}, newTestMetrics(), next)
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

	handler := buildStack(hp, MiddlewareConfig{RequireOptInHeader: true}, newTestMetrics(), next)
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
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		return emptyStreamResponse(), nil
	})

	handler := buildStack(hp, MiddlewareConfig{RequireOptInHeader: true}, newTestMetrics(), next)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, hp.Calls(), "should call ProvideHints when opt-in header is present")
	require.Equal(t, hintStart, gotStart, "should narrow to hint start")
	require.Equal(t, hintEnd, gotEnd, "should narrow to hint end")
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
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		return emptyStreamResponse(), nil
	})

	cfg := MiddlewareConfig{RequireOptInHeader: false}
	handler := buildStack(hp, cfg, newTestMetrics(), next)
	ctx := testTenantContext() // header intentionally omitted
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, hp.Calls(), "should call ProvideHints when header requirement is disabled")
	require.Equal(t, hintStart, gotStart, "should narrow to hint start")
	require.Equal(t, hintEnd, gotEnd, "should narrow to hint end")
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

	cfg := MiddlewareConfig{DryRun: true, NgramLength: 3}
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
	cfg := MiddlewareConfig{DryRun: true, NgramLength: 3}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next)
	ctx := testTenantContext()
	req := newTestLokiRequest(`{job="test"} |= "failure"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)

	logLine := findLogLine(logs.String(), "logline dry-run verification")
	require.NotEmpty(t, logLine)
	require.Contains(t, logLine, "correct=true")
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
			cfg := MiddlewareConfig{DryRun: true, NgramLength: 3}
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
	cfg := MiddlewareConfig{DryRun: true, NgramLength: 3}
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
	cfg := MiddlewareConfig{DryRun: true, NgramLength: 3}
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
	cfg := MiddlewareConfig{DryRun: true, NgramLength: 3}
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

	cfg := MiddlewareConfig{DryRun: true, NgramLength: 3, QueryIngestersWithin: 3 * time.Hour}
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
	cfg := MiddlewareConfig{DryRun: true, NgramLength: 3, QueryIngestersWithin: 3 * time.Hour}
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
	cfg := MiddlewareConfig{DryRun: true, NgramLength: 3, HintTimeout: 5 * time.Second}
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
	cfg := MiddlewareConfig{DryRun: true, NgramLength: 3, HintTimeout: 5 * time.Second}
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
	cfg := MiddlewareConfig{DryRun: true, NgramLength: 3, HintTimeout: 5 * time.Second}
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
	cfg := MiddlewareConfig{DryRun: true, NgramLength: 3, HintTimeout: 5 * time.Second}
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
	cfg := MiddlewareConfig{DryRun: true, NgramLength: 3}
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

	cfg := MiddlewareConfig{DryRun: true, NgramLength: 3}
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

	cfg := MiddlewareConfig{DryRun: true, NgramLength: 3, MinQueryBytesForIndex: 500}
	handler := buildStack(hp, cfg, newTestMetrics(), querier)
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
	cfg := MiddlewareConfig{DryRun: true, RequireOptInHeader: true, NgramLength: 3}
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

	cfg := MiddlewareConfig{DryRun: true, RequireOptInHeader: true, NgramLength: 3}
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
		loki.ConfigWrapper{},
		Config{Enabled: false},
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

	cfg := MiddlewareConfig{MinQueryBytesForIndex: 500}
	handler := buildStack(hp, cfg, newTestMetrics(), querier)
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
		&logproto.IndexStatsResponse{Bytes: 1000},
		nil,
		&statsCalls,
		queryHandler,
	)

	cfg := MiddlewareConfig{MinQueryBytesForIndex: 500}
	handler := buildStack(hp, cfg, newTestMetrics(), querier)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, statsCalls, "should call index stats once for gating")
	require.Equal(t, 1, hp.Calls(), "should prefetch hints when query bytes exceed threshold")
	require.Equal(t, 1, queryCalls)
	require.Equal(t, hintStart, gotStart, "should narrow to hint start")
	require.Equal(t, hintEnd, gotEnd, "should narrow to hint end")
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
	queryHandler := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		return emptyStreamResponse(), nil
	})

	statsCalls := 0
	querier := buildStatsAwareQuerier(
		&logproto.IndexStatsResponse{Bytes: 1000},
		nil,
		&statsCalls,
		queryHandler,
	)
	settings := mockTenantSettings{
		minQueryBytesForIndex: map[string]int64{"test": 500},
	}

	cfg := MiddlewareConfig{MinQueryBytesForIndex: 5000}
	handler := buildStack(hp, cfg, newTestMetrics(), querier, settings)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, statsCalls, "should call index stats once for tenant gating")
	require.Equal(t, 1, hp.Calls(), "tenant threshold should allow hint prefetch")
	require.Equal(t, hintStart, gotStart, "should narrow to hint start")
	require.Equal(t, hintEnd, gotEnd, "should narrow to hint end")
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

	cfg := MiddlewareConfig{MinQueryBytesForIndex: 500}
	handler := buildStack(hp, cfg, newTestMetrics(), querier)
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
	queryHandler := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		return emptyStreamResponse(), nil
	})

	statsCalls := 0
	querier := buildStatsAwareQuerier(
		&logproto.IndexStatsResponse{Bytes: 1000},
		nil,
		&statsCalls,
		queryHandler,
	)

	cfg := MiddlewareConfig{MinQueryBytesForIndex: 0}
	handler := buildStack(hp, cfg, newTestMetrics(), querier)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 0, statsCalls, "stats gating should be disabled when threshold is zero")
	require.Equal(t, 1, hp.Calls(), "hint prefetch should run when stats gating is disabled")
	require.Equal(t, hintStart, gotStart, "should narrow to hint start")
	require.Equal(t, hintEnd, gotEnd, "should narrow to hint end")
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
	queryHandler := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		return emptyStreamResponse(), nil
	})

	statsCalls := 0
	querier := buildStatsAwareQuerier(
		&logproto.IndexStatsResponse{Bytes: 1000},
		nil,
		&statsCalls,
		queryHandler,
	)
	settings := mockTenantSettings{
		minQueryBytesForIndex: map[string]int64{"test": 0},
	}

	cfg := MiddlewareConfig{MinQueryBytesForIndex: 500}
	handler := buildStack(hp, cfg, newTestMetrics(), querier, settings)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 0, statsCalls, "tenant zero threshold should disable stats gating")
	require.Equal(t, 1, hp.Calls(), "hint prefetch should run when tenant disables stats gating")
	require.Equal(t, hintStart, gotStart, "should narrow to hint start")
	require.Equal(t, hintEnd, gotEnd, "should narrow to hint end")
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
	cfg := MiddlewareConfig{MinQueryBytesForIndex: 500}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, querier)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, statsCalls)

	logLine := findLogLine(logs.String(), "hint prefetch completed")
	require.NotEmpty(t, logLine)
	require.Contains(t, logLine, "query_bytes=700")
	require.Contains(t, logLine, "hint_total_seconds=900.0")
	require.Contains(t, logLine, "hint_ranges_detail=")
	require.Contains(t, logLine, " +15m0s]")
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

	handler := buildStack(hp, MiddlewareConfig{RequireOptInHeader: true}, newTestMetrics(), next)
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

	handler := buildStack(hp, MiddlewareConfig{RequireOptInHeader: true}, newTestMetrics(), next)
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

	handler := buildStack(hp, MiddlewareConfig{RequireOptInHeader: true}, newTestMetrics(), next)
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

	handler := buildStack(hp, MiddlewareConfig{RequireOptInHeader: true}, newTestMetrics(), next)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)
	resp, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 0, nextCalled, "querier should NOT be called when hints are empty")

	lokiResp, ok := resp.(*queryrange.LokiResponse)
	require.True(t, ok)
	require.Equal(t, "success", lokiResp.Status)
}

func TestPrefetchFilter_NarrowsToHintRanges(t *testing.T) {
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
		return emptyStreamResponse(), nil
	})

	handler := buildStack(hp, MiddlewareConfig{RequireOptInHeader: true}, newTestMetrics(), next)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, hintStart, gotStart, "should narrow to hint start")
	require.Equal(t, hintEnd, gotEnd, "should narrow to hint end")
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
	var prefetchResult *hintPrefetchResult
	next := queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		prefetchResult = hintPrefetchFromContext(ctx)
		return emptyStreamResponse(), nil
	})

	handler := buildStack(hp, MiddlewareConfig{RequireOptInHeader: true}, newTestMetrics(), next)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, reqStart, reqEnd)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, prefetchResult)

	require.Equal(t, reqStart.UTC(), gotStart, "pre-min-date hint should pass through original start")
	require.Equal(t, reqEnd.UTC(), gotEnd, "pre-min-date hint should pass through original end")

	impact := prefetchResult.impactSnapshot()
	require.Equal(t, int64(1), impact.totalIntervals)
	require.Equal(t, int64(0), impact.narrowedIntervals)
	require.Equal(t, int64(1), impact.passthroughIntervals)
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

	handler := buildStack(hp, MiddlewareConfig{RequireOptInHeader: true}, newTestMetrics(), next)
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
	prefetchMW := NewLoglinePrefetchMiddleware(hp, MiddlewareConfig{RequireOptInHeader: true}, nil, metrics, nil)
	filterMW := NewLoglineFilterMiddleware(10*time.Millisecond, metrics, nil)
	var prefetchResult *hintPrefetchResult
	next := queryrangebase.HandlerFunc(func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		prefetchResult = hintPrefetchFromContext(ctx)
		return emptyStreamResponse(), nil
	})
	handler := prefetchMW.Wrap(filterMW.Wrap(next))

	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, prefetchResult)
	stats := prefetchResult.stats
	require.NotNil(t, stats)
	snap := stats.Snapshot()
	require.Equal(t, int32(1), snap.PrefetchCalls)
	require.Equal(t, int32(1), snap.PrefetchTimeouts)
}

func TestGroupHintEnvelopes(t *testing.T) {
	utc := func(h, m int) time.Time {
		return time.Date(2026, 9, 8, h, m, 0, 0, time.UTC)
	}
	hint := func(sh, sm, eh, em int) hintprovider.HintTimeRange {
		return hintprovider.HintTimeRange{Start: utc(sh, sm), End: utc(eh, em)}
	}
	env := func(sh, sm, eh, em int) hintEnvelope {
		return hintEnvelope{Start: utc(sh, sm), End: utc(eh, em)}
	}

	intervalStart := utc(12, 0)
	intervalEnd := utc(12, 59)

	tests := []struct {
		name      string
		start     time.Time
		end       time.Time
		hints     []hintprovider.HintTimeRange
		maxGroups int
		want      []hintEnvelope
	}{
		{
			name:  "five hints k=4 cuts the three largest gaps",
			start: intervalStart,
			end:   intervalEnd,
			hints: []hintprovider.HintTimeRange{
				hint(12, 5, 12, 6),
				hint(12, 21, 12, 22),
				hint(12, 35, 12, 36),
				hint(12, 41, 12, 42),
				hint(12, 51, 12, 52),
			},
			maxGroups: 4,
			want: []hintEnvelope{
				env(12, 5, 12, 6),
				env(12, 21, 12, 22),
				env(12, 35, 12, 42),
				env(12, 51, 12, 52),
			},
		},
		{
			name:  "k=1 unions all hints",
			start: intervalStart,
			end:   intervalEnd,
			hints: []hintprovider.HintTimeRange{
				hint(12, 5, 12, 6),
				hint(12, 21, 12, 22),
				hint(12, 35, 12, 36),
			},
			maxGroups: 1,
			want:      []hintEnvelope{env(12, 5, 12, 36)},
		},
		{
			name:  "overlapping hints are never split",
			start: intervalStart,
			end:   intervalEnd,
			hints: []hintprovider.HintTimeRange{
				hint(12, 5, 12, 20),
				hint(12, 18, 12, 22),
				hint(12, 40, 12, 41),
			},
			maxGroups: 4,
			want: []hintEnvelope{
				env(12, 5, 12, 22),
				env(12, 40, 12, 41),
			},
		},
		{
			name:  "clips to the request interval",
			start: utc(12, 10),
			end:   utc(12, 40),
			hints: []hintprovider.HintTimeRange{
				hint(12, 0, 12, 20),
				hint(12, 30, 12, 50),
			},
			maxGroups: 2,
			want: []hintEnvelope{
				env(12, 10, 12, 20),
				env(12, 30, 12, 40),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := groupHintEnvelopes(tc.hints, tc.start, tc.end, tc.maxGroups)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestEnvelopeBudget(t *testing.T) {
	require.Equal(t, 1, envelopeBudget(15*time.Minute))
	require.Equal(t, 2, envelopeBudget(15*time.Minute+time.Nanosecond))
	require.Equal(t, 4, envelopeBudget(59*time.Minute))
	require.Equal(t, 4, envelopeBudget(time.Hour))
	require.Equal(t, maxEnvelopesPerInterval, envelopeBudget(24*time.Hour))
	require.Equal(t, 1, envelopeBudget(0))
}

func TestPrefetchFilter_MultipleHintRanges(t *testing.T) {
	// 12:00–12:59 / 15m target → k=4. Five disjoint hints cut at the three
	// largest gaps: isolate the far ones, keep 12:35–12:42 together.
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

	var got [][2]time.Time
	var prefetchResult *hintPrefetchResult
	next := queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		got = append(got, [2]time.Time{lokiReq.StartTs, lokiReq.EndTs})
		prefetchResult = hintPrefetchFromContext(ctx)
		return emptyStreamResponse(), nil
	})

	metrics := newTestMetrics()
	handler := buildStack(hp, MiddlewareConfig{RequireOptInHeader: true}, metrics, next)
	req := newTestLokiRequest(`{job="test"} |= "error"`, intervalStart, intervalEnd)
	_, err := handler.Do(testTenantContextWithLive(), req)
	require.NoError(t, err)
	require.Equal(t, [][2]time.Time{
		{hints[0].Start, hints[0].End},
		{hints[1].Start, hints[1].End},
		{hints[2].Start, hints[3].End},
		{hints[4].Start, hints[4].End},
	}, got)

	require.NotNil(t, prefetchResult)
	impact := prefetchResult.impactSnapshot()
	require.Equal(t, int64(1), impact.totalIntervals)
	require.Equal(t, int64(1), impact.narrowedIntervals)
	require.Equal(t, 59*time.Minute, impact.originalDuration)
	require.Equal(t, 10*time.Minute, impact.queryDuration, "queried duration must be the sum of k envelopes")
	require.Equal(t, 1.0, testutil.ToFloat64(metrics.hintSubRequests.WithLabelValues("narrowed")))
	require.Equal(t, 0.0, testutil.ToFloat64(metrics.hintSubRequests.WithLabelValues("skipped")))
}

func TestPrefetchFilter_EnvelopeClippedToRequest(t *testing.T) {
	intervalStart := time.Date(2026, 9, 8, 12, 10, 0, 0, time.UTC)
	intervalEnd := time.Date(2026, 9, 8, 12, 40, 0, 0, time.UTC)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{
			{Start: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 8, 12, 20, 0, 0, time.UTC)},
			{Start: time.Date(2026, 9, 8, 12, 30, 0, 0, time.UTC), End: time.Date(2026, 9, 8, 12, 50, 0, 0, time.UTC)},
		}},
	}

	var got [][2]time.Time
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		got = append(got, [2]time.Time{lokiReq.StartTs, lokiReq.EndTs})
		return emptyStreamResponse(), nil
	})

	handler := buildStack(hp, MiddlewareConfig{RequireOptInHeader: true}, newTestMetrics(), next)
	req := newTestLokiRequest(`{job="test"} |= "error"`, intervalStart, intervalEnd)
	_, err := handler.Do(testTenantContextWithLive(), req)
	require.NoError(t, err)
	require.Equal(t, [][2]time.Time{
		{intervalStart, time.Date(2026, 9, 8, 12, 20, 0, 0, time.UTC)},
		{time.Date(2026, 9, 8, 12, 30, 0, 0, time.UTC), intervalEnd},
	}, got)
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

	var gotStart, gotEnd time.Time
	nextCalled := 0
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		nextCalled++
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		return emptyStreamResponse(), nil
	})

	handler := buildStack(hp, MiddlewareConfig{RequireOptInHeader: true}, newTestMetrics(), next)
	req := newTestLokiRequest(`{job="test"} |= "error"`, intervalStart, intervalEnd)
	_, err := handler.Do(testTenantContextWithLive(), req)
	require.NoError(t, err)
	require.Equal(t, 1, nextCalled)
	require.Equal(t, inside.Start, gotStart)
	require.Equal(t, inside.End, gotEnd)
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

	handler := buildStack(hp, MiddlewareConfig{RequireOptInHeader: true}, newTestMetrics(), next)
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

	handler := buildStack(hp, MiddlewareConfig{RequireOptInHeader: true}, newTestMetrics(), next)
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
	require.Len(t, got, 2)
	require.Equal(t, time.Date(2026, 9, 8, 12, 5, 0, 0, time.UTC), got[0].StartTs)
	require.Equal(t, time.Date(2026, 9, 8, 12, 6, 0, 0, time.UTC), got[0].EndTs)
	require.Equal(t, time.Date(2026, 9, 8, 12, 35, 0, 0, time.UTC), got[1].StartTs)
	require.Equal(t, time.Date(2026, 9, 8, 12, 36, 0, 0, time.UTC), got[1].EndTs)
	for _, g := range got {
		require.Equal(t, req.Query, g.Query)
		require.Equal(t, req.Limit, g.Limit)
		require.Equal(t, req.Step, g.Step)
		require.Equal(t, req.Interval, g.Interval)
		require.Equal(t, req.Direction, g.Direction)
		require.Equal(t, req.Path, g.Path)
		require.Equal(t, req.Shards, g.Shards)
		require.Equal(t, req.StoreChunks, g.StoreChunks)
	}
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

	cfg := MiddlewareConfig{QueryIngestersWithin: 3 * time.Hour}
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
	cfg := MiddlewareConfig{QueryIngestersWithin: 3 * time.Hour}
	metrics := newTestMetrics()

	prefetchMW := NewLoglinePrefetchMiddleware(hp, cfg, nil, metrics, nil)

	var mu sync.Mutex
	type subReqInfo struct {
		start, end time.Time
	}
	var gotReqs []subReqInfo

	// The filter + querier for both intervals.
	filterMW := NewLoglineFilterMiddleware(10*time.Second, metrics, nil)
	querier := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		mu.Lock()
		gotReqs = append(gotReqs, subReqInfo{start: lokiReq.StartTs, end: lokiReq.EndTs})
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

	// Covered interval should be narrowed to the hint range.
	// Ingester interval should pass through to querier.
	require.Len(t, gotReqs, 2, "should have narrowed + ingester sub-requests")

	// One should be the hint range, one should be the ingester window.
	hasNarrowed := false
	hasIngester := false
	for _, r := range gotReqs {
		if r.start.Equal(hintRange.Start) && r.end.Equal(hintRange.End) {
			hasNarrowed = true
		}
		if r.end.Equal(now) {
			hasIngester = true
		}
	}
	require.True(t, hasNarrowed, "should have a narrowed sub-request")
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

	cfg := MiddlewareConfig{QueryIngestersWithin: 3 * time.Hour}
	metrics := newTestMetrics()

	prefetchMW := NewLoglinePrefetchMiddleware(hp, cfg, nil, metrics, nil)
	filterMW := NewLoglineFilterMiddleware(10*time.Second, metrics, nil)

	var mu sync.Mutex
	type subReqInfo struct{ start, end time.Time }
	var gotReqs []subReqInfo
	querier := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		mu.Lock()
		gotReqs = append(gotReqs, subReqInfo{start: lokiReq.StartTs, end: lokiReq.EndTs})
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

	// Covered interval [-24h, -3h] should narrow to hint range [-12h, -10h].
	// Ingester interval [-3h, now] should pass through.
	require.Equal(t, 2, len(gotReqs), "expected narrowed + ingester sub-requests, got %d", len(gotReqs))

	hasNarrowed := false
	hasIngester := false
	for _, r := range gotReqs {
		if r.start.Equal(hintRange.Start) && r.end.Equal(hintRange.End) {
			hasNarrowed = true
		}
		if r.end.Equal(now) {
			hasIngester = true
			dur := r.end.Sub(r.start)
			require.InDelta(t, (3 * time.Hour).Seconds(), dur.Seconds(), 5)
		}
	}
	require.True(t, hasNarrowed, "should have narrowed sub-request")
	require.True(t, hasIngester, "should have ingester passthrough")
}

func TestPrefetchFilter_ImpactCountersAcrossSkipNarrowPassthrough(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hintRange := hintprovider.HintTimeRange{
		Start: now.Add(-6 * time.Hour),
		End:   now.Add(-5 * time.Hour),
	}

	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{hintRange}},
	}

	cfg := MiddlewareConfig{QueryIngestersWithin: 2 * time.Hour}
	metrics := newTestMetrics()
	prefetchMW := NewLoglinePrefetchMiddleware(hp, cfg, nil, metrics, nil)
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
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, prefetchResult)

	impact := prefetchResult.impactSnapshot()
	require.Equal(t, int64(3), impact.totalIntervals)
	require.Equal(t, int64(1), impact.skippedIntervals)
	require.Equal(t, int64(1), impact.narrowedIntervals)
	require.Equal(t, int64(1), impact.passthroughIntervals)
	require.Equal(t, 5*time.Hour, impact.originalDuration)
	require.Equal(t, 2*time.Hour, impact.queryDuration)
	require.InDelta(t, 0.6, impact.timeReductionRatio, 0.0001)
}

func TestPrefetchFilter_LogsHintImpactSummary(t *testing.T) {
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
		resp := emptyStreamResponse()
		resp.Statistics.Querier.Store.TotalChunksRef = 10
		resp.Statistics.Querier.Store.TotalChunksDownloaded = 4
		resp.Statistics.Querier.Store.Chunk.DecompressedBytes = 1024
		resp.Statistics.Querier.Store.Chunk.DecompressedLines = 100
		resp.Statistics.Ingester.Store.TotalChunksRef = 2
		resp.Statistics.Ingester.Store.TotalChunksDownloaded = 1
		resp.Statistics.Ingester.Store.Chunk.DecompressedBytes = 128
		resp.Statistics.Ingester.Store.Chunk.DecompressedLines = 10
		resp.Statistics.Summary.TotalEntriesReturned = 9
		return resp, nil
	})

	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	handler := buildStackWithLogger(hp, MiddlewareConfig{RequireOptInHeader: true}, newTestMetrics(), logger, next)
	ctx := testTenantContextWithLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)

	logLine := findLogLine(logs.String(), "query hint impact")
	require.NotEmpty(t, logLine)
	require.Contains(t, logLine, "total_intervals=1")
	require.Contains(t, logLine, "skipped_intervals=0")
	require.Contains(t, logLine, "narrowed_intervals=1")
	require.Contains(t, logLine, "passthrough_intervals=0")
	require.Contains(t, logLine, "time_reduction_ratio=0.5")
	require.Contains(t, logLine, "total_chunks_ref=12")
	require.Contains(t, logLine, "total_chunks_downloaded=5")
	require.Contains(t, logLine, "decompressed_bytes=1152")
	require.Contains(t, logLine, "decompressed_lines=110")
	require.Contains(t, logLine, "total_entries_returned=9")
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
		})
	}

	// Empty input.
	require.Nil(t, rangesOverlapping(nil, base, base.Add(time.Hour)))
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
	settings := mockTenantSettings{
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

	cfg := MiddlewareConfig{DryRun: true, RequireOptInHeader: false, NgramLength: 3}
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
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		return emptyStreamResponse(), nil
	})

	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	cfg := MiddlewareConfig{DryRun: true, RequireOptInHeader: false, NgramLength: 3}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next)
	ctx := testTenantContextWithForceLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, hp.Calls(), "force-live should still call ProvideHints")
	require.Equal(t, hintStart, gotStart, "force-live should narrow to hint start")
	require.Equal(t, hintEnd, gotEnd, "force-live should narrow to hint end")

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
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		return emptyStreamResponse(), nil
	})

	cfg := MiddlewareConfig{DryRun: true, RequireOptInHeader: true, NgramLength: 3}
	handler := buildStack(hp, cfg, newTestMetrics(), next)
	ctx := testTenantContextWithForceLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, hp.Calls(), "force-live with requireOptInHeader should still activate (header is present)")
	require.Equal(t, hintStart, gotStart, "should narrow to hint start")
	require.Equal(t, hintEnd, gotEnd, "should narrow to hint end")
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
	cfg := MiddlewareConfig{DryRun: true, RequireOptInHeader: true, NgramLength: 3}
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
	settings := mockTenantSettings{
		modes: map[string]Mode{"test": ModeOff},
	}

	var gotStart, gotEnd time.Time
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		return emptyStreamResponse(), nil
	})

	cfg := MiddlewareConfig{RequireOptInHeader: false}
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
	settings := mockTenantSettings{
		modes: map[string]Mode{"test": ModeOff},
	}

	var gotStart, gotEnd time.Time
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		return emptyStreamResponse(), nil
	})

	cfg := MiddlewareConfig{DryRun: false, RequireOptInHeader: false}
	handler := buildStack(hp, cfg, newTestMetrics(), next, settings)
	ctx := testTenantContextWithForceLive()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, hp.Calls(), "force-live should override tenant off mode")
	require.Equal(t, hintStart, gotStart, "force-live should narrow to hint start")
	require.Equal(t, hintEnd, gotEnd, "force-live should narrow to hint end")
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
	settings := mockTenantSettings{
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

	cfg := MiddlewareConfig{DryRun: false, RequireOptInHeader: false, NgramLength: 3}
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
	settings := mockTenantSettings{
		modes: map[string]Mode{"test": ModeLive},
	}

	var gotStart, gotEnd time.Time
	var logs bytes.Buffer
	logger := log.NewLogfmtLogger(&logs)
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		return emptyStreamResponse(), nil
	})

	cfg := MiddlewareConfig{DryRun: true, RequireOptInHeader: false}
	handler := buildStackWithLogger(hp, cfg, newTestMetrics(), logger, next, settings)
	ctx := testTenantContext()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, hp.Calls())
	require.Equal(t, hintStart, gotStart, "tenant live mode should narrow even when global dry-run is true")
	require.Equal(t, hintEnd, gotEnd, "tenant live mode should narrow even when global dry-run is true")
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
	settings := mockTenantSettings{
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

	cfg := MiddlewareConfig{DryRun: false, RequireOptInHeader: false, NgramLength: 3}
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
	settings := mockTenantSettings{
		modes: map[string]Mode{"test": ModeLive},
	}

	var gotStart, gotEnd time.Time
	next := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotStart = lokiReq.StartTs
		gotEnd = lokiReq.EndTs
		return emptyStreamResponse(), nil
	})

	cfg := MiddlewareConfig{DryRun: false, RequireOptInHeader: true}
	handler := buildStack(hp, cfg, newTestMetrics(), next, settings)
	ctx := testTenantContext()
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, hp.Calls(), "tenant live mode should bypass opt-in-header gating")
	require.Equal(t, hintStart, gotStart, "tenant live mode should narrow to hint start")
	require.Equal(t, hintEnd, gotEnd, "tenant live mode should narrow to hint end")
}
