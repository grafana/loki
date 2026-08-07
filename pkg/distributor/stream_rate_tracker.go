package distributor

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

const (
	// ScalingModeNone reports the locally observed rate as-is.
	ScalingModeNone = "none"

	// RateSourceRateStore derives shard counts from the [rateStore], which
	// polls the ingesters.
	RateSourceRateStore = "ratestore"

	// RateSourceLocal derives shard counts from the [streamRateTracker], which
	// measures the traffic this distributor admits.
	RateSourceLocal = "local"

	// ScalingModeHealthyDistributors extrapolates the locally observed rate to
	// a fleet-wide estimate by multiplying it with the number of healthy
	// distributors. It assumes pushes for a stream are spread evenly across
	// distributors, which is the same assumption the global ingestion rate
	// strategy already makes when it divides a global limit by the same count
	// (see [globalStrategy]).
	ScalingModeHealthyDistributors = "healthy-distributors"
)

// StreamRateTrackerConfig configures the distributor-local stream rate tracker.
type StreamRateTrackerConfig struct {
	UpdateInterval  time.Duration `yaml:"update_interval"`
	KeepAlive       time.Duration `yaml:"keep_alive"`
	SmoothingFactor float64       `yaml:"smoothing_factor"`
	ScalingMode     string        `yaml:"scaling_mode"`
}

func (cfg *StreamRateTrackerConfig) RegisterFlagsWithPrefix(prefix string, fs *flag.FlagSet) {
	fs.DurationVar(&cfg.UpdateInterval, prefix+".update-interval", time.Second, "The interval on which locally observed stream sizes are folded into the smoothed per-stream rates.")
	fs.DurationVar(&cfg.KeepAlive, prefix+".keep-alive", 10*time.Minute, "How long a stream is kept after the last push for it was observed.")
	fs.Float64Var(&cfg.SmoothingFactor, prefix+".smoothing-factor", smoothingFactor, "The factor used to weight the exponential moving average of stream rates. Must be in the range (0, 1]. A larger factor weights recent samples more heavily.")
	fs.StringVar(&cfg.ScalingMode, prefix+".scaling-mode", ScalingModeHealthyDistributors, fmt.Sprintf("How the locally observed rate is extrapolated to a fleet-wide rate. Supported values are %q and %q.", ScalingModeNone, ScalingModeHealthyDistributors))
}

func (cfg *StreamRateTrackerConfig) Validate() error {
	if cfg.UpdateInterval <= 0 {
		return errors.New("stream rate tracker update interval must be greater than 0")
	}
	if cfg.KeepAlive < cfg.UpdateInterval {
		return errors.New("stream rate tracker keep alive must not be shorter than the update interval")
	}
	if cfg.SmoothingFactor <= 0 || cfg.SmoothingFactor > 1 {
		return errors.New("stream rate tracker smoothing factor must be in the range (0, 1]")
	}
	switch cfg.ScalingMode {
	case ScalingModeNone, ScalingModeHealthyDistributors:
	default:
		return fmt.Errorf("unsupported stream rate tracker scaling mode: %q", cfg.ScalingMode)
	}
	return nil
}

// rateTrackerEntry is the per-stream state of the [streamRateTracker].
//
// bytes and pushes accumulate the observations of the current interval and are
// reset on every fold. rate and pushRate hold the smoothed values across
// intervals and are what [streamRateTracker.RateFor] reports. All fields are
// guarded by the stripe lock the entry lives under.
type rateTrackerEntry struct {
	bytes  uint64
	pushes uint64

	rate     float64
	pushRate float64

	// idle accumulates the time during which no push was observed. It is reset
	// on every observation and drives eviction, which avoids having to read the
	// clock on the hot path.
	idle time.Duration
}

// streamRateTracker tracks the byte rate of every stream this distributor
// observes, keyed by the unsharded stream hash.
//
// Unlike the [rateStore] it replaces, the rates are measured at admission time
// rather than polled from the ingesters, so they neither lag behind Kafka
// consumers nor require O(distributors x ingesters) connections. The trade-off
// is that a distributor only observes its own share of the traffic, so the
// result has to be extrapolated to the fleet, see [ScalingModeHealthyDistributors].
//
// It implements the [RateStore] interface and can therefore be used
// interchangeably with the [rateStore].
type streamRateTracker struct {
	services.Service

	cfg StreamRateTrackerConfig

	// instanceCount reports the number of healthy distributors. It is only
	// consulted for [ScalingModeHealthyDistributors] and may be nil otherwise.
	instanceCount ReadLifecycler

	// Lookup pattern: stripe -> tenant -> unsharded stream hash -> entry.
	size    int
	stripes []map[string]map[uint64]*rateTrackerEntry
	locks   []stripeLock

	// lastFold is only accessed from the goroutine running the fold, either the
	// timer service or a test, and therefore needs no synchronization.
	lastFold time.Time

	metrics *streamRateTrackerMetrics
}

func newStreamRateTracker(cfg StreamRateTrackerConfig, instanceCount ReadLifecycler, reg prometheus.Registerer) *streamRateTracker {
	t := &streamRateTracker{
		cfg:           cfg,
		instanceCount: instanceCount,
		size:          defaultStripeSize,
		stripes:       make([]map[string]map[uint64]*rateTrackerEntry, defaultStripeSize),
		locks:         make([]stripeLock, defaultStripeSize),
		metrics:       newStreamRateTrackerMetrics(reg),
	}
	for i := range t.stripes {
		t.stripes[i] = make(map[string]map[uint64]*rateTrackerEntry)
	}
	t.Service = services.
		NewTimerService(cfg.UpdateInterval, t.starting, t.iteration, nil).
		WithName("stream rate tracker")
	return t
}

func (t *streamRateTracker) starting(context.Context) error {
	t.lastFold = time.Now()
	return nil
}

func (t *streamRateTracker) iteration(context.Context) error {
	t.fold(time.Now())
	// Never fail the service: a tracker that stops folding degrades shard
	// counts, it must not take down the distributor.
	return nil
}

// Observe records that a push of size bytes was received for the stream
// identified by streamHash, which must be the unsharded stream hash.
//
// This runs on the hot write path, once per stream per push.
func (t *streamRateTracker) Observe(tenant string, streamHash uint64, size int) {
	i := streamHash & uint64(t.size-1)

	t.locks[i].Lock()
	defer t.locks[i].Unlock()

	streams, ok := t.stripes[i][tenant]
	if !ok {
		streams = make(map[uint64]*rateTrackerEntry)
		t.stripes[i][tenant] = streams
	}
	entry, ok := streams[streamHash]
	if !ok {
		entry = &rateTrackerEntry{}
		streams[streamHash] = entry
	}
	if size > 0 {
		entry.bytes += uint64(size)
	}
	entry.pushes++
	entry.idle = 0
}

// RateFor implements the [RateStore] interface. It returns the estimated
// fleet-wide rate in bytes per second and the estimated fleet-wide number of
// pushes per second for the stream. Both are zero for a stream that has not
// been observed, or that was observed for the first time in the current
// interval.
func (t *streamRateTracker) RateFor(tenant string, streamHash uint64) (int64, float64) {
	i := streamHash & uint64(t.size-1)

	t.locks[i].RLock()
	entry, ok := t.stripes[i][tenant][streamHash]
	var rate, pushRate float64
	if ok {
		rate, pushRate = entry.rate, entry.pushRate
	}
	t.locks[i].RUnlock()

	if !ok {
		return 0, 0
	}
	factor := t.scalingFactor()
	return int64(rate * factor), pushRate * factor
}

// scalingFactor returns the multiplier that turns a locally observed rate into
// a fleet-wide estimate.
func (t *streamRateTracker) scalingFactor() float64 {
	if t.cfg.ScalingMode != ScalingModeHealthyDistributors || t.instanceCount == nil {
		return 1
	}
	// A count of zero means the ring has not been read yet. Scaling by the
	// local view is the safer error: it under-shards rather than exploding the
	// stream count.
	if n := t.instanceCount.HealthyInstancesCount(); n > 1 {
		return float64(n)
	}
	return 1
}

// fold turns the observations accumulated since the previous fold into a
// per-second sample, folds it into the smoothed rates, and evicts streams that
// have been idle for longer than the configured keep alive.
//
// It must not be called concurrently with itself.
func (t *streamRateTracker) fold(now time.Time) {
	elapsed := now.Sub(t.lastFold)
	if elapsed <= 0 {
		// A non-monotonic or duplicate fold would divide by zero or inflate the
		// sample. Skip it and keep the observations for the next one.
		return
	}
	t.lastFold = now

	var (
		start   = time.Now()
		seconds = elapsed.Seconds()
		streams int
		expired int
		maxRate float64
	)
	for i := range t.stripes {
		t.locks[i].Lock()
		for tenant, tenantStreams := range t.stripes[i] {
			for hash, entry := range tenantStreams {
				if entry.pushes == 0 {
					entry.idle += elapsed
					if entry.idle > t.cfg.KeepAlive {
						delete(tenantStreams, hash)
						expired++
						continue
					}
				}
				entry.rate = smoothRate(float64(entry.bytes)/seconds, entry.rate, t.cfg.SmoothingFactor)
				entry.pushRate = smoothRate(float64(entry.pushes)/seconds, entry.pushRate, t.cfg.SmoothingFactor)
				entry.bytes = 0
				entry.pushes = 0

				streams++
				if entry.rate > maxRate {
					maxRate = entry.rate
				}
				t.metrics.streamRate.Observe(entry.rate)
			}
			if len(tenantStreams) == 0 {
				delete(t.stripes[i], tenant)
			}
		}
		t.locks[i].Unlock()
	}

	factor := t.scalingFactor()
	t.metrics.streams.Set(float64(streams))
	t.metrics.expiredStreams.Add(float64(expired))
	t.metrics.maxStreamRate.Set(maxRate)
	t.metrics.scalingFactor.Set(factor)
	t.metrics.foldDuration.Observe(time.Since(start).Seconds())
}

// smoothRate folds next into the exponential moving average last.
//
// This is the same computation as [weightedMovingAverageF], but with a
// configurable factor instead of the [smoothingFactor] constant the [rateStore]
// is hard-coded to.
//
// https://en.wikipedia.org/wiki/Moving_average#Exponential_moving_average
func smoothRate(next, last, factor float64) float64 {
	return (factor * next) + ((1 - factor) * last)
}

// shardCountComparison reports how the shard counts derived from the
// distributor-local [streamRateTracker] differ from those derived from the
// [rateStore].
//
// It exists so that the local estimates can be validated against the system
// they replace while it is still running, and is removed together with the
// rateStore.
type shardCountComparison struct {
	// The counter children are resolved once, this is on the hot write path.
	equal       prometheus.Counter
	localHigher prometheus.Counter
	localLower  prometheus.Counter

	rateRatio prometheus.Histogram
}

func newShardCountComparison(reg prometheus.Registerer) *shardCountComparison {
	shardCounts := promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "distributor_shard_count_comparison_total",
		Help:      "How the shard count derived from the locally tracked stream rate compares to the one derived from the rate store.",
	}, []string{"result"})
	return &shardCountComparison{
		equal:       shardCounts.WithLabelValues("equal"),
		localHigher: shardCounts.WithLabelValues("local_higher"),
		localLower:  shardCounts.WithLabelValues("local_lower"),
		rateRatio: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "distributor_stream_rate_ratio",
			Help:      "The ratio of the locally tracked stream rate to the rate reported by the rate store. Only observed for streams the rate store knows a non-zero rate for.",
			Buckets:   prometheus.ExponentialBuckets(0.0625, 2, 9), // 0.0625 .. 16, centered on 1
		}),
	}
}

func (c *shardCountComparison) observe(rate, localRate int64, shards, localShards int) {
	switch {
	case localShards == shards:
		c.equal.Inc()
	case localShards > shards:
		c.localHigher.Inc()
	default:
		c.localLower.Inc()
	}
	// A zero rate means the rate store has nothing to compare against, either
	// because the stream is new or because the ingesters have not reported it
	// yet. Those would all collapse into the same bucket and say nothing about
	// the agreement of the two sources.
	if rate > 0 {
		c.rateRatio.Observe(float64(localRate) / float64(rate))
	}
}

type streamRateTrackerMetrics struct {
	streams        prometheus.Gauge
	expiredStreams prometheus.Counter
	maxStreamRate  prometheus.Gauge
	streamRate     prometheus.Histogram
	scalingFactor  prometheus.Gauge
	foldDuration   prometheus.Histogram
}

func newStreamRateTrackerMetrics(reg prometheus.Registerer) *streamRateTrackerMetrics {
	return &streamRateTrackerMetrics{
		streams: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "distributor_stream_rate_tracker_streams",
			Help:      "The number of unique unsharded streams tracked by this distributor.",
		}),
		expiredStreams: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_stream_rate_tracker_expired_streams_total",
			Help:      "The total number of streams evicted after being idle for longer than the keep alive.",
		}),
		maxStreamRate: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "distributor_stream_rate_tracker_max_stream_rate_bytes",
			Help:      "The maximum locally observed rate of any stream, before fleet scaling is applied.",
		}),
		streamRate: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "distributor_stream_rate_tracker_stream_rate_bytes",
			Help:      "The distribution of locally observed stream rates, before fleet scaling is applied.",
			Buckets:   prometheus.ExponentialBuckets(20000, 2, 14), // biggest bucket is 20000*2^(14-1) = 163,840,000 (~163.84MB)
		}),
		scalingFactor: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "distributor_stream_rate_tracker_scaling_factor",
			Help:      "The multiplier currently applied to locally observed rates to estimate the fleet-wide rate.",
		}),
		foldDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "distributor_stream_rate_tracker_fold_duration_seconds",
			Help:      "Time spent folding observations into the smoothed per-stream rates.",
			Buckets:   prometheus.DefBuckets,
		}),
	}
}
