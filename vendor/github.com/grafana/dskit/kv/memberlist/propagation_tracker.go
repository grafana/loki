package memberlist

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/atomic"

	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/kv/codec"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/timeutil"
)

const (
	// propagationDelayTrackerKey is the KV store key used for storing propagation delay tracker state.
	propagationDelayTrackerKey = "memberlist-propagation-delay-tracker"
)

// PropagationDelayTrackerConfig configures the propagation delay tracker.
type PropagationDelayTrackerConfig struct {
	Enabled                     bool          `yaml:"enabled" category:"experimental"`
	BeaconInterval              time.Duration `yaml:"beacon_interval" category:"experimental"`
	BeaconLifetime              time.Duration `yaml:"beacon_lifetime" category:"experimental"`
	LogBeaconsLatencyLongerThan time.Duration `yaml:"log_beacons_latency_longer_than" category:"experimental"`
}

// RegisterFlags registers flags with default names.
func (cfg *PropagationDelayTrackerConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix(f, "")
}

// RegisterFlagsWithPrefix registers flags with the given prefix.
func (cfg *PropagationDelayTrackerConfig) RegisterFlagsWithPrefix(f *flag.FlagSet, prefix string) {
	f.BoolVar(&cfg.Enabled, prefix+"enabled", false, "Enable the propagation delay tracker to measure gossip propagation delay.")
	f.DurationVar(&cfg.BeaconInterval, prefix+"beacon-interval", 1*time.Minute, "How often to publish beacons for propagation tracking.")
	f.DurationVar(&cfg.BeaconLifetime, prefix+"beacon-lifetime", 10*time.Minute, "How long a beacon lives before being garbage collected.")
	f.DurationVar(&cfg.LogBeaconsLatencyLongerThan, prefix+"log-beacons-latency-longer-than", 0, "Log warning when beacon propagation delay exceeds this threshold. 0 disables logging.")
}

// PropagationDelayTracker is a service that tracks gossip propagation delay across
// the memberlist cluster by periodically publishing beacons and measuring
// how long it takes for beacons to propagate.
type PropagationDelayTracker struct {
	services.Service

	cfg      PropagationDelayTrackerConfig
	nodeName string
	kv       *KV
	codec    codec.Codec
	logger   log.Logger

	// seenBeacons tracks beacon IDs that have already been processed to avoid
	// measuring the same beacon twice.
	seenBeaconsMu sync.RWMutex
	seenBeacons   map[uint64]struct{}

	// pendingBeaconID is the beacon ID currently being published. This prevents a race
	// where cleanupSeenBeacons could remove a beacon ID from seenBeacons before the
	// CAS operation that publishes it completes.
	pendingBeaconID atomic.Uint64

	// startupTime is the timestamp (milliseconds since epoch) when the tracker started.
	// Beacons published before this time are skipped to avoid measuring pre-existing beacons.
	startupTime int64

	// Metrics
	propagationDelay      prometheus.Histogram
	beaconsPublishedTotal prometheus.Counter
	beaconsReceivedTotal  prometheus.Counter
}

// NewPropagationDelayTracker creates a new PropagationDelayTracker service.
func NewPropagationDelayTracker(
	kv *KV,
	cfg PropagationDelayTrackerConfig,
	nodeName string,
	logger log.Logger,
	registerer prometheus.Registerer,
) *PropagationDelayTracker {
	t := &PropagationDelayTracker{
		cfg:         cfg,
		nodeName:    nodeName,
		kv:          kv,
		codec:       GetPropagationDelayTrackerCodec(),
		logger:      log.With(logger, "component", "memberlist-propagation-delay-tracker"),
		seenBeacons: make(map[uint64]struct{}),
		propagationDelay: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Name:                            "memberlist_propagation_tracker_delay_seconds",
			Help:                            "Time from beacon publish to receive.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 1 * time.Hour,
			Buckets:                         prometheus.DefBuckets,
		}),
		beaconsPublishedTotal: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Name: "memberlist_propagation_tracker_beacons_published_total",
			Help: "Total number of beacons published by this node.",
		}),
		beaconsReceivedTotal: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Name: "memberlist_propagation_tracker_beacons_received_total",
			Help: "Total number of unique beacons received.",
		}),
	}

	t.Service = services.NewBasicService(nil, t.running, nil).WithName("propagation-delay-tracker")

	return t
}

func (t *PropagationDelayTracker) running(ctx context.Context) error {
	t.startupTime = time.Now().UnixMilli()
	level.Info(t.logger).Log("msg", "propagation delay tracker started", "beacon_interval", t.cfg.BeaconInterval, "beacon_lifetime", t.cfg.BeaconLifetime)

	// Start the goroutine to track beacon arrivals in real-time
	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()

	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		t.watchBeacons(watchCtx)
	}()
	defer func() {
		select {
		case <-watchDone:
		case <-time.After(10 * time.Second):
			level.Warn(t.logger).Log("msg", "timed out waiting for watch goroutine to finish")
		}
	}()

	// Start the beacon publish ticker. The first tick uses a random delay between 1ns and
	// beaconInterval to distribute beacon publishing over time when multiple processes
	// start simultaneously (e.g., during a rollout).
	initialDelay := 1 + time.Duration(rand.Int63n(int64(t.cfg.BeaconInterval)))
	stopTicker, tickerChan := timeutil.NewVariableTicker(initialDelay, t.cfg.BeaconInterval)
	defer stopTicker()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tickerChan:
			t.onBeaconInterval(ctx)
		}
	}
}

// watchBeacons uses WatchKey to receive beacon updates in real-time and measure delay.
// It loops with backoff until the context is canceled.
func (t *PropagationDelayTracker) watchBeacons(ctx context.Context) {
	boff := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 5 * time.Second,
		MaxRetries: 0, // Retry indefinitely until context is canceled.
	})

	for boff.Ongoing() {
		t.kv.WatchKey(ctx, propagationDelayTrackerKey, t.codec, func(val interface{}) bool {
			if val == nil {
				return true
			}

			desc, ok := val.(*PropagationDelayTrackerDesc)
			if !ok {
				level.Warn(t.logger).Log("msg", "unexpected value type in watch callback", "type", fmt.Sprintf("%T", val))
				return true
			}

			t.onBeaconsReceived(desc)
			return true
		})

		// WatchKey exited, wait before retrying.
		boff.Wait()
	}
}

// onBeaconsReceived processes received beacons and records delay for unseen beacons.
func (t *PropagationDelayTracker) onBeaconsReceived(desc *PropagationDelayTrackerDesc) {
	now := time.Now()
	receivedCount := 0

	for beaconID, beacon := range desc.Beacons {
		// Skip deleted beacons (tombstones)
		if beacon.DeletedAt != 0 {
			continue
		}

		if t.alreadySeen(beaconID) {
			continue
		}
		t.markAsSeen(beaconID)

		// Skip delay recording for beacons published before the tracker started.
		if beacon.PublishedAt < t.startupTime {
			continue
		}

		receivedCount++
		delay := now.Sub(beacon.GetPublishedAtTime())
		if delay >= 0 {
			t.propagationDelay.Observe(delay.Seconds())

			if t.cfg.LogBeaconsLatencyLongerThan > 0 && delay >= t.cfg.LogBeaconsLatencyLongerThan {
				level.Warn(t.logger).Log(
					"msg", "high latency beacon detected",
					"beacon_id", beaconID,
					"published_by", beacon.PublishedBy,
					"published_at", beacon.GetPublishedAtTime().Format(time.RFC3339Nano),
					"received_at", now.Format(time.RFC3339Nano),
					"delay", delay,
				)
			}
		}
	}

	if receivedCount > 0 {
		t.beaconsReceivedTotal.Add(float64(receivedCount))
	}

	// Clean up beacons that are no longer in the KV store (or are deleted tombstones).
	t.cleanupSeenBeacons(desc)
}

func (t *PropagationDelayTracker) alreadySeen(beaconID uint64) bool {
	t.seenBeaconsMu.RLock()
	defer t.seenBeaconsMu.RUnlock()
	_, seen := t.seenBeacons[beaconID]
	return seen
}

func (t *PropagationDelayTracker) markAsSeen(beaconID uint64) {
	t.seenBeaconsMu.Lock()
	defer t.seenBeaconsMu.Unlock()
	t.seenBeacons[beaconID] = struct{}{}
}

// cleanupSeenBeacons removes entries from seenBeacons that are no longer active
// in the KV store. Tombstone handling is done by the memberlist client,
// so we just need to keep seenBeacons in sync with active beacons.
func (t *PropagationDelayTracker) cleanupSeenBeacons(desc *PropagationDelayTrackerDesc) {
	t.seenBeaconsMu.Lock()
	defer t.seenBeaconsMu.Unlock()

	// Get the pending beacon ID to avoid cleaning it up during publish.
	pendingID := t.pendingBeaconID.Load()

	for beaconID := range t.seenBeacons {
		// Skip the pending beacon ID - it may not be in the KV store yet.
		if beaconID == pendingID {
			continue
		}

		beacon, exists := desc.Beacons[beaconID]
		// Remove from seenBeacons if not in KV store or if it's a tombstone
		if !exists || beacon.DeletedAt != 0 {
			delete(t.seenBeacons, beaconID)
		}
	}
}

// onBeaconInterval is called on each beacon interval tick to:
// 1. Mark beacons older than beaconLifetime as tombstones for garbage collection
// 2. Publish a new beacon if no recent beacon exists
func (t *PropagationDelayTracker) onBeaconInterval(ctx context.Context) {
	val, err := t.kv.Get(propagationDelayTrackerKey, t.codec)
	if err != nil {
		level.Warn(t.logger).Log("msg", "failed to get beacons", "err", err)
		return
	}

	desc := GetOrCreatePropagationDelayTrackerDesc(val)
	now := time.Now()

	// Collect old beacons that should be marked as tombstones
	var beaconsToDelete []uint64
	hasRecentBeacon := false

	for beaconID, beacon := range desc.Beacons {
		if beacon.DeletedAt != 0 {
			continue
		}
		age := now.Sub(beacon.GetPublishedAtTime())
		if age >= t.cfg.BeaconLifetime {
			beaconsToDelete = append(beaconsToDelete, beaconID)
		} else if age < t.cfg.BeaconInterval {
			hasRecentBeacon = true
		}
	}

	// Mark old beacons as tombstones
	if len(beaconsToDelete) > 0 {
		t.deleteBeacons(ctx, beaconsToDelete)
	}

	// Publish new beacon if no recent one exists
	if !hasRecentBeacon {
		t.publishBeacon(ctx)
	}
}

func (t *PropagationDelayTracker) publishBeacon(ctx context.Context) {
	// Generate a non-zero beacon ID. We need beaconID > 0 because 0 is used as
	// the "no pending beacon" sentinel value in pendingBeaconID.
	beaconID := rand.Uint64()
	if beaconID == 0 {
		beaconID = 1
	}
	now := time.Now()

	// Set pendingBeaconID before marking as seen to prevent cleanupSeenBeacons
	// from removing it during the CAS operation window.
	t.pendingBeaconID.Store(beaconID)
	defer t.pendingBeaconID.Store(0)

	// Mark as seen before publishing to avoid measuring our own beacon.
	t.markAsSeen(beaconID)

	err := t.kv.CAS(ctx, propagationDelayTrackerKey, t.codec, func(in interface{}) (out interface{}, retry bool, err error) {
		desc := GetOrCreatePropagationDelayTrackerDesc(in)

		// Skip publishing if we already have a beacon with the same ID (extremely rare beacon ID collision).
		// In case of a collision, the beacon ID is tracked as seen anyway, but we don't care given it's a rare case.
		if _, exists := desc.Beacons[beaconID]; exists {
			return nil, false, nil
		}

		desc.Beacons[beaconID] = BeaconDesc{
			PublishedAt: now.UnixMilli(),
			PublishedBy: t.nodeName,
		}

		return desc, true, nil
	})

	if err != nil {
		level.Warn(t.logger).Log("msg", "failed to publish beacon", "err", err)
		return
	}

	t.beaconsPublishedTotal.Inc()
}

// deleteBeacons marks the specified beacons as tombstones by setting their DeletedAt timestamp.
func (t *PropagationDelayTracker) deleteBeacons(ctx context.Context, beaconIDs []uint64) {
	err := t.kv.CAS(ctx, propagationDelayTrackerKey, t.codec, func(in interface{}) (out interface{}, retry bool, err error) {
		desc := GetOrCreatePropagationDelayTrackerDesc(in)
		now := time.Now()

		changed := false
		for _, beaconID := range beaconIDs {
			beacon, exists := desc.Beacons[beaconID]
			if exists && beacon.DeletedAt == 0 {
				beacon.DeletedAt = now.UnixMilli()
				desc.Beacons[beaconID] = beacon
				changed = true
			}
		}

		if !changed {
			return nil, false, nil
		}
		return desc, true, nil
	})

	if err != nil {
		level.Warn(t.logger).Log("msg", "failed to mark beacons as deleted", "err", err)
	}
}
