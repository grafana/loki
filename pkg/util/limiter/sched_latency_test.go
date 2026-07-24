package limiter

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// preRecordedSchedLatencyScanner replays instantaneous wait-seconds values,
// accumulating them into the cumulative counter Scan returns.
type preRecordedSchedLatencyScanner struct {
	instantWaitSeconds []float64
	totalWaitSeconds   float64
	err                error
}

func (s *preRecordedSchedLatencyScanner) Scan() (float64, error) {
	if s.err != nil {
		return 0, s.err
	}
	if len(s.instantWaitSeconds) > 0 {
		s.totalWaitSeconds += s.instantWaitSeconds[0]
		s.instantWaitSeconds = s.instantWaitSeconds[1:]
	}
	return s.totalWaitSeconds, nil
}

func TestSchedulerBacklogLimiter(t *testing.T) {
	setup := func(t *testing.T, backlogLimit float64, gomaxprocs int, instantWaitSeconds []float64) (*SchedulerBacklogLimiter, *preRecordedSchedLatencyScanner, prometheus.Gatherer) {
		scanner := &preRecordedSchedLatencyScanner{instantWaitSeconds: instantWaitSeconds}
		reg := prometheus.NewPedanticRegistry()
		lim := NewSchedulerBacklogLimiter(backlogLimit, log.NewNopLogger(), reg)
		lim.scanner = scanner
		lim.gomaxprocs = gomaxprocs
		require.Empty(t, lim.LimitingReason(), "Limiting should initially be disabled")

		return lim, scanner, reg
	}

	warmup := func(lim *SchedulerBacklogLimiter, tim *time.Time) {
		for i := 0; i < int(resourceUtilizationSlidingWindow.Seconds()); i++ {
			lim.compute(func() time.Time { return *tim })
			*tim = tim.Add(resourceUtilizationUpdateInterval)
		}
	}

	t.Run("no limiting during the warmup period despite high backlog", func(t *testing.T) {
		// 4 goroutine-seconds of waiting per second on 2 procs = backlog 2 per proc.
		lim, _, _ := setup(t, 1, 2, generateConstCPUUtilization(120, 4))

		tim := time.Now()
		for i := 0; i < int(resourceUtilizationSlidingWindow.Seconds()); i++ {
			backlog := lim.compute(func() time.Time { return tim })
			tim = tim.Add(resourceUtilizationUpdateInterval)
			require.Zero(t, backlog, "Backlog should be 0 during warmup")
			require.Empty(t, lim.LimitingReason(), "Limiting should be disabled during warmup")
		}

		lim.compute(func() time.Time { return tim })
		require.Equal(t, "scheduler", lim.LimitingReason(), "Limiting should be enabled after warmup")
	})

	t.Run("limiting is enabled above the limit and disabled when backlog drops", func(t *testing.T) {
		// Backlog 2 per proc until just past the warmup, then idle.
		values := generateConstCPUUtilization(61, 4)
		values = append(values, generateConstCPUUtilization(120, 0)...)
		lim, _, reg := setup(t, 1, 2, values)

		tim := time.Now()
		warmup(lim, &tim)

		lim.compute(func() time.Time { return tim })
		tim = tim.Add(resourceUtilizationUpdateInterval)
		require.Equal(t, "scheduler", lim.LimitingReason(), "Limiting should be enabled due to scheduler backlog")

		// The pre-recorded backlog drops to 0, the moving average decays below the limit.
		for i := 0; i < 60; i++ {
			lim.compute(func() time.Time { return tim })
			tim = tim.Add(resourceUtilizationUpdateInterval)
		}
		require.Empty(t, lim.LimitingReason(), "Limiting should be disabled again")

		count, err := testutil.GatherAndCount(reg, "utilization_limiter_current_scheduler_backlog")
		require.NoError(t, err)
		require.Equal(t, 1, count)
	})

	t.Run("limiting is disabled when the limit is 0", func(t *testing.T) {
		lim, _, _ := setup(t, 0, 2, generateConstCPUUtilization(180, 100))

		tim := time.Now()
		warmup(lim, &tim)

		for i := 0; i < 60; i++ {
			lim.compute(func() time.Time { return tim })
			tim = tim.Add(resourceUtilizationUpdateInterval)
			require.Empty(t, lim.LimitingReason(), "Limiting should be disabled")
		}
	})

	t.Run("scan errors clear an active limiting reason", func(t *testing.T) {
		lim, scanner, _ := setup(t, 1, 2, generateConstCPUUtilization(180, 4))

		tim := time.Now()
		warmup(lim, &tim)
		lim.compute(func() time.Time { return tim })
		require.Equal(t, "scheduler", lim.LimitingReason())

		scanner.err = errors.New("boom")
		lim.compute(func() time.Time { return tim })
		require.Empty(t, lim.LimitingReason(), "Limiting should fail open on scan errors")
	})

	t.Run("updates elapsing less than half the interval are skipped", func(t *testing.T) {
		lim, _, _ := setup(t, 1, 2, generateConstCPUUtilization(10, 1))

		tim := time.Now()
		lim.compute(func() time.Time { return tim })
		require.InDelta(t, 1, lim.lastTotalWait, 0.0001)

		for expected := 2; expected <= 5; expected++ {
			tim = tim.Add(resourceUtilizationUpdateInterval)
			lim.compute(func() time.Time { return tim })
			require.InDelta(t, float64(expected), lim.lastTotalWait, 0.0001)
		}

		// Consecutive computations at a very short interval do not update the counter.
		for i := 0; i < 5; i++ {
			tim = tim.Add(resourceUtilizationUpdateInterval / 10)
			lim.compute(func() time.Time { return tim })
			require.InDelta(t, float64(5), lim.lastTotalWait, 0.0001)
		}

		// One more short tick crosses the half-interval mark and is tracked.
		tim = tim.Add(resourceUtilizationUpdateInterval / 10)
		lim.compute(func() time.Time { return tim })
		require.InDelta(t, float64(10), lim.lastTotalWait, 0.0001)
	})

	t.Run("backlog is normalized by GOMAXPROCS", func(t *testing.T) {
		// Same wait input: 4 goroutine-seconds of waiting per second.
		limSingle, _, _ := setup(t, 0, 1, generateConstCPUUtilization(180, 4))
		limQuad, _, _ := setup(t, 0, 4, generateConstCPUUtilization(180, 4))

		tim1, tim4 := time.Now(), time.Now()
		warmup(limSingle, &tim1)
		warmup(limQuad, &tim4)

		var backlogSingle, backlogQuad float64
		for i := 0; i < 60; i++ {
			backlogSingle = limSingle.compute(func() time.Time { return tim1 })
			tim1 = tim1.Add(resourceUtilizationUpdateInterval)
			backlogQuad = limQuad.compute(func() time.Time { return tim4 })
			tim4 = tim4.Add(resourceUtilizationUpdateInterval)
		}

		assert.InDelta(t, 4, backlogSingle, 0.1)
		assert.InDelta(t, 1, backlogQuad, 0.1)
		assert.InDelta(t, backlogSingle/4, backlogQuad, 0.01)
	})

	t.Run("gauge reports the current backlog", func(t *testing.T) {
		// Constant backlog of 1 per proc: 2 wait-seconds per second on 2 procs.
		lim, _, reg := setup(t, 0, 2, generateConstCPUUtilization(180, 2))

		tim := time.Now()
		warmup(lim, &tim)
		var backlog float64
		for i := 0; i < 60; i++ {
			backlog = lim.compute(func() time.Time { return tim })
			tim = tim.Add(resourceUtilizationUpdateInterval)
		}

		// The moving average asymptotically approaches the constant input.
		assert.InDelta(t, 1, backlog, 0.05)

		mfs, err := reg.Gather()
		require.NoError(t, err)
		require.Len(t, mfs, 1)
		require.Equal(t, "utilization_limiter_current_scheduler_backlog", mfs[0].GetName())
		require.Len(t, mfs[0].GetMetric(), 1)
		assert.Equal(t, backlog, mfs[0].GetMetric()[0].GetGauge().GetValue())
	})
}

func TestRuntimeSchedLatencyScanner(t *testing.T) {
	scanner, err := newRuntimeSchedLatencyScanner()
	require.NoError(t, err)

	first, err := scanner.Scan()
	require.NoError(t, err)
	require.GreaterOrEqual(t, first, 0.0)

	// Churn some goroutines to generate scheduling events.
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Millisecond)
		}()
	}
	wg.Wait()

	second, err := scanner.Scan()
	require.NoError(t, err)
	require.GreaterOrEqual(t, second, first, "cumulative wait time must be monotonic")
}
