package drain

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestLogClusterIterator_WithConfigurableSampleInterval(t *testing.T) {
	t.Run("should use configurable sample interval in chunks", func(t *testing.T) {
		customChunkDuration := 30 * time.Minute
		customSampleInterval := 5 * time.Second

		cluster := &LogCluster{
			Tokens: []string{"test", "pattern"},
		}

		// Add samples to the cluster
		ts1 := model.Time(5000)  // 5 seconds
		ts2 := model.Time(10000) // 10 seconds

		result1 := cluster.append(ts1, customChunkDuration, customSampleInterval)
		result2 := cluster.append(ts2, customChunkDuration, customSampleInterval)

		require.Nil(t, result1, "first append should return nil")
		require.NotNil(t, result2, "second append should return previous sample")

		// Test that Iterator uses the configurable sample interval
		sampleIntervalMs := model.Time(customSampleInterval.Nanoseconds() / 1e6)
		it := cluster.Iterator("info", model.Time(0), model.Time(15000), sampleIntervalMs, sampleIntervalMs)
		require.NotNil(t, it, "iterator should not be nil")

		var samples []logproto.PatternSample
		for it.Next() {
			samples = append(samples, it.At())
		}
		require.NoError(t, it.Close())

		// Should have samples at the configured interval
		require.Len(t, samples, 2, "should have 2 samples")
		require.Equal(t, int64(1), samples[0].Value)
		require.Equal(t, int64(1), samples[1].Value)
	})

	t.Run("should return direct samples when step equals sample interval", func(t *testing.T) {
		customChunkDuration := 30 * time.Minute
		customSampleInterval := 1 * time.Second // High resolution input

		cluster := &LogCluster{
			Tokens: []string{"test", "pattern"},
		}

		// Add samples every second
		timestamps := []model.Time{
			model.Time(1000), // 1s
			model.Time(2000), // 2s
			model.Time(3000), // 3s
			model.Time(6000), // 6s
			model.Time(7000), // 7s
			model.Time(8000), // 8s
		}

		for _, ts := range timestamps {
			cluster.append(ts, customChunkDuration, customSampleInterval)
		}

		// When step == sampleInterval, should return direct samples (pass-through)
		step := model.Time(5000)           // 5s step
		sampleInterval := model.Time(5000) // 5s sample interval (same as step)
		it := cluster.Iterator("info", model.Time(0), model.Time(10000), step, sampleInterval)
		require.NotNil(t, it, "iterator should not be nil")

		var samples []logproto.PatternSample
		for it.Next() {
			samples = append(samples, it.At())
		}
		require.NoError(t, it.Close())

		// Should return direct samples since step == sampleInterval
		require.Len(t, samples, 6, "should have 6 direct samples")
		require.Equal(t, model.Time(1000), samples[0].Timestamp)
		require.Equal(t, int64(1), samples[0].Value)
		require.Equal(t, model.Time(2000), samples[1].Timestamp)
		require.Equal(t, int64(1), samples[1].Value)
		require.Equal(t, model.Time(3000), samples[2].Timestamp)
		require.Equal(t, int64(1), samples[2].Value)
		require.Equal(t, model.Time(6000), samples[3].Timestamp)
		require.Equal(t, int64(1), samples[3].Value)
		require.Equal(t, model.Time(7000), samples[4].Timestamp)
		require.Equal(t, int64(1), samples[4].Value)
		require.Equal(t, model.Time(8000), samples[5].Timestamp)
		require.Equal(t, int64(1), samples[5].Value)
	})

	t.Run("should aggregate samples when step differs from sample interval", func(t *testing.T) {
		customChunkDuration := 30 * time.Minute
		customSampleInterval := 2 * time.Second // 2s resolution input

		cluster := &LogCluster{
			Tokens: []string{"test", "pattern"},
		}

		// Add samples at 2-second intervals
		timestamps := []model.Time{
			model.Time(2000),  // 2s
			model.Time(4000),  // 4s
			model.Time(6000),  // 6s
			model.Time(8000),  // 8s
			model.Time(12000), // 12s
			model.Time(14000), // 14s
		}

		for _, ts := range timestamps {
			cluster.append(ts, customChunkDuration, customSampleInterval)
		}

		// Use different step (10s) from sample interval (5s) to force aggregation
		step := model.Time(10000)          // 10s step for aggregation
		sampleInterval := model.Time(5000) // 5s sample interval (different from step)
		it := cluster.Iterator("info", model.Time(0), model.Time(16000), step, sampleInterval)
		require.NotNil(t, it, "iterator should not be nil")

		var samples []logproto.PatternSample
		for it.Next() {
			samples = append(samples, it.At())
		}
		require.NoError(t, it.Close())

		// Should aggregate into 10s buckets since step != sampleInterval
		require.Len(t, samples, 2, "should have 2 aggregated samples")
		require.Equal(t, model.Time(0), samples[0].Timestamp)     // Bucket 0-10s
		require.Equal(t, int64(4), samples[0].Value)              // Contains 4 samples: 2s, 4s, 6s, 8s
		require.Equal(t, model.Time(10000), samples[1].Timestamp) // Bucket 10-20s
		require.Equal(t, int64(2), samples[1].Value)              // Contains 2 samples: 12s, 14s
	})

	t.Run("should show truncation effects during ingestion", func(t *testing.T) {
		customChunkDuration := 30 * time.Minute
		customSampleInterval := 3 * time.Second // 3s truncation during ingestion

		cluster := &LogCluster{
			Tokens: []string{"test", "pattern"},
		}

		// Add samples at odd intervals (will be truncated to 3s boundaries during ingestion)
		timestamps := []model.Time{
			model.Time(2000),  // 2s -> truncated to 0s
			model.Time(7000),  // 7s -> truncated to 6s
			model.Time(13000), // 13s -> truncated to 12s
			model.Time(18000), // 18s -> truncated to 18s
		}

		for _, ts := range timestamps {
			cluster.append(ts, customChunkDuration, customSampleInterval)
		}

		// Use 10-second step that differs from 3-second sample interval
		step := model.Time(10000)           // 10s step
		sampleInterval := model.Time(10000) // 10s sample interval (different from input 3s)
		it := cluster.Iterator("info", model.Time(0), model.Time(25000), step, sampleInterval)
		require.NotNil(t, it, "iterator should not be nil")

		var samples []logproto.PatternSample
		for it.Next() {
			samples = append(samples, it.At())
		}
		require.NoError(t, it.Close())

		// Should return truncated samples directly (step != sampleInterval but no aggregation occurs)
		// Input samples after truncation: 0s, 6s, 12s, 18s
		require.Len(t, samples, 4, "should have 4 direct samples")
		require.Equal(t, model.Time(0), samples[0].Timestamp) // Sample truncated to 0s
		require.Equal(t, int64(1), samples[0].Value)
		require.Equal(t, model.Time(6000), samples[1].Timestamp) // Sample truncated to 6s
		require.Equal(t, int64(1), samples[1].Value)
		require.Equal(t, model.Time(12000), samples[2].Timestamp) // Sample truncated to 12s
		require.Equal(t, int64(1), samples[2].Value)
		require.Equal(t, model.Time(18000), samples[3].Timestamp) // Sample truncated to 18s
		require.Equal(t, int64(1), samples[3].Value)
	})

	t.Run("should handle different step vs sample interval parameters", func(t *testing.T) {
		customChunkDuration := 30 * time.Minute
		customSampleInterval := 2 * time.Second

		cluster := &LogCluster{
			Tokens: []string{"test", "pattern"},
		}

		// Add samples at 2-second intervals
		timestamps := []model.Time{
			model.Time(2000),  // 2s
			model.Time(4000),  // 4s
			model.Time(6000),  // 6s
			model.Time(8000),  // 8s
			model.Time(12000), // 12s
			model.Time(14000), // 14s
		}

		for _, ts := range timestamps {
			cluster.append(ts, customChunkDuration, customSampleInterval)
		}

		// Use different step and sample interval to test the distinction
		step := model.Time(5000)           // 5s step for output bucketing
		sampleInterval := model.Time(3000) // 3s sample interval (different from both input and step)
		it := cluster.Iterator("info", model.Time(0), model.Time(16000), step, sampleInterval)
		require.NotNil(t, it, "iterator should not be nil")

		var samples []logproto.PatternSample
		for it.Next() {
			samples = append(samples, it.At())
		}
		require.NoError(t, it.Close())

		// Should use step for bucketing, with aggregation since step != sampleInterval
		require.Len(t, samples, 3, "should have 3 step-sized buckets")
		require.Equal(t, model.Time(0), samples[0].Timestamp)     // Bucket 0-5s
		require.Equal(t, int64(2), samples[0].Value)              // Contains samples at 2s, 4s (both truncated to 0s and 2s)
		require.Equal(t, model.Time(5000), samples[1].Timestamp)  // Bucket 5-10s
		require.Equal(t, int64(2), samples[1].Value)              // Contains samples at 6s, 8s (both truncated to 6s, 8s)
		require.Equal(t, model.Time(10000), samples[2].Timestamp) // Bucket 10-15s
		require.Equal(t, int64(2), samples[2].Value)              // Contains samples at 12s, 14s (both truncated to 12s, 14s)
	})
}
