package ingester

import (
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"go.uber.org/atomic"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/ingester/index"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"
)

func TestPrepareShutdownMarkerPathNotSet(t *testing.T) {
	ingesterConfig := defaultIngesterTestConfig(t)
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	store := &mockStore{
		chunks: map[string][]chunk.Chunk{},
	}

	mockRing := mockReadRingWithOneActiveIngester()

	i, err := New(ingesterConfig, client.Config{}, store, limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, log.NewNopLogger(), nil, mockRing, nil)
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck

	resp := httptest.NewRecorder()
	i.PrepareShutdown(resp, httptest.NewRequest("GET", "/ingester/prepare-shutdown", nil))
	require.Equal(t, 500, resp.Code)
}

func TestPrepareShutdown(t *testing.T) {
	tempDir := t.TempDir()
	ingesterConfig := defaultIngesterTestConfig(t)
	ingesterConfig.ShutdownMarkerPath = tempDir
	ingesterConfig.WAL.Enabled = true
	ingesterConfig.WAL.Dir = tempDir
	ingesterConfig.LifecyclerConfig.UnregisterOnShutdown = false
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	store := &mockStore{
		chunks: map[string][]chunk.Chunk{},
	}

	readRingMock := mockReadRingWithOneActiveIngester()

	i, err := New(ingesterConfig, client.Config{}, store, limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck

	i.lifecycler.SetFlushOnShutdown(false)
	i.lifecycler.SetUnregisterOnShutdown(false)

	t.Run("GET", func(t *testing.T) {
		resp := httptest.NewRecorder()
		i.PrepareShutdown(resp, httptest.NewRequest("GET", "/ingester/prepare-shutdown", nil))
		require.Equal(t, 200, resp.Code)
		require.Equal(t, "unset", resp.Body.String())
	})

	t.Run("POST", func(t *testing.T) {
		resp := httptest.NewRecorder()
		i.PrepareShutdown(resp, httptest.NewRequest("POST", "/ingester/prepare-shutdown", nil))
		require.Equal(t, 204, resp.Code)
		require.True(t, i.lifecycler.FlushOnShutdown())
		require.True(t, i.lifecycler.ShouldUnregisterOnShutdown())
	})

	t.Run("GET again", func(t *testing.T) {
		resp := httptest.NewRecorder()
		i.PrepareShutdown(resp, httptest.NewRequest("GET", "/ingester/prepare-shutdown", nil))
		require.Equal(t, 200, resp.Code)
		require.Equal(t, "set", resp.Body.String())
	})

	t.Run("DELETE", func(t *testing.T) {
		resp := httptest.NewRecorder()
		i.PrepareShutdown(resp, httptest.NewRequest("DELETE", "/ingester/prepare-shutdown", nil))
		require.Equal(t, 204, resp.Code)
		require.False(t, i.lifecycler.FlushOnShutdown())
		require.False(t, i.lifecycler.ShouldUnregisterOnShutdown())
	})

	t.Run("GET last time", func(t *testing.T) {
		resp := httptest.NewRecorder()
		i.PrepareShutdown(resp, httptest.NewRequest("GET", "/ingester/prepare-shutdown", nil))
		require.Equal(t, 200, resp.Code)
		require.Equal(t, "unset", resp.Body.String())
	})

	t.Run("Unknown HTTP verb", func(t *testing.T) {
		resp := httptest.NewRecorder()
		i.PrepareShutdown(resp, httptest.NewRequest("PAUSE", "/ingester/prepare-shutdown", nil))
		require.Equal(t, 405, resp.Code)
		require.Empty(t, resp.Body.String())
	})
}

func TestIngester_GetStreamRates_Correctness(t *testing.T) {
	ingesterConfig := defaultIngesterTestConfig(t)
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	store := &mockStore{
		chunks: map[string][]chunk.Chunk{},
	}

	readRingMock := mockReadRingWithOneActiveIngester()

	i, err := New(ingesterConfig, client.Config{}, store, limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck

	i.lifecycler.SetFlushOnShutdown(false)
	i.lifecycler.SetUnregisterOnShutdown(false)

	for idx := 0; idx < 100; idx++ {
		// push 100 different streams.
		i.streamRateCalculator.Record("fake", uint64(idx), uint64(idx), 10)
	}

	i.streamRateCalculator.updateRates()

	resp, err := i.GetStreamRates(context.TODO(), nil)
	require.NoError(t, err)
	require.Len(t, resp.StreamRates, 100)
	for idx := 0; idx < 100; idx++ {
		resp.StreamRates[idx].StreamHash = uint64(idx)
		resp.StreamRates[idx].Rate = 10
	}
}

func BenchmarkGetStreamRatesAllocs(b *testing.B) {
	ingesterConfig := defaultIngesterTestConfig(b)
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(b, err)

	store := &mockStore{
		chunks: map[string][]chunk.Chunk{},
	}
	readRingMock := mockReadRingWithOneActiveIngester()

	i, err := New(ingesterConfig, client.Config{}, store, limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(b, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck

	for idx := 0; idx < 1000; idx++ {
		i.streamRateCalculator.Record("fake", uint64(idx), uint64(idx), 10)
	}
	i.streamRateCalculator.updateRates()

	b.ReportAllocs()
	for idx := 0; idx < b.N; idx++ {
		i.GetStreamRates(context.TODO(), nil) //nolint:errcheck
	}
}

func TestIngester(t *testing.T) {
	ingesterConfig := defaultIngesterTestConfig(t)
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	store := &mockStore{
		chunks: map[string][]chunk.Chunk{},
	}

	readRingMock := mockReadRingWithOneActiveIngester()

	i, err := New(ingesterConfig, client.Config{}, store, limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck

	req := logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="bar",bar="baz1"}`,
			},
			{
				Labels: `{foo="bar",bar="baz2"}`,
			},
		},
	}
	for i := 0; i < 10; i++ {
		req.Streams[0].Entries = append(req.Streams[0].Entries, logproto.Entry{
			Timestamp: time.Unix(0, 0),
			Line:      fmt.Sprintf("line %d", i),
		})
		req.Streams[1].Entries = append(req.Streams[1].Entries, logproto.Entry{
			Timestamp: time.Unix(0, 0),
			Line:      fmt.Sprintf("line %d", i),
		})
	}

	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = i.Push(ctx, &req)
	require.NoError(t, err)

	result := mockQuerierServer{
		ctx: ctx,
	}
	err = i.Query(&logproto.QueryRequest{
		Selector: `{foo="bar"}`,
		Limit:    100,
		Start:    time.Unix(0, 0),
		End:      time.Unix(1, 0),
	}, &result)
	require.NoError(t, err)
	// We always send an empty batch to make sure stats are sent, so there will always be one empty response.
	require.Len(t, result.resps, 2)
	require.Len(t, result.resps[0].Streams, 2)

	result = mockQuerierServer{
		ctx: ctx,
	}
	err = i.Query(&logproto.QueryRequest{
		Selector: `{foo="bar",bar="baz1"}`,
		Limit:    100,
		Start:    time.Unix(0, 0),
		End:      time.Unix(1, 0),
	}, &result)
	require.NoError(t, err)
	// We always send an empty batch to make sure stats are sent, so there will always be one empty response.
	require.Len(t, result.resps, 2)
	require.Len(t, result.resps[0].Streams, 1)
	require.Equal(t, `{bar="baz1", foo="bar"}`, result.resps[0].Streams[0].Labels)

	result = mockQuerierServer{
		ctx: ctx,
	}
	err = i.Query(&logproto.QueryRequest{
		Selector: `{foo="bar",bar="baz2"}`,
		Limit:    100,
		Start:    time.Unix(0, 0),
		End:      time.Unix(1, 0),
	}, &result)
	require.NoError(t, err)
	// We always send an empty batch to make sure stats are sent, so there will always be one empty response.
	require.Len(t, result.resps, 2)
	require.Len(t, result.resps[0].Streams, 1)
	require.Equal(t, `{bar="baz2", foo="bar"}`, result.resps[0].Streams[0].Labels)

	// Series

	// empty matcher return all series
	resp, err := i.Series(ctx, &logproto.SeriesRequest{
		Start: time.Unix(0, 0),
		End:   time.Unix(1, 0),
	})
	require.Nil(t, err)
	require.ElementsMatch(t, []logproto.SeriesIdentifier{
		{
			Labels: logproto.MustNewSeriesEntries(
				"bar", "baz1",
				"foo", "bar",
			),
		},
		{
			Labels: logproto.MustNewSeriesEntries(
				"bar", "baz2",
				"foo", "bar",
			),
		},
	}, resp.GetSeries())

	// wrong matchers fmt
	_, err = i.Series(ctx, &logproto.SeriesRequest{
		Start:  time.Unix(0, 0),
		End:    time.Unix(1, 0),
		Groups: []string{`{a="b`},
	})
	require.Error(t, err)

	// no selectors
	_, err = i.Series(ctx, &logproto.SeriesRequest{
		Start:  time.Unix(0, 0),
		End:    time.Unix(1, 0),
		Groups: []string{`{foo="bar"}`, `{}`},
	})
	require.Error(t, err)

	// foo=bar
	resp, err = i.Series(ctx, &logproto.SeriesRequest{
		Start:  time.Unix(0, 0),
		End:    time.Unix(1, 0),
		Groups: []string{`{foo="bar"}`},
	})
	require.Nil(t, err)
	require.ElementsMatch(t, []logproto.SeriesIdentifier{
		{
			Labels: logproto.MustNewSeriesEntries(
				"bar", "baz1",
				"foo", "bar",
			),
		},
		{
			Labels: logproto.MustNewSeriesEntries(
				"bar", "baz2",
				"foo", "bar",
			),
		},
	}, resp.GetSeries())

	// foo=bar, bar=~"baz[2-9]"
	resp, err = i.Series(ctx, &logproto.SeriesRequest{
		Start:  time.Unix(0, 0),
		End:    time.Unix(1, 0),
		Groups: []string{`{foo="bar", bar=~"baz[2-9]"}`},
	})
	require.Nil(t, err)
	require.ElementsMatch(t, []logproto.SeriesIdentifier{
		{
			Labels: logproto.MustNewSeriesEntries(
				"bar", "baz2",
				"foo", "bar",
			),
		},
	}, resp.GetSeries())

	// foo=bar, bar=~"baz[2-9]" in different groups should OR the results
	resp, err = i.Series(ctx, &logproto.SeriesRequest{
		Start:  time.Unix(0, 0),
		End:    time.Unix(1, 0),
		Groups: []string{`{foo="bar"}`, `{bar=~"baz[2-9]"}`},
	})
	require.Nil(t, err)
	require.ElementsMatch(t, []logproto.SeriesIdentifier{
		{
			Labels: logproto.MustNewSeriesEntries(
				"bar", "baz1",
				"foo", "bar",
			),
		},
		{
			Labels: logproto.MustNewSeriesEntries(
				"bar", "baz2",
				"foo", "bar",
			),
		},
	}, resp.GetSeries())
}

func TestIngesterStreamLimitExceeded(t *testing.T) {
	ingesterConfig := defaultIngesterTestConfig(t)
	defaultLimits := defaultLimitsTestConfig()
	defaultLimits.MaxLocalStreamsPerUser = 1
	overrides, err := validation.NewOverrides(defaultLimits, nil)

	require.NoError(t, err)

	store := &mockStore{
		chunks: map[string][]chunk.Chunk{},
	}

	readRingMock := mockReadRingWithOneActiveIngester()

	i, err := New(ingesterConfig, client.Config{}, store, overrides, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck

	req := logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="bar",bar="baz1"}`,
			},
		},
	}
	for i := 0; i < 10; i++ {
		req.Streams[0].Entries = append(req.Streams[0].Entries, logproto.Entry{
			Timestamp: time.Unix(0, 0),
			Line:      fmt.Sprintf("line %d", i),
		})
	}

	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = i.Push(ctx, &req)
	require.NoError(t, err)

	req.Streams[0].Labels = `{foo="bar",bar="baz2"}`
	// The labels in error messages are sorted lexicographically and cleaned up via Push -> ParseLabels.
	expectedLabels, _ := syntax.ParseLabels(req.Streams[0].Labels)

	_, err = i.Push(ctx, &req)
	if resp, ok := httpgrpc.HTTPResponseFromError(err); !ok || resp.Code != http.StatusTooManyRequests {
		t.Fatalf("expected error about exceeding metrics per user, got %v", err)
	}
	require.Contains(t, err.Error(), expectedLabels.String())
}

type mockStore struct {
	mtx    sync.Mutex
	chunks map[string][]chunk.Chunk
}

func (s *mockStore) Put(ctx context.Context, chunks []chunk.Chunk) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	userid, err := tenant.TenantID(ctx)
	if err != nil {
		return err
	}

	s.chunks[userid] = append(s.chunks[userid], chunks...)
	return nil
}

func (s *mockStore) SelectLogs(_ context.Context, _ logql.SelectLogParams) (iter.EntryIterator, error) {
	return nil, nil
}

func (s *mockStore) SelectSamples(_ context.Context, _ logql.SelectSampleParams) (iter.SampleIterator, error) {
	return nil, nil
}

func (s *mockStore) SelectSeries(_ context.Context, _ logql.SelectLogParams) ([]logproto.SeriesIdentifier, error) {
	return nil, nil
}

func (s *mockStore) GetSchemaConfigs() []config.PeriodConfig {
	return defaultPeriodConfigs
}

func (s *mockStore) SetChunkFilterer(_ chunk.RequestChunkFilterer) {
}

// chunk.Store methods
func (s *mockStore) PutOne(_ context.Context, _, _ model.Time, _ chunk.Chunk) error {
	return nil
}

func (s *mockStore) GetChunks(_ context.Context, _ string, _, _ model.Time, _ chunk.Predicate, _ *logproto.ChunkRefGroup) ([][]chunk.Chunk, []*fetcher.Fetcher, error) {
	return nil, nil, nil
}

func (s *mockStore) LabelValuesForMetricName(_ context.Context, _ string, _, _ model.Time, _ string, _ string, _ ...*labels.Matcher) ([]string, error) {
	return []string{"val1", "val2"}, nil
}

func (s *mockStore) LabelNamesForMetricName(_ context.Context, _ string, _, _ model.Time, _ string, _ ...*labels.Matcher) ([]string, error) {
	return nil, nil
}

func (s *mockStore) GetChunkFetcher(_ model.Time) *fetcher.Fetcher {
	return nil
}

func (s *mockStore) Stats(_ context.Context, _ string, _, _ model.Time, _ ...*labels.Matcher) (*stats.Stats, error) {
	return &stats.Stats{
		Streams: 2,
		Chunks:  5,
		Bytes:   25,
		Entries: 100,
	}, nil
}

func (s *mockStore) GetShards(_ context.Context, _ string, _, _ model.Time, _ uint64, _ chunk.Predicate) (*logproto.ShardsResponse, error) {
	return nil, nil
}

func (s *mockStore) HasForSeries(_, _ model.Time) (sharding.ForSeries, bool) {
	return nil, false
}

func (s *mockStore) Volume(_ context.Context, _ string, _, _ model.Time, limit int32, _ []string, _ string, _ ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	return &logproto.VolumeResponse{
		Volumes: []logproto.Volume{
			{Name: `{foo="bar"}`, Volume: 38},
		},
		Limit: limit,
	}, nil
}

func (s *mockStore) Stop() {}

type mockQuerierServer struct {
	ctx   context.Context
	resps []*logproto.QueryResponse
	grpc.ServerStream
}

func (*mockQuerierServer) SetTrailer(metadata.MD) {}

func (m *mockQuerierServer) Send(resp *logproto.QueryResponse) error {
	m.resps = append(m.resps, resp)
	return nil
}

func (m *mockQuerierServer) Context() context.Context {
	return m.ctx
}

func defaultLimitsTestConfig() validation.Limits {
	limits := validation.Limits{}
	flagext.DefaultValues(&limits)
	return limits
}

func TestIngester_buildStoreRequest(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name                       string
		queryStore                 bool
		maxLookBackPeriod          time.Duration
		start, end                 time.Time
		expectedStart, expectedEnd time.Time
		shouldQuery                bool
	}{
		{
			name:        "do not query store",
			queryStore:  false,
			start:       now.Add(-time.Minute),
			end:         now,
			shouldQuery: false,
		},
		{
			name:              "query store with max look back covering whole request duration",
			queryStore:        true,
			maxLookBackPeriod: time.Hour,
			start:             now.Add(-10 * time.Minute),
			end:               now,
			expectedStart:     now.Add(-10 * time.Minute),
			expectedEnd:       now,
			shouldQuery:       true,
		},
		{
			name:              "query store with max look back covering partial request duration",
			queryStore:        true,
			maxLookBackPeriod: time.Hour,
			start:             now.Add(-2 * time.Hour),
			end:               now,
			expectedStart:     now.Add(-time.Hour),
			expectedEnd:       now,
			shouldQuery:       true,
		},
		{
			name:              "query store with max look back not covering request duration at all",
			queryStore:        true,
			maxLookBackPeriod: time.Hour,
			start:             now.Add(-4 * time.Hour),
			end:               now.Add(-2 * time.Hour),
			shouldQuery:       false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ingesterConfig := defaultIngesterTestConfig(t)
			ingesterConfig.QueryStore = tc.queryStore
			ingesterConfig.QueryStoreMaxLookBackPeriod = tc.maxLookBackPeriod

			start, end, ok := buildStoreRequest(ingesterConfig, tc.start, tc.end, now)

			if !tc.shouldQuery {
				require.False(t, ok)
				return
			}
			require.Equal(t, tc.expectedEnd, end, "end")
			require.Equal(t, tc.expectedStart, start, "start")
		})
	}
}

func TestIngester_asyncStoreMaxLookBack(t *testing.T) {
	now := model.Now()

	for _, tc := range []struct {
		name                string
		periodicConfigs     []config.PeriodConfig
		expectedMaxLookBack time.Duration
	}{
		{
			name: "not using async index store",
			periodicConfigs: []config.PeriodConfig{
				{
					From:      config.DayTime{Time: now.Add(-24 * time.Hour)},
					IndexType: "bigtable",
				},
			},
		},
		{
			name: "just one periodic config with boltdb-shipper",
			periodicConfigs: []config.PeriodConfig{
				{
					From:      config.DayTime{Time: now.Add(-24 * time.Hour)},
					IndexType: "boltdb-shipper",
				},
			},
			expectedMaxLookBack: time.Since(now.Add(-24 * time.Hour).Time()),
		},
		{
			name: "just one periodic config with tsdb",
			periodicConfigs: []config.PeriodConfig{
				{
					From:      config.DayTime{Time: now.Add(-24 * time.Hour)},
					IndexType: "tsdb",
				},
			},
			expectedMaxLookBack: time.Since(now.Add(-24 * time.Hour).Time()),
		},
		{
			name: "active config boltdb-shipper, previous config non async index store",
			periodicConfigs: []config.PeriodConfig{
				{
					From:      config.DayTime{Time: now.Add(-48 * time.Hour)},
					IndexType: "bigtable",
				},
				{
					From:      config.DayTime{Time: now.Add(-24 * time.Hour)},
					IndexType: "boltdb-shipper",
				},
			},
			expectedMaxLookBack: time.Since(now.Add(-24 * time.Hour).Time()),
		},
		{
			name: "current and previous config both using async index store",
			periodicConfigs: []config.PeriodConfig{
				{
					From:      config.DayTime{Time: now.Add(-48 * time.Hour)},
					IndexType: "boltdb-shipper",
				},
				{
					From:      config.DayTime{Time: now.Add(-24 * time.Hour)},
					IndexType: "tsdb",
				},
			},
			expectedMaxLookBack: time.Since(now.Add(-48 * time.Hour).Time()),
		},
		{
			name: "active config non async index store, previous config tsdb",
			periodicConfigs: []config.PeriodConfig{
				{
					From:      config.DayTime{Time: now.Add(-48 * time.Hour)},
					IndexType: "tsdb",
				},
				{
					From:      config.DayTime{Time: now.Add(-24 * time.Hour)},
					IndexType: "bigtable",
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ingester := Ingester{periodicConfigs: tc.periodicConfigs}
			mlb := ingester.asyncStoreMaxLookBack()
			require.InDelta(t, tc.expectedMaxLookBack, mlb, float64(time.Second))
		})
	}
}

func TestValidate(t *testing.T) {
	for i, tc := range []struct {
		in          Config
		expected    Config
		expectedErr string
	}{
		{
			in: Config{
				ChunkEncoding: compression.GZIP.String(),
				FlushOpBackoff: backoff.Config{
					MinBackoff: 100 * time.Millisecond,
					MaxBackoff: 10 * time.Second,
					MaxRetries: 1,
				},
				FlushOpTimeout: 15 * time.Second,
				IndexShards:    index.DefaultIndexShards,
				MaxChunkAge:    time.Minute,
			},
			expected: Config{
				ChunkEncoding: compression.GZIP.String(),
				FlushOpBackoff: backoff.Config{
					MinBackoff: 100 * time.Millisecond,
					MaxBackoff: 10 * time.Second,
					MaxRetries: 1,
				},
				FlushOpTimeout: 15 * time.Second,
				IndexShards:    index.DefaultIndexShards,
				MaxChunkAge:    time.Minute,
				parsedEncoding: compression.GZIP,
			},
		},
		{
			in: Config{
				ChunkEncoding: compression.Snappy.String(),
				FlushOpBackoff: backoff.Config{
					MinBackoff: 100 * time.Millisecond,
					MaxBackoff: 10 * time.Second,
					MaxRetries: 1,
				},
				FlushOpTimeout: 15 * time.Second,
				IndexShards:    index.DefaultIndexShards,
			},
			expected: Config{
				ChunkEncoding: compression.Snappy.String(),
				FlushOpBackoff: backoff.Config{
					MinBackoff: 100 * time.Millisecond,
					MaxBackoff: 10 * time.Second,
					MaxRetries: 1,
				},
				FlushOpTimeout: 15 * time.Second,
				IndexShards:    index.DefaultIndexShards,
				parsedEncoding: compression.Snappy,
			},
		},
		{
			in: Config{
				ChunkEncoding: "bad-enc",
				FlushOpBackoff: backoff.Config{
					MinBackoff: 100 * time.Millisecond,
					MaxBackoff: 10 * time.Second,
					MaxRetries: 1,
				},
				FlushOpTimeout: 15 * time.Second,
				IndexShards:    index.DefaultIndexShards,
			},
			expectedErr: "invalid encoding: bad-enc, supported: none, gzip, lz4-64k, snappy, lz4-256k, lz4-1M, lz4, flate, zstd",
		},
		{
			in: Config{
				ChunkEncoding: compression.GZIP.String(),
				FlushOpBackoff: backoff.Config{
					MinBackoff: 100 * time.Millisecond,
					MaxBackoff: 10 * time.Second,
				},
				FlushOpTimeout: 15 * time.Second,
				IndexShards:    index.DefaultIndexShards,
				MaxChunkAge:    time.Minute,
			},
			expectedErr: "invalid flush op max retries: 0",
		},
		{
			in: Config{
				ChunkEncoding: compression.GZIP.String(),
				FlushOpBackoff: backoff.Config{
					MinBackoff: 100 * time.Millisecond,
					MaxBackoff: 10 * time.Second,
					MaxRetries: 1,
				},
				IndexShards: index.DefaultIndexShards,
				MaxChunkAge: time.Minute,
			},
			expectedErr: "invalid flush op timeout: 0s",
		},
		{
			in: Config{
				ChunkEncoding: compression.GZIP.String(),
				FlushOpBackoff: backoff.Config{
					MinBackoff: 100 * time.Millisecond,
					MaxBackoff: 10 * time.Second,
					MaxRetries: 1,
				},
				FlushOpTimeout: 15 * time.Second,
				MaxChunkAge:    time.Minute,
			},
			expectedErr: "invalid ingester index shard factor: 0",
		},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			err := tc.in.Validate()
			if tc.expectedErr != "" {
				require.EqualError(t, err, tc.expectedErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected, tc.in)
			}
		})
	}
}

func Test_InMemoryLabels(t *testing.T) {
	ingesterConfig := defaultIngesterTestConfig(t)
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	store := &mockStore{
		chunks: map[string][]chunk.Chunk{},
	}

	readRingMock := mockReadRingWithOneActiveIngester()

	i, err := New(ingesterConfig, client.Config{}, store, limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck

	req := logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="bar",bar="baz1"}`,
			},
			{
				Labels: `{foo="bar",bar="baz2"}`,
			},
		},
	}
	for i := 0; i < 10; i++ {
		req.Streams[0].Entries = append(req.Streams[0].Entries, logproto.Entry{
			Timestamp: time.Unix(0, 0),
			Line:      fmt.Sprintf("line %d", i),
		})
		req.Streams[1].Entries = append(req.Streams[1].Entries, logproto.Entry{
			Timestamp: time.Unix(0, 0),
			Line:      fmt.Sprintf("line %d", i),
		})
	}

	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = i.Push(ctx, &req)
	require.NoError(t, err)

	start := time.Unix(0, 0)
	res, err := i.Label(ctx, &logproto.LabelRequest{
		Start:  &start,
		Name:   "bar",
		Values: true,
	})

	require.NoError(t, err)
	require.Equal(t, []string{"baz1", "baz2"}, res.Values)

	res, err = i.Label(ctx, &logproto.LabelRequest{Start: &start})
	require.NoError(t, err)
	require.Equal(t, []string{"bar", "foo"}, res.Values)
}

func TestIngester_GetDetectedLabels(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "test")

	ingesterConfig := defaultIngesterTestConfig(t)
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	store := &mockStore{
		chunks: map[string][]chunk.Chunk{},
	}
	readRingMock := mockReadRingWithOneActiveIngester()

	i, err := New(ingesterConfig, client.Config{}, store, limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck

	// Push labels
	req := logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="bar",bar="baz1"}`,
			},
			{
				Labels: `{foo="bar",bar="baz2"}`,
			},
			{
				Labels: `{foo="bar1",bar="baz3"}`,
			},
			{
				Labels: `{foo="foo1",bar="baz1"}`,
			},
			{
				Labels: `{foo="foo",bar="baz1"}`,
			},
		},
	}
	for i := 0; i < 10; i++ {
		req.Streams[0].Entries = append(req.Streams[0].Entries, logproto.Entry{
			Timestamp: time.Unix(0, 0),
			Line:      fmt.Sprintf("line %d", i),
		})
		req.Streams[1].Entries = append(req.Streams[1].Entries, logproto.Entry{
			Timestamp: time.Unix(0, 0),
			Line:      fmt.Sprintf("line %d", i),
		})
	}

	_, err = i.Push(ctx, &req)
	require.NoError(t, err)

	res, err := i.GetDetectedLabels(ctx, &logproto.DetectedLabelsRequest{
		Start: []time.Time{time.Now().Add(11 * time.Nanosecond)}[0],
		End:   []time.Time{time.Now().Add(12 * time.Nanosecond)}[0],
		Query: "",
	})

	require.NoError(t, err)
	fooValues, ok := res.Labels["foo"]
	require.True(t, ok)
	barValues, ok := res.Labels["bar"]
	require.True(t, ok)
	require.Equal(t, 4, len(fooValues.Values))
	require.Equal(t, 3, len(barValues.Values))
}

func TestIngester_GetDetectedLabelsWithQuery(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "test")

	ingesterConfig := defaultIngesterTestConfig(t)
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	store := &mockStore{
		chunks: map[string][]chunk.Chunk{},
	}
	readRingMock := mockReadRingWithOneActiveIngester()

	i, err := New(ingesterConfig, client.Config{}, store, limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck

	// Push labels
	req := logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="bar",bar="baz1"}`,
			},
			{
				Labels: `{foo="bar",bar="baz2"}`,
			},
			{
				Labels: `{foo="bar1",bar="baz3"}`,
			},
			{
				Labels: `{foo="foo1",bar="baz4"}`,
			},
		},
	}
	for i := 0; i < 10; i++ {
		req.Streams[0].Entries = append(req.Streams[0].Entries, logproto.Entry{
			Timestamp: time.Unix(0, 0),
			Line:      fmt.Sprintf("line %d", i),
		})
		req.Streams[1].Entries = append(req.Streams[1].Entries, logproto.Entry{
			Timestamp: time.Unix(0, 0),
			Line:      fmt.Sprintf("line %d", i),
		})
	}

	_, err = i.Push(ctx, &req)
	require.NoError(t, err)

	res, err := i.GetDetectedLabels(ctx, &logproto.DetectedLabelsRequest{
		Start: []time.Time{time.Now().Add(11 * time.Nanosecond)}[0],
		End:   []time.Time{time.Now().Add(11 * time.Nanosecond)}[0],
		Query: `{foo="bar"}`,
	})

	require.NoError(t, err)
	fooValues, ok := res.Labels["foo"]
	require.True(t, ok)
	barValues, ok := res.Labels["bar"]
	require.True(t, ok)
	require.Equal(t, 1, len(fooValues.Values))
	require.Equal(t, 2, len(barValues.Values))
}

func Test_DedupeIngester(t *testing.T) {
	var (
		requests      = int64(400)
		streamCount   = int64(20)
		streams       []labels.Labels
		streamHashes  []uint64
		ingesterCount = 100

		ingesterConfig = defaultIngesterTestConfig(t)
		ctx, _         = user.InjectIntoGRPCRequest(user.InjectOrgID(context.Background(), "foo"))
	)
	// make sure we will cut blocks and chunks and use head chunks
	ingesterConfig.TargetChunkSize = 800
	ingesterConfig.BlockSize = 300

	// created many different ingesters
	ingesterSet, closer := createIngesterSets(t, ingesterConfig, ingesterCount)
	defer closer()

	for i := int64(0); i < streamCount; i++ {
		s := labels.FromStrings("foo", "bar", "bar", fmt.Sprintf("baz%d", i))
		streams = append(streams, s)
		streamHashes = append(streamHashes, s.Hash())
	}
	sort.Slice(streamHashes, func(i, j int) bool { return streamHashes[i] < streamHashes[j] })

	for i := int64(0); i < requests; i++ {
		for _, ing := range ingesterSet {
			_, err := ing.Push(ctx, buildPushRequest(i, streams))
			require.NoError(t, err)
		}
	}

	t.Run("backward log", func(t *testing.T) {
		iterators := make([]iter.EntryIterator, 0, len(ingesterSet))
		for _, client := range ingesterSet {
			stream, err := client.Query(ctx, &logproto.QueryRequest{
				Selector:  `{foo="bar"} | label_format bar=""`, // making it difficult to dedupe by removing uncommon label.
				Start:     time.Unix(0, 0),
				End:       time.Unix(0, requests+1),
				Limit:     uint32(requests * streamCount),
				Direction: logproto.BACKWARD,
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`{foo="bar"} | label_format bar=""`),
				},
			})
			require.NoError(t, err)
			iterators = append(iterators, iter.NewQueryClientIterator(stream, logproto.BACKWARD))
		}
		it := iter.NewMergeEntryIterator(ctx, iterators, logproto.BACKWARD)

		for i := requests - 1; i >= 0; i-- {
			actualHashes := []uint64{}
			for j := 0; j < int(streamCount); j++ {
				require.True(t, it.Next())
				require.Equal(t, fmt.Sprintf("line %d", i), it.At().Line)
				require.Equal(t, i, it.At().Timestamp.UnixNano())
				require.Equal(t, `{bar="", foo="bar"}`, it.Labels())
				actualHashes = append(actualHashes, it.StreamHash())
			}
			sort.Slice(actualHashes, func(i, j int) bool { return actualHashes[i] < actualHashes[j] })
			require.Equal(t, streamHashes, actualHashes)
		}
		require.False(t, it.Next())
		require.NoError(t, it.Err())
	})
	t.Run("forward log", func(t *testing.T) {
		iterators := make([]iter.EntryIterator, 0, len(ingesterSet))
		for _, client := range ingesterSet {
			stream, err := client.Query(ctx, &logproto.QueryRequest{
				Selector:  `{foo="bar"} | label_format bar=""`, // making it difficult to dedupe by removing uncommon label.
				Start:     time.Unix(0, 0),
				End:       time.Unix(0, requests+1),
				Limit:     uint32(requests * streamCount),
				Direction: logproto.FORWARD,
			})
			require.NoError(t, err)
			iterators = append(iterators, iter.NewQueryClientIterator(stream, logproto.FORWARD))
		}
		it := iter.NewMergeEntryIterator(ctx, iterators, logproto.FORWARD)

		for i := int64(0); i < requests; i++ {
			actualHashes := []uint64{}
			for j := 0; j < int(streamCount); j++ {
				require.True(t, it.Next())
				require.Equal(t, fmt.Sprintf("line %d", i), it.At().Line)
				require.Equal(t, i, it.At().Timestamp.UnixNano())
				require.Equal(t, `{bar="", foo="bar"}`, it.Labels())
				actualHashes = append(actualHashes, it.StreamHash())
			}
			sort.Slice(actualHashes, func(i, j int) bool { return actualHashes[i] < actualHashes[j] })
			require.Equal(t, streamHashes, actualHashes)
		}
		require.False(t, it.Next())
		require.NoError(t, it.Err())
	})
	t.Run("sum by metrics", func(t *testing.T) {
		iterators := make([]iter.SampleIterator, 0, len(ingesterSet))
		for _, client := range ingesterSet {
			stream, err := client.QuerySample(ctx, &logproto.SampleQueryRequest{
				Selector: `sum(rate({foo="bar"}[1m])) by (bar)`,
				Start:    time.Unix(0, 0),
				End:      time.Unix(0, requests+1),
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum(rate({foo="bar"}[1m])) by (bar)`),
				},
			})
			require.NoError(t, err)
			iterators = append(iterators, iter.NewSampleQueryClientIterator(stream))
		}
		it := iter.NewMergeSampleIterator(ctx, iterators)
		var expectedLabels []string
		for _, s := range streams {
			expectedLabels = append(expectedLabels, labels.NewBuilder(s).Del("foo").Labels().String())
		}
		sort.Strings(expectedLabels)
		for i := int64(0); i < requests; i++ {
			labels := []string{}
			actualHashes := []uint64{}
			for j := 0; j < int(streamCount); j++ {
				require.True(t, it.Next())
				require.Equal(t, float64(1), it.At().Value)
				require.Equal(t, i, it.At().Timestamp)
				labels = append(labels, it.Labels())
				actualHashes = append(actualHashes, it.StreamHash())
			}
			sort.Strings(labels)
			sort.Slice(actualHashes, func(i, j int) bool { return actualHashes[i] < actualHashes[j] })
			require.Equal(t, expectedLabels, labels)
			require.Equal(t, streamHashes, actualHashes)
		}
		require.False(t, it.Next())
		require.NoError(t, it.Err())
	})
	t.Run("sum metrics", func(t *testing.T) {
		iterators := make([]iter.SampleIterator, 0, len(ingesterSet))
		for _, client := range ingesterSet {
			stream, err := client.QuerySample(ctx, &logproto.SampleQueryRequest{
				Selector: `sum(rate({foo="bar"}[1m]))`,
				Start:    time.Unix(0, 0),
				End:      time.Unix(0, requests+1),
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum(rate({foo="bar"}[1m]))`),
				},
			})
			require.NoError(t, err)
			iterators = append(iterators, iter.NewSampleQueryClientIterator(stream))
		}
		it := iter.NewMergeSampleIterator(ctx, iterators)
		for i := int64(0); i < requests; i++ {
			actualHashes := []uint64{}
			for j := 0; j < int(streamCount); j++ {
				require.True(t, it.Next())
				require.Equal(t, float64(1), it.At().Value)
				require.Equal(t, i, it.At().Timestamp)
				require.Equal(t, "{}", it.Labels())
				actualHashes = append(actualHashes, it.StreamHash())
			}
			sort.Slice(actualHashes, func(i, j int) bool { return actualHashes[i] < actualHashes[j] })
			require.Equal(t, streamHashes, actualHashes)
		}
		require.False(t, it.Next())
		require.NoError(t, it.Err())
	})
}

func Test_DedupeIngesterParser(t *testing.T) {
	var (
		requests      = 100
		streamCount   = 10
		streams       []labels.Labels
		ingesterCount = 30

		ingesterConfig = defaultIngesterTestConfig(t)
		ctx, _         = user.InjectIntoGRPCRequest(user.InjectOrgID(context.Background(), "foo"))
	)
	// make sure we will cut blocks and chunks and use head chunks
	ingesterConfig.TargetChunkSize = 800
	ingesterConfig.BlockSize = 300

	// created many different ingesters
	ingesterSet, closer := createIngesterSets(t, ingesterConfig, ingesterCount)
	defer closer()

	for i := 0; i < streamCount; i++ {
		streams = append(streams, labels.FromStrings("foo", "bar", "bar", fmt.Sprintf("baz%d", i)))
	}

	for i := 0; i < requests; i++ {
		for _, ing := range ingesterSet {
			_, err := ing.Push(ctx, buildPushJSONRequest(int64(i), streams))
			require.NoError(t, err)
		}
	}

	t.Run("backward log", func(t *testing.T) {
		iterators := make([]iter.EntryIterator, 0, len(ingesterSet))
		for _, client := range ingesterSet {
			stream, err := client.Query(ctx, &logproto.QueryRequest{
				Selector:  `{foo="bar"} | json`,
				Start:     time.Unix(0, 0),
				End:       time.Unix(0, int64(requests+1)),
				Limit:     uint32(requests * streamCount * 2),
				Direction: logproto.BACKWARD,
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`{foo="bar"} | json`),
				},
			})
			require.NoError(t, err)
			iterators = append(iterators, iter.NewQueryClientIterator(stream, logproto.BACKWARD))
		}
		it := iter.NewMergeEntryIterator(ctx, iterators, logproto.BACKWARD)

		for i := requests - 1; i >= 0; i-- {
			for j := 0; j < streamCount; j++ {
				for k := 0; k < 2; k++ { // 2 line per entry
					require.True(t, it.Next())
					require.Equal(t, int64(i), it.At().Timestamp.UnixNano())
				}
			}
		}
		require.False(t, it.Next())
		require.NoError(t, it.Err())
	})

	t.Run("forward log", func(t *testing.T) {
		iterators := make([]iter.EntryIterator, 0, len(ingesterSet))
		for _, client := range ingesterSet {
			stream, err := client.Query(ctx, &logproto.QueryRequest{
				Selector:  `{foo="bar"} | json`, // making it difficult to dedupe by removing uncommon label.
				Start:     time.Unix(0, 0),
				End:       time.Unix(0, int64(requests+1)),
				Limit:     uint32(requests * streamCount * 2),
				Direction: logproto.FORWARD,
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`{foo="bar"} | json`),
				},
			})
			require.NoError(t, err)
			iterators = append(iterators, iter.NewQueryClientIterator(stream, logproto.FORWARD))
		}
		it := iter.NewMergeEntryIterator(ctx, iterators, logproto.FORWARD)

		for i := 0; i < requests; i++ {
			for j := 0; j < streamCount; j++ {
				for k := 0; k < 2; k++ { // 2 line per entry
					require.True(t, it.Next())
					require.Equal(t, int64(i), it.At().Timestamp.UnixNano())
				}
			}
		}
		require.False(t, it.Next())
		require.NoError(t, it.Err())
	})
	t.Run("no sum metrics", func(t *testing.T) {
		iterators := make([]iter.SampleIterator, 0, len(ingesterSet))
		for _, client := range ingesterSet {
			stream, err := client.QuerySample(ctx, &logproto.SampleQueryRequest{
				Selector: `rate({foo="bar"} | json [1m])`,
				Start:    time.Unix(0, 0),
				End:      time.Unix(0, int64(requests+1)),
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`rate({foo="bar"} | json [1m])`),
				},
			})
			require.NoError(t, err)
			iterators = append(iterators, iter.NewSampleQueryClientIterator(stream))
		}
		it := iter.NewMergeSampleIterator(ctx, iterators)

		for i := 0; i < requests; i++ {
			for j := 0; j < streamCount; j++ {
				for k := 0; k < 2; k++ { // 2 line per entry
					require.True(t, it.Next())
					require.Equal(t, float64(1), it.At().Value)
					require.Equal(t, int64(i), it.At().Timestamp)
				}
			}
		}
		require.False(t, it.Next())
		require.NoError(t, it.Err())
	})
	t.Run("sum metrics", func(t *testing.T) {
		iterators := make([]iter.SampleIterator, 0, len(ingesterSet))
		for _, client := range ingesterSet {
			stream, err := client.QuerySample(ctx, &logproto.SampleQueryRequest{
				Selector: `sum by (c,d,e,foo) (rate({foo="bar"} | json [1m]))`,
				Start:    time.Unix(0, 0),
				End:      time.Unix(0, int64(requests+1)),
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum by (c,d,e,foo) (rate({foo="bar"} | json [1m]))`),
				},
			})
			require.NoError(t, err)
			iterators = append(iterators, iter.NewSampleQueryClientIterator(stream))
		}
		it := iter.NewMergeSampleIterator(ctx, iterators)

		for i := 0; i < requests; i++ {
			for j := 0; j < streamCount; j++ {
				for k := 0; k < 2; k++ { // 2 line per entry
					require.True(t, it.Next())
					require.Equal(t, float64(1), it.At().Value)
					require.Equal(t, int64(i), it.At().Timestamp)
				}
			}
		}
		require.False(t, it.Next())
		require.NoError(t, it.Err())
	})
}

func TestStats(t *testing.T) {
	ingesterConfig := defaultIngesterTestConfig(t)
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	readRingMock := mockReadRingWithOneActiveIngester()

	i, err := New(ingesterConfig, client.Config{}, &mockStore{}, limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)

	i.instances["test"] = defaultInstance(t)

	ctx := user.InjectOrgID(context.Background(), "test")

	resp, err := i.GetStats(ctx, &logproto.IndexStatsRequest{
		From:     0,
		Through:  11000,
		Matchers: `{host="agent"}`,
	})
	require.NoError(t, err)

	require.Equal(t, &logproto.IndexStatsResponse{
		Streams: 4,
		Chunks:  7,
		Bytes:   185,
		Entries: 110,
	}, resp)
}

func TestVolume(t *testing.T) {
	ingesterConfig := defaultIngesterTestConfig(t)
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	readRingMock := mockReadRingWithOneActiveIngester()

	i, err := New(ingesterConfig, client.Config{}, &mockStore{}, limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)

	i.instances["test"] = defaultInstance(t)

	ctx := user.InjectOrgID(context.Background(), "test")

	t.Run("matching a single label", func(t *testing.T) {
		volumes, err := i.GetVolume(ctx, &logproto.VolumeRequest{
			From:        0,
			Through:     10000,
			Matchers:    `{log_stream=~"dispatcher|worker"}`,
			AggregateBy: seriesvolume.Series,
			Limit:       2,
		})
		require.NoError(t, err)

		require.Equal(t, []logproto.Volume{
			{Name: `{log_stream="dispatcher"}`, Volume: 90},
			{Name: `{log_stream="worker"}`, Volume: 70},
		}, volumes.Volumes)
	})

	t.Run("matching multiple labels, exact", func(t *testing.T) {
		volumes, err := i.GetVolume(ctx, &logproto.VolumeRequest{
			From:     0,
			Through:  10000,
			Matchers: `{log_stream=~"dispatcher|worker", host="agent"}`,
			Limit:    2,
		})
		require.NoError(t, err)

		require.Equal(t, []logproto.Volume{
			{Name: `{host="agent", log_stream="dispatcher"}`, Volume: 90},
			{Name: `{host="agent", log_stream="worker"}`, Volume: 70},
		}, volumes.Volumes)
	})

	t.Run("matching multiple labels, regex", func(t *testing.T) {
		volumes, err := i.GetVolume(ctx, &logproto.VolumeRequest{
			From:     0,
			Through:  10000,
			Matchers: `{log_stream=~"dispatcher|worker", host=~".+"}`,
			Limit:    2,
		})
		require.NoError(t, err)

		require.Equal(t, []logproto.Volume{
			{Name: `{host="agent", log_stream="dispatcher"}`, Volume: 90},
			{Name: `{host="agent", log_stream="worker"}`, Volume: 70},
		}, volumes.Volumes)
	})
}

type ingesterClient struct {
	logproto.PusherClient
	logproto.QuerierClient
}

func createIngesterSets(t *testing.T, config Config, count int) ([]ingesterClient, func()) {
	result := make([]ingesterClient, count)
	closers := make([]func(), count)
	for i := 0; i < count; i++ {
		ingester, closer := createIngesterServer(t, config)
		result[i] = ingester
		closers[i] = closer
	}
	return result, func() {
		for _, closer := range closers {
			closer()
		}
	}
}

func createIngesterServer(t *testing.T, ingesterConfig Config) (ingesterClient, func()) {
	t.Helper()
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	readRingMock := mockReadRingWithOneActiveIngester()

	ing, err := New(ingesterConfig, client.Config{}, &mockStore{}, limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)

	listener := bufconn.Listen(1024 * 1024)

	server := grpc.NewServer(grpc.ChainStreamInterceptor(func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return middleware.StreamServerUserHeaderInterceptor(srv, ss, info, handler)
	}), grpc.ChainUnaryInterceptor(func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		return middleware.ServerUserHeaderInterceptor(ctx, req, info, handler)
	}))

	logproto.RegisterPusherServer(server, ing)
	logproto.RegisterQuerierServer(server, ing)
	go func() {
		if err := server.Serve(listener); err != nil {
			level.Error(ing.logger).Log(err)
		}
	}()

	// nolint:staticcheck // grpc.DialContext() has been deprecated; we'll address it before upgrading to gRPC 2.
	conn, err := grpc.DialContext(context.Background(), "", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
		return listener.Dial()
	}))
	require.NoError(t, err)

	return ingesterClient{
			PusherClient:  logproto.NewPusherClient(conn),
			QuerierClient: logproto.NewQuerierClient(conn),
		}, func() {
			_ = services.StopAndAwaitTerminated(context.Background(), ing)
			server.Stop()
			_ = listener.Close()
		}
}

func buildPushRequest(ts int64, streams []labels.Labels) *logproto.PushRequest {
	req := &logproto.PushRequest{}

	for _, stream := range streams {
		req.Streams = append(req.Streams, logproto.Stream{
			Labels: stream.String(),
			Entries: []logproto.Entry{
				{
					Timestamp: time.Unix(0, ts),
					Line:      fmt.Sprintf("line %d", ts),
				},
			},
		})
	}

	return req
}

func buildPushJSONRequest(ts int64, streams []labels.Labels) *logproto.PushRequest {
	req := &logproto.PushRequest{}

	for _, stream := range streams {
		req.Streams = append(req.Streams, logproto.Stream{
			Labels: stream.String(),
			Entries: []logproto.Entry{
				{
					Timestamp: time.Unix(0, ts),
					Line:      jsonLine(ts, 0),
				},
				{
					Timestamp: time.Unix(0, ts),
					Line:      jsonLine(ts, 1),
				},
			},
		})
	}

	return req
}

func jsonLine(ts int64, i int) string {
	if i%2 == 0 {
		return fmt.Sprintf(`{"a":"b", "c":"d", "e":"f", "g":"h", "ts":"%d"}`, ts)
	}
	return fmt.Sprintf(`{"e":"f", "h":"i", "j":"k", "g":"h", "ts":"%d"}`, ts)
}

type readRingMock struct {
	replicationSet          ring.ReplicationSet
	getAllHealthyCallsCount atomic.Int32
	tokenRangesByIngester   map[string]ring.TokenRanges
}

func (r *readRingMock) HealthyInstancesCount() int {
	return len(r.replicationSet.Instances)
}

func newReadRingMock(ingesters []ring.InstanceDesc, maxErrors int) *readRingMock {
	return &readRingMock{
		replicationSet: ring.ReplicationSet{
			Instances: ingesters,
			MaxErrors: maxErrors,
		},
	}
}

func (r *readRingMock) Describe(_ chan<- *prometheus.Desc) {
}

func (r *readRingMock) Collect(_ chan<- prometheus.Metric) {
}

func (r *readRingMock) Get(_ uint32, _ ring.Operation, _ []ring.InstanceDesc, _ []string, _ []string) (ring.ReplicationSet, error) {
	return r.replicationSet, nil
}

func (r *readRingMock) GetWithOptions(_ uint32, _ ring.Operation, _ ...ring.Option) (ring.ReplicationSet, error) {
	return r.replicationSet, nil
}

func (r *readRingMock) ShuffleShard(_ string, size int) ring.ReadRing {
	// pass by value to copy
	return func(r readRingMock) *readRingMock {
		r.replicationSet.Instances = r.replicationSet.Instances[:size]
		return &r
	}(*r)
}

func (r *readRingMock) BatchGet(_ []uint32, _ ring.Operation) ([]ring.ReplicationSet, error) {
	return []ring.ReplicationSet{r.replicationSet}, nil
}

func (r *readRingMock) GetAllHealthy(_ ring.Operation) (ring.ReplicationSet, error) {
	r.getAllHealthyCallsCount.Add(1)
	return r.replicationSet, nil
}

func (r *readRingMock) GetReplicationSetForOperation(_ ring.Operation) (ring.ReplicationSet, error) {
	return r.replicationSet, nil
}

func (r *readRingMock) ReplicationFactor() int {
	return 1
}

func (r *readRingMock) InstancesCount() int {
	return len(r.replicationSet.Instances)
}

func (r *readRingMock) InstancesInZoneCount(_ string) int {
	return len(r.replicationSet.Instances)
}

func (r *readRingMock) InstancesWithTokensCount() int {
	return len(r.replicationSet.Instances)
}

func (r *readRingMock) InstancesWithTokensInZoneCount(_ string) int {
	return len(r.replicationSet.Instances)
}

func (r *readRingMock) ZonesCount() int {
	return 1
}

func (r *readRingMock) HealthyInstancesInZoneCount() int {
	return len(r.replicationSet.Instances)
}

func (r *readRingMock) Subring(_ uint32, _ int) ring.ReadRing {
	return r
}

func (r *readRingMock) HasInstance(instanceID string) bool {
	for _, ing := range r.replicationSet.Instances {
		if ing.Addr != instanceID {
			return true
		}
	}
	return false
}

func (r *readRingMock) ShuffleShardWithLookback(_ string, _ int, _ time.Duration, _ time.Time) ring.ReadRing {
	return r
}

func (r *readRingMock) CleanupShuffleShardCache(_ string) {}

func (r *readRingMock) GetInstanceState(_ string) (ring.InstanceState, error) {
	return 0, nil
}

func (r *readRingMock) GetTokenRangesForInstance(instance string) (ring.TokenRanges, error) {
	if r.tokenRangesByIngester != nil {
		ranges, exists := r.tokenRangesByIngester[instance]
		if !exists {
			return nil, ring.ErrInstanceNotFound
		}
		return ranges, nil
	}
	tr := ring.TokenRanges{0, math.MaxUint32}
	return tr, nil
}

// WritableInstancesWithTokensCount returns the number of writable instances in the ring that have tokens.
func (r *readRingMock) WritableInstancesWithTokensCount() int {
	return len(r.replicationSet.Instances)
}

// WritableInstancesWithTokensInZoneCount returns the number of writable instances in the ring that are registered in given zone and have tokens.
func (r *readRingMock) WritableInstancesWithTokensInZoneCount(_ string) int {
	return len(r.replicationSet.Instances)
}

func mockReadRingWithOneActiveIngester() *readRingMock {
	return newReadRingMock([]ring.InstanceDesc{
		{Addr: "test", Timestamp: time.Now().UnixNano(), State: ring.ACTIVE, Tokens: []uint32{1, 2, 3}},
	}, 0)
}

func TestUpdateOwnedStreams(t *testing.T) {
	ingesterConfig := defaultIngesterTestConfig(t)
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	readRingMock := mockReadRingWithOneActiveIngester()

	i, err := New(ingesterConfig, client.Config{}, &mockStore{}, limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)

	i.instances["test"] = defaultInstance(t)

	tt := time.Now().Add(-5 * time.Minute)
	err = i.instances["test"].Push(context.Background(), &logproto.PushRequest{Streams: []logproto.Stream{
		// both label sets have FastFingerprint=e002a3a451262627
		{Labels: "{app=\"l\",uniq0=\"0\",uniq1=\"1\"}", Entries: entries(5, tt.Add(time.Minute))},
		{Labels: "{uniq0=\"1\",app=\"m\",uniq1=\"1\"}", Entries: entries(5, tt)},

		// e002a3a451262247
		{Labels: "{app=\"l\",uniq0=\"1\",uniq1=\"0\"}", Entries: entries(5, tt.Add(time.Minute))},
		{Labels: "{uniq1=\"0\",app=\"m\",uniq0=\"0\"}", Entries: entries(5, tt)},

		// e002a2a4512624f4
		{Labels: "{app=\"l\",uniq0=\"0\",uniq1=\"0\"}", Entries: entries(5, tt.Add(time.Minute))},
		{Labels: "{uniq0=\"1\",uniq1=\"0\",app=\"m\"}", Entries: entries(5, tt)},
	}})
	require.NoError(t, err)

	// streams are pushed, let's check owned stream counts
	ownedStreams := i.instances["test"].ownedStreamsSvc.getOwnedStreamCount()
	require.Equal(t, 8, ownedStreams)
}
