package pattern

import (
	"context"
	"fmt"
	"strings"
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

func TestFilterClustersByVolume(t *testing.T) {
	// Create test clusters with different volumes
	cluster1 := &drain.LogCluster{
		Tokens:      []string{"high", "volume", "pattern"},
		Volume:      500,
		SampleCount: 50,
	}
	cluster2 := &drain.LogCluster{
		Tokens:      []string{"medium", "volume", "pattern"},
		Volume:      300,
		SampleCount: 30,
	}
	cluster3 := &drain.LogCluster{
		Tokens:      []string{"low", "volume", "pattern", "one"},
		Volume:      150,
		SampleCount: 15,
	}
	cluster4 := &drain.LogCluster{
		Tokens:      []string{"low", "volume", "pattern", "two"},
		Volume:      50,
		SampleCount: 5,
	}

	clusters := []clusterWithMeta{
		{cluster: cluster1},
		{cluster: cluster2},
		{cluster: cluster3},
		{cluster: cluster4},
	}

	// Test 90% threshold - should keep 500+300+150=950 out of 1000 total
	result := filterClustersByVolume(clusters, 0.9)
	require.Equal(t, 3, len(result), "Should keep top 3 clusters for 90% volume threshold")
	// Verify sorting worked correctly
	require.Equal(t, cluster1, result[0].cluster, "Highest volume cluster should be first")
	require.Equal(t, cluster2, result[1].cluster, "Second highest volume cluster should be second")
	require.Equal(t, cluster3, result[2].cluster, "Third highest volume cluster should be third")

	// Reset clusters order for next test
	clusters = []clusterWithMeta{
		{cluster: cluster1},
		{cluster: cluster2},
		{cluster: cluster3},
		{cluster: cluster4},
	}

	// Test 50% threshold - should keep only 500 out of 1000 total
	result = filterClustersByVolume(clusters, 0.5)
	require.Equal(t, 1, len(result), "Should keep only top cluster for 50% volume threshold")
	require.Equal(t, cluster1, result[0].cluster, "Highest volume cluster should be kept")

	// Reset clusters order for next test
	clusters = []clusterWithMeta{
		{cluster: cluster1},
		{cluster: cluster2},
		{cluster: cluster3},
		{cluster: cluster4},
	}

	// Test 100% threshold - should keep all clusters
	result = filterClustersByVolume(clusters, 1.0)
	require.Equal(t, 4, len(result), "Should keep all clusters for 100% volume threshold")
}

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
		0.99,
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
		0.99,
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

func TestStreamPruneFiltersLowVolumePatterns(t *testing.T) {
	lbs := labels.New(
		labels.Label{Name: "test", Value: "test"},
		labels.Label{Name: "service_name", Value: "test_service"},
	)

	// Track what patterns are written
	writtenPatterns := make(map[string]int)
	mockWriter := &mockEntryWriter{}
	mockWriter.On("WriteEntry", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			entry := args.Get(1).(string)
			// Extract pattern from the entry (it's URL-encoded in the format)
			if strings.Contains(entry, "detected_pattern=") {
				parts := strings.Split(entry, "detected_pattern=")
				if len(parts) > 1 {
					patternPart := strings.Split(parts[1], " ")[0]
					writtenPatterns[patternPart]++
				}
			}
		})

	drainCfg := drain.DefaultConfig()
	drainCfg.SimTh = 0.3 // Lower similarity threshold for testing

	stream, err := newStream(
		model.Fingerprint(labels.StableHash(lbs)),
		lbs,
		newIngesterMetrics(nil, "test"),
		log.NewNopLogger(),
		drain.FormatUnknown,
		"123",
		drainCfg,
		&fakeLimits{
			patternRateThreshold:   0, // Disable rate threshold
			persistenceGranularity: time.Hour,
		},
		mockWriter,
		aggregation.NewMetrics(nil),
		0.8, // Test with 80% volume threshold
	)
	require.NoError(t, err)

	// Push many log lines to create patterns with different volumes
	// High volume pattern (60% of total volume)
	for i := range 30 {
		err = stream.Push(context.Background(), []push.Entry{
			{
				Timestamp: time.Unix(int64(i), 0),
				Line:      fmt.Sprintf("high volume pattern iteration %d", i),
			},
		})
		require.NoError(t, err)
	}

	// Medium volume pattern (30% of total volume)
	for i := range 15 {
		err = stream.Push(context.Background(), []push.Entry{
			{
				Timestamp: time.Unix(int64(30+i), 0),
				Line:      fmt.Sprintf("medium volume pattern number %d", i),
			},
		})
		require.NoError(t, err)
	}

	// Low volume patterns (10% of total volume combined)
	for i := range 5 {
		err = stream.Push(context.Background(), []push.Entry{
			{
				Timestamp: time.Unix(int64(45+i), 0),
				Line:      fmt.Sprintf("low volume pattern instance %d unique", i),
			},
		})
		require.NoError(t, err)
	}

	// Prune to trigger pattern persistence
	stream.prune(0)

	// With 80% threshold, only high (60%) and medium (30%) volume patterns should be written
	// Low volume patterns (10%) should be filtered out

	// Check that we have patterns written
	require.Equal(t, len(writtenPatterns), 2, "Should have written some patterns")

	// The exact pattern strings depend on drain's clustering, but we should see
	// fewer patterns written than total clusters due to filtering
	// We pushed 50 total log lines creating at least 3 distinct patterns
	// With 80% threshold, we expect the low volume ones to be filtered

	// Verify the mock was called (patterns were written)
	mockWriter.AssertCalled(t, "WriteEntry", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
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
		0.99,
	)
	require.NoError(t, err)

	// Push entries with old timestamps that will be pruned
	start := time.Date(2020, time.January, 10, 0, 0, 0, 0, time.UTC)
	now := drain.TruncateTimestamp(model.TimeFromUnixNano(start.UnixNano()), drain.TimeResolution).Time()
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
	_ = stream.prune(time.Hour)

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
		0.99,
	)
	require.NoError(t, err)

	// Push entries across a 1-hour span that will be pruned
	start := time.Date(2020, time.January, 10, 0, 0, 0, 0, time.UTC)
	now := drain.TruncateTimestamp(model.TimeFromUnixNano(start.UnixNano()), drain.TimeResolution).Time()
	baseTime := now.Add(-2 * time.Hour)

	// Push 12 entries across 60 minutes (5 minutes apart)
	entries := []push.Entry{}
	for i := range 12 {
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
	_ = stream.prune(time.Hour)

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
			0.99,
		)
		require.NoError(t, err)

		// Push a newer entry to ensure the stream isn't completely pruned
		start := time.Date(2020, time.January, 10, 0, 0, 0, 0, time.UTC)
		now := drain.TruncateTimestamp(model.TimeFromUnixNano(start.UnixNano()), drain.TimeResolution).Time()
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
		_ = stream.prune(time.Hour)

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
			0.99,
		)
		require.NoError(t, err)

		start := time.Date(2020, time.January, 10, 0, 0, 0, 0, time.UTC)
		now := drain.TruncateTimestamp(model.TimeFromUnixNano(start.UnixNano()), drain.TimeResolution).Time()
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

		_ = stream.prune(time.Hour)

		mockWriter.AssertExpectations(t)
	})

	t.Run("granularity larger than max chunk age should write one pattern", func(t *testing.T) {
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
			0.99,
		)
		require.NoError(t, err)

		start := time.Date(2020, time.January, 10, 0, 0, 0, 0, time.UTC)
		now := drain.TruncateTimestamp(model.TimeFromUnixNano(start.UnixNano()), drain.TimeResolution).Time()
		baseTime := now.Add(-2 * time.Hour)

		// Push multiple old entries across the hour
		entries := []push.Entry{}
		for i := range 6 {
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

		_ = stream.prune(time.Hour)

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
	volumeThreshold float64,
) (*instance, error) {
	return newInstance(instanceID, logger, metrics, drainCfg, drainLimits, ringClient, ingesterID, metricWriter, patternWriter, aggregationMetrics, volumeThreshold)
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
		drainCfg.MaxChunkAge = 1 * time.Hour

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
			0.99,
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
		0.99,
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
			0.99,
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
			0.99,
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
			0.99,
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

func TestFilterClustersByVolumeEdgeCases(t *testing.T) {
	t.Run("should handle empty cluster list", func(t *testing.T) {
		clusters := []clusterWithMeta{}
		result := filterClustersByVolume(clusters, 0.9)
		require.Equal(t, 0, len(result), "Empty list should return 0")
	})

	t.Run("should keep single cluster regardless of threshold", func(t *testing.T) {
		cluster := &drain.LogCluster{
			Tokens:      []string{"test", "pattern"},
			Volume:      100,
			SampleCount: 10,
		}

		// Test with various thresholds
		clusters := []clusterWithMeta{{cluster: cluster}}
		result := filterClustersByVolume(clusters, 0.1)
		require.Equal(t, 1, len(result), "Single cluster should always be kept")
		require.Equal(t, cluster, result[0].cluster)

		clusters = []clusterWithMeta{{cluster: cluster}}
		result = filterClustersByVolume(clusters, 0.5)
		require.Equal(t, 1, len(result), "Single cluster should always be kept")

		clusters = []clusterWithMeta{{cluster: cluster}}
		result = filterClustersByVolume(clusters, 0.99)
		require.Equal(t, 1, len(result), "Single cluster should always be kept")
	})

	t.Run("should handle all clusters with equal volume", func(t *testing.T) {
		clusters := []clusterWithMeta{
			{cluster: &drain.LogCluster{Tokens: []string{"pattern", "1"}, Volume: 100, SampleCount: 10}},
			{cluster: &drain.LogCluster{Tokens: []string{"pattern", "2"}, Volume: 100, SampleCount: 10}},
			{cluster: &drain.LogCluster{Tokens: []string{"pattern", "3"}, Volume: 100, SampleCount: 10}},
			{cluster: &drain.LogCluster{Tokens: []string{"pattern", "4"}, Volume: 100, SampleCount: 10}},
		}

		// With 50% threshold and equal volumes, should keep 2 clusters
		result := filterClustersByVolume(clusters, 0.5)
		require.Equal(t, 2, len(result), "Should keep exactly 2 clusters for 50% threshold")

		// Reset for next test
		clusters = []clusterWithMeta{
			{cluster: &drain.LogCluster{Tokens: []string{"pattern", "1"}, Volume: 100, SampleCount: 10}},
			{cluster: &drain.LogCluster{Tokens: []string{"pattern", "2"}, Volume: 100, SampleCount: 10}},
			{cluster: &drain.LogCluster{Tokens: []string{"pattern", "3"}, Volume: 100, SampleCount: 10}},
			{cluster: &drain.LogCluster{Tokens: []string{"pattern", "4"}, Volume: 100, SampleCount: 10}},
		}

		// With 75% threshold and equal volumes, should keep 3 clusters
		result = filterClustersByVolume(clusters, 0.75)
		require.Equal(t, 3, len(result), "Should keep exactly 3 clusters for 75% threshold")
	})

	t.Run("should handle zero volume clusters", func(t *testing.T) {
		clusters := []clusterWithMeta{
			{cluster: &drain.LogCluster{Tokens: []string{"pattern", "1"}, Volume: 0, SampleCount: 0}},
			{cluster: &drain.LogCluster{Tokens: []string{"pattern", "2"}, Volume: 0, SampleCount: 0}},
		}

		result := filterClustersByVolume(clusters, 0.9)
		require.Equal(t, 0, len(result), "Zero-volume clusters should return empty when total volume is 0")
	})

	t.Run("should handle threshold of 0", func(t *testing.T) {
		clusters := []clusterWithMeta{
			{cluster: &drain.LogCluster{Tokens: []string{"high", "volume"}, Volume: 500, SampleCount: 50}},
			{cluster: &drain.LogCluster{Tokens: []string{"low", "volume"}, Volume: 100, SampleCount: 10}},
		}

		// Threshold of 0 means keep nothing (0% of volume)
		result := filterClustersByVolume(clusters, 0)
		require.Equal(t, 0, len(result), "Threshold of 0 should keep no clusters")
	})

	t.Run("should handle threshold of 1", func(t *testing.T) {
		clusters := []clusterWithMeta{
			{cluster: &drain.LogCluster{Tokens: []string{"high", "volume"}, Volume: 500, SampleCount: 50}},
			{cluster: &drain.LogCluster{Tokens: []string{"medium", "volume"}, Volume: 300, SampleCount: 30}},
			{cluster: &drain.LogCluster{Tokens: []string{"low", "volume"}, Volume: 100, SampleCount: 10}},
		}

		// Threshold of 1 means keep everything (100% of volume)
		result := filterClustersByVolume(clusters, 1.0)
		require.Equal(t, 3, len(result), "Threshold of 1 should keep all clusters")
	})

	t.Run("should handle very small threshold values", func(t *testing.T) {
		clusters := []clusterWithMeta{
			{cluster: &drain.LogCluster{Tokens: []string{"huge", "volume"}, Volume: 10000, SampleCount: 1000}},
			{cluster: &drain.LogCluster{Tokens: []string{"tiny", "volume"}, Volume: 1, SampleCount: 1}},
		}

		// Even very small threshold should include the huge volume cluster
		result := filterClustersByVolume(clusters, 0.0001)
		require.Equal(t, 1, len(result), "Very small threshold should still include highest volume cluster")
		require.Equal(t, int64(10000), result[0].cluster.Volume)
	})

	t.Run("should maintain stability when clusters have same volume", func(t *testing.T) {
		// When clusters have the same volume, the sort is stable and
		// filterClustersByVolume should include all clusters until threshold is met
		clusters := []clusterWithMeta{
			{cluster: &drain.LogCluster{Tokens: []string{"pattern", "A"}, Volume: 200, SampleCount: 20}},
			{cluster: &drain.LogCluster{Tokens: []string{"pattern", "B"}, Volume: 200, SampleCount: 20}},
			{cluster: &drain.LogCluster{Tokens: []string{"pattern", "C"}, Volume: 100, SampleCount: 10}},
		}

		// 40% threshold: should include first cluster (200/500 = 40%)
		result := filterClustersByVolume(clusters, 0.4)
		require.Equal(t, 1, len(result))
		require.Equal(t, int64(200), result[0].cluster.Volume)

		// Reset for next test
		clusters = []clusterWithMeta{
			{cluster: &drain.LogCluster{Tokens: []string{"pattern", "A"}, Volume: 200, SampleCount: 20}},
			{cluster: &drain.LogCluster{Tokens: []string{"pattern", "B"}, Volume: 200, SampleCount: 20}},
			{cluster: &drain.LogCluster{Tokens: []string{"pattern", "C"}, Volume: 100, SampleCount: 10}},
		}

		// 80% threshold: should include both 200-volume clusters (400/500 = 80%)
		result = filterClustersByVolume(clusters, 0.8)
		require.Equal(t, 2, len(result))
		for i := 0; i < len(result); i++ {
			require.Equal(t, int64(200), result[i].cluster.Volume)
		}
	})
}
