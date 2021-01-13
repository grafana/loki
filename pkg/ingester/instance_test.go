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

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util/validation"
)

func defaultConfig() *Config {
	cfg := Config{
		BlockSize:     512,
		ChunkEncoding: "gzip",
	}
	if err := cfg.Validate(); err != nil {
		panic(errors.Wrap(err, "error building default test config"))
	}
	return &cfg
}

var NilMetrics = newIngesterMetrics(nil)

func TestLabelsCollisions(t *testing.T) {
	limits, err := validation.NewOverrides(validation.Limits{MaxLocalStreamsPerUser: 1000}, nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, &ringCountMock{count: 1}, 1)

	i := newInstance(defaultConfig(), "test", limiter, noopWAL{}, nil, &OnceSwitch{})

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
	limits, err := validation.NewOverrides(validation.Limits{MaxLocalStreamsPerUser: 1000}, nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, &ringCountMock{count: 1}, 1)

	inst := newInstance(defaultConfig(), "test", limiter, noopWAL{}, NilMetrics, &OnceSwitch{})

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

func TestSyncPeriod(t *testing.T) {
	limits, err := validation.NewOverrides(validation.Limits{MaxLocalStreamsPerUser: 1000}, nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, &ringCountMock{count: 1}, 1)

	const (
		syncPeriod = 1 * time.Minute
		randomStep = time.Second
		entries    = 1000
		minUtil    = 0.20
	)

	inst := newInstance(defaultConfig(), "test", limiter, noopWAL{}, NilMetrics, &OnceSwitch{})
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
	s, err := inst.getOrCreateStream(pr.Streams[0], false, recordPool.GetRecord())
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

func Test_SeriesQuery(t *testing.T) {
	limits, err := validation.NewOverrides(validation.Limits{MaxLocalStreamsPerUser: 1000}, nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, &ringCountMock{count: 1}, 1)

	// just some random values
	cfg := defaultConfig()
	cfg.SyncPeriod = 1 * time.Minute
	cfg.SyncMinUtilization = 0.20

	instance := newInstance(cfg, "test", limiter, noopWAL{}, NilMetrics, &OnceSwitch{})

	currentTime := time.Now()

	testStreams := []logproto.Stream{
		{Labels: "{app=\"test\",job=\"varlogs\"}", Entries: entries(5, currentTime)},
		{Labels: "{app=\"test2\",job=\"varlogs\"}", Entries: entries(5, currentTime.Add(6*time.Nanosecond))},
	}

	for _, testStream := range testStreams {
		stream, err := instance.getOrCreateStream(testStream, false, recordPool.GetRecord())
		require.NoError(t, err)
		chunk := newStream(cfg, 0, nil, NilMetrics).NewChunk()
		for _, entry := range testStream.Entries {
			err = chunk.Append(&entry)
			require.NoError(t, err)
		}
		stream.chunks = append(stream.chunks, chunkDesc{chunk: chunk})
	}

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
		result = append(result, logproto.Entry{Timestamp: t, Line: fmt.Sprintf("hello %d", i)})
		t = t.Add(time.Nanosecond)
	}
	return result
}

var labelNames = []string{"app", "instance", "namespace", "user", "cluster"}

func makeRandomLabels() labels.Labels {
	ls := labels.NewBuilder(nil)
	for _, ln := range labelNames {
		ls.Set(ln, fmt.Sprintf("%d", rand.Int31()))
	}
	return ls.Labels()
}

func Benchmark_PushInstance(b *testing.B) {
	limits, err := validation.NewOverrides(validation.Limits{MaxLocalStreamsPerUser: 1000}, nil)
	require.NoError(b, err)
	limiter := NewLimiter(limits, &ringCountMock{count: 1}, 1)

	i := newInstance(&Config{}, "test", limiter, noopWAL{}, NilMetrics, &OnceSwitch{})
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
	limits, err := validation.NewOverrides(validation.Limits{MaxLocalStreamsPerUser: 100000}, nil)
	require.NoError(b, err)
	limiter := NewLimiter(limits, &ringCountMock{count: 1}, 1)

	ctx := context.Background()

	inst := newInstance(&Config{}, "test", limiter, noopWAL{}, NilMetrics, &OnceSwitch{})
	t, err := newTailer("foo", `{namespace="foo",pod="bar",instance=~"10.*"}`, nil)
	require.NoError(b, err)
	for i := 0; i < 10000; i++ {
		require.NoError(b, inst.Push(ctx, &logproto.PushRequest{
			Streams: []logproto.Stream{},
		}))
	}
	b.Run("addNewTailer", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			_ = inst.addNewTailer(t)
		}
	})
	lbs := makeRandomLabels()
	b.Run("addTailersToNewStream", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			inst.addTailersToNewStream(newStream(nil, 0, lbs, NilMetrics))
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
