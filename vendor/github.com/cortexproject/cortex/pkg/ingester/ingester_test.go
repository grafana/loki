package ingester

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	net_context "golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util/chunkcompat"
	"github.com/cortexproject/cortex/pkg/util/validation"
	"github.com/cortexproject/cortex/pkg/util/wire"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
)

type testStore struct {
	mtx sync.Mutex
	// Chunks keyed by userID.
	chunks map[string][]chunk.Chunk
}

func newTestStore(t require.TestingT, cfg Config, clientConfig client.Config, limits validation.Limits) (*testStore, *Ingester) {
	store := &testStore{
		chunks: map[string][]chunk.Chunk{},
	}
	overrides, err := validation.NewOverrides(limits)
	require.NoError(t, err)

	ing, err := New(cfg, clientConfig, overrides, store)
	require.NoError(t, err)

	return store, ing
}

func newDefaultTestStore(t require.TestingT) (*testStore, *Ingester) {
	return newTestStore(t,
		defaultIngesterTestConfig(),
		defaultClientTestConfig(),
		defaultLimitsTestConfig())
}

func (s *testStore) Put(ctx context.Context, chunks []chunk.Chunk) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return err
	}
	for _, chunk := range chunks {
		for k, v := range chunk.Metric {
			if v == "" {
				return fmt.Errorf("Chunk has blank label %q", k)
			}
		}
	}
	s.chunks[userID] = append(s.chunks[userID], chunks...)
	return nil
}

func (s *testStore) Stop() {}

// check that the store is holding data equivalent to what we expect
func (s *testStore) checkData(t *testing.T, userIDs []string, testData map[string]model.Matrix) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	for _, userID := range userIDs {
		res, err := chunk.ChunksToMatrix(context.Background(), s.chunks[userID], model.Time(0), model.Time(math.MaxInt64))
		require.NoError(t, err)
		sort.Sort(res)
		assert.Equal(t, testData[userID], res)
	}
}

func buildTestMatrix(numSeries int, samplesPerSeries int, offset int) model.Matrix {
	m := make(model.Matrix, 0, numSeries)
	for i := 0; i < numSeries; i++ {
		ss := model.SampleStream{
			Metric: model.Metric{
				model.MetricNameLabel: model.LabelValue(fmt.Sprintf("testmetric_%d", i)),
				model.JobLabel:        "testjob",
			},
			Values: make([]model.SamplePair, 0, samplesPerSeries),
		}
		for j := 0; j < samplesPerSeries; j++ {
			ss.Values = append(ss.Values, model.SamplePair{
				Timestamp: model.Time(i + j + offset),
				Value:     model.SampleValue(i + j + offset),
			})
		}
		m = append(m, &ss)
	}
	sort.Sort(m)
	return m
}

func matrixToSamples(m model.Matrix) []model.Sample {
	var samples []model.Sample
	for _, ss := range m {
		for _, sp := range ss.Values {
			samples = append(samples, model.Sample{
				Metric:    ss.Metric,
				Timestamp: sp.Timestamp,
				Value:     sp.Value,
			})
		}
	}
	return samples
}

func runTestQuery(ctx context.Context, t *testing.T, ing *Ingester, ty labels.MatchType, n, v string) (model.Matrix, *client.QueryRequest, error) {
	matcher, err := labels.NewMatcher(ty, n, v)
	if err != nil {
		return nil, nil, err
	}
	req, err := client.ToQueryRequest(model.Earliest, model.Latest, []*labels.Matcher{matcher})
	if err != nil {
		return nil, nil, err
	}
	resp, err := ing.Query(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	res := client.FromQueryResponse(resp)
	sort.Sort(res)
	return res, req, nil
}

func pushTestSamples(t *testing.T, ing *Ingester, numSeries, samplesPerSeries int) ([]string, map[string]model.Matrix) {
	userIDs := []string{"1", "2", "3"}

	// Create test samples.
	testData := map[string]model.Matrix{}
	for i, userID := range userIDs {
		testData[userID] = buildTestMatrix(numSeries, samplesPerSeries, i)
	}

	// Append samples.
	for _, userID := range userIDs {
		ctx := user.InjectOrgID(context.Background(), userID)
		_, err := ing.Push(ctx, client.ToWriteRequest(matrixToSamples(testData[userID]), client.API))
		require.NoError(t, err)
	}
	return userIDs, testData
}

func TestIngesterAppend(t *testing.T) {
	store, ing := newDefaultTestStore(t)

	userIDs, testData := pushTestSamples(t, ing, 10, 1000)

	// Read samples back via ingester queries.
	for _, userID := range userIDs {
		ctx := user.InjectOrgID(context.Background(), userID)
		res, req, err := runTestQuery(ctx, t, ing, labels.MatchRegexp, model.JobLabel, ".+")
		require.NoError(t, err)
		assert.Equal(t, testData[userID], res)

		s := stream{
			ctx: ctx,
		}
		err = ing.QueryStream(req, &s)
		require.NoError(t, err)

		res, err = chunkcompat.StreamsToMatrix(model.Earliest, model.Latest, s.responses)
		require.NoError(t, err)
		assert.Equal(t, testData[userID].String(), res.String())
	}

	// Read samples back via chunk store.
	ing.Shutdown()
	store.checkData(t, userIDs, testData)
}

func TestIngesterIdleFlush(t *testing.T) {
	// Create test ingester with short flush cycle
	cfg := defaultIngesterTestConfig()
	cfg.FlushCheckPeriod = 20 * time.Millisecond
	cfg.MaxChunkIdle = 100 * time.Millisecond
	cfg.RetainPeriod = 500 * time.Millisecond
	store, ing := newTestStore(t, cfg, defaultClientTestConfig(), defaultLimitsTestConfig())

	userIDs, testData := pushTestSamples(t, ing, 4, 100)

	// wait beyond idle time so samples flush
	time.Sleep(cfg.MaxChunkIdle * 2)

	store.checkData(t, userIDs, testData)

	// Check data is still retained by ingester
	for _, userID := range userIDs {
		ctx := user.InjectOrgID(context.Background(), userID)
		res, _, err := runTestQuery(ctx, t, ing, labels.MatchRegexp, model.JobLabel, ".+")
		require.NoError(t, err)
		assert.Equal(t, testData[userID], res)
	}

	// now wait beyond retain time so chunks are removed from memory
	time.Sleep(cfg.RetainPeriod)

	// Check data has gone from ingester
	for _, userID := range userIDs {
		ctx := user.InjectOrgID(context.Background(), userID)
		res, _, err := runTestQuery(ctx, t, ing, labels.MatchRegexp, model.JobLabel, ".+")
		require.NoError(t, err)
		assert.Equal(t, model.Matrix{}, res)
	}
}

type stream struct {
	grpc.ServerStream
	ctx       context.Context
	responses []*client.QueryStreamResponse
}

func (s *stream) Context() net_context.Context {
	return s.ctx
}

func (s *stream) Send(response *client.QueryStreamResponse) error {
	s.responses = append(s.responses, response)
	return nil
}

func TestIngesterAppendOutOfOrderAndDuplicate(t *testing.T) {
	_, ing := newDefaultTestStore(t)
	defer ing.Shutdown()

	m := labelPairs{
		{Name: []byte(model.MetricNameLabel), Value: []byte("testmetric")},
	}
	ctx := user.InjectOrgID(context.Background(), userID)
	err := ing.append(ctx, m, 1, 0, client.API)
	require.NoError(t, err)

	// Two times exactly the same sample (noop).
	err = ing.append(ctx, m, 1, 0, client.API)
	require.NoError(t, err)

	// Earlier sample than previous one.
	err = ing.append(ctx, m, 0, 0, client.API)
	require.Contains(t, err.Error(), "sample timestamp out of order")
	errResp, ok := httpgrpc.HTTPResponseFromError(err)
	require.True(t, ok)
	require.Equal(t, errResp.Code, int32(400))

	// Same timestamp as previous sample, but different value.
	err = ing.append(ctx, m, 1, 1, client.API)
	require.Contains(t, err.Error(), "sample with repeated timestamp but different value")
	errResp, ok = httpgrpc.HTTPResponseFromError(err)
	require.True(t, ok)
	require.Equal(t, errResp.Code, int32(400))
}

// Test that blank labels are removed by the ingester
func TestIngesterAppendBlankLabel(t *testing.T) {
	_, ing := newDefaultTestStore(t)
	defer ing.Shutdown()

	lp := labelPairs{
		{Name: []byte(model.MetricNameLabel), Value: []byte("testmetric")},
		{Name: []byte("foo"), Value: []byte("")},
		{Name: []byte("bar"), Value: []byte("")},
	}
	ctx := user.InjectOrgID(context.Background(), userID)
	err := ing.append(ctx, lp, 1, 0, client.API)
	require.NoError(t, err)

	res, _, err := runTestQuery(ctx, t, ing, labels.MatchEqual, model.MetricNameLabel, "testmetric")
	require.NoError(t, err)

	expected := model.Matrix{
		{
			Metric: model.Metric{model.MetricNameLabel: "testmetric"},
			Values: []model.SamplePair{
				{Timestamp: 1, Value: 0},
			},
		},
	}

	assert.Equal(t, expected, res)
}

func TestIngesterUserSeriesLimitExceeded(t *testing.T) {
	limits := defaultLimitsTestConfig()
	limits.MaxSeriesPerUser = 1

	_, ing := newTestStore(t, defaultIngesterTestConfig(), defaultClientTestConfig(), limits)
	defer ing.Shutdown()

	userID := "1"
	sample1 := model.Sample{
		Metric:    model.Metric{model.MetricNameLabel: "testmetric", "foo": "bar"},
		Timestamp: 0,
		Value:     1,
	}
	sample2 := model.Sample{
		Metric:    model.Metric{model.MetricNameLabel: "testmetric", "foo": "bar"},
		Timestamp: 1,
		Value:     2,
	}
	sample3 := model.Sample{
		Metric:    model.Metric{model.MetricNameLabel: "testmetric", "foo": "biz"},
		Timestamp: 1,
		Value:     3,
	}

	// Append only one series first, expect no error.
	ctx := user.InjectOrgID(context.Background(), userID)
	_, err := ing.Push(ctx, client.ToWriteRequest([]model.Sample{sample1}, client.API))
	require.NoError(t, err)

	// Append to two series, expect series-exceeded error.
	_, err = ing.Push(ctx, client.ToWriteRequest([]model.Sample{sample2, sample3}, client.API))
	if resp, ok := httpgrpc.HTTPResponseFromError(err); !ok || resp.Code != http.StatusTooManyRequests {
		t.Fatalf("expected error about exceeding metrics per user, got %v", err)
	}

	// Read samples back via ingester queries.
	res, _, err := runTestQuery(ctx, t, ing, labels.MatchEqual, model.MetricNameLabel, "testmetric")
	require.NoError(t, err)

	expected := model.Matrix{
		{
			Metric: sample1.Metric,
			Values: []model.SamplePair{
				{
					Timestamp: sample1.Timestamp,
					Value:     sample1.Value,
				},
				{
					Timestamp: sample2.Timestamp,
					Value:     sample2.Value,
				},
			},
		},
	}

	assert.Equal(t, expected, res)
}

func TestIngesterMetricSeriesLimitExceeded(t *testing.T) {
	limits := defaultLimitsTestConfig()
	limits.MaxSeriesPerMetric = 1

	_, ing := newTestStore(t, defaultIngesterTestConfig(), defaultClientTestConfig(), limits)
	defer ing.Shutdown()

	userID := "1"
	sample1 := model.Sample{
		Metric:    model.Metric{model.MetricNameLabel: "testmetric", "foo": "bar"},
		Timestamp: 0,
		Value:     1,
	}
	sample2 := model.Sample{
		Metric:    model.Metric{model.MetricNameLabel: "testmetric", "foo": "bar"},
		Timestamp: 1,
		Value:     2,
	}
	sample3 := model.Sample{
		Metric:    model.Metric{model.MetricNameLabel: "testmetric", "foo": "biz"},
		Timestamp: 1,
		Value:     3,
	}

	// Append only one series first, expect no error.
	ctx := user.InjectOrgID(context.Background(), userID)
	_, err := ing.Push(ctx, client.ToWriteRequest([]model.Sample{sample1}, client.API))
	require.NoError(t, err)

	// Append to two series, expect series-exceeded error.
	_, err = ing.Push(ctx, client.ToWriteRequest([]model.Sample{sample2, sample3}, client.API))
	if resp, ok := httpgrpc.HTTPResponseFromError(err); !ok || resp.Code != http.StatusTooManyRequests {
		t.Fatalf("expected error about exceeding series per metric, got %v", err)
	}

	// Read samples back via ingester queries.
	res, _, err := runTestQuery(ctx, t, ing, labels.MatchEqual, model.MetricNameLabel, "testmetric")
	require.NoError(t, err)

	expected := model.Matrix{
		{
			Metric: sample1.Metric,
			Values: []model.SamplePair{
				{
					Timestamp: sample1.Timestamp,
					Value:     sample1.Value,
				},
				{
					Timestamp: sample2.Timestamp,
					Value:     sample2.Value,
				},
			},
		},
	}

	assert.Equal(t, expected, res)
}

func BenchmarkIngesterSeriesCreationLocking(b *testing.B) {
	for i := 1; i <= 32; i++ {
		b.Run(strconv.Itoa(i), func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				benchmarkIngesterSeriesCreationLocking(b, i)
			}
		})
	}
}

func benchmarkIngesterSeriesCreationLocking(b *testing.B, parallelism int) {
	_, ing := newDefaultTestStore(b)
	defer ing.Shutdown()

	var (
		wg     sync.WaitGroup
		series = int(1e4)
		ctx    = context.Background()
	)
	wg.Add(parallelism)
	ctx = user.InjectOrgID(ctx, "1")
	for i := 0; i < parallelism; i++ {
		seriesPerGoroutine := series / parallelism
		go func(from, through int) {
			defer wg.Done()

			for j := from; j < through; j++ {
				_, err := ing.Push(ctx, &client.WriteRequest{
					Timeseries: []client.PreallocTimeseries{
						{
							TimeSeries: client.TimeSeries{
								Labels: []client.LabelPair{
									{Name: wire.Bytes("__name__"), Value: wire.Bytes(fmt.Sprintf("metric_%d", j))},
								},
								Samples: []client.Sample{
									{TimestampMs: int64(j), Value: float64(j)},
								},
							},
						},
					},
				})
				require.NoError(b, err)
			}

		}(i*seriesPerGoroutine, (i+1)*seriesPerGoroutine)
	}

	wg.Wait()
}

func BenchmarkIngesterPush(b *testing.B) {
	cfg := defaultIngesterTestConfig()
	clientCfg := defaultClientTestConfig()
	limits := defaultLimitsTestConfig()

	const (
		series  = 100
		samples = 100
	)

	// Construct a set of realistic-looking samples, all with slightly different label sets
	labels := chunk.BenchmarkMetric.Clone()
	ts := make([]client.PreallocTimeseries, 0, series)
	for j := 0; j < series; j++ {
		labels["cpu"] = model.LabelValue(fmt.Sprintf("cpu%02d", j))
		ts = append(ts, client.PreallocTimeseries{
			TimeSeries: client.TimeSeries{
				Labels: client.ToLabelPairs(labels),
				Samples: []client.Sample{
					{TimestampMs: 0, Value: float64(j)},
				},
			},
		})
	}
	ctx := user.InjectOrgID(context.Background(), "1")
	b.ResetTimer()
	for iter := 0; iter < b.N; iter++ {
		_, ing := newTestStore(b, cfg, clientCfg, limits)
		// Bump the timestamp on each of our test samples each time round the loop
		for j := 0; j < samples; j++ {
			for i := range ts {
				ts[i].TimeSeries.Samples[0].TimestampMs = int64(i)
			}
			_, err := ing.Push(ctx, &client.WriteRequest{
				Timeseries: ts,
			})
			require.NoError(b, err)
		}
		ing.Shutdown()
	}
}
