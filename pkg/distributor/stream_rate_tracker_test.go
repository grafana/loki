package distributor

import (
	"flag"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// mockInstanceCount implements [ReadLifecycler].
type mockInstanceCount struct {
	count int
}

func (m *mockInstanceCount) HealthyInstancesCount() int { return m.count }

// defaultStreamRateTrackerConfig returns the config that flag registration
// produces, so tests exercise the same defaults as a running distributor.
func defaultStreamRateTrackerConfig() StreamRateTrackerConfig {
	var cfg StreamRateTrackerConfig
	cfg.RegisterFlagsWithPrefix("test", flag.NewFlagSet("test", flag.PanicOnError))
	return cfg
}

// newTestStreamRateTracker returns a tracker whose folds are driven manually.
// The service is never started, so nothing folds concurrently.
func newTestStreamRateTracker(t *testing.T, cfg StreamRateTrackerConfig, instances ReadLifecycler) (*streamRateTracker, time.Time) {
	t.Helper()
	tracker := newStreamRateTracker(cfg, instances, prometheus.NewRegistry())
	now := time.Unix(0, 0)
	tracker.lastFold = now
	return tracker, now
}

// numStreams returns the number of tracked streams across all tenants.
func (t *streamRateTracker) numStreams() int {
	var n int
	for i := range t.stripes {
		t.locks[i].RLock()
		for _, streams := range t.stripes[i] {
			n += len(streams)
		}
		t.locks[i].RUnlock()
	}
	return n
}

func TestStreamRateTrackerConfig_Validate(t *testing.T) {
	tests := []struct {
		name          string
		mutate        func(cfg *StreamRateTrackerConfig)
		expectedError string
	}{{
		name:   "defaults are valid",
		mutate: func(*StreamRateTrackerConfig) {},
	}, {
		name:          "zero update interval",
		mutate:        func(cfg *StreamRateTrackerConfig) { cfg.UpdateInterval = 0 },
		expectedError: "update interval must be greater than 0",
	}, {
		name:          "keep alive shorter than update interval",
		mutate:        func(cfg *StreamRateTrackerConfig) { cfg.KeepAlive = cfg.UpdateInterval - 1 },
		expectedError: "keep alive must not be shorter than the update interval",
	}, {
		name:          "zero smoothing factor",
		mutate:        func(cfg *StreamRateTrackerConfig) { cfg.SmoothingFactor = 0 },
		expectedError: "smoothing factor must be in the range (0, 1]",
	}, {
		name:          "smoothing factor greater than one",
		mutate:        func(cfg *StreamRateTrackerConfig) { cfg.SmoothingFactor = 1.1 },
		expectedError: "smoothing factor must be in the range (0, 1]",
	}, {
		name:   "smoothing factor of one",
		mutate: func(cfg *StreamRateTrackerConfig) { cfg.SmoothingFactor = 1 },
	}, {
		name:   "scaling mode none",
		mutate: func(cfg *StreamRateTrackerConfig) { cfg.ScalingMode = ScalingModeNone },
	}, {
		name:          "unknown scaling mode",
		mutate:        func(cfg *StreamRateTrackerConfig) { cfg.ScalingMode = "global" },
		expectedError: `unsupported stream rate tracker scaling mode: "global"`,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := defaultStreamRateTrackerConfig()
			test.mutate(&cfg)
			err := cfg.Validate()
			if test.expectedError != "" {
				require.ErrorContains(t, err, test.expectedError)
				return
			}
			require.NoError(t, err)
		})
	}
}

// An unobserved stream, and a stream observed for the first time within the
// current interval, must both report a zero rate. shardCountFor relies on the
// zero push rate to not shard a stream before its rate is understood.
func TestStreamRateTracker_RateForUnknownStream(t *testing.T) {
	cfg := defaultStreamRateTrackerConfig()
	tracker, now := newTestStreamRateTracker(t, cfg, &mockInstanceCount{count: 1})

	rate, pushRate := tracker.RateFor("tenant", 0x1)
	require.Zero(t, rate)
	require.Zero(t, pushRate)

	tracker.Observe("tenant", 0x1, 1000)
	rate, pushRate = tracker.RateFor("tenant", 0x1)
	require.Zero(t, rate, "observations must not be visible before the first fold")
	require.Zero(t, pushRate)

	tracker.fold(now.Add(cfg.UpdateInterval))
	rate, pushRate = tracker.RateFor("tenant", 0x1)
	require.NotZero(t, rate)
	require.NotZero(t, pushRate)
}

// The tracker must produce the same exponential moving average as the rateStore
// it replaces, given the same series of per-interval samples.
func TestStreamRateTracker_MatchesRateStoreEWMA(t *testing.T) {
	cfg := defaultStreamRateTrackerConfig()
	require.Equal(t, smoothingFactor, cfg.SmoothingFactor, "the default must match the rateStore")

	tracker, now := newTestStreamRateTracker(t, cfg, &mockInstanceCount{count: 1})

	// One push of 1000 bytes per interval, which is a steady 1000 bytes/sec at
	// the default interval of one second.
	var expected float64
	for i := 0; i < 20; i++ {
		tracker.Observe("tenant", 0x1, 1000)
		now = now.Add(cfg.UpdateInterval)
		tracker.fold(now)

		expected = weightedMovingAverageF(1000, expected)
		rate, _ := tracker.RateFor("tenant", 0x1)
		require.Equal(t, int64(expected), rate)
	}

	// The average converges towards the steady state from below.
	rate, pushRate := tracker.RateFor("tenant", 0x1)
	require.Greater(t, rate, int64(990))
	require.LessOrEqual(t, rate, int64(1000))
	require.InDelta(t, 1.0, pushRate, 0.01)

	// Once the pushes stop, it decays back towards zero.
	for i := 0; i < 20; i++ {
		now = now.Add(cfg.UpdateInterval)
		tracker.fold(now)
	}
	rate, pushRate = tracker.RateFor("tenant", 0x1)
	require.Less(t, rate, int64(10))
	require.Less(t, pushRate, 0.01)
}

// The sample is normalized by the time actually elapsed, not by the configured
// interval, so a delayed fold does not inflate the rate.
func TestStreamRateTracker_NormalizesByElapsedTime(t *testing.T) {
	cfg := defaultStreamRateTrackerConfig()
	cfg.SmoothingFactor = 1 // No smoothing, so the sample is the rate.
	tracker, now := newTestStreamRateTracker(t, cfg, &mockInstanceCount{count: 1})

	// 4000 bytes observed over four seconds is 1000 bytes/sec, even though the
	// configured interval is one second.
	tracker.Observe("tenant", 0x1, 4000)
	tracker.fold(now.Add(4 * time.Second))

	rate, pushRate := tracker.RateFor("tenant", 0x1)
	require.Equal(t, int64(1000), rate)
	require.InDelta(t, 0.25, pushRate, 0.001)
}

// A fold that does not advance the clock must be a no-op rather than dividing
// by zero or discarding the accumulated observations.
func TestStreamRateTracker_FoldWithoutElapsedTime(t *testing.T) {
	cfg := defaultStreamRateTrackerConfig()
	tracker, now := newTestStreamRateTracker(t, cfg, &mockInstanceCount{count: 1})

	tracker.Observe("tenant", 0x1, 1000)
	tracker.fold(now)
	rate, _ := tracker.RateFor("tenant", 0x1)
	require.Zero(t, rate)

	tracker.fold(now.Add(-time.Second))
	rate, _ = tracker.RateFor("tenant", 0x1)
	require.Zero(t, rate)

	// The observation is still pending and is folded into the next real fold.
	tracker.fold(now.Add(cfg.UpdateInterval))
	rate, _ = tracker.RateFor("tenant", 0x1)
	require.NotZero(t, rate)
}

func TestStreamRateTracker_Eviction(t *testing.T) {
	cfg := defaultStreamRateTrackerConfig()
	cfg.KeepAlive = 5 * cfg.UpdateInterval
	tracker, now := newTestStreamRateTracker(t, cfg, &mockInstanceCount{count: 1})

	tracker.Observe("tenant", 0x1, 1000)
	now = now.Add(cfg.UpdateInterval)
	tracker.fold(now)
	require.Equal(t, 1, tracker.numStreams())

	// Idle, but within the keep alive.
	for i := 0; i < 5; i++ {
		now = now.Add(cfg.UpdateInterval)
		tracker.fold(now)
		require.Equal(t, 1, tracker.numStreams())
	}

	// One more idle interval exceeds the keep alive.
	now = now.Add(cfg.UpdateInterval)
	tracker.fold(now)
	require.Zero(t, tracker.numStreams())

	// The tenant is dropped along with its last stream.
	tracker.locks[0x1&uint64(tracker.size-1)].RLock()
	_, ok := tracker.stripes[0x1&uint64(tracker.size-1)]["tenant"]
	tracker.locks[0x1&uint64(tracker.size-1)].RUnlock()
	require.False(t, ok)

	rate, pushRate := tracker.RateFor("tenant", 0x1)
	require.Zero(t, rate)
	require.Zero(t, pushRate)
}

// A stream that keeps being pushed to must never be evicted, no matter how long
// it has been tracked.
func TestStreamRateTracker_NoEvictionWhileActive(t *testing.T) {
	cfg := defaultStreamRateTrackerConfig()
	cfg.KeepAlive = 2 * cfg.UpdateInterval
	tracker, now := newTestStreamRateTracker(t, cfg, &mockInstanceCount{count: 1})

	for i := 0; i < 20; i++ {
		tracker.Observe("tenant", 0x1, 1000)
		now = now.Add(cfg.UpdateInterval)
		tracker.fold(now)
		require.Equal(t, 1, tracker.numStreams())
	}
}

func TestStreamRateTracker_ScalingFactor(t *testing.T) {
	tests := []struct {
		name           string
		scalingMode    string
		instances      ReadLifecycler
		expectedFactor float64
	}{{
		name:           "no scaling",
		scalingMode:    ScalingModeNone,
		instances:      &mockInstanceCount{count: 8},
		expectedFactor: 1,
	}, {
		name:           "scaled by healthy distributors",
		scalingMode:    ScalingModeHealthyDistributors,
		instances:      &mockInstanceCount{count: 8},
		expectedFactor: 8,
	}, {
		name:           "an empty ring does not zero the rate",
		scalingMode:    ScalingModeHealthyDistributors,
		instances:      &mockInstanceCount{count: 0},
		expectedFactor: 1,
	}, {
		name:           "a single distributor is not scaled",
		scalingMode:    ScalingModeHealthyDistributors,
		instances:      &mockInstanceCount{count: 1},
		expectedFactor: 1,
	}, {
		name:           "a missing lifecycler does not scale",
		scalingMode:    ScalingModeHealthyDistributors,
		instances:      nil,
		expectedFactor: 1,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := defaultStreamRateTrackerConfig()
			cfg.SmoothingFactor = 1 // No smoothing, so the sample is the rate.
			cfg.ScalingMode = test.scalingMode
			tracker, now := newTestStreamRateTracker(t, cfg, test.instances)

			require.Equal(t, test.expectedFactor, tracker.scalingFactor())

			tracker.Observe("tenant", 0x1, 1000)
			tracker.fold(now.Add(time.Second))

			rate, pushRate := tracker.RateFor("tenant", 0x1)
			require.Equal(t, int64(1000*test.expectedFactor), rate)
			require.InDelta(t, test.expectedFactor, pushRate, 0.001)
		})
	}
}

// Streams and tenants must be accounted separately.
func TestStreamRateTracker_SeparatesTenantsAndStreams(t *testing.T) {
	cfg := defaultStreamRateTrackerConfig()
	cfg.SmoothingFactor = 1
	tracker, now := newTestStreamRateTracker(t, cfg, &mockInstanceCount{count: 1})

	tracker.Observe("tenant-a", 0x1, 1000)
	tracker.Observe("tenant-a", 0x2, 2000)
	tracker.Observe("tenant-b", 0x1, 3000)
	tracker.fold(now.Add(time.Second))

	rate, _ := tracker.RateFor("tenant-a", 0x1)
	require.Equal(t, int64(1000), rate)
	rate, _ = tracker.RateFor("tenant-a", 0x2)
	require.Equal(t, int64(2000), rate)
	rate, _ = tracker.RateFor("tenant-b", 0x1)
	require.Equal(t, int64(3000), rate)
	rate, _ = tracker.RateFor("tenant-c", 0x1)
	require.Zero(t, rate)
}

// Multiple pushes within one interval accumulate.
func TestStreamRateTracker_AccumulatesWithinInterval(t *testing.T) {
	cfg := defaultStreamRateTrackerConfig()
	cfg.SmoothingFactor = 1
	tracker, now := newTestStreamRateTracker(t, cfg, &mockInstanceCount{count: 1})

	for i := 0; i < 4; i++ {
		tracker.Observe("tenant", 0x1, 250)
	}
	tracker.fold(now.Add(time.Second))

	rate, pushRate := tracker.RateFor("tenant", 0x1)
	require.Equal(t, int64(1000), rate)
	require.InDelta(t, 4.0, pushRate, 0.001)
}

// A push with no bytes still counts as a push, so that an empty stream is not
// mistaken for an unobserved one.
func TestStreamRateTracker_ObserveZeroBytes(t *testing.T) {
	cfg := defaultStreamRateTrackerConfig()
	cfg.SmoothingFactor = 1
	tracker, now := newTestStreamRateTracker(t, cfg, &mockInstanceCount{count: 1})

	tracker.Observe("tenant", 0x1, 0)
	tracker.fold(now.Add(time.Second))

	rate, pushRate := tracker.RateFor("tenant", 0x1)
	require.Zero(t, rate)
	require.InDelta(t, 1.0, pushRate, 0.001)
}

func TestStreamRateTracker_Concurrency(t *testing.T) {
	cfg := defaultStreamRateTrackerConfig()
	tracker, now := newTestStreamRateTracker(t, cfg, &mockInstanceCount{count: 4})

	const (
		writers    = 8
		readers    = 4
		iterations = 500
	)

	var (
		wg   sync.WaitGroup
		done = make(chan struct{})
	)

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				tenant := fmt.Sprintf("tenant-%d", i%4)
				tracker.Observe(tenant, uint64(i%64), 100)
			}
		}()
	}
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				tracker.RateFor(fmt.Sprintf("tenant-%d", i%4), uint64(i%64))
			}
		}()
	}
	// Fold concurrently with the writers and readers. Only one goroutine folds,
	// which is the contract of fold. The virtual clock advances by one interval
	// per fold and must stay well below the keep alive so that nothing is
	// evicted while the writers are still running.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; ; i++ {
			select {
			case <-done:
				return
			default:
			}
			tracker.fold(now.Add(time.Duration(i) * cfg.UpdateInterval))
			time.Sleep(time.Millisecond)
		}
	}()

	wgWait := make(chan struct{})
	go func() {
		wg.Wait()
		close(wgWait)
	}()
	// Writers and readers finish first, then the folder is told to stop.
	time.Sleep(50 * time.Millisecond)
	close(done)
	<-wgWait

	require.NotZero(t, tracker.numStreams())
}

func BenchmarkStreamRateTracker_Observe(b *testing.B) {
	var cfg StreamRateTrackerConfig
	cfg.RegisterFlagsWithPrefix("test", flag.NewFlagSet("test", flag.PanicOnError))
	tracker := newStreamRateTracker(cfg, &mockInstanceCount{count: 8}, prometheus.NewRegistry())

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var i uint64
		for pb.Next() {
			tracker.Observe("tenant", i%10000, 1000)
			i++
		}
	})
}
