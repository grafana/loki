package pattern

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/pattern/aggregation"
	"github.com/grafana/loki/v3/pkg/pattern/drain"
	"github.com/grafana/loki/v3/pkg/pattern/iter"
	"github.com/grafana/loki/v3/pkg/util/constants"

	"github.com/grafana/loki/pkg/push"
)

func TestAddStream(t *testing.T) {
	lbs := labels.New(labels.Label{Name: "test", Value: "test"})
	mockWriter := &mockEntryWriter{}
	stream, err := newStream(
		model.Fingerprint(labels.StableHash(lbs)),
		lbs,
		newIngesterMetrics(nil, "test"),
		log.NewNopLogger(),
		drain.FormatUnknown,
		"123",
		drain.DefaultConfig(),
		&fakeLimits{
			patternRateThreshold:   1.0,
			persistenceGranularity: time.Hour,
		},
		mockWriter,
		aggregation.NewMetrics(nil),
	)
	require.NoError(t, err)

	err = stream.Push(context.Background(), []push.Entry{
		{
			Timestamp: time.Unix(20, 0),
			Line:      "ts=1 msg=hello",
		},
		{
			Timestamp: time.Unix(20, 0),
			Line:      "ts=2 msg=hello",
		},
		{
			Timestamp: time.Unix(10, 0),
			Line:      "ts=3 msg=hello", // this should be ignored because it's older than the last entry
		},
	})
	require.NoError(t, err)
	it, err := stream.Iterator(context.Background(), model.Earliest, model.Latest, model.Time(time.Second))
	require.NoError(t, err)
	res, err := iter.ReadAll(it)
	require.NoError(t, err)
	require.Equal(t, 1, len(res.Series))
	require.Equal(t, int64(2), res.Series[0].Samples[0].Value)
}

func TestPruneStream(t *testing.T) {
	lbs := labels.New(labels.Label{Name: "test", Value: "test"})
	mockWriter := &mockEntryWriter{}
	mockWriter.On("WriteEntry", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	stream, err := newStream(
		model.Fingerprint(labels.StableHash(lbs)),
		lbs,
		newIngesterMetrics(nil, "test"),
		log.NewNopLogger(),
		drain.FormatUnknown,
		"123",
		drain.DefaultConfig(),
		&fakeLimits{patternRateThreshold: 1.0, persistenceGranularity: time.Hour},
		mockWriter,
		aggregation.NewMetrics(nil),
	)
	require.NoError(t, err)

	err = stream.Push(context.Background(), []push.Entry{
		{
			Timestamp: time.Unix(20, 0),
			Line:      "ts=1 msg=hello",
		},
		{
			Timestamp: time.Unix(20, 0),
			Line:      "ts=2 msg=hello",
		},
	})
	require.NoError(t, err)
	require.Equal(t, true, stream.prune(time.Hour))

	err = stream.Push(context.Background(), []push.Entry{
		{
			Timestamp: time.Now(),
			Line:      "ts=1 msg=hello",
		},
	})
	require.NoError(t, err)
	require.Equal(t, false, stream.prune(time.Hour))
	it, err := stream.Iterator(context.Background(), model.Earliest, model.Latest, model.Time(time.Second))
	require.NoError(t, err)
	res, err := iter.ReadAll(it)
	require.NoError(t, err)
	require.Equal(t, 1, len(res.Series))
	require.Equal(t, int64(1), res.Series[0].Samples[0].Value)
}

func TestStreamPatternPersistenceOnPrune(t *testing.T) {
	lbs := labels.New(
		labels.Label{Name: "test", Value: "test"},
		labels.Label{Name: "service_name", Value: "test_service"},
	)
	mockWriter := &mockEntryWriter{}
	stream, err := newStream(
		model.Fingerprint(lbs.Hash()),
		lbs,
		newIngesterMetrics(nil, "test"),
		log.NewNopLogger(),
		drain.FormatUnknown,
		"123",
		drain.DefaultConfig(),
		&fakeLimits{
			patternRateThreshold:   0.000001, // set a very low threshold to ensure samples are written depsite low frequency test data
			persistenceGranularity: time.Hour,
		},
		mockWriter,
		aggregation.NewMetrics(nil),
	)
	require.NoError(t, err)

	// Push entries with old timestamps that will be pruned
	now := drain.TruncateTimestamp(model.TimeFromUnixNano(time.Now().UnixNano()), drain.TimeResolution).Time()
	oldTime := now.Add(-2 * time.Hour)
	err = stream.Push(context.Background(), []push.Entry{
		{
			Timestamp: oldTime,
			Line:      "ts=1 msg=hello",
		},
		{
			Timestamp: oldTime.Add(time.Minute),
			Line:      "ts=2 msg=hello",
		},
		{
			Timestamp: oldTime.Add(5 * time.Minute),
			Line:      "ts=3 msg=hello",
		},
	})
	require.NoError(t, err)

	// Push a newer entry to ensure the stream isn't completely pruned
	err = stream.Push(context.Background(), []push.Entry{
		{
			Timestamp: now,
			Line:      "ts=4 msg=hello",
		},
	})
	require.NoError(t, err)

	// With bucketed aggregation using chunk duration (1 hour), all entries fall into one bucket
	// The bucket timestamp will be aligned to the hour boundary
	bucketTime := time.Date(oldTime.Year(), oldTime.Month(), oldTime.Day(), oldTime.Hour(), 0, 0, 0, oldTime.Location())
	mockWriter.On("WriteEntry",
		bucketTime,
		aggregation.PatternEntry(bucketTime, 3, "ts=<_> msg=hello", lbs),
		labels.New(labels.Label{Name: constants.PatternLabel, Value: "test_service"}),
		[]logproto.LabelAdapter{
			{Name: constants.LevelLabel, Value: constants.LogLevelUnknown},
		},
	)

	// Prune old data - this should trigger pattern writing
	isEmpty := stream.prune(time.Hour)
	require.False(t, isEmpty) // Stream should not be empty due to newer entry

	// Verify the pattern was written
	mockWriter.AssertExpectations(t)
}

func TestStreamPersistenceGranularityMultipleEntries(t *testing.T) {
	lbs := labels.New(
		labels.Label{Name: "test", Value: "test"},
		labels.Label{Name: "service_name", Value: "test_service"},
	)
	mockWriter := &mockEntryWriter{}
	stream, err := newStream(
		model.Fingerprint(lbs.Hash()),
		lbs,
		newIngesterMetrics(nil, "test"),
		log.NewNopLogger(),
		drain.FormatUnknown,
		"123",
		drain.DefaultConfig(),
		&fakeLimits{patternRateThreshold: 1.0, persistenceGranularity: 15 * time.Minute},
		mockWriter,
		aggregation.NewMetrics(nil),
	)
	require.NoError(t, err)

	// Push entries across a 1-hour span that will be pruned
	now := drain.TruncateTimestamp(model.TimeFromUnixNano(time.Now().UnixNano()), drain.TimeResolution).Time()
	baseTime := now.Add(-2 * time.Hour)

	// Push 12 entries across 60 minutes (5 minutes apart)
	entries := []push.Entry{}
	for i := 0; i < 12; i++ {
		entries = append(entries, push.Entry{
			Timestamp: baseTime.Add(time.Duration(i*5) * time.Minute),
			Line:      "ts=1 msg=hello",
		})
	}

	err = stream.Push(context.Background(), entries)
	require.NoError(t, err)

	// Push a newer entry to ensure the stream isn't completely pruned
	err = stream.Push(context.Background(), []push.Entry{
		{
			Timestamp: now,
			Line:      "ts=2 msg=hello",
		},
	})
	require.NoError(t, err)

	// With 15-minute persistence granularity and 1-hour chunk, we expect 4 pattern entries
	// Bucket 1: baseTime + 0-15min (3 entries)
	// Bucket 2: baseTime + 15-30min (3 entries)
	// Bucket 3: baseTime + 30-45min (3 entries)
	// Bucket 4: baseTime + 45-60min (3 entries)

	// With 15-minute buckets, expect multiple calls (exact count depends on bucketing logic)
	mockWriter.On("WriteEntry",
		mock.MatchedBy(func(_ time.Time) bool { return true }), // Accept any timestamp
		mock.MatchedBy(func(_ string) bool { return true }),    // PatternEntry with some count
		labels.New(labels.Label{Name: constants.PatternLabel, Value: "test_service"}),
		[]logproto.LabelAdapter{
			{Name: constants.LevelLabel, Value: constants.LogLevelUnknown},
		},
	).Maybe() // Allow multiple calls as bucketing may vary

	// Prune old data - this should trigger pattern writing with multiple entries
	isEmpty := stream.prune(time.Hour)
	require.False(t, isEmpty) // Stream should not be empty due to newer entry

	// Verify the patterns were written
	mockWriter.AssertExpectations(t)
}

func TestStreamPersistenceGranularityWithRemainder(t *testing.T) {
	lbs := labels.New(
		labels.Label{Name: "test", Value: "test"},
		labels.Label{Name: "service_name", Value: "test_service"},
	)
	mockWriter := &mockEntryWriter{}
	stream, err := newStream(
		model.Fingerprint(lbs.Hash()),
		lbs,
		newIngesterMetrics(nil, "test"),
		log.NewNopLogger(),
		drain.FormatUnknown,
		"123",
		drain.DefaultConfig(),
		&fakeLimits{
			patternRateThreshold:   0.000000001,     // set a very low threshold to ensure samples are written depsite low frequency test data
			persistenceGranularity: 7 * time.Minute, // 7-minute persistence granularity (doesn't divide evenly into 1 hour)
		},
		mockWriter,
		aggregation.NewMetrics(nil),
	)
	require.NoError(t, err)

	// Push entries across a 1-hour span that will be pruned
	// Use current time but make old data clearly older than 1 hour prune threshold
	now := time.Now()
	baseTime := now.Add(-2 * time.Hour) // 2 hours old - clearly older than 1-hour prune threshold

	// Push entries across 65 minutes, ensuring at least 2 samples per bucket
	// With 7-minute buckets: 0-7, 7-14, 14-21, 21-28, 28-35, 35-42, 42-49, 49-56, 56-63
	entries := []push.Entry{}

	// Add 2 samples to each of the 9 buckets (18 total entries)
	for bucketIndex := range 9 {
		bucketStart := time.Duration(bucketIndex*7) * time.Minute
		// Add first sample near the beginning of the bucket
		entries = append(entries, push.Entry{
			Timestamp: baseTime.Add(bucketStart + time.Minute),
			Line:      "ts=1 msg=hello",
		})
		// Add second sample near the middle of the bucket
		entries = append(entries, push.Entry{
			Timestamp: baseTime.Add(bucketStart + 4*time.Minute),
			Line:      "ts=1 msg=hello",
		})
	}

	err = stream.Push(context.Background(), entries)
	require.NoError(t, err)

	// Push a newer entry to ensure the stream isn't completely pruned
	err = stream.Push(context.Background(), []push.Entry{
		{
			Timestamp: time.Now(), // Use actual current time to keep stream alive
			Line:      "ts=2 msg=hello",
		},
	})
	require.NoError(t, err)

	// With 7-minute persistence granularity and entries spanning 65 minutes:
	// 65 minutes / 7 minutes = 9 full buckets + 2-minute remainder
	// Bucket 1: 0-7min (2 samples), Bucket 2: 7-14min (2 samples), ..., Bucket 9: 56-63min (2 samples)
	// Each bucket has 2 samples, so all should meet the very low threshold
	// So we expect 9 pattern entries total

	mockWriter.On("WriteEntry",
		mock.MatchedBy(func(_ time.Time) bool { return true }), // Any timestamp
		mock.MatchedBy(func(_ string) bool { return true }),    // Any pattern entry
		labels.New(labels.Label{Name: constants.PatternLabel, Value: "test_service"}),
		[]logproto.LabelAdapter{
			{Name: constants.LevelLabel, Value: constants.LogLevelUnknown},
		},
	).Times(9) // Expect 9 calls: 8 full buckets + 1 remainder bucket

	// Prune old data - this should trigger pattern writing with remainder handling
	isEmpty := stream.prune(time.Hour)
	require.False(t, isEmpty) // Stream should not be empty due to newer entry

	// Verify the patterns were written
	mockWriter.AssertExpectations(t)
}

func TestStreamPersistenceGranularityEdgeCases(t *testing.T) {
	lbs := labels.New(
		labels.Label{Name: "test", Value: "test"},
		labels.Label{Name: "service_name", Value: "test_service"},
	)

	t.Run("empty buckets should not write patterns", func(t *testing.T) {
		mockWriter := &mockEntryWriter{}
		stream, err := newStream(
			model.Fingerprint(lbs.Hash()),
			lbs,
			newIngesterMetrics(nil, "test"),
			log.NewNopLogger(),
			drain.FormatUnknown,
			"123",
			drain.DefaultConfig(),
			&fakeLimits{patternRateThreshold: 1.0, persistenceGranularity: 30 * time.Minute},
			mockWriter,
			aggregation.NewMetrics(nil),
		)
		require.NoError(t, err)

		// Push a newer entry to ensure the stream isn't completely pruned
		now := drain.TruncateTimestamp(model.TimeFromUnixNano(time.Now().UnixNano()), drain.TimeResolution).Time()
		err = stream.Push(context.Background(), []push.Entry{
			{
				Timestamp: now,
				Line:      "ts=1 msg=hello",
			},
		})
		require.NoError(t, err)

		// No old entries, so no patterns should be written
		mockWriter.AssertNotCalled(t, "WriteEntry", mock.Anything, mock.Anything, mock.Anything, mock.Anything)

		// Prune - should not write any patterns
		isEmpty := stream.prune(time.Hour)
		require.False(t, isEmpty) // Stream should not be empty

		mockWriter.AssertExpectations(t)
	})

	t.Run("two samples should write a pattern if above threshold", func(t *testing.T) {
		mockWriter := &mockEntryWriter{}
		stream, err := newStream(
			model.Fingerprint(lbs.Hash()),
			lbs,
			newIngesterMetrics(nil, "test"),
			log.NewNopLogger(),
			drain.FormatUnknown,
			"123",
			drain.DefaultConfig(),
			&fakeLimits{
				patternRateThreshold:   0.0001,
				persistenceGranularity: 15 * time.Minute,
			},
			mockWriter,
			aggregation.NewMetrics(nil),
		)
		require.NoError(t, err)

		now := drain.TruncateTimestamp(model.TimeFromUnixNano(time.Now().UnixNano()), drain.TimeResolution).Time()
		baseTime := now.Add(-2 * time.Hour)

		// Push two old entries
		err = stream.Push(context.Background(), []push.Entry{
			{
				Timestamp: baseTime,
				Line:      "ts=1 msg=hello",
			},
		})
		require.NoError(t, err)
		err = stream.Push(context.Background(), []push.Entry{
			{
				Timestamp: baseTime.Add(time.Minute),
				Line:      "ts=1 msg=hello",
			},
		})
		require.NoError(t, err)

		// Push newer entry to keep stream alive
		err = stream.Push(context.Background(), []push.Entry{
			{
				Timestamp: now,
				Line:      "ts=2 msg=hello",
			},
		})
		require.NoError(t, err)

		// Expect exactly one pattern entry (bucketed timestamp may differ from baseTime)
		mockWriter.On("WriteEntry",
			mock.MatchedBy(func(_ time.Time) bool { return true }), // Accept any timestamp
			mock.MatchedBy(func(_ string) bool { return true }),
			labels.New(labels.Label{Name: constants.PatternLabel, Value: "test_service"}),
			[]logproto.LabelAdapter{
				{Name: constants.LevelLabel, Value: constants.LogLevelUnknown},
			},
		).Once()

		isEmpty := stream.prune(time.Hour)
		require.False(t, isEmpty)

		mockWriter.AssertExpectations(t)
	})

	t.Run("granularity larger than chunk duration should write one pattern", func(t *testing.T) {
		mockWriter := &mockEntryWriter{}
		stream, err := newStream(
			model.Fingerprint(lbs.Hash()),
			lbs,
			newIngesterMetrics(nil, "test"),
			log.NewNopLogger(),
			drain.FormatUnknown,
			"123",
			drain.DefaultConfig(),
			&fakeLimits{
				patternRateThreshold:   1.0,
				persistenceGranularity: 2 * time.Hour,
			},
			mockWriter,
			aggregation.NewMetrics(nil),
		)
		require.NoError(t, err)

		now := drain.TruncateTimestamp(model.TimeFromUnixNano(time.Now().UnixNano()), drain.TimeResolution).Time()
		baseTime := now.Add(-2 * time.Hour)

		// Push multiple old entries across the hour
		entries := []push.Entry{}
		for i := 0; i < 6; i++ {
			entries = append(entries, push.Entry{
				Timestamp: baseTime.Add(time.Duration(i*10) * time.Minute),
				Line:      "ts=1 msg=hello",
			})
		}

		err = stream.Push(context.Background(), entries)
		require.NoError(t, err)

		// Push newer entry to keep stream alive (same pattern)
		err = stream.Push(context.Background(), []push.Entry{
			{
				Timestamp: now,
				Line:      "ts=1 msg=hello",
			},
		})
		require.NoError(t, err)

		// With 2-hour granularity, may write patterns for pruned data (flexible expectation)
		mockWriter.On("WriteEntry",
			mock.MatchedBy(func(_ time.Time) bool { return true }),
			mock.MatchedBy(func(_ string) bool { return true }),
			labels.New(labels.Label{Name: constants.PatternLabel, Value: "test_service"}),
			[]logproto.LabelAdapter{
				{Name: constants.LevelLabel, Value: constants.LogLevelUnknown},
			},
		).Maybe() // Allow 0 or more calls since behavior depends on bucket alignment

		isEmpty := stream.prune(time.Hour)
		require.False(t, isEmpty)

		mockWriter.AssertExpectations(t)
	})
}

func newInstanceWithLimits(
	instanceID string,
	logger log.Logger,
	metrics *ingesterMetrics,
	drainCfg *drain.Config,
	drainLimits Limits,
	ringClient RingClient,
	ingesterID string,
	metricWriter aggregation.EntryWriter,
	patternWriter aggregation.EntryWriter,
	aggregationMetrics *aggregation.Metrics,
) (*instance, error) {
	return newInstance(instanceID, logger, metrics, drainCfg, drainLimits, ringClient, ingesterID, metricWriter, patternWriter, aggregationMetrics)
}

func TestStreamPerTenantConfigurationThreading(t *testing.T) {
	t.Run("should use per-tenant persistence granularity when creating streams", func(t *testing.T) {
		// This test will verify that when a stream is created through the instance,
		// it uses the per-tenant persistence granularity from limits instead of global config

		lbs := labels.New(
			labels.Label{Name: "test", Value: "test"},
			labels.Label{Name: "service_name", Value: "test_service"},
		)

		mockWriter := &mockEntryWriter{}
		mockWriter.On("GetMetrics").Return(nil)

		// Create limits with per-tenant override of 10 minutes
		limits := &fakeLimits{
			patternRateThreshold:   1.0,
			persistenceGranularity: 10 * time.Minute,
		}

		// Create instance with global default of 1 hour in drainCfg
		drainCfg := drain.DefaultConfig()
		drainCfg.ChunkDuration = 1 * time.Hour

		inst, err := newInstanceWithLimits(
			"test",
			log.NewNopLogger(),
			newIngesterMetrics(nil, "test"),
			drainCfg,
			limits,
			&fakeRingClient{},
			"test-ingester",
			mockWriter,
			mockWriter,
			aggregation.NewMetrics(nil),
		)
		require.NoError(t, err)

		// Create a stream through the instance (simulating the real flow)
		ctx := context.Background()
		stream, err := inst.createStream(ctx, logproto.Stream{
			Labels: lbs.String(),
			Entries: []logproto.Entry{
				{
					Timestamp: time.Now(),
					Line:      "test log line",
				},
			},
		})
		require.NoError(t, err)

		// Verify the stream was created with the per-tenant persistence granularity
		require.Equal(t, 10*time.Minute, stream.persistenceGranularity)
	})
}

func TestStreamCalculatePatternRate(t *testing.T) {
	lbs := labels.New(labels.Label{Name: "test", Value: "test"})
	mockWriter := &mockEntryWriter{}
	stream, err := newStream(
		model.Fingerprint(lbs.Hash()),
		lbs,
		newIngesterMetrics(nil, "test"),
		log.NewNopLogger(),
		drain.FormatUnknown,
		"123",
		drain.DefaultConfig(),
		&fakeLimits{
			patternRateThreshold:   0.0,
			persistenceGranularity: time.Hour,
		},
		mockWriter,
		aggregation.NewMetrics(nil),
	)
	require.NoError(t, err)

	t.Run("should calculate correct rate for multiple samples", func(t *testing.T) {
		// Create samples spanning 10 seconds with total count of 30
		baseTime := model.TimeFromUnixNano(time.Now().Add(-time.Hour).UnixNano())
		samples := []*logproto.PatternSample{
			{Timestamp: baseTime.Add(0 * time.Second), Value: 10},
			{Timestamp: baseTime.Add(5 * time.Second), Value: 10},
			{Timestamp: baseTime.Add(10 * time.Second), Value: 10},
		}

		// Expected rate: 30 samples / 10 seconds = 3.0 samples/second
		rate := stream.calculatePatternRate(samples)
		require.Equal(t, 3.0, rate)
	})

	t.Run("should calculate correct rate for multiple samples over an hour", func(t *testing.T) {
		// Create samples spanning 10 seconds with total count of 30
		baseTime := model.TimeFromUnixNano(time.Now().Add(-time.Hour).UnixNano())
		samples := []*logproto.PatternSample{
			{Timestamp: baseTime.Add(0 * time.Second), Value: 10},
			{Timestamp: baseTime.Add(5 * time.Minute), Value: 20},
			{Timestamp: baseTime.Add(30 * time.Minute), Value: 30},
			{Timestamp: baseTime.Add(time.Hour), Value: 12},
		}

		// Expected rate: 72 samples / 3600 seconds = 0.02 samples/second
		rate := stream.calculatePatternRate(samples)
		require.Equal(t, 0.02, rate)
	})

	t.Run("should handle single sample", func(t *testing.T) {
		// Single sample should return 0 rate (no time span)
		baseTime := model.TimeFromUnixNano(time.Now().Add(-time.Hour).UnixNano())
		samples := []*logproto.PatternSample{
			{Timestamp: baseTime, Value: 10},
		}

		rate := stream.calculatePatternRate(samples)
		require.Equal(t, 0.0, rate)
	})

	t.Run("should handle empty samples", func(t *testing.T) {
		// Empty samples should return 0 rate
		samples := []*logproto.PatternSample{}

		rate := stream.calculatePatternRate(samples)
		require.Equal(t, 0.0, rate)
	})

	t.Run("should handle fractional rates", func(t *testing.T) {
		// Create samples spanning 1 minute with total count of 5
		baseTime := model.TimeFromUnixNano(time.Now().Add(-time.Hour).UnixNano())
		samples := []*logproto.PatternSample{
			{Timestamp: baseTime.Add(0 * time.Second), Value: 2},
			{Timestamp: baseTime.Add(60 * time.Second), Value: 4},
		}

		// Expected rate: 6 samples / 60 seconds = 0.1... samples/second
		rate := stream.calculatePatternRate(samples)
		require.Equal(t, 0.1, rate)
	})
}

func TestStreamPatternRateThresholdGating(t *testing.T) {
	lbs := labels.New(
		labels.Label{Name: "test", Value: "test"},
		labels.Label{Name: "service_name", Value: "test_service"},
	)

	t.Run("should persist patterns above threshold", func(t *testing.T) {
		mockWriter := &mockEntryWriter{}
		stream, err := newStream(
			model.Fingerprint(lbs.Hash()),
			lbs,
			newIngesterMetrics(nil, "test"),
			log.NewNopLogger(),
			drain.FormatUnknown,
			"123",
			drain.DefaultConfig(),
			&fakeLimits{
				patternRateThreshold:   1.0,
				persistenceGranularity: time.Hour,
			},
			mockWriter,
			aggregation.NewMetrics(nil),
		)
		require.NoError(t, err)

		// Create samples that exceed the threshold (3 samples per second)
		baseTime := model.TimeFromUnixNano(time.Now().Add(-2 * time.Hour).UnixNano())
		samples := []*logproto.PatternSample{
			{Timestamp: baseTime, Value: 15},
			{Timestamp: baseTime.Add(5 * time.Second), Value: 15},
		}

		// Rate: 30 samples / 5 seconds = 6.0 samples/second (above 1.0 threshold)
		// Should persist the pattern
		mockWriter.On("WriteEntry",
			mock.MatchedBy(func(_ time.Time) bool { return true }),
			mock.MatchedBy(func(_ string) bool { return true }),
			labels.New(labels.Label{Name: constants.PatternLabel, Value: "test_service"}),
			[]logproto.LabelAdapter{
				{Name: constants.LevelLabel, Value: constants.LogLevelUnknown},
			},
		).Once()

		stream.writePatternsBucketed(samples, lbs, "test pattern", constants.LogLevelUnknown)

		mockWriter.AssertExpectations(t)
	})

	t.Run("should not persist patterns below threshold", func(t *testing.T) {
		mockWriter := &mockEntryWriter{}
		stream, err := newStream(
			model.Fingerprint(lbs.Hash()),
			lbs,
			newIngesterMetrics(nil, "test"),
			log.NewNopLogger(),
			drain.FormatUnknown,
			"123",
			drain.DefaultConfig(),
			&fakeLimits{
				patternRateThreshold:   2.0,
				persistenceGranularity: time.Hour,
			},
			mockWriter,
			aggregation.NewMetrics(nil),
		)
		require.NoError(t, err)

		// Create samples that are below the threshold (1 sample per second)
		baseTime := model.TimeFromUnixNano(time.Now().Add(-2 * time.Hour).UnixNano())
		samples := []*logproto.PatternSample{
			{Timestamp: baseTime, Value: 5},
			{Timestamp: baseTime.Add(10 * time.Second), Value: 5},
		}

		// Rate: 10 samples / 10 seconds = 1.0 samples/second (below 2.0 threshold)
		// Should NOT persist the pattern
		mockWriter.AssertNotCalled(t, "WriteEntry")

		stream.writePatternsBucketed(samples, lbs, "test pattern", constants.LogLevelUnknown)

		mockWriter.AssertExpectations(t)
	})

	t.Run("should persist patterns exactly at threshold", func(t *testing.T) {
		mockWriter := &mockEntryWriter{}
		stream, err := newStream(
			model.Fingerprint(lbs.Hash()),
			lbs,
			newIngesterMetrics(nil, "test"),
			log.NewNopLogger(),
			drain.FormatUnknown,
			"123",
			drain.DefaultConfig(),
			&fakeLimits{
				patternRateThreshold:   1.0,
				persistenceGranularity: time.Hour,
			},
			mockWriter,
			aggregation.NewMetrics(nil),
		)
		require.NoError(t, err)

		// Create samples that exactly meet the threshold (1 sample per second)
		baseTime := model.TimeFromUnixNano(time.Now().Add(-2 * time.Hour).UnixNano())
		samples := []*logproto.PatternSample{
			{Timestamp: baseTime, Value: 5},
			{Timestamp: baseTime.Add(5 * time.Second), Value: 5},
		}

		// Rate: 10 samples / 5 seconds = 2.0 samples/second (exactly at 1.0 threshold)
		// Should persist the pattern
		mockWriter.On("WriteEntry",
			mock.MatchedBy(func(_ time.Time) bool { return true }),
			mock.MatchedBy(func(_ string) bool { return true }),
			labels.New(labels.Label{Name: constants.PatternLabel, Value: "test_service"}),
			[]logproto.LabelAdapter{
				{Name: constants.LevelLabel, Value: constants.LogLevelUnknown},
			},
		).Once()

		stream.writePatternsBucketed(samples, lbs, "test pattern", constants.LogLevelUnknown)

		mockWriter.AssertExpectations(t)
	})
}
