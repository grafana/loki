package ingester

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/distributor/shardstreams"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/querier/astmapper"
	loki_runtime "github.com/grafana/loki/pkg/runtime"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/validation"
)

func defaultConfig() *Config {
	cfg := Config{
		BlockSize:     512,
		ChunkEncoding: "gzip",
		IndexShards:   32,
	}
	if err := cfg.Validate(); err != nil {
		panic(errors.Wrap(err, "error building default test config"))
	}
	return &cfg
}

func MustParseDayTime(s string) config.DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return config.DayTime{Time: model.TimeFromUnix(t.Unix())}
}

var defaultPeriodConfigs = []config.PeriodConfig{
	{
		From:      MustParseDayTime("1900-01-01"),
		IndexType: config.StorageTypeBigTable,
	},
}

var NilMetrics = newIngesterMetrics(nil)

func TestLabelsCollisions(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, &ringCountMock{count: 1}, 1)

	i, err := newInstance(defaultConfig(), defaultPeriodConfigs, "test", limiter, loki_runtime.DefaultTenantConfigs(), noopWAL{}, NilMetrics, &OnceSwitch{}, nil, NewStreamRateCalculator(), nil)
	require.Nil(t, err)

	// avoid entries from the future.
	tt := time.Now().Add(-5 * time.Minute)

	// Notice how labels aren't sorted.
	err = i.Push(context.Background(), &logproto.PushRequest{Streams: []logproto.Stream{
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
}

func TestConcurrentPushes(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, &ringCountMock{count: 1}, 1)

	inst, err := newInstance(defaultConfig(), defaultPeriodConfigs, "test", limiter, loki_runtime.DefaultTenantConfigs(), noopWAL{}, NilMetrics, &OnceSwitch{}, nil, NewStreamRateCalculator(), nil)
	require.Nil(t, err)

	const (
		concurrent          = 10
		iterations          = 100
		entriesPerIteration = 100
	)

	uniqueLabels := map[string]bool{}
	startChannel := make(chan struct{})

	wg := sync.WaitGroup{}
	for i := 0; i < concurrent; i++ {
		l := makeRandomLabels()
		for uniqueLabels[l.String()] {
			l = makeRandomLabels()
		}
		uniqueLabels[l.String()] = true

		wg.Add(1)
		go func(labels string) {
			defer wg.Done()

			<-startChannel

			tt := time.Now().Add(-5 * time.Minute)

			for i := 0; i < iterations; i++ {
				err := inst.Push(context.Background(), &logproto.PushRequest{Streams: []logproto.Stream{
					{Labels: labels, Entries: entries(entriesPerIteration, tt)},
				}})

				require.NoError(t, err)

				tt = tt.Add(entriesPerIteration * time.Nanosecond)
			}
		}(l.String())
	}

	time.Sleep(100 * time.Millisecond) // ready
	close(startChannel)                // go!

	wg.Wait()
	// test passes if no goroutine reports error
}

func TestGetStreamRates(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, &ringCountMock{count: 1}, 1)

	inst, err := newInstance(defaultConfig(), defaultPeriodConfigs, "test", limiter, loki_runtime.DefaultTenantConfigs(), noopWAL{}, NilMetrics, &OnceSwitch{}, nil, NewStreamRateCalculator(), nil)
	require.NoError(t, err)

	const (
		concurrent          = 10
		iterations          = 100
		entriesPerIteration = 100
	)

	uniqueLabels := map[string]bool{}
	startChannel := make(chan struct{})
	labelsByHash := map[uint64]labels.Labels{}

	wg := sync.WaitGroup{}
	for i := 0; i < concurrent; i++ {
		l := makeRandomLabels()
		for uniqueLabels[l.String()] {
			l = makeRandomLabels()
		}
		uniqueLabels[l.String()] = true
		labelsByHash[l.Hash()] = l

		wg.Add(1)
		go func(labels string) {
			defer wg.Done()

			<-startChannel

			tt := time.Now().Add(-5 * time.Minute)

			for i := 0; i < iterations; i++ {
				// each iteration generated the entries [hello 0, hello 100) for a total of 790 bytes per push
				_ = inst.Push(context.Background(), &logproto.PushRequest{Streams: []logproto.Stream{
					{Labels: labels, Entries: entries(entriesPerIteration, tt)},
				}})
				tt = tt.Add(entriesPerIteration * time.Nanosecond)
			}
		}(l.String())
	}

	close(startChannel)
	wg.Wait()

	var rates []logproto.StreamRate
	require.Eventually(t, func() bool {
		rates = inst.streamRateCalculator.Rates()

		if len(rates) != concurrent {
			return false
		}

		valid := true
		for i := 0; i < len(rates); i++ {
			streamRates := rates[i]
			origLabels, ok := labelsByHash[streamRates.StreamHash]

			valid = valid && ok &&
				streamRates.Rate == 79000 && // Each stream gets 100 pushes of 790 bytes
				labelHashNoShard(origLabels) == streamRates.StreamHashNoShard
		}

		return valid
	}, 3*time.Second, 100*time.Millisecond)

	// Decay back to 0
	require.Eventually(t, func() bool {
		rates = inst.streamRateCalculator.Rates()
		for _, r := range rates {
			if r.Rate != 0 {
				return false
			}
		}
		return true
	}, 3*time.Second, 100*time.Millisecond)
}

func labelHashNoShard(l labels.Labels) uint64 {
	buf := make([]byte, 256)
	hash, _ := l.HashWithoutLabels(buf, ShardLbName)
	return hash
}

func TestSyncPeriod(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, &ringCountMock{count: 1}, 1)

	const (
		syncPeriod = 1 * time.Minute
		randomStep = time.Second
		entries    = 1000
		minUtil    = 0.20
	)

	inst, err := newInstance(defaultConfig(), defaultPeriodConfigs, "test", limiter, loki_runtime.DefaultTenantConfigs(), noopWAL{}, NilMetrics, &OnceSwitch{}, nil, NewStreamRateCalculator(), nil)
	require.Nil(t, err)

	lbls := makeRandomLabels()

	tt := time.Now()

	var result []logproto.Entry
	for i := 0; i < entries; i++ {
		result = append(result, logproto.Entry{Timestamp: tt, Line: fmt.Sprintf("hello %d", i)})
		tt = tt.Add(time.Duration(1 + rand.Int63n(randomStep.Nanoseconds())))
	}
	pr := &logproto.PushRequest{Streams: []logproto.Stream{{Labels: lbls.String(), Entries: result}}}
	err = inst.Push(context.Background(), pr)
	require.NoError(t, err)

	// let's verify results
	s, err := inst.getOrCreateStream(pr.Streams[0], recordPool.GetRecord())
	require.NoError(t, err)

	// make sure each chunk spans max 'sync period' time
	for _, c := range s.chunks {
		start, end := c.chunk.Bounds()
		span := end.Sub(start)

		const format = "15:04:05.000"
		t.Log(start.Format(format), "--", end.Format(format), span, c.chunk.Utilization())

		require.True(t, span < syncPeriod || c.chunk.Utilization() >= minUtil)
	}
}

func setupTestStreams(t *testing.T) (*instance, time.Time, int) {
	t.Helper()
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, &ringCountMock{count: 1}, 1)
	indexShards := 2

	// just some random values
	cfg := defaultConfig()
	cfg.SyncPeriod = 1 * time.Minute
	cfg.SyncMinUtilization = 0.20
	cfg.IndexShards = indexShards

	instance, err := newInstance(cfg, defaultPeriodConfigs, "test", limiter, loki_runtime.DefaultTenantConfigs(), noopWAL{}, NilMetrics, &OnceSwitch{}, nil, NewStreamRateCalculator(), nil)
	require.Nil(t, err)

	currentTime := time.Now()

	testStreams := []logproto.Stream{
		{Labels: "{app=\"test\",job=\"varlogs\"}", Entries: entries(5, currentTime)},
		{Labels: "{app=\"test2\",job=\"varlogs\"}", Entries: entries(5, currentTime.Add(6*time.Nanosecond))},
		{Labels: "{app=\"test\",job=\"varlogs2\"}", Entries: entries(5, currentTime.Add(12*time.Nanosecond))},
	}

	for _, testStream := range testStreams {
		stream, err := instance.getOrCreateStream(testStream, recordPool.GetRecord())
		require.NoError(t, err)
		chunk := newStream(cfg, limiter, "fake", 0, nil, true, NewStreamRateCalculator(), NilMetrics, nil).NewChunk()
		for _, entry := range testStream.Entries {
			err = chunk.Append(&entry)
			require.NoError(t, err)
		}
		stream.chunks = append(stream.chunks, chunkDesc{chunk: chunk})
	}

	return instance, currentTime, indexShards
}

func Test_LabelQuery(t *testing.T) {
	instance, currentTime, _ := setupTestStreams(t)
	start := &[]time.Time{currentTime.Add(11 * time.Nanosecond)}[0]
	end := &[]time.Time{currentTime.Add(12 * time.Nanosecond)}[0]
	m, err := labels.NewMatcher(labels.MatchEqual, "app", "test")
	require.NoError(t, err)

	tests := []struct {
		name             string
		req              *logproto.LabelRequest
		expectedResponse logproto.LabelResponse
		matchers         []*labels.Matcher
	}{
		{
			"label names - no matchers",
			&logproto.LabelRequest{
				Start: start,
				End:   end,
			},
			logproto.LabelResponse{
				Values: []string{"app", "job"},
			},
			nil,
		},
		{
			"label names - with matcher",
			&logproto.LabelRequest{
				Start: start,
				End:   end,
			},
			logproto.LabelResponse{
				Values: []string{"app", "job"},
			},
			[]*labels.Matcher{m},
		},
		{
			"label values - no matchers",
			&logproto.LabelRequest{
				Name:   "app",
				Values: true,
				Start:  start,
				End:    end,
			},
			logproto.LabelResponse{
				Values: []string{"test", "test2"},
			},
			nil,
		},
		{
			"label values - with matcher",
			&logproto.LabelRequest{
				Name:   "app",
				Values: true,
				Start:  start,
				End:    end,
			},
			logproto.LabelResponse{
				Values: []string{"test"},
			},
			[]*labels.Matcher{m},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := instance.Label(context.Background(), tc.req, tc.matchers...)
			require.NoError(t, err)

			require.Equal(t, tc.expectedResponse.Values, resp.Values)
		})
	}
}

func Test_SeriesQuery(t *testing.T) {
	instance, currentTime, indexShards := setupTestStreams(t)

	tests := []struct {
		name             string
		req              *logproto.SeriesRequest
		expectedResponse []logproto.SeriesIdentifier
	}{
		{
			"non overlapping request",
			&logproto.SeriesRequest{
				Start:  currentTime.Add(11 * time.Nanosecond),
				End:    currentTime.Add(12 * time.Nanosecond),
				Groups: []string{`{job="varlogs"}`},
			},
			[]logproto.SeriesIdentifier{},
		},
		{
			"overlapping request",
			&logproto.SeriesRequest{
				Start:  currentTime.Add(1 * time.Nanosecond),
				End:    currentTime.Add(7 * time.Nanosecond),
				Groups: []string{`{job="varlogs"}`},
			},
			[]logproto.SeriesIdentifier{
				{Labels: map[string]string{"app": "test", "job": "varlogs"}},
				{Labels: map[string]string{"app": "test2", "job": "varlogs"}},
			},
		},
		{
			"overlapping request with shard param",
			&logproto.SeriesRequest{
				Start:  currentTime.Add(1 * time.Nanosecond),
				End:    currentTime.Add(7 * time.Nanosecond),
				Groups: []string{`{job="varlogs"}`},
				Shards: []string{astmapper.ShardAnnotation{
					Shard: 1,
					Of:    indexShards,
				}.String()},
			},
			[]logproto.SeriesIdentifier{
				// Separated by shard number
				{Labels: map[string]string{"app": "test2", "job": "varlogs"}},
			},
		},
		{
			"request end time overlaps stream start time",
			&logproto.SeriesRequest{
				Start:  currentTime.Add(1 * time.Nanosecond),
				End:    currentTime.Add(6 * time.Nanosecond),
				Groups: []string{`{job="varlogs"}`},
			},
			[]logproto.SeriesIdentifier{
				{Labels: map[string]string{"app": "test", "job": "varlogs"}},
			},
		},
		{
			"request start time overlaps stream end time",
			&logproto.SeriesRequest{
				Start:  currentTime.Add(10 * time.Nanosecond),
				End:    currentTime.Add(11 * time.Nanosecond),
				Groups: []string{`{job="varlogs"}`},
			},
			[]logproto.SeriesIdentifier{
				{Labels: map[string]string{"app": "test2", "job": "varlogs"}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := instance.Series(context.Background(), tc.req)
			require.NoError(t, err)

			sort.Slice(resp.Series, func(i, j int) bool {
				return resp.Series[i].String() < resp.Series[j].String()
			})
			sort.Slice(tc.expectedResponse, func(i, j int) bool {
				return tc.expectedResponse[i].String() < tc.expectedResponse[j].String()
			})
			require.Equal(t, tc.expectedResponse, resp.Series)
		})
	}
}

func entries(n int, t time.Time) []logproto.Entry {
	result := make([]logproto.Entry, 0, n)
	for i := 0; i < n; i++ {
		result = append(result, logproto.Entry{Timestamp: t, Line: fmt.Sprintf("hello %d", i), MetadataLabels: labels.Labels{}.String()})
		t = t.Add(time.Nanosecond)
	}
	return result
}

var labelNames = []string{"app", "instance", "namespace", "user", "cluster", ShardLbName}

func makeRandomLabels() labels.Labels {
	ls := labels.NewBuilder(nil)
	for _, ln := range labelNames {
		ls.Set(ln, fmt.Sprintf("%d", rand.Int31()))
	}
	return ls.Labels()
}

func Benchmark_PushInstance(b *testing.B) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(b, err)
	limiter := NewLimiter(limits, NilMetrics, &ringCountMock{count: 1}, 1)

	i, _ := newInstance(&Config{IndexShards: 1}, defaultPeriodConfigs, "test", limiter, loki_runtime.DefaultTenantConfigs(), noopWAL{}, NilMetrics, &OnceSwitch{}, nil, NewStreamRateCalculator(), nil)
	ctx := context.Background()

	for n := 0; n < b.N; n++ {
		_ = i.Push(ctx, &logproto.PushRequest{
			Streams: []logproto.Stream{
				{
					Labels: `{cpu="10",endpoint="https",instance="10.253.57.87:9100",job="node-exporter",mode="idle",namespace="observability",pod="node-exporter-l454v",service="node-exporter"}`,
					Entries: []logproto.Entry{
						{Timestamp: time.Now(), Line: "1"},
						{Timestamp: time.Now(), Line: "2"},
						{Timestamp: time.Now(), Line: "3"},
					},
				},
				{
					Labels: `{cpu="35",endpoint="https",instance="10.253.57.87:9100",job="node-exporter",mode="idle",namespace="observability",pod="node-exporter-l454v",service="node-exporter"}`,
					Entries: []logproto.Entry{
						{Timestamp: time.Now(), Line: "1"},
						{Timestamp: time.Now(), Line: "2"},
						{Timestamp: time.Now(), Line: "3"},
					},
				},
				{
					Labels: `{cpu="89",endpoint="https",instance="10.253.57.87:9100",job="node-exporter",mode="idle",namespace="observability",pod="node-exporter-l454v",service="node-exporter"}`,
					Entries: []logproto.Entry{
						{Timestamp: time.Now(), Line: "1"},
						{Timestamp: time.Now(), Line: "2"},
						{Timestamp: time.Now(), Line: "3"},
					},
				},
			},
		})
	}
}

func Benchmark_instance_addNewTailer(b *testing.B) {
	l := defaultLimitsTestConfig()
	l.MaxLocalStreamsPerUser = 100000
	limits, err := validation.NewOverrides(l, nil)
	require.NoError(b, err)
	limiter := NewLimiter(limits, NilMetrics, &ringCountMock{count: 1}, 1)

	ctx := context.Background()

	inst, _ := newInstance(&Config{}, defaultPeriodConfigs, "test", limiter, loki_runtime.DefaultTenantConfigs(), noopWAL{}, NilMetrics, &OnceSwitch{}, nil, NewStreamRateCalculator(), nil)
	t, err := newTailer("foo", `{namespace="foo",pod="bar",instance=~"10.*"}`, nil, 10)
	require.NoError(b, err)
	for i := 0; i < 10000; i++ {
		require.NoError(b, inst.Push(ctx, &logproto.PushRequest{
			Streams: []logproto.Stream{},
		}))
	}
	b.Run("addNewTailer", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			_ = inst.addNewTailer(context.Background(), t)
		}
	})
	lbs := makeRandomLabels()
	b.Run("addTailersToNewStream", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			inst.addTailersToNewStream(newStream(nil, limiter, "fake", 0, lbs, true, NewStreamRateCalculator(), NilMetrics, nil))
		}
	})
}

func Benchmark_OnceSwitch(b *testing.B) {
	threads := runtime.GOMAXPROCS(0)

	// limit threads
	if threads > 4 {
		threads = 4
	}

	for n := 0; n < b.N; n++ {
		x := &OnceSwitch{}
		var wg sync.WaitGroup
		for i := 0; i < threads; i++ {
			wg.Add(1)
			go func() {
				for i := 0; i < 1000; i++ {
					x.Trigger()
				}
				wg.Done()
			}()
		}
		wg.Wait()
	}
}

func Test_Iterator(t *testing.T) {
	instance := defaultInstance(t)

	it, err := instance.Query(context.TODO(),
		logql.SelectLogParams{
			QueryRequest: &logproto.QueryRequest{
				Selector:  `{job="3"} | logfmt`,
				Limit:     uint32(2),
				Start:     time.Unix(0, 0),
				End:       time.Unix(0, 100000000),
				Direction: logproto.BACKWARD,
			},
		},
	)
	require.NoError(t, err)

	// assert the order is preserved.
	var res *logproto.QueryResponse
	require.NoError(t,
		sendBatches(context.TODO(), it,
			fakeQueryServer(
				func(qr *logproto.QueryResponse) error {
					res = qr
					return nil
				},
			),
			int32(2)),
	)
	require.Equal(t, 2, len(res.Streams))
	// each entry translated into a unique stream
	require.Equal(t, 1, len(res.Streams[0].Entries))
	require.Equal(t, 1, len(res.Streams[1].Entries))
	// sort by entries we expect 9 and 8 this is because readbatch uses a map to build the response.
	// map have no order guarantee
	sort.Slice(res.Streams, func(i, j int) bool {
		return res.Streams[i].Entries[0].Timestamp.UnixNano() > res.Streams[j].Entries[0].Timestamp.UnixNano()
	})
	require.Equal(t, int64(9*1e6), res.Streams[0].Entries[0].Timestamp.UnixNano())
	require.Equal(t, int64(8*1e6), res.Streams[1].Entries[0].Timestamp.UnixNano())
}

type testFilter struct{}

func (t *testFilter) ForRequest(_ context.Context) chunk.Filterer {
	return t
}

func (t *testFilter) ShouldFilter(lbs labels.Labels) bool {
	return lbs.Get("log_stream") == "dispatcher"
}

func Test_ChunkFilter(t *testing.T) {
	instance := defaultInstance(t)
	instance.chunkFilter = &testFilter{}

	it, err := instance.Query(context.TODO(),
		logql.SelectLogParams{
			QueryRequest: &logproto.QueryRequest{
				Selector:  `{job="3"}`,
				Limit:     uint32(2),
				Start:     time.Unix(0, 0),
				End:       time.Unix(0, 100000000),
				Direction: logproto.BACKWARD,
			},
		},
	)
	require.NoError(t, err)
	defer it.Close()

	for it.Next() {
		require.NoError(t, it.Error())
		lbs, err := syntax.ParseLabels(it.Labels())
		require.NoError(t, err)
		require.NotEqual(t, "dispatcher", lbs.Get("log_stream"))
	}
}

func Test_QueryWithDelete(t *testing.T) {
	instance := defaultInstance(t)

	it, err := instance.Query(context.TODO(),
		logql.SelectLogParams{
			QueryRequest: &logproto.QueryRequest{
				Selector:  `{job="3"}`,
				Limit:     uint32(2),
				Start:     time.Unix(0, 0),
				End:       time.Unix(0, 100000000),
				Direction: logproto.BACKWARD,
				Deletes: []*logproto.Delete{
					{
						Selector: `{log_stream="worker"}`,
						Start:    0,
						End:      10 * 1e6,
					},
					{
						Selector: `{log_stream="dispatcher"}`,
						Start:    0,
						End:      5 * 1e6,
					},
					{
						Selector: `{log_stream="dispatcher"} |= "9"`,
						Start:    0,
						End:      10 * 1e6,
					},
				},
			},
		},
	)
	require.NoError(t, err)
	defer it.Close()

	var logs []string
	for it.Next() {
		logs = append(logs, it.Entry().Line)
	}

	require.Equal(t, logs, []string{`msg="dispatcher_7"`})
}

func Test_QuerySampleWithDelete(t *testing.T) {
	instance := defaultInstance(t)

	it, err := instance.QuerySample(context.TODO(),
		logql.SelectSampleParams{
			SampleQueryRequest: &logproto.SampleQueryRequest{
				Selector: `count_over_time({job="3"}[5m])`,
				Start:    time.Unix(0, 0),
				End:      time.Unix(0, 110000000),
				Deletes: []*logproto.Delete{
					{
						Selector: `{log_stream="worker"}`,
						Start:    0,
						End:      10 * 1e6,
					},
					{
						Selector: `{log_stream="dispatcher"}`,
						Start:    0,
						End:      5 * 1e6,
					},
					{
						Selector: `{log_stream="dispatcher"} |= "9"`,
						Start:    0,
						End:      10 * 1e6,
					},
				},
			},
		},
	)
	require.NoError(t, err)
	defer it.Close()

	var samples []float64
	for it.Next() {
		samples = append(samples, it.Sample().Value)
	}

	require.Equal(t, samples, []float64{1.})
}

type fakeLimits struct {
	limits map[string]*validation.Limits
}

func (f fakeLimits) TenantLimits(userID string) *validation.Limits {
	limits, ok := f.limits[userID]
	if !ok {
		return nil
	}

	return limits
}

func (f fakeLimits) AllByUserID() map[string]*validation.Limits {
	return f.limits
}

func TestStreamShardingUsage(t *testing.T) {
	setupCustomTenantLimit := func(perStreamLimit string) *validation.Limits {
		shardStreamsCfg := &shardstreams.Config{Enabled: true, LoggingEnabled: true}
		shardStreamsCfg.DesiredRate.Set("6MB") //nolint:errcheck

		customTenantLimits := &validation.Limits{}
		flagext.DefaultValues(customTenantLimits)

		customTenantLimits.PerStreamRateLimit.Set(perStreamLimit)      //nolint:errcheck
		customTenantLimits.PerStreamRateLimitBurst.Set(perStreamLimit) //nolint:errcheck
		customTenantLimits.ShardStreams = shardStreamsCfg

		return customTenantLimits
	}

	customTenant1 := "my-org1"
	customTenant2 := "my-org2"

	limitsDefinition := &fakeLimits{
		limits: make(map[string]*validation.Limits),
	}
	// testing with 1 because although 1 is enough to accept at least the
	// first line entry, because per-stream sharding is enabled,
	// all entries are rejected if one of them isn't to be accepted.
	limitsDefinition.limits[customTenant1] = setupCustomTenantLimit("1")
	limitsDefinition.limits[customTenant2] = setupCustomTenantLimit("4")

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), limitsDefinition)
	require.NoError(t, err)

	limiter := NewLimiter(limits, NilMetrics, &ringCountMock{count: 1}, 1)

	defaultShardStreamsCfg := limiter.limits.ShardStreams("fake")
	tenantShardStreamsCfg := limiter.limits.ShardStreams(customTenant1)

	t.Run("test default configuration", func(t *testing.T) {
		require.Equal(t, false, defaultShardStreamsCfg.Enabled)
		require.Equal(t, "3MB", defaultShardStreamsCfg.DesiredRate.String())
		require.Equal(t, false, defaultShardStreamsCfg.LoggingEnabled)
	})

	t.Run("test configuration being applied", func(t *testing.T) {
		require.Equal(t, true, tenantShardStreamsCfg.Enabled)
		require.Equal(t, "6MB", tenantShardStreamsCfg.DesiredRate.String())
		require.Equal(t, true, tenantShardStreamsCfg.LoggingEnabled)
	})

	t.Run("invalid push returns error", func(t *testing.T) {
		i, _ := newInstance(&Config{IndexShards: 1}, defaultPeriodConfigs, customTenant1, limiter, loki_runtime.DefaultTenantConfigs(), noopWAL{}, NilMetrics, &OnceSwitch{}, nil, NewStreamRateCalculator(), nil)
		ctx := context.Background()

		err = i.Push(ctx, &logproto.PushRequest{
			Streams: []logproto.Stream{
				{
					Labels: `{cpu="10",endpoint="https",instance="10.253.57.87:9100",job="node-exporter",mode="idle",namespace="observability",pod="node-exporter-l454v",service="node-exporter"}`,
					Entries: []logproto.Entry{
						{Timestamp: time.Now(), Line: "1"},
						{Timestamp: time.Now(), Line: "2"},
						{Timestamp: time.Now(), Line: "3"},
					},
				},
			},
		})
		require.Error(t, err)
	})

	t.Run("valid push returns no error", func(t *testing.T) {
		i, _ := newInstance(&Config{IndexShards: 1}, defaultPeriodConfigs, customTenant2, limiter, loki_runtime.DefaultTenantConfigs(), noopWAL{}, NilMetrics, &OnceSwitch{}, nil, NewStreamRateCalculator(), nil)
		ctx := context.Background()

		err = i.Push(ctx, &logproto.PushRequest{
			Streams: []logproto.Stream{
				{
					Labels: `{myotherlabel="myothervalue"}`,
					Entries: []logproto.Entry{
						{Timestamp: time.Now(), Line: "1"},
						{Timestamp: time.Now(), Line: "2"},
						{Timestamp: time.Now(), Line: "3"},
					},
				},
			},
		})
		require.NoError(t, err)
	})
}

func TestInstance_SeriesVolume(t *testing.T) {
	t.Run("no matchers", func(t *testing.T) {
		instance := defaultInstance(t)
		volumes, err := instance.GetSeriesVolume(context.Background(), &logproto.VolumeRequest{
			From:     0,
			Through:  1.1 * 1e3, //milliseconds
			Matchers: "{}",
			Limit:    2,
		})
		require.NoError(t, err)

		require.Equal(t, []logproto.Volume{
			{Name: `{host="agent", job="3", log_stream="dispatcher"}`, Volume: 90},
			{Name: `{host="agent", job="3", log_stream="worker"}`, Volume: 70},
		}, volumes.Volumes)
	})

	t.Run("with matchers", func(t *testing.T) {
		instance := defaultInstance(t)
		volumes, err := instance.GetSeriesVolume(context.Background(), &logproto.VolumeRequest{
			From:     0,
			Through:  1.1 * 1e3, //milliseconds
			Matchers: `{log_stream="dispatcher"}`,
			Limit:    2,
		})
		require.NoError(t, err)

		require.Equal(t, []logproto.Volume{
			{Name: `{log_stream="dispatcher"}`, Volume: 90},
		}, volumes.Volumes)
	})

	t.Run("excludes streams outside of time bounds", func(t *testing.T) {
		instance := defaultInstance(t)
		volumes, err := instance.GetSeriesVolume(context.Background(), &logproto.VolumeRequest{
			From:     5,
			Through:  1.1 * 1e3, //milliseconds
			Matchers: "{}",
			Limit:    3,
		})
		require.NoError(t, err)

		require.Equal(t, []logproto.Volume{
			{Name: `{host="agent", job="3", log_stream="dispatcher"}`, Volume: 45},
			{Name: `{host="agent", job="3", log_stream="worker"}`, Volume: 26},
		}, volumes.Volumes)
	})

	t.Run("enforces the limit", func(t *testing.T) {
		instance := defaultInstance(t)
		volumes, err := instance.GetSeriesVolume(context.Background(), &logproto.VolumeRequest{
			From:     0,
			Through:  11000,
			Matchers: "{}",
			Limit:    1,
		})
		require.NoError(t, err)

		require.Equal(t, []logproto.Volume{
			{Name: `{host="agent", job="3", log_stream="dispatcher"}`, Volume: 90},
		}, volumes.Volumes)
	})

	t.Run("with targetLabels", func(t *testing.T) {
		t.Run("all targetLabels are added to matchers", func(t *testing.T) {
			instance := defaultInstance(t)
			volumes, err := instance.GetSeriesVolume(context.Background(), &logproto.VolumeRequest{
				From:         0,
				Through:      1.1 * 1e3, //milliseconds
				Matchers:     `{}`,
				Limit:        2,
				TargetLabels: []string{"log_stream"},
			})
			require.NoError(t, err)

			require.Equal(t, []logproto.Volume{
				{Name: `{log_stream="dispatcher"}`, Volume: 90},
				{Name: `{log_stream="worker"}`, Volume: 70},
			}, volumes.Volumes)
		})

		t.Run("with a specific equals matcher", func(t *testing.T) {
			instance := defaultInstance(t)
			volumes, err := instance.GetSeriesVolume(context.Background(), &logproto.VolumeRequest{
				From:         0,
				Through:      1.1 * 1e3, //milliseconds
				Matchers:     `{log_stream="dispatcher"}`,
				Limit:        2,
				TargetLabels: []string{"host"},
			})
			require.NoError(t, err)

			require.Equal(t, []logproto.Volume{
				{Name: `{host="agent"}`, Volume: 90},
			}, volumes.Volumes)
		})

		t.Run("with a specific regexp matcher", func(t *testing.T) {
			instance := defaultInstance(t)
			volumes, err := instance.GetSeriesVolume(context.Background(), &logproto.VolumeRequest{
				From:         0,
				Through:      1.1 * 1e3, //milliseconds
				Matchers:     `{log_stream=~".+"}`,
				Limit:        2,
				TargetLabels: []string{"host", "job"},
			})
			require.NoError(t, err)

			require.Equal(t, []logproto.Volume{
				{Name: `{host="agent", job="3"}`, Volume: 160},
			}, volumes.Volumes)
		})
	})
}

func TestGetStats(t *testing.T) {
	instance := defaultInstance(t)
	resp, err := instance.GetStats(context.Background(), &logproto.IndexStatsRequest{
		From:     0,
		Through:  11000,
		Matchers: `{host="agent"}`,
	})
	require.NoError(t, err)

	require.Equal(t, &logproto.IndexStatsResponse{
		Streams: 2,
		Chunks:  2,
		Bytes:   160,
		Entries: 10,
	}, resp)
}

func defaultInstance(t *testing.T) *instance {
	ingesterConfig := defaultIngesterTestConfig(t)
	defaultLimits := defaultLimitsTestConfig()
	overrides, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)
	instance, err := newInstance(
		&ingesterConfig,
		defaultPeriodConfigs,
		"fake",
		NewLimiter(overrides, NilMetrics, &ringCountMock{count: 1}, 1),
		loki_runtime.DefaultTenantConfigs(),
		noopWAL{},
		NilMetrics,
		nil,
		nil,
		NewStreamRateCalculator(),
		nil,
	)
	require.Nil(t, err)
	insertData(t, instance)

	return instance
}

// inserts 160 bytes into the instance. 90 for the dispatcher label and 70 for the worker label
func insertData(t *testing.T, instance *instance) {
	for i := 0; i < 10; i++ {
		// nolint
		stream := "dispatcher"
		if i%2 == 0 {
			stream = "worker"
		}

		require.NoError(t,
			instance.Push(context.TODO(), &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: fmt.Sprintf(`{host="agent", log_stream="%s",job="3"}`, stream),
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(0, int64(i)*1e6), Line: fmt.Sprintf(`msg="%s_%d"`, stream, i)},
						},
					},
				},
			}),
		)
	}
}

type fakeQueryServer func(*logproto.QueryResponse) error

func (f fakeQueryServer) Send(res *logproto.QueryResponse) error {
	return f(res)
}
func (f fakeQueryServer) Context() context.Context { return context.TODO() }
