package hintprovider

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

type mockHintCacheBackend struct {
	mu sync.Mutex

	data map[string][]byte

	fetchErr error
	storeErr error

	fetchCalls int
	storeCalls int
	stopCalls  int
}

func newMockHintCacheBackend() *mockHintCacheBackend {
	return &mockHintCacheBackend{
		data: map[string][]byte{},
	}
}

func (m *mockHintCacheBackend) Store(_ context.Context, keys []string, bufs [][]byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.storeCalls++
	if m.storeErr != nil {
		return m.storeErr
	}
	for i, key := range keys {
		if i >= len(bufs) {
			continue
		}
		m.data[key] = append([]byte(nil), bufs[i]...)
	}
	return nil
}

func (m *mockHintCacheBackend) Fetch(_ context.Context, keys []string) ([]string, [][]byte, []string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.fetchCalls++
	if m.fetchErr != nil {
		return nil, nil, keys, m.fetchErr
	}

	found := make([]string, 0, len(keys))
	bufs := make([][]byte, 0, len(keys))
	missing := make([]string, 0, len(keys))
	for _, key := range keys {
		value, ok := m.data[key]
		if !ok {
			missing = append(missing, key)
			continue
		}
		found = append(found, key)
		bufs = append(bufs, append([]byte(nil), value...))
	}
	return found, bufs, missing, nil
}

func (m *mockHintCacheBackend) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCalls++
}

func (m *mockHintCacheBackend) GetCacheType() stats.CacheType {
	return stats.CacheType("hint-cache-test")
}

func (m *mockHintCacheBackend) FetchCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.fetchCalls
}

func (m *mockHintCacheBackend) StoreCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.storeCalls
}

func (m *mockHintCacheBackend) StopCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopCalls
}

type stubHintProvider struct {
	mu sync.Mutex

	hints   *Hints
	stats   *QueryStats
	err     error
	delay   time.Duration
	minDate time.Time

	calls int
}

func (s *stubHintProvider) ProvideHints(
	_ context.Context,
	_ string,
	_ syntax.Expr,
	_,
	_ model.Time,
) (*Hints, *QueryStats, error) {
	s.mu.Lock()
	s.calls++
	hints := cloneHints(s.hints)
	stats := s.stats
	err := s.err
	delay := s.delay
	s.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}
	return hints, stats, err
}

func (s *stubHintProvider) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *stubHintProvider) MinDate() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.minDate
}

func cloneHints(h *Hints) *Hints {
	if h == nil {
		return nil
	}
	out := &Hints{TimeRanges: make([]HintTimeRange, len(h.TimeRanges))}
	copy(out.TimeRanges, h.TimeRanges)
	return out
}

func TestCachingHintProvider_FullHitAcrossAllDays(t *testing.T) {
	reg := prometheus.NewRegistry()
	backend := newMockHintCacheBackend()
	delegate := &stubHintProvider{
		hints: &Hints{
			TimeRanges: []HintTimeRange{
				{
					Start: time.Date(2026, 3, 10, 1, 0, 0, 0, time.UTC),
					End:   time.Date(2026, 3, 10, 1, 30, 0, 0, time.UTC),
				},
			},
		},
	}
	provider := NewCachingHintProvider(delegate, backend, reg)

	expr := mustParseExpr(t, `{job="api"} |= "error"`)
	tenant := "tenant-a"
	from := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)
	through := time.Date(2026, 3, 12, 11, 0, 0, 0, time.UTC)
	days := buildDayWindows(tenant, expr.String(), "", from, through)
	require.Len(t, days, 3)

	dayPayloads := map[string][]HintTimeRange{
		days[0].hashedKey: {
			{
				Start: time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC),
				End:   time.Date(2026, 3, 10, 10, 10, 0, 0, time.UTC),
			},
		},
		days[1].hashedKey: {
			{
				Start: time.Date(2026, 3, 11, 6, 0, 0, 0, time.UTC),
				End:   time.Date(2026, 3, 11, 6, 5, 0, 0, time.UTC),
			},
		},
		days[2].hashedKey: nil,
	}
	for key, ranges := range dayPayloads {
		encoded, err := marshalCachedHints(ranges)
		require.NoError(t, err)
		backend.data[key] = encoded
	}

	hints, stats, err := provider.ProvideHints(
		context.Background(),
		tenant,
		expr,
		model.TimeFromUnixNano(from.UnixNano()),
		model.TimeFromUnixNano(through.UnixNano()),
	)
	require.NoError(t, err)
	require.NotNil(t, hints)
	require.NotNil(t, stats)
	require.Equal(t, 0, delegate.Calls(), "delegate should not be called on full cache hit")
	require.Equal(t, []HintTimeRange{
		{
			Start: time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 3, 10, 10, 10, 0, 0, time.UTC),
		},
		{
			Start: time.Date(2026, 3, 11, 6, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 3, 11, 6, 5, 0, 0, time.UTC),
		},
	}, hints.TimeRanges)
}

func TestCachingHintProvider_PartialMissFetchesDelegateAndBackfillsDays(t *testing.T) {
	reg := prometheus.NewRegistry()
	backend := newMockHintCacheBackend()
	delegate := &stubHintProvider{
		hints: &Hints{
			TimeRanges: []HintTimeRange{
				{
					Start: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC),
					End:   time.Date(2026, 3, 10, 12, 15, 0, 0, time.UTC),
				},
				{
					Start: time.Date(2026, 3, 11, 9, 0, 0, 0, time.UTC),
					End:   time.Date(2026, 3, 11, 9, 10, 0, 0, time.UTC),
				},
			},
		},
		stats: NewQueryStats(),
	}
	provider := NewCachingHintProvider(delegate, backend, reg)

	expr := mustParseExpr(t, `{job="api"} |= "error"`)
	tenant := "tenant-a"
	from := time.Date(2026, 3, 10, 8, 0, 0, 0, time.UTC)
	through := time.Date(2026, 3, 11, 17, 0, 0, 0, time.UTC)
	days := buildDayWindows(tenant, expr.String(), "", from, through)
	require.Len(t, days, 2)

	// Preload only the first day so the first request is a partial miss.
	preloaded, err := marshalCachedHints([]HintTimeRange{
		{
			Start: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 3, 10, 12, 15, 0, 0, time.UTC),
		},
	})
	require.NoError(t, err)
	backend.data[days[0].hashedKey] = preloaded

	gotFirst, _, err := provider.ProvideHints(
		context.Background(),
		tenant,
		expr,
		model.TimeFromUnixNano(from.UnixNano()),
		model.TimeFromUnixNano(through.UnixNano()),
	)
	require.NoError(t, err)
	require.Equal(t, 1, delegate.Calls(), "partial miss should call delegate")
	require.Equal(t, 1, backend.StoreCalls(), "delegate success should backfill day entries")
	require.Equal(t, delegate.hints.TimeRanges, gotFirst.TimeRanges)

	// Second request should be fully served from cache.
	gotSecond, _, err := provider.ProvideHints(
		context.Background(),
		tenant,
		expr,
		model.TimeFromUnixNano(from.UnixNano()),
		model.TimeFromUnixNano(through.UnixNano()),
	)
	require.NoError(t, err)
	require.Equal(t, 1, delegate.Calls(), "full hit should avoid additional delegate calls")
	require.Equal(t, delegate.hints.TimeRanges, gotSecond.TimeRanges)
}

// A hint range crossing UTC midnight is split over two day payloads. The two
// halves must abut, otherwise the middleware skips the gap between them and
// drops any log line inside it.
func TestCachingHintProvider_DayPayloadsAbutAtMidnight(t *testing.T) {
	midnight := time.Date(2026, 3, 11, 0, 0, 0, 0, time.UTC)
	spanning := HintTimeRange{Start: midnight.Add(-10 * time.Minute), End: midnight.Add(10 * time.Minute)}

	backend := newMockHintCacheBackend()
	delegate := &stubHintProvider{
		hints: &Hints{TimeRanges: []HintTimeRange{spanning}},
		stats: NewQueryStats(),
	}
	provider := NewCachingHintProvider(delegate, backend, prometheus.NewRegistry())

	expr := mustParseExpr(t, `{job="api"} |= "error"`)
	from := model.TimeFromUnixNano(midnight.Add(-2 * time.Hour).UnixNano())
	through := model.TimeFromUnixNano(midnight.Add(2 * time.Hour).UnixNano())

	// Fill both day entries.
	_, _, err := provider.ProvideHints(context.Background(), "tenant-a", expr, from, through)
	require.NoError(t, err)

	// Serve the same window from cache.
	hit, _, err := provider.ProvideHints(context.Background(), "tenant-a", expr, from, through)
	require.NoError(t, err)
	require.Equal(t, []HintTimeRange{spanning}, hit.TimeRanges)
}

// clipRangesToDay must keep each day payload inside its own day so payloads stay
// independently composable, and consecutive payloads must rejoin without a gap.
func TestClipRangesToDay_PayloadsStayWithinTheirDayAndRejoin(t *testing.T) {
	d10 := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	d11 := d10.Add(24 * time.Hour)
	d12 := d11.Add(24 * time.Hour)
	days := [][2]time.Time{{d10, d11}, {d11, d12}}

	for _, tc := range []struct {
		name  string
		input HintTimeRange
	}{
		{"inside one day", HintTimeRange{Start: d10.Add(10 * time.Hour), End: d10.Add(11 * time.Hour)}},
		{"spans midnight", HintTimeRange{Start: d11.Add(-10 * time.Minute), End: d11.Add(10 * time.Minute)}},
		{"ends exactly at midnight", HintTimeRange{Start: d11.Add(-time.Minute), End: d11}},
		{"starts exactly at midnight", HintTimeRange{Start: d11, End: d11.Add(time.Minute)}},
		{"final millisecond of the day", HintTimeRange{Start: d11.Add(-time.Millisecond), End: d11}},
		{"spans a whole day", HintTimeRange{Start: d10.Add(23 * time.Hour), End: d12.Add(time.Hour)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var union []HintTimeRange
			for _, day := range days {
				// Round trip through the cache: the payload loses sub-millisecond
				// precision, which is exactly where the day boundary can drift.
				encoded, err := marshalCachedHints(clipRangesToDay([]HintTimeRange{tc.input}, day[0], day[1]))
				require.NoError(t, err)
				payload, err := unmarshalCachedHints(encoded)
				require.NoError(t, err)

				for _, p := range payload {
					require.False(t, p.Start.Before(day[0]),
						"payload %v starts before its day %s", p, day[0])
					require.False(t, p.End.After(day[1]),
						"payload %v ends after its day %s", p, day[1])
				}
				union = append(union, payload...)
			}

			// Reassembling the days must reproduce the same coverage as clipping the
			// whole span at once. Any gap or spill at midnight shows up here.
			require.Equal(t,
				clipRangesToDay([]HintTimeRange{tc.input}, d10, d12),
				normalizeRanges(union),
			)
		})
	}
}

func TestBuildDayWindows_SplitsAcrossUTCMidnight(t *testing.T) {
	expr := mustParseExpr(t, `{job="api"} |= "error"`)
	days := buildDayWindows(
		"tenant-a",
		expr.String(),
		"",
		time.Date(2026, 3, 10, 23, 50, 0, 0, time.UTC),
		time.Date(2026, 3, 12, 0, 5, 0, 0, time.UTC),
	)
	require.Equal(t, []string{"2026-03-10", "2026-03-11", "2026-03-12"}, []string{
		days[0].day,
		days[1].day,
		days[2].day,
	})

	oneDay := buildDayWindows(
		"tenant-a",
		expr.String(),
		"",
		time.Date(2026, 3, 10, 1, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 10, 23, 59, 0, 0, time.UTC),
	)
	require.Len(t, oneDay, 1)
	require.Equal(t, "2026-03-10", oneDay[0].day)
}

func TestBuildHintCacheLogicalKey_HashRoundTrip(t *testing.T) {
	tenant := "tenant-a"
	query := `{job="api"} |= "error"`
	minDate := "2026-03-08"
	day := "2026-03-10"
	logical := buildHintCacheLogicalKey(tenant, query, minDate, day)

	require.Equal(t, "logline:1:tenant-a:{job=\"api\"} |= \"error\":2026-03-08:2026-03-10", logical)
	require.Equal(t, cache.HashKey(logical), buildDayWindows(tenant, query,
		minDate,
		time.Date(2026, 3, 10, 1, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 10, 2, 0, 0, 0, time.UTC),
	)[0].hashedKey)

	otherMinDateLogical := buildHintCacheLogicalKey(tenant, query, "2026-03-09", day)
	require.NotEqual(t, cache.HashKey(logical), cache.HashKey(otherMinDateLogical), "min-date changes must alter cache keys")
}

func TestCachingHintProvider_SingleflightDeduplicatesConcurrentMisses(t *testing.T) {
	reg := prometheus.NewRegistry()
	backend := newMockHintCacheBackend()
	delegate := &stubHintProvider{
		delay: 100 * time.Millisecond,
		hints: &Hints{
			TimeRanges: []HintTimeRange{
				{
					Start: time.Date(2026, 3, 10, 4, 0, 0, 0, time.UTC),
					End:   time.Date(2026, 3, 10, 4, 10, 0, 0, time.UTC),
				},
			},
		},
	}
	provider := NewCachingHintProvider(delegate, backend, reg)

	expr := mustParseExpr(t, `{job="api"} |= "error"`)
	tenant := "tenant-a"
	from := model.TimeFromUnixNano(time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC).UnixNano())
	through := model.TimeFromUnixNano(time.Date(2026, 3, 10, 23, 0, 0, 0, time.UTC).UnixNano())

	const workers = 8
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for range workers {
		wg.Go(func() {
			_, _, err := provider.ProvideHints(context.Background(), tenant, expr, from, through)
			errCh <- err
		})
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}
	require.Equal(t, 1, delegate.Calls(), "singleflight should coalesce identical misses")
	require.Equal(t, 1, backend.StoreCalls(), "coalesced miss should store once")
}

func TestCachedHints_JSONRoundTripOmitsSource(t *testing.T) {
	input := []HintTimeRange{
		{
			Start:  time.Date(2026, 3, 10, 2, 0, 0, 0, time.UTC),
			End:    time.Date(2026, 3, 10, 2, 5, 0, 0, time.UTC),
			Source: "debug-source",
		},
	}

	encoded, err := marshalCachedHints(input)
	require.NoError(t, err)
	require.JSONEq(t, `{"r":[{"s":1773108000000,"e":1773108300000}]}`, string(encoded))

	decoded, err := unmarshalCachedHints(encoded)
	require.NoError(t, err)
	require.Equal(t, []HintTimeRange{
		{
			Start: time.Date(2026, 3, 10, 2, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 3, 10, 2, 5, 0, 0, time.UTC),
		},
	}, decoded)

	// Empty day must still decode successfully.
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(`{}`), &parsed))
	decodedEmpty, err := unmarshalCachedHints([]byte(`{}`))
	require.NoError(t, err)
	require.Nil(t, decodedEmpty)
}

func TestCachingHintProvider_FiltersOutOfWindowRanges(t *testing.T) {
	expr := mustParseExpr(t, `{job="api"} |= "error"`)
	tenant := "tenant-a"
	from := time.Date(2026, 6, 4, 0, 0, 0, 0, time.UTC)
	through := time.Date(2026, 6, 4, 0, 5, 0, 0, time.UTC)
	// Non-zero duration required: normalizeRanges drops empty [start, end) ranges.
	inWindow := HintTimeRange{
		Start: time.Date(2026, 6, 4, 0, 3, 21, 0, time.UTC),
		End:   time.Date(2026, 6, 4, 0, 3, 21, 0, time.UTC).Add(time.Millisecond),
	}
	outOfWindow := HintTimeRange{
		Start: time.Date(2026, 6, 4, 0, 25, 17, 0, time.UTC),
		End:   time.Date(2026, 6, 4, 0, 25, 17, 0, time.UTC).Add(time.Millisecond),
	}
	allRanges := []HintTimeRange{inWindow, outOfWindow}

	t.Run("cache hit", func(t *testing.T) {
		backend := newMockHintCacheBackend()
		delegate := &stubHintProvider{hints: &Hints{TimeRanges: allRanges}}
		provider := NewCachingHintProvider(delegate, backend, prometheus.NewRegistry())
		days := buildDayWindows(tenant, expr.String(), "", from, through)
		require.Len(t, days, 1)

		encoded, err := marshalCachedHints(allRanges)
		require.NoError(t, err)
		backend.data[days[0].hashedKey] = encoded

		hints, _, err := provider.ProvideHints(
			context.Background(),
			tenant,
			expr,
			model.TimeFromUnixNano(from.UnixNano()),
			model.TimeFromUnixNano(through.UnixNano()),
		)
		require.NoError(t, err)
		require.Equal(t, 0, delegate.Calls(), "delegate should not be called on full cache hit")
		require.Equal(t, []HintTimeRange{inWindow}, hints.TimeRanges)
	})

	t.Run("cache miss", func(t *testing.T) {
		backend := newMockHintCacheBackend()
		delegate := &stubHintProvider{hints: &Hints{TimeRanges: allRanges}}
		provider := NewCachingHintProvider(delegate, backend, prometheus.NewRegistry())

		hints, _, err := provider.ProvideHints(
			context.Background(),
			tenant,
			expr,
			model.TimeFromUnixNano(from.UnixNano()),
			model.TimeFromUnixNano(through.UnixNano()),
		)
		require.NoError(t, err)
		require.Equal(t, 1, delegate.Calls())
		require.Equal(t, []HintTimeRange{inWindow}, hints.TimeRanges)
	})

	t.Run("nil cache", func(t *testing.T) {
		delegate := &stubHintProvider{hints: &Hints{TimeRanges: allRanges}}
		provider := NewCachingHintProvider(delegate, nil, prometheus.NewRegistry())

		hints, _, err := provider.ProvideHints(
			context.Background(),
			tenant,
			expr,
			model.TimeFromUnixNano(from.UnixNano()),
			model.TimeFromUnixNano(through.UnixNano()),
		)
		require.NoError(t, err)
		require.Equal(t, 1, delegate.Calls())
		require.Equal(t, []HintTimeRange{inWindow}, hints.TimeRanges)
	})

	t.Run("skip cache", func(t *testing.T) {
		backend := newMockHintCacheBackend()
		delegate := &stubHintProvider{hints: &Hints{TimeRanges: allRanges}}
		provider := NewCachingHintProvider(delegate, backend, prometheus.NewRegistry())

		hints, _, err := provider.ProvideHints(
			WithSkipCache(context.Background()),
			tenant,
			expr,
			model.TimeFromUnixNano(from.UnixNano()),
			model.TimeFromUnixNano(through.UnixNano()),
		)
		require.NoError(t, err)
		require.Equal(t, 1, delegate.Calls())
		require.Equal(t, 0, backend.FetchCalls(), "skip should avoid cache fetch")
		require.Equal(t, 0, backend.StoreCalls(), "skip should avoid cache store")
		require.Equal(t, []HintTimeRange{inWindow}, hints.TimeRanges)
	})
}

func TestCachingHintProvider_NilCachePassthrough(t *testing.T) {
	delegate := &stubHintProvider{
		hints: &Hints{
			TimeRanges: []HintTimeRange{
				{
					Start: time.Date(2026, 3, 10, 3, 0, 0, 0, time.UTC),
					End:   time.Date(2026, 3, 10, 3, 1, 0, 0, time.UTC),
				},
			},
		},
	}
	provider := NewCachingHintProvider(delegate, nil, prometheus.NewRegistry())
	expr := mustParseExpr(t, `{job="api"} |= "error"`)

	_, _, err := provider.ProvideHints(
		context.Background(),
		"tenant-a",
		expr,
		model.TimeFromUnixNano(time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC).UnixNano()),
		model.TimeFromUnixNano(time.Date(2026, 3, 10, 6, 0, 0, 0, time.UTC).UnixNano()),
	)
	require.NoError(t, err)
	require.Equal(t, 1, delegate.Calls())
}

func TestCachingHintProvider_ErrUnsupportedNotCached(t *testing.T) {
	reg := prometheus.NewRegistry()
	backend := newMockHintCacheBackend()
	delegate := &stubHintProvider{err: ErrUnsupported}
	provider := NewCachingHintProvider(delegate, backend, reg)

	expr := mustParseExpr(t, `{job="api"} |~ "error.*"`)
	from := model.TimeFromUnixNano(time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC).UnixNano())
	through := model.TimeFromUnixNano(time.Date(2026, 3, 10, 1, 0, 0, 0, time.UTC).UnixNano())

	_, _, err := provider.ProvideHints(context.Background(), "tenant-a", expr, from, through)
	require.ErrorIs(t, err, ErrUnsupported)
	_, _, err = provider.ProvideHints(context.Background(), "tenant-a", expr, from, through)
	require.ErrorIs(t, err, ErrUnsupported)

	require.Equal(t, 2, delegate.Calls(), "errors must not be cached")
	require.Equal(t, 0, backend.StoreCalls(), "errors must not be stored")
}

func TestCachingHintProvider_FetchErrorFallsBackToDelegate(t *testing.T) {
	reg := prometheus.NewRegistry()
	backend := newMockHintCacheBackend()
	backend.fetchErr = errors.New("cache backend offline")
	delegate := &stubHintProvider{
		hints: &Hints{
			TimeRanges: []HintTimeRange{
				{
					Start: time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC),
					End:   time.Date(2026, 3, 10, 9, 30, 0, 0, time.UTC),
				},
				{
					Start: time.Date(2026, 3, 11, 1, 0, 0, 0, time.UTC),
					End:   time.Date(2026, 3, 11, 1, 30, 0, 0, time.UTC),
				},
			},
		},
		stats: NewQueryStats(),
	}
	provider := NewCachingHintProvider(delegate, backend, reg)

	expr := mustParseExpr(t, `{job="api"} |= "error"`)
	from := time.Date(2026, 3, 10, 8, 0, 0, 0, time.UTC)
	through := time.Date(2026, 3, 11, 4, 0, 0, 0, time.UTC)

	hints, _, err := provider.ProvideHints(
		context.Background(),
		"tenant-a",
		expr,
		model.TimeFromUnixNano(from.UnixNano()),
		model.TimeFromUnixNano(through.UnixNano()),
	)
	require.NoError(t, err, "cache fetch failure should fall back to delegate")
	require.Equal(t, 2, delegate.Calls(), "delegate should be called for each missed day")
	require.ElementsMatch(t, delegate.hints.TimeRanges, hints.TimeRanges)
}

func TestCachingHintProvider_SkipCacheBypassesFetchAndStore(t *testing.T) {
	reg := prometheus.NewRegistry()
	backend := newMockHintCacheBackend()
	delegate := &stubHintProvider{
		hints: &Hints{
			TimeRanges: []HintTimeRange{
				{
					Start: time.Date(2026, 3, 10, 8, 0, 0, 0, time.UTC),
					End:   time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC),
				},
			},
		},
	}
	provider := NewCachingHintProvider(delegate, backend, reg)
	expr := mustParseExpr(t, `{job="api"} |= "error"`)

	ctx := WithSkipCache(context.Background())
	require.True(t, SkipCache(ctx))

	_, _, err := provider.ProvideHints(
		ctx,
		"tenant-a",
		expr,
		model.TimeFromUnixNano(time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC).UnixNano()),
		model.TimeFromUnixNano(time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC).UnixNano()),
	)
	require.NoError(t, err)
	require.Equal(t, 1, delegate.Calls())
	require.Equal(t, 0, backend.FetchCalls(), "skip should avoid cache fetch")
	require.Equal(t, 0, backend.StoreCalls(), "skip should avoid cache store")
}

func TestCachingHintProvider_CachesEmptyDays(t *testing.T) {
	reg := prometheus.NewRegistry()
	backend := newMockHintCacheBackend()
	delegate := &stubHintProvider{
		hints: &Hints{
			TimeRanges: []HintTimeRange{
				{
					Start: time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC),
					End:   time.Date(2026, 3, 10, 10, 10, 0, 0, time.UTC),
				},
			},
		},
	}
	provider := NewCachingHintProvider(delegate, backend, reg)
	expr := mustParseExpr(t, `{job="api"} |= "error"`)
	tenant := "tenant-a"
	from := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	through := time.Date(2026, 3, 11, 23, 0, 0, 0, time.UTC)

	_, _, err := provider.ProvideHints(
		context.Background(),
		tenant,
		expr,
		model.TimeFromUnixNano(from.UnixNano()),
		model.TimeFromUnixNano(through.UnixNano()),
	)
	require.NoError(t, err)
	require.Equal(t, 2, delegate.Calls(), "all missed days should fetch independently")
	require.Equal(t, 2, backend.StoreCalls(), "each missed day should store one payload")

	days := buildDayWindows(tenant, expr.String(), "", from, through)
	require.Len(t, days, 2)

	decodedDayOne, err := unmarshalCachedHints(backend.data[days[0].hashedKey])
	require.NoError(t, err)
	require.Len(t, decodedDayOne, 1)

	decodedDayTwo, err := unmarshalCachedHints(backend.data[days[1].hashedKey])
	require.NoError(t, err)
	require.Nil(t, decodedDayTwo, "empty days should still be cached as empty payloads")
}

func TestCachingHintProvider_UsesDelegateMinDateInCacheKeys(t *testing.T) {
	reg := prometheus.NewRegistry()
	backend := newMockHintCacheBackend()
	delegate := &stubHintProvider{
		minDate: time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC),
		hints: &Hints{
			TimeRanges: []HintTimeRange{
				{
					Start: time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC),
					End:   time.Date(2026, 3, 10, 10, 10, 0, 0, time.UTC),
				},
			},
		},
	}
	provider := NewCachingHintProvider(delegate, backend, reg)
	expr := mustParseExpr(t, `{job="api"} |= "error"`)
	tenant := "tenant-a"
	from := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	through := time.Date(2026, 3, 10, 23, 0, 0, 0, time.UTC)

	_, _, err := provider.ProvideHints(
		context.Background(),
		tenant,
		expr,
		model.TimeFromUnixNano(from.UnixNano()),
		model.TimeFromUnixNano(through.UnixNano()),
	)
	require.NoError(t, err)

	withMinDate := buildDayWindows(tenant, expr.String(), "2026-03-08", from, through)
	withoutMinDate := buildDayWindows(tenant, expr.String(), "", from, through)
	require.Contains(t, backend.data, withMinDate[0].hashedKey)
	require.NotContains(t, backend.data, withoutMinDate[0].hashedKey)
}

type mutableWindowHintProvider struct {
	mu sync.Mutex

	indexRanges []HintTimeRange

	calls int
}

func (p *mutableWindowHintProvider) ProvideHints(
	_ context.Context,
	_ string,
	_ syntax.Expr,
	from,
	through model.Time,
) (*Hints, *QueryStats, error) {
	p.mu.Lock()
	p.calls++
	indexRanges := append([]HintTimeRange(nil), p.indexRanges...)
	p.mu.Unlock()

	start := from.Time().UTC()
	end := through.Time().UTC()
	if end.Before(start) {
		end = start
	}
	out := make([]HintTimeRange, 0, len(indexRanges))
	for _, r := range indexRanges {
		if r.End.Before(start) || end.Before(r.Start) {
			continue
		}
		out = append(out, r)
	}
	return &Hints{TimeRanges: normalizeRanges(out)}, NewQueryStats(), nil
}

func (p *mutableWindowHintProvider) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *mutableWindowHintProvider) SetRanges(ranges []HintTimeRange) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.indexRanges = append([]HintTimeRange(nil), ranges...)
}

func (p *mutableWindowHintProvider) MinDate() time.Time {
	return time.Time{}
}

func TestCachingHintProvider_WindowWideningShouldNotReturnStaleHints(t *testing.T) {
	reg := prometheus.NewRegistry()
	backend := newMockHintCacheBackend()

	oldRange := HintTimeRange{
		Start: time.Date(2026, 5, 8, 3, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 5, 8, 3, 0, 1, 0, time.UTC),
	}
	newRange := HintTimeRange{
		Start: time.Date(2026, 5, 8, 12, 0, 8, 0, time.UTC),
		End:   time.Date(2026, 5, 8, 12, 0, 10, 0, time.UTC),
	}
	delegate := &mutableWindowHintProvider{
		indexRanges: []HintTimeRange{oldRange, newRange},
	}
	provider := NewCachingHintProvider(delegate, backend, reg)

	expr := mustParseExpr(t, `{job="api"} |= "needle"`)
	tenant := "tenant-a"
	from := time.Date(2026, 5, 8, 2, 50, 0, 0, time.UTC)
	narrowEnd := time.Date(2026, 5, 8, 12, 0, 7, 500_000_000, time.UTC)
	widerEnd := time.Date(2026, 5, 8, 12, 0, 13, 600_000_000, time.UTC)

	firstHints, _, err := provider.ProvideHints(
		context.Background(),
		tenant,
		expr,
		model.TimeFromUnixNano(from.UnixNano()),
		model.TimeFromUnixNano(narrowEnd.UnixNano()),
	)
	require.NoError(t, err)
	require.Equal(t, []HintTimeRange{oldRange}, firstHints.TimeRanges)

	// Expected behavior: same-day follow-up query should still include hints
	// needed for the wider window.
	cachedHints, cachedStats, err := provider.ProvideHints(
		context.Background(),
		tenant,
		expr,
		model.TimeFromUnixNano(from.UnixNano()),
		model.TimeFromUnixNano(widerEnd.UnixNano()),
	)
	require.NoError(t, err)
	require.NotNil(t, cachedStats)
	require.ElementsMatch(t, []HintTimeRange{oldRange, newRange}, cachedHints.TimeRanges)

	// Bypass cache for control: fresh delegate result includes both ranges.
	freshHints, _, err := provider.ProvideHints(
		WithSkipCache(context.Background()),
		tenant,
		expr,
		model.TimeFromUnixNano(from.UnixNano()),
		model.TimeFromUnixNano(widerEnd.UnixNano()),
	)
	require.NoError(t, err)
	require.ElementsMatch(t, []HintTimeRange{oldRange, newRange}, freshHints.TimeRanges)
}
