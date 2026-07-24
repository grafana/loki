package limiter

import (
	"context"
	"fmt"
	gomath "math"
	"runtime"
	"runtime/metrics"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/util/math"
)

const schedLatenciesMetricName = "/sched/latencies:seconds"

type schedLatencyScanner interface {
	// Scan returns the cumulative time, in seconds, that goroutines have spent
	// runnable waiting for a processor since process start, or an error.
	Scan() (float64, error)
}

// runtimeSchedLatencyScanner sums the Go runtime's scheduler latency histogram.
type runtimeSchedLatencyScanner struct {
	sample []metrics.Sample
	// bucketValues holds the lower bound of each histogram bucket. Bucket
	// boundaries are guaranteed constant for the process lifetime, so they are
	// computed once.
	bucketValues []float64
}

func newRuntimeSchedLatencyScanner() (*runtimeSchedLatencyScanner, error) {
	sample := []metrics.Sample{{Name: schedLatenciesMetricName}}
	metrics.Read(sample)
	if sample[0].Value.Kind() != metrics.KindFloat64Histogram {
		return nil, fmt.Errorf("runtime metric %s is not a histogram", schedLatenciesMetricName)
	}

	h := sample[0].Value.Float64Histogram()
	bucketValues := make([]float64, len(h.Counts))
	for i := range h.Counts {
		lb := h.Buckets[i]
		if gomath.IsInf(lb, -1) {
			// A latency cannot be negative, count the first bucket as zero.
			lb = 0
		}
		bucketValues[i] = lb
	}

	return &runtimeSchedLatencyScanner{
		sample:       sample,
		bucketValues: bucketValues,
	}, nil
}

func (s *runtimeSchedLatencyScanner) Scan() (float64, error) {
	metrics.Read(s.sample)
	h := s.sample[0].Value.Float64Histogram()

	// Estimate the total wait time from the buckets' lower bounds, the same
	// (slightly underestimating) approach the Prometheus Go collector uses for
	// go_sched_latencies_seconds, so this value can be cross-checked against
	// rate(go_sched_latencies_seconds_sum[1m]) on dashboards.
	var total float64
	for i, c := range h.Counts {
		if c != 0 {
			total += s.bucketValues[i] * float64(c)
		}
	}
	return total, nil
}

// SchedulerBacklogLimiter is a Service offering limiting based on the Go scheduler
// backlog: the average number of runnable goroutines waiting for a processor, per
// processor (GOMAXPROCS). It is derived from the scheduler latency histogram via
// Little's law: wait-seconds accumulated per second equals the average number of
// goroutines waiting at any instant.
//
// The backlog is close to 0 on a healthy instance and exceeds 1 when there is more
// runnable work than processors. Unlike CPU utilization, it also captures
// saturation from memory-mapped file page faults, which block processors without
// consuming CPU.
type SchedulerBacklogLimiter struct {
	services.Service

	logger  log.Logger
	scanner schedLatencyScanner

	// Backlog limit in waiting goroutines per GOMAXPROCS. The limit is enabled if the value is > 0.
	backlogLimit float64
	// Number of processors used to normalize the backlog.
	gomaxprocs int
	// Last cumulative scheduler wait time counter, in seconds.
	lastTotalWait float64
	// The time of the first update.
	firstUpdate time.Time
	// The time of the last update.
	lastUpdate       time.Time
	backlogMovingAvg *math.EwmaRate
	limitingReason   atomic.String
	currBacklog      atomic.Float64
}

// NewSchedulerBacklogLimiter returns a SchedulerBacklogLimiter configured with backlogLimit.
func NewSchedulerBacklogLimiter(backlogLimit float64, logger log.Logger, reg prometheus.Registerer) *SchedulerBacklogLimiter {
	// Calculate alpha for a minute long window, same as the utilization based limiter.
	alpha := 2 / (resourceUtilizationSlidingWindow.Seconds()/resourceUtilizationUpdateInterval.Seconds() + 1)
	l := &SchedulerBacklogLimiter{
		logger:           logger,
		backlogLimit:     backlogLimit,
		gomaxprocs:       runtime.GOMAXPROCS(0),
		backlogMovingAvg: math.NewEWMARate(alpha, resourceUtilizationUpdateInterval),
	}
	l.Service = services.NewTimerService(resourceUtilizationUpdateInterval, l.starting, l.update, nil)

	if reg != nil {
		promauto.With(reg).NewGaugeFunc(prometheus.GaugeOpts{
			Name: "utilization_limiter_current_scheduler_backlog",
			Help: "Current average scheduler backlog (runnable goroutines waiting per GOMAXPROCS) calculated by the scheduler backlog limiter.",
		}, func() float64 {
			return l.currBacklog.Load()
		})
	}

	return l
}

// LimitingReason returns the current reason for limiting, if any.
// If an empty string is returned, limiting is disabled.
func (l *SchedulerBacklogLimiter) LimitingReason() string {
	return l.limitingReason.Load()
}

func (l *SchedulerBacklogLimiter) starting(_ context.Context) error {
	var err error
	l.scanner, err = newRuntimeSchedLatencyScanner()
	if err != nil {
		return fmt.Errorf("unable to read Go scheduler latency metrics. Please disable scheduler backlog based limiting: %w", err)
	}

	return nil
}

func (l *SchedulerBacklogLimiter) update(_ context.Context) error {
	l.compute(time.Now)
	return nil
}

// compute and return the current scheduler backlog per processor.
// This function must be called at a regular interval (resourceUtilizationUpdateInterval) to get a predictable behaviour.
func (l *SchedulerBacklogLimiter) compute(nowFn func() time.Time) (currBacklog float64) {
	totalWait, err := l.scanner.Scan()
	if err != nil {
		level.Warn(l.logger).Log("msg", "failed to get scheduler latency stats", "err", err.Error())
		// Disable any limiting, since we can't tell the scheduler backlog
		l.limitingReason.Store("")
		return
	}

	// Get wall time after the scan, in case there's a delay before the value is
	// returned, which would cause us to compute too high of a backlog.
	now := nowFn()

	// Add the instant backlog to the moving average. It can only be computed
	// starting from the 2nd tick.
	if prevUpdate, prevTotalWait := l.lastUpdate, l.lastTotalWait; !prevUpdate.IsZero() {
		timeSincePrevUpdate := now.Sub(prevUpdate)

		// Skip updates elapsing less than half the expected interval, e.g. two
		// consecutive ticks on an overloaded process. See the equivalent guard in
		// UtilizationBasedLimiter.compute.
		if timeSincePrevUpdate > resourceUtilizationUpdateInterval/2 {
			// Little's law: wait-seconds accumulated per second equals the average
			// number of goroutines waiting at any instant. Normalize by GOMAXPROCS
			// to get a per-processor backlog comparable across pod sizes.
			instBacklog := (totalWait - prevTotalWait) / timeSincePrevUpdate.Seconds() / float64(l.gomaxprocs)
			l.backlogMovingAvg.Add(int64(instBacklog * 100))
			l.backlogMovingAvg.Tick()

			l.lastUpdate = now
			l.lastTotalWait = totalWait
		}
	} else {
		// First time we read the scheduler latency.
		l.lastUpdate = now
		l.lastTotalWait = totalWait
	}

	// The moving average requires a warmup period before getting stable results,
	// during which the reported backlog will be 0. Same approach as the
	// utilization based limiter.
	if l.firstUpdate.IsZero() {
		l.firstUpdate = now
	} else if now.Sub(l.firstUpdate) >= resourceUtilizationSlidingWindow {
		currBacklog = l.backlogMovingAvg.Rate() / 100
		l.currBacklog.Store(currBacklog)
	}

	var reason string
	if l.backlogLimit > 0 && currBacklog >= l.backlogLimit {
		reason = "scheduler"
	}

	enable := reason != ""
	prevEnable := l.limitingReason.Load() != ""
	if enable == prevEnable {
		// No change
		return
	}

	if enable {
		level.Info(l.logger).Log("msg", "enabling scheduler backlog based limiting",
			"backlog_limit", formatCPU(l.backlogLimit), "scheduler_backlog", formatCPU(currBacklog))
	} else {
		level.Info(l.logger).Log("msg", "disabling scheduler backlog based limiting",
			"backlog_limit", formatCPU(l.backlogLimit), "scheduler_backlog", formatCPU(currBacklog))
	}

	l.limitingReason.Store(reason)
	return
}
