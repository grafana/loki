package ingester

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	gokitlog "github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/runtime"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util/flagext"
	"github.com/grafana/loki/v3/pkg/validation"
)

var (
	countExtractor = func() log.StreamSampleExtractor {
		ex, err := log.NewLineSampleExtractor(log.CountExtractor, nil, nil, false, false)
		if err != nil {
			panic(err)
		}
		return ex.ForStream(labels.Labels{})
	}
)

func TestMaxReturnedStreamsErrors(t *testing.T) {
	numLogs := 100

	tt := []struct {
		name       string
		limit      int
		expectErrs int
	}{
		{"10", 10, 10},
		{"unlimited", 0, numLogs},
	}

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.MaxReturnedErrors = tc.limit

			chunkfmt, headfmt := defaultChunkFormat(t)
			s := newStream(
				chunkfmt,
				headfmt,
				cfg,
				limiter.rateLimitStrategy,
				"fake",
				model.Fingerprint(0),
				labels.Labels{
					{Name: "foo", Value: "bar"},
				},
				true,
				NewStreamRateCalculator(),
				NilMetrics,
				nil,
				nil,
			)

			_, err := s.Push(context.Background(), []logproto.Entry{
				{Timestamp: time.Unix(int64(numLogs), 0), Line: "log"},
			}, recordPool.GetRecord(), 0, true, false, nil)
			require.NoError(t, err)

			newLines := make([]logproto.Entry, numLogs)
			for i := 0; i < numLogs; i++ {
				newLines[i] = logproto.Entry{Timestamp: time.Unix(int64(i), 0), Line: "log"}
			}

			var expected bytes.Buffer
			for i := 0; i < tc.expectErrs; i++ {
				fmt.Fprintf(&expected,
					"entry with timestamp %s ignored, reason: 'entry too far behind, entry timestamp is: %s, oldest acceptable timestamp is: %s',\n",
					time.Unix(int64(i), 0).String(),
					newLines[i].Timestamp.Format(time.RFC3339),
					time.Unix(int64(numLogs), 0).Format(time.RFC3339),
				)
			}

			fmt.Fprintf(&expected, "user 'fake', total ignored: %d out of %d for stream: {foo=\"bar\"}", numLogs, numLogs)
			expectErr := httpgrpc.Errorf(http.StatusBadRequest, "%s", expected.String())

			_, err = s.Push(context.Background(), newLines, recordPool.GetRecord(), 0, true, false, nil)
			require.Error(t, err)
			require.Equal(t, expectErr.Error(), err.Error())
		})
	}
}

func TestPushDeduplication(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})

	chunkfmt, headfmt := defaultChunkFormat(t)

	s := newStream(
		chunkfmt,
		headfmt,
		defaultConfig(),
		limiter.rateLimitStrategy,
		"fake",
		model.Fingerprint(0),
		labels.Labels{
			{Name: "foo", Value: "bar"},
		},
		true,
		NewStreamRateCalculator(),
		NilMetrics,
		nil,
		nil,
	)

	written, err := s.Push(context.Background(), []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "test"},
		{Timestamp: time.Unix(1, 0), Line: "test"},
		{Timestamp: time.Unix(1, 0), Line: "newer, better test"},
	}, recordPool.GetRecord(), 0, true, false, nil)
	require.NoError(t, err)
	require.Len(t, s.chunks, 1)
	require.Equal(t, s.chunks[0].chunk.Size(), 2,
		"expected exact duplicate to be dropped and newer content with same timestamp to be appended")
	require.Equal(t, len("test"+"newer, better test"), written)
}

func TestPushDeduplicationExtraMetrics(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})

	chunkfmt, headfmt := defaultChunkFormat(t)

	buf := bytes.NewBuffer(nil)
	logger := gokitlog.NewLogfmtLogger(buf)

	provider := &providerMock{
		tenantConfig: func(tenantID string) *runtime.Config {
			if tenantID == "fake" {
				return &runtime.Config{
					LogDuplicateMetrics:    true,
					LogDuplicateStreamInfo: true,
				}
			}

			return &runtime.Config{}
		},
	}

	runtimeCfg, err := runtime.NewTenantConfigs(provider)

	registry := prometheus.NewRegistry()
	manager := writefailures.NewManager(logger, registry, writefailures.Cfg{LogRate: flagext.ByteSize(1000), AddInsightsLabel: true}, runtimeCfg, "ingester")

	require.NoError(t, err)
	metrics := newIngesterMetrics(registry, "loki")

	s := newStream(
		chunkfmt,
		headfmt,
		defaultConfig(),
		limiter.rateLimitStrategy,
		"fake",
		model.Fingerprint(0),
		labels.Labels{
			{Name: "foo", Value: "bar"},
		},
		true,
		NewStreamRateCalculator(),
		metrics,
		manager,
		runtimeCfg,
	)

	_, err = s.Push(context.Background(), []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "test"},
	}, recordPool.GetRecord(), 0, true, false, nil)
	require.NoError(t, err)
	_, err = s.Push(context.Background(), []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "not a test"},
	}, recordPool.GetRecord(), 0, true, false, nil)
	require.NoError(t, err)
	_, err = s.Push(context.Background(), []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "test"},
	}, recordPool.GetRecord(), 0, true, false, nil)
	require.NoError(t, err)
	require.Len(t, s.chunks, 1)
	require.Equal(t, 2, s.chunks[0].chunk.Size(), "expected exact duplicate to be dropped and newer content with same timestamp to be appended")
	require.Equal(t, float64(4), testutil.ToFloat64(metrics.duplicateLogBytesTotal.WithLabelValues("fake")))

	content := buf.String()
	require.NotEmpty(t, content)
	require.Contains(t, content, "insight")
	require.Contains(t, content, "duplicate")
}

func TestPushRejectOldCounter(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})

	chunkfmt, headfmt := defaultChunkFormat(t)

	s := newStream(
		chunkfmt,
		headfmt,
		defaultConfig(),
		limiter.rateLimitStrategy,
		"fake",
		model.Fingerprint(0),
		labels.Labels{
			{Name: "foo", Value: "bar"},
		},
		true,
		NewStreamRateCalculator(),
		NilMetrics,
		nil,
		nil,
	)

	// counter should be 2 now since the first line will be deduped
	_, err = s.Push(context.Background(), []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "test"},
		{Timestamp: time.Unix(1, 0), Line: "test"},
		{Timestamp: time.Unix(1, 0), Line: "newer, better test"},
	}, recordPool.GetRecord(), 0, true, false, nil)
	require.NoError(t, err)
	require.Len(t, s.chunks, 1)
	require.Equal(t, s.chunks[0].chunk.Size(), 2,
		"expected exact duplicate to be dropped and newer content with same timestamp to be appended")

	// fail to push with a counter <= the streams internal counter
	_, err = s.Push(context.Background(), []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "test"},
	}, recordPool.GetRecord(), 2, true, false, nil)
	require.Equal(t, ErrEntriesExist, err)

	// succeed with a greater counter
	_, err = s.Push(context.Background(), []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "test"},
	}, recordPool.GetRecord(), 3, true, false, nil)
	require.Nil(t, err)

}

func TestStreamIterator(t *testing.T) {
	const chunks = 3
	const entries = 100

	for _, chk := range []struct {
		name string
		new  func() *chunkenc.MemChunk
	}{
		{"gzipChunk", func() *chunkenc.MemChunk {
			chunkfmt, headfmt := defaultChunkFormat(t)

			return chunkenc.NewMemChunk(chunkfmt, compression.GZIP, headfmt, 256*1024, 0)
		}},
	} {
		t.Run(chk.name, func(t *testing.T) {
			var s stream
			for i := int64(0); i < chunks; i++ {
				chunk := chk.new()
				for j := int64(0); j < entries; j++ {
					k := i*entries + j
					dup, err := chunk.Append(&logproto.Entry{
						Timestamp: time.Unix(k, 0),
						Line:      fmt.Sprintf("line %d", k),
					})
					require.False(t, dup)
					require.NoError(t, err)
				}
				s.chunks = append(s.chunks, chunkDesc{chunk: chunk})
			}

			for i := 0; i < 100; i++ {
				from := rand.Intn(chunks*entries - 1)
				length := rand.Intn(chunks*entries-from) + 1
				iter, err := s.Iterator(context.TODO(), nil, time.Unix(int64(from), 0), time.Unix(int64(from+length), 0), logproto.FORWARD, log.NewNoopPipeline().ForStream(s.labels))
				require.NotNil(t, iter)
				require.NoError(t, err)
				testIteratorForward(t, iter, int64(from), int64(from+length))
				_ = iter.Close()
			}

			for i := 0; i < 100; i++ {
				from := rand.Intn(entries - 1)
				length := rand.Intn(chunks*entries-from) + 1
				iter, err := s.Iterator(context.TODO(), nil, time.Unix(int64(from), 0), time.Unix(int64(from+length), 0), logproto.BACKWARD, log.NewNoopPipeline().ForStream(s.labels))
				require.NotNil(t, iter)
				require.NoError(t, err)
				testIteratorBackward(t, iter, int64(from), int64(from+length))
				_ = iter.Close()
			}
		})
	}
}

func TestEntryErrorCorrectlyReported(t *testing.T) {
	cfg := defaultIngesterTestConfig(t)
	cfg.MaxChunkAge = time.Minute
	l := validation.Limits{
		PerStreamRateLimit:      15,
		PerStreamRateLimitBurst: 15,
	}
	limits, err := validation.NewOverrides(l, nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})

	chunkfmt, headfmt := defaultChunkFormat(t)

	s := newStream(
		chunkfmt,
		headfmt,
		&cfg,
		limiter.rateLimitStrategy,
		"fake",
		model.Fingerprint(0),
		labels.Labels{
			{Name: "foo", Value: "bar"},
		},
		true,
		NewStreamRateCalculator(),
		NilMetrics,
		nil,
		nil,
	)
	s.highestTs = time.Now()

	entries := []logproto.Entry{
		{Line: "observability", Timestamp: time.Now().AddDate(-1 /* year */, 0 /* month */, 0 /* day */)},
		{Line: "short", Timestamp: time.Now()},
	}
	tracker := &mockUsageTracker{}

	_, failed := s.validateEntries(context.Background(), entries, false, true, tracker)
	require.NotEmpty(t, failed)
	require.False(t, hasRateLimitErr(failed))
	require.Equal(t, 13.0, tracker.discardedBytes)
}

func TestUnorderedPush(t *testing.T) {
	cfg := defaultIngesterTestConfig(t)
	cfg.MaxChunkAge = 10 * time.Second
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})

	chunkfmt, headfmt := defaultChunkFormat(t)

	s := newStream(
		chunkfmt,
		headfmt,
		&cfg,
		limiter.rateLimitStrategy,
		"fake",
		model.Fingerprint(0),
		labels.Labels{
			{Name: "foo", Value: "bar"},
		},
		true,
		NewStreamRateCalculator(),
		NilMetrics,
		nil,
		nil,
	)

	for _, x := range []struct {
		cutBefore bool
		entries   []logproto.Entry
		err       bool
		written   int
	}{
		{
			entries: []logproto.Entry{
				{Timestamp: time.Unix(2, 0), Line: "x"},
				{Timestamp: time.Unix(1, 0), Line: "x"},
				{Timestamp: time.Unix(2, 0), Line: "x"}, // duplicate ts/line is ignored
				{Timestamp: time.Unix(2, 0), Line: "x"}, // duplicate ts/line is ignored
				{Timestamp: time.Unix(10, 0), Line: "x"},
			},
			written: 4, // 1 ignored
		},
		// highest ts is now 10, validity bound is (10-10/2) = 5
		{
			entries: []logproto.Entry{
				{Timestamp: time.Unix(4, 0), Line: "x"}, // ordering err, too far
				{Timestamp: time.Unix(8, 0), Line: "x"},
				{Timestamp: time.Unix(9, 0), Line: "x"},
			},
			err:     true,
			written: 2, // 1 ignored
		},
		// force a chunk cut and then push data overlapping with previous chunk.
		// This ultimately ensures the iterators implementation respects unordered chunks.
		{
			cutBefore: true,
			entries: []logproto.Entry{
				{Timestamp: time.Unix(11, 0), Line: "x"},
				{Timestamp: time.Unix(7, 0), Line: "x"},
			},
			written: 2,
		},
	} {
		if x.cutBefore {
			_ = s.cutChunk(context.Background())
		}
		written, err := s.Push(context.Background(), x.entries, recordPool.GetRecord(), 0, true, false, nil)
		if x.err {
			require.NotNil(t, err)
		} else {
			require.Nil(t, err)
		}
		require.Equal(t, x.written, written)
	}

	require.Equal(t, 2, len(s.chunks))

	exp := []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "x"},
		{Timestamp: time.Unix(2, 0), Line: "x"},
		{Timestamp: time.Unix(7, 0), Line: "x"},
		{Timestamp: time.Unix(8, 0), Line: "x"},
		{Timestamp: time.Unix(9, 0), Line: "x"},
		{Timestamp: time.Unix(10, 0), Line: "x"},
		{Timestamp: time.Unix(11, 0), Line: "x"},
	}

	itr, err := s.Iterator(context.Background(), nil, time.Unix(int64(0), 0), time.Unix(12, 0), logproto.FORWARD, log.NewNoopPipeline().ForStream(s.labels))
	require.Nil(t, err)
	iterEq(t, exp, itr)

	sItr, err := s.SampleIterator(context.Background(), nil, time.Unix(int64(0), 0), time.Unix(12, 0), countExtractor())
	require.Nil(t, err)
	for _, x := range exp {
		require.Equal(t, true, sItr.Next())
		require.Equal(t, x.Timestamp, time.Unix(0, sItr.At().Timestamp))
		require.Equal(t, float64(1), sItr.At().Value)
	}
	require.Equal(t, false, sItr.Next())
}

func TestPushRateLimit(t *testing.T) {
	l := validation.Limits{
		PerStreamRateLimit:      10,
		PerStreamRateLimitBurst: 10,
	}
	limits, err := validation.NewOverrides(l, nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})

	chunkfmt, headfmt := defaultChunkFormat(t)

	s := newStream(
		chunkfmt,
		headfmt,
		defaultConfig(),
		limiter.rateLimitStrategy,
		"fake",
		model.Fingerprint(0),
		labels.Labels{
			{Name: "foo", Value: "bar"},
		},
		true,
		NewStreamRateCalculator(),
		NilMetrics,
		nil,
		nil,
	)

	entries := []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "aaaaaaaaaa"},
		{Timestamp: time.Unix(1, 0), Line: "aaaaaaaaab"},
	}
	// Counter should be 2 now since the first line will be deduped.
	tracker := &mockUsageTracker{}
	_, err = s.Push(context.Background(), entries, recordPool.GetRecord(), 0, true, true, tracker)
	require.Error(t, err)
	require.Contains(t, err.Error(), (&validation.ErrStreamRateLimit{RateLimit: l.PerStreamRateLimit, Labels: s.labelsString, Bytes: flagext.ByteSize(len(entries[1].Line))}).Error())
	require.Equal(t, 20.0, tracker.discardedBytes)
}

func TestPushRateLimitAllOrNothing(t *testing.T) {
	l := validation.Limits{
		PerStreamRateLimit:      10,
		PerStreamRateLimitBurst: 10,
	}
	limits, err := validation.NewOverrides(l, nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})

	cfg := defaultConfig()
	chunkfmt, headfmt := defaultChunkFormat(t)

	s := newStream(
		chunkfmt,
		headfmt,
		cfg,
		limiter.rateLimitStrategy,
		"fake",
		model.Fingerprint(0),
		labels.Labels{
			{Name: "foo", Value: "bar"},
		},
		true,
		NewStreamRateCalculator(),
		NilMetrics,
		nil,
		nil,
	)

	entries := []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "aaaaaaaaaa"},
		{Timestamp: time.Unix(1, 0), Line: "aaaaaaaaab"},
	}

	// Both entries have errors because rate limiting is done all at once
	tracker := &mockUsageTracker{}
	_, err = s.Push(context.Background(), entries, recordPool.GetRecord(), 0, true, true, tracker)
	require.Error(t, err)
	require.Contains(t, err.Error(), (&validation.ErrStreamRateLimit{RateLimit: l.PerStreamRateLimit, Labels: s.labelsString, Bytes: flagext.ByteSize(len(entries[0].Line))}).Error())
	require.Contains(t, err.Error(), (&validation.ErrStreamRateLimit{RateLimit: l.PerStreamRateLimit, Labels: s.labelsString, Bytes: flagext.ByteSize(len(entries[1].Line))}).Error())
	require.Equal(t, 20.0, tracker.discardedBytes)
}

func TestReplayAppendIgnoresValidityWindow(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})

	cfg := defaultConfig()
	cfg.MaxChunkAge = time.Minute
	chunkfmt, headfmt := defaultChunkFormat(t)

	s := newStream(
		chunkfmt,
		headfmt,
		cfg,
		limiter.rateLimitStrategy,
		"fake",
		model.Fingerprint(0),
		labels.Labels{
			{Name: "foo", Value: "bar"},
		},
		true,
		NewStreamRateCalculator(),
		NilMetrics,
		nil,
		nil,
	)

	base := time.Now()

	entries := []logproto.Entry{
		{Timestamp: base, Line: "1"},
	}

	// Push a first entry (it doesn't matter if we look like we're replaying or not)
	_, err = s.Push(context.Background(), entries, nil, 1, true, false, nil)
	require.Nil(t, err)

	// Create a sample outside the validity window
	entries = []logproto.Entry{
		{Timestamp: base.Add(-time.Hour), Line: "2"},
	}

	// Pretend it's not a replay, ensure we error
	_, err = s.Push(context.Background(), entries, recordPool.GetRecord(), 0, true, false, nil)
	require.NotNil(t, err)

	// Now pretend it's a replay. The same write should succeed.
	_, err = s.Push(context.Background(), entries, nil, 2, true, false, nil)
	require.Nil(t, err)

}

func iterEq(t *testing.T, exp []logproto.Entry, got iter.EntryIterator) {
	var i int
	for got.Next() {
		require.Equal(t, exp[i].Timestamp, got.At().Timestamp, "failed on the (%d) ts", i)
		require.Equal(t, exp[i].Line, got.At().Line)
		i++
	}
	require.Equal(t, i, len(exp), "incorrect number of entries expected")
}

func Benchmark_PushStream(b *testing.B) {
	ls := labels.Labels{
		labels.Label{Name: "namespace", Value: "loki-dev"},
		labels.Label{Name: "cluster", Value: "dev-us-central1"},
		labels.Label{Name: "job", Value: "loki-dev/ingester"},
		labels.Label{Name: "container", Value: "ingester"},
	}

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(b, err)
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})
	chunkfmt, headfmt := defaultChunkFormat(b)

	s := newStream(chunkfmt, headfmt, &Config{MaxChunkAge: 24 * time.Hour}, limiter.rateLimitStrategy, "fake", model.Fingerprint(0), ls, true, NewStreamRateCalculator(), NilMetrics, nil, nil)
	expr, err := syntax.ParseLogSelector(`{namespace="loki-dev"}`, true)
	require.NoError(b, err)
	t, err := newTailer("foo", expr, &fakeTailServer{}, 10)
	require.NoError(b, err)

	go t.loop()
	defer t.close()

	s.tailers[1] = t
	ctx := context.Background()
	e := entries(100, time.Now())
	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		rec := recordPool.GetRecord()
		_, err := s.Push(ctx, e, rec, 0, true, false, nil)
		require.NoError(b, err)
		recordPool.PutRecord(rec)
	}
}

func defaultChunkFormat(t testing.TB) (byte, chunkenc.HeadBlockFmt) {
	t.Helper()

	cfg := defaultPeriodConfigs[0]

	chunkfmt, headfmt, err := cfg.ChunkFormat()

	require.NoError(t, err)

	return chunkfmt, headfmt
}

type providerMock struct {
	tenantConfig func(string) *runtime.Config
}

func (m *providerMock) TenantConfig(userID string) *runtime.Config {
	return m.tenantConfig(userID)
}
