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
	"github.com/grafana/loki/v3/pkg/util"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/loghttp/push"
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
	retentionHours := util.RetentionHours(limiter.limits.RetentionPeriod("fake"))

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
				labels.FromStrings("foo", "bar"),
				NewStreamRateCalculator(),
				NilMetrics,
				nil,
				nil,
				retentionHours,
				noPolicy,
			)

			_, err := lockThenPush(context.Background(), s, []logproto.Entry{
				{Timestamp: time.Unix(int64(numLogs), 0), Line: "log"},
			}, recordPool.GetRecord(), false, nil, "loki")
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

			_, err = lockThenPush(context.Background(), s, newLines, recordPool.GetRecord(), false, nil, "loki")
			require.Error(t, err)
			require.Equal(t, expectErr.Error(), err.Error())
		})
	}
}

func TestPushDeduplication(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})
	retentionHours := util.RetentionHours(limiter.limits.RetentionPeriod("fake"))
	chunkfmt, headfmt := defaultChunkFormat(t)

	s := newStream(
		chunkfmt,
		headfmt,
		defaultConfig(),
		limiter.rateLimitStrategy,
		"fake",
		model.Fingerprint(0),
		labels.FromStrings("foo", "bar"),
		NewStreamRateCalculator(),
		NilMetrics,
		nil,
		nil,
		retentionHours,
		noPolicy,
	)

	written, err := lockThenPush(context.Background(), s, []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "test"},
		{Timestamp: time.Unix(1, 0), Line: "test"},
		{Timestamp: time.Unix(1, 0), Line: "newer, better test"},
	}, recordPool.GetRecord(), false, nil, "loki")
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
	retentionHours := util.RetentionHours(limiter.limits.RetentionPeriod("fake"))
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
		labels.FromStrings("foo", "bar"),
		NewStreamRateCalculator(),
		metrics,
		manager,
		runtimeCfg,
		retentionHours,
		noPolicy,
	)

	_, err = lockThenPush(context.Background(), s, []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "test"},
	}, recordPool.GetRecord(), false, nil, "loki")
	require.NoError(t, err)
	_, err = lockThenPush(context.Background(), s, []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "not a test"},
	}, recordPool.GetRecord(), false, nil, "loki")
	require.NoError(t, err)
	_, err = lockThenPush(context.Background(), s, []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "test"},
	}, recordPool.GetRecord(), false, nil, "loki")
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
	retentionHours := util.RetentionHours(limiter.limits.RetentionPeriod("fake"))
	chunkfmt, headfmt := defaultChunkFormat(t)

	s := newStream(
		chunkfmt,
		headfmt,
		defaultConfig(),
		limiter.rateLimitStrategy,
		"fake",
		model.Fingerprint(0),
		labels.FromStrings("foo", "bar"),
		NewStreamRateCalculator(),
		NilMetrics,
		nil,
		nil,
		retentionHours,
		noPolicy,
	)

	// counter should be 2 now since the first line will be deduped
	_, err = lockThenPush(context.Background(), s, []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "test"},
		{Timestamp: time.Unix(1, 0), Line: "test"},
		{Timestamp: time.Unix(1, 0), Line: "newer, better test"},
	}, recordPool.GetRecord(), false, nil, "loki")
	require.NoError(t, err)
	require.Len(t, s.chunks, 1)
	require.Equal(t, s.chunks[0].chunk.Size(), 2,
		"expected exact duplicate to be dropped and newer content with same timestamp to be appended")

	// fail to push with a counter <= the streams internal counter
	_, err = s.PushWalReplay(context.Background(), []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "test"},
	}, 2, nil)
	require.Equal(t, ErrEntriesExist, err)

	// succeed with a greater counter
	_, err = s.PushWalReplay(context.Background(), []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "test"},
	}, 3, nil)
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
	retentionHours := util.RetentionHours(limiter.limits.RetentionPeriod("fake"))
	chunkfmt, headfmt := defaultChunkFormat(t)

	s := newStream(
		chunkfmt,
		headfmt,
		&cfg,
		limiter.rateLimitStrategy,
		"fake",
		model.Fingerprint(0),
		labels.FromStrings("foo", "bar"),
		NewStreamRateCalculator(),
		NilMetrics,
		nil,
		nil,
		retentionHours,
		noPolicy,
	)
	s.highestTs = time.Now()

	entries := []logproto.Entry{
		{Line: "observability", Timestamp: time.Now().AddDate(-1 /* year */, 0 /* month */, 0 /* day */)},
		{Line: "short", Timestamp: time.Now()},
	}
	tracker := &mockUsageTracker{}

	_, failed := s.validateEntries(context.Background(), flatEntryBatch(entries), false, true, tracker, "loki")
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
	retentionHours := util.RetentionHours(limiter.limits.RetentionPeriod("fake"))
	chunkfmt, headfmt := defaultChunkFormat(t)

	s := newStream(
		chunkfmt,
		headfmt,
		&cfg,
		limiter.rateLimitStrategy,
		"fake",
		model.Fingerprint(0),
		labels.FromStrings("foo", "bar"),
		NewStreamRateCalculator(),
		NilMetrics,
		nil,
		nil,
		retentionHours,
		noPolicy,
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
		written, err := lockThenPush(context.Background(), s, x.entries, recordPool.GetRecord(), false, nil, "loki")
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
	retentionHours := util.RetentionHours(limiter.limits.RetentionPeriod("fake"))
	chunkfmt, headfmt := defaultChunkFormat(t)

	s := newStream(
		chunkfmt,
		headfmt,
		defaultConfig(),
		limiter.rateLimitStrategy,
		"fake",
		model.Fingerprint(0),
		labels.FromStrings("foo", "bar"),
		NewStreamRateCalculator(),
		NilMetrics,
		nil,
		nil,
		retentionHours,
		noPolicy,
	)

	entries := []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "aaaaaaaaaa"},
		{Timestamp: time.Unix(1, 0), Line: "aaaaaaaaab"},
	}
	// Counter should be 2 now since the first line will be deduped.
	tracker := &mockUsageTracker{}
	_, err = lockThenPush(context.Background(), s, entries, recordPool.GetRecord(), true, tracker, "loki")
	require.Error(t, err)
	require.Contains(t, err.Error(), (&validation.ErrStreamRateLimit{RateLimit: l.PerStreamRateLimit, Labels: s.labelsString, Bytes: flagext.ByteSize(len(entries[1].Line))}).Error())
	require.Equal(t, 20.0, tracker.discardedBytes)
}

func TestPushRateLimitPolicyOverride(t *testing.T) {
	const policy = "finance"
	// Tenant limit is generous (would allow the push), but the per-policy override is strict
	// and must REPLACE it for streams resolved to the policy.
	l := validation.Limits{
		PerStreamRateLimit:      100,
		PerStreamRateLimitBurst: 100,
		PolicyOverrideLimits: map[string]validation.PolicyOverridableLimits{
			policy: {
				PerStreamRateLimit:      ptr(flagext.ByteSize(10)),
				PerStreamRateLimitBurst: ptr(flagext.ByteSize(10)),
			},
		},
	}
	limits, err := validation.NewOverrides(l, nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})
	retentionHours := util.RetentionHours(limiter.limits.RetentionPeriod("fake"))
	chunkfmt, headfmt := defaultChunkFormat(t)

	s := newStream(
		chunkfmt,
		headfmt,
		defaultConfig(),
		limiter.rateLimitStrategy,
		"fake",
		model.Fingerprint(0),
		labels.FromStrings("foo", "bar"),
		NewStreamRateCalculator(),
		NilMetrics,
		nil,
		nil,
		retentionHours,
		policy,
	)

	entries := []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "aaaaaaaaaa"},
		{Timestamp: time.Unix(1, 0), Line: "aaaaaaaaab"},
	}
	tracker := &mockUsageTracker{}
	_, err = lockThenPush(context.Background(), s, entries, recordPool.GetRecord(), true, tracker, "loki")
	require.Error(t, err)
	// The error must reference the policy override limit (10), not the tenant limit (100).
	require.Contains(t, err.Error(), (&validation.ErrStreamRateLimit{RateLimit: *l.PolicyOverrideLimits[policy].PerStreamRateLimit, Labels: s.labelsString, Bytes: flagext.ByteSize(len(entries[1].Line))}).Error())
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
	retentionHours := util.RetentionHours(limiter.limits.RetentionPeriod("fake"))
	cfg := defaultConfig()
	chunkfmt, headfmt := defaultChunkFormat(t)

	s := newStream(
		chunkfmt,
		headfmt,
		cfg,
		limiter.rateLimitStrategy,
		"fake",
		model.Fingerprint(0),
		labels.FromStrings("foo", "bar"),
		NewStreamRateCalculator(),
		NilMetrics,
		nil,
		nil,
		retentionHours,
		noPolicy,
	)

	entries := []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "aaaaaaaaaa"},
		{Timestamp: time.Unix(1, 0), Line: "aaaaaaaaab"},
	}

	// Both entries have errors because rate limiting is done all at once
	tracker := &mockUsageTracker{}
	_, err = lockThenPush(context.Background(), s, entries, recordPool.GetRecord(), true, tracker, "loki")
	require.Error(t, err)
	require.Contains(t, err.Error(), (&validation.ErrStreamRateLimit{RateLimit: l.PerStreamRateLimit, Labels: s.labelsString, Bytes: flagext.ByteSize(len(entries[0].Line))}).Error())
	require.Contains(t, err.Error(), (&validation.ErrStreamRateLimit{RateLimit: l.PerStreamRateLimit, Labels: s.labelsString, Bytes: flagext.ByteSize(len(entries[1].Line))}).Error())
	require.Equal(t, 20.0, tracker.discardedBytes)
}

func TestReplayAppendIgnoresValidityWindow(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})
	retentionHours := util.RetentionHours(limiter.limits.RetentionPeriod("fake"))
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
		labels.FromStrings("foo", "bar"),
		NewStreamRateCalculator(),
		NilMetrics,
		nil,
		nil,
		retentionHours,
		noPolicy,
	)

	base := time.Now()

	entries := []logproto.Entry{
		{Timestamp: base, Line: "1"},
	}

	// Push a first entry through the replay path
	_, err = s.PushWalReplay(context.Background(), entries, 1, nil)
	require.Nil(t, err)

	// Create a sample outside the validity window
	entries = []logproto.Entry{
		{Timestamp: base.Add(-time.Hour), Line: "2"},
	}

	// A live push outside the validity window must error
	_, err = lockThenPush(context.Background(), s, entries, recordPool.GetRecord(), false, nil, "loki")
	require.NotNil(t, err)

	// The same write through the replay path should succeed and be stored.
	_, err = s.PushWalReplay(context.Background(), entries, 2, nil)
	require.Nil(t, err)
	require.Equal(t, 2, s.chunks[0].chunk.Size())

}

// WAL V1 records carry no counter and decode to 0; they must not be dropped as duplicates.
func TestPushWalReplayZeroCounter(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})
	retentionHours := util.RetentionHours(limiter.limits.RetentionPeriod("fake"))
	chunkfmt, headfmt := defaultChunkFormat(t)

	cfg := defaultConfig()
	cfg.MaxChunkAge = time.Minute
	s := newStream(
		chunkfmt,
		headfmt,
		cfg,
		limiter.rateLimitStrategy,
		"fake",
		model.Fingerprint(0),
		labels.FromStrings("foo", "bar"),
		NewStreamRateCalculator(),
		NilMetrics,
		nil,
		nil,
		retentionHours,
		noPolicy,
	)

	base := time.Now()
	_, err = s.PushWalReplay(context.Background(), []logproto.Entry{
		{Timestamp: base, Line: "a"},
	}, 0, nil)
	require.NoError(t, err)
	require.Equal(t, 1, s.chunks[0].chunk.Size())

	// A second counterless push still isn't a duplicate, even though entryCt > 0.
	_, err = s.PushWalReplay(context.Background(), []logproto.Entry{
		{Timestamp: base.Add(time.Second), Line: "b"},
	}, 0, nil)
	require.NoError(t, err)
	require.Equal(t, 2, s.chunks[0].chunk.Size())

	// Counterless records are validated like live pushes, so entries outside
	// the validity window are rejected.
	_, err = s.PushWalReplay(context.Background(), []logproto.Entry{
		{Timestamp: base.Add(-time.Hour), Line: "old"},
	}, 0, nil)
	require.Error(t, err)
	require.Equal(t, 2, s.chunks[0].chunk.Size())

	// A positive counter that is not ahead of the stream is still a duplicate.
	_, err = s.PushWalReplay(context.Background(), []logproto.Entry{
		{Timestamp: base.Add(2 * time.Second), Line: "c"},
	}, 1, nil)
	require.Equal(t, ErrEntriesExist, err)
	require.Equal(t, 2, s.chunks[0].chunk.Size())
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
	ls := labels.FromStrings(
		"namespace", "loki-dev",
		"cluster", "dev-us-central1",
		"job", "loki-dev/ingester",
		"container", "ingester",
	)

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(b, err)
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})
	chunkfmt, headfmt := defaultChunkFormat(b)
	retentionHours := util.RetentionHours(limiter.limits.RetentionPeriod("fake"))
	s := newStream(chunkfmt, headfmt, &Config{MaxChunkAge: 24 * time.Hour}, limiter.rateLimitStrategy, "fake", model.Fingerprint(0), ls, NewStreamRateCalculator(), NilMetrics, nil, nil, retentionHours, noPolicy)
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
		_, err := lockThenPush(ctx, s, e, rec, false, nil, "loki")
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

// lockThenPush takes chunkMtx and then calls s.Push, as instance.Push does.
func lockThenPush(ctx context.Context, s *stream, entries []logproto.Entry, record *wal.Record, rateLimitWholeStream bool, usageTracker push.UsageTracker, format string) (int, error) {
	s.chunkMtx.Lock()
	defer s.chunkMtx.Unlock()
	return s.Push(ctx, entries, record, rateLimitWholeStream, usageTracker, format)
}

func TestPushInternalMatchesExpanded(t *testing.T) {
	for _, whole := range []bool{false, true} {
		for _, limited := range []bool{false, true} {
			t.Run(fmt.Sprintf("whole=%t/limited=%t", whole, limited), func(t *testing.T) {
				l := defaultLimitsTestConfig()
				if limited {
					l.PerStreamRateLimit = 1
					l.PerStreamRateLimitBurst = 70
				}
				limits, err := validation.NewOverrides(l, nil)
				require.NoError(t, err)
				strategy := &TenantBasedStrategy{limits: limits}
				makeStream := func() *stream {
					cfg := defaultConfig()
					cfg.MaxChunkAge = 2 * time.Hour
					cfg.TargetChunkSize = 160
					cfg.BlockSize = 60
					return newStream(chunkenc.ChunkFormatV4, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, cfg, strategy, "nested-test", 1, labels.FromStrings("app", "test"), NewStreamRateCalculator(), NilMetrics, nil, nil, "0", noPolicy)
				}
				flatStream, nestedStream := makeStream(), makeStream()
				at := time.Unix(100000, 0).UTC()
				nested := logproto.InternalStreamAdapter{Labels: `{app="test"}`, ResourceLogs: []logproto.ResourceLogs{{
					Attrs: []logproto.LabelAdapter{{Name: "r", Value: "resource"}, {Name: "override", Value: "resource"}, {Name: "detected_level", Value: "info"}},
					ScopeLogs: []logproto.ScopeLogs{
						{Attrs: []logproto.LabelAdapter{{Name: "override", Value: "scope"}}, Entries: []logproto.Entry{
							{Timestamp: at, Line: "first"},
							{Timestamp: at, Line: "first"},
							{Timestamp: at.Add(time.Second), Line: "second", StructuredMetadata: []logproto.LabelAdapter{{Name: "override", Value: ""}}},
						}},
						{Entries: []logproto.Entry{
							{Timestamp: at.Add(-2 * time.Hour), Line: "too old"},
							{Timestamp: at.Add(2 * time.Second), Line: "third"},
						}},
					},
				}}}
				original, err := nested.Marshal()
				require.NoError(t, err)
				flat := nested.FlatView()
				// Repeated pushes also exercise last-line deduplication across requests.
				for range 2 {
					flatRecord, nestedRecord := recordPool.GetRecord(), recordPool.GetRecord()
					flatTracker, nestedTracker := &mockUsageTracker{}, &mockUsageTracker{}
					flatBytes, flatErr := flatStream.Push(t.Context(), flat.Entries, flatRecord, whole, flatTracker, "otlp")
					nestedBytes, nestedErr := nestedStream.PushInternal(t.Context(), &nested, nestedRecord, whole, nestedTracker, "otlp")
					require.Equal(t, flatErr, nestedErr)
					require.Equal(t, flatBytes, nestedBytes)
					require.Equal(t, flatTracker.discardedBytes, nestedTracker.discardedBytes)
					require.Len(t, nestedRecord.RefEntries, len(flatRecord.RefEntries))
					decoded := &wal.Record{}
					require.NoError(t, wal.DecodeRecord(nestedRecord.EncodeEntries(wal.CurrentEntriesRec, nil), decoded))
					decodedFlat := &wal.Record{}
					require.NoError(t, wal.DecodeRecord(flatRecord.EncodeEntries(wal.WALRecordEntriesV3, nil), decodedFlat))
					for i, ref := range decoded.RefEntries {
						require.Equal(t, decodedFlat.RefEntries[i].Ref, ref.Ref)
						require.Equal(t, decodedFlat.RefEntries[i].Counter, ref.Counter)
						require.Equal(t, decodedFlat.RefEntries[i].Entries, flattenEntries(walEntryBatch(ref)))
					}
					recordPool.PutRecord(flatRecord)
					recordPool.PutRecord(nestedRecord)
				}
				after, err := nested.Marshal()
				require.NoError(t, err)
				require.Equal(t, original, after)
				require.Equal(t, flatStream.entryCt, nestedStream.entryCt)
				require.Equal(t, flatStream.highestTs, nestedStream.highestTs)
				require.Len(t, nestedStream.chunks, len(flatStream.chunks))
				for i := range flatStream.chunks {
					require.NoError(t, flatStream.chunks[i].chunk.Close())
					require.NoError(t, nestedStream.chunks[i].chunk.Close())
					want, err := flatStream.chunks[i].chunk.Bytes()
					require.NoError(t, err)
					got, err := nestedStream.chunks[i].chunk.Bytes()
					require.NoError(t, err)
					require.Equal(t, want, got, "chunk %d", i)
				}
			})
		}
	}
}

func TestPushInternalWALReplayAndTail(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	makeStream := func() *stream {
		cfg := defaultConfig()
		cfg.MaxChunkAge = 2 * time.Hour
		return newStream(chunkenc.ChunkFormatV4, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, cfg, &TenantBasedStrategy{limits: limits}, "nested-test", 1, labels.FromStrings("app", "test"), NewStreamRateCalculator(), NilMetrics, nil, nil, "0", noPolicy)
	}
	live, replay := makeStream(), makeStream()
	nested := logproto.InternalStreamAdapter{Labels: `{app="test"}`, ResourceLogs: []logproto.ResourceLogs{{
		Attrs: []logproto.LabelAdapter{{Name: "resource", Value: "kept"}}, ScopeLogs: []logproto.ScopeLogs{{
			Attrs: []logproto.LabelAdapter{{Name: "scope", Value: "kept"}}, Entries: []logproto.Entry{{Timestamp: time.Unix(100000, 0).UTC(), Line: "test"}},
		}},
	}}}
	expr, err := syntax.ParseLogSelector(nested.Labels, true)
	require.NoError(t, err)
	tail, err := newTailer("nested-test", expr, &fakeTailServer{}, 10)
	require.NoError(t, err)
	defer tail.close()
	live.addTailer(tail)
	record := recordPool.GetRecord()
	defer recordPool.PutRecord(record)
	_, err = live.PushInternal(t.Context(), &nested, record, false, nil, "otlp")
	require.NoError(t, err)
	require.Len(t, record.RefEntries, 1)
	expected := nested.FlatView().Entries
	require.Empty(t, record.RefEntries[0].Entries[0].StructuredMetadata)
	require.Len(t, record.RefEntries[0].MetadataGroups, 1)
	require.Equal(t, expected, flattenEntries(walEntryBatch(record.RefEntries[0])))
	select {
	case queued := <-tail.queue:
		require.Equal(t, expected, queued.stream.Entries)
	default:
		t.Fatal("tail did not receive stored entries")
	}
	// Replay the encoded record without expanding its shared metadata.
	decoded := &wal.Record{}
	encoded := record.EncodeEntries(wal.CurrentEntriesRec, nil)
	require.NoError(t, wal.DecodeRecord(encoded, decoded))
	_, err = replay.pushWALReplay(t.Context(), walEntryBatch(decoded.RefEntries[0]), decoded.RefEntries[0].Counter, nil)
	require.NoError(t, err)
	require.Equal(t, live.entryCt, replay.entryCt)
	_, err = replay.pushWALReplay(t.Context(), walEntryBatch(decoded.RefEntries[0]), decoded.RefEntries[0].Counter, nil)
	require.ErrorIs(t, err, ErrEntriesExist)
	require.NoError(t, live.chunks[0].chunk.Close())
	require.NoError(t, replay.chunks[0].chunk.Close())
	want, err := live.chunks[0].chunk.Bytes()
	require.NoError(t, err)
	got, err := replay.chunks[0].chunk.Bytes()
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestWALEntriesPreservesGroupGaps(t *testing.T) {
	shared := &entryMetadata{attrs: []logproto.LabelAdapter{{Name: "shared", Value: "value"}}}
	entries := []logproto.Entry{{Line: "native before"}, {Line: "shared"}, {Line: "native gap"}, {Line: "shared again"}, {Line: "shared too"}, {Line: "native after"}}
	batch := flatEntryBatch(entries)
	for _, i := range []int{1, 3, 4} {
		batch[i].shared = shared
	}
	stored, groups := walEntries(batch)
	require.Equal(t, entries, stored)
	require.Equal(t, []wal.MetadataGroup{{Start: 1, End: 2, Attrs: shared.attrs}, {Start: 3, End: 5, Attrs: shared.attrs}}, groups)
	require.Equal(t, flattenEntries(batch), flattenEntries(walEntryBatch(wal.RefEntries{Entries: stored, MetadataGroups: groups})))
	entries[0].Line = "changed"
	require.Equal(t, "native before", stored[0].Line)
}
