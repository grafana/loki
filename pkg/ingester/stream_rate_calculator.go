package ingester

import (
	"context"
	"sync"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
)

const (
	// defaultStripeSize is the default number of entries to allocate in the
	// stripeSeries list.
	defaultStripeSize = 1 << 10

	// The intent is for a per-second rate so this is hard coded
	updateInterval = time.Second
)

// stripeLock is taken from ruler/storage/wal/series.go
type stripeLock struct {
	sync.RWMutex
	// Padding to avoid multiple locks being on the same cache line.
	_ [40]byte
}

type StreamRateCalculator struct {
	size     int
	samples  []map[string]map[uint64]logproto.StreamRate
	locks    []stripeLock
	stopchan chan struct{}

	rateLock      sync.RWMutex
	allRates      []logproto.StreamRate
	kafka         *kafkaRateState
	kafkaSnapshot *logproto.KafkaRateSnapshot
}

// NewStreamRateCalculator starts a calculator for direct ingestion.
func NewStreamRateCalculator() *StreamRateCalculator {
	calc := newStreamRateCalculator(nil)
	go calc.updateLoop()
	return calc
}

// newStreamRateCalculator initializes all state before callers start the sampling loop.
func newStreamRateCalculator(state *kafkaRateState) *StreamRateCalculator {
	calc := &StreamRateCalculator{
		size: defaultStripeSize,
		// Lookup pattern: tenant -> fingerprint -> rate
		samples:  make([]map[string]map[uint64]logproto.StreamRate, defaultStripeSize),
		locks:    make([]stripeLock, defaultStripeSize),
		stopchan: make(chan struct{}),
		kafka:    state,
	}

	for i := 0; i < defaultStripeSize; i++ {
		calc.samples[i] = make(map[string]map[uint64]logproto.StreamRate)
	}

	if state != nil {
		calc.kafkaSnapshot = &logproto.KafkaRateSnapshot{PartitionID: state.partitionID, InstanceID: state.instanceID, SessionID: state.sessionID}
	}

	return calc
}

func (c *StreamRateCalculator) updateLoop() {
	t := time.NewTicker(updateInterval)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			c.updateRates()
		case <-c.stopchan:
			return
		}
	}
}

func (c *StreamRateCalculator) updateRates() {
	state := c.kafka
	var samples []map[string]map[uint64]logproto.StreamRate
	var snapshot *logproto.KafkaRateSnapshot
	if state != nil {
		samples = make([]map[string]map[uint64]logproto.StreamRate, c.size)
		for i := range samples {
			samples[i] = make(map[string]map[uint64]logproto.StreamRate)
		}
		// Lock all stripes before Kafka state so sampling cannot split a writer's rate and progress updates.
		for i := range c.locks {
			c.locks[i].Lock()
		}
		state.mu.Lock()
		// Swap maps so rates and Kafka progress share a boundary, then collect rates without blocking writers.
		samples, c.samples = c.samples, samples
		snapshot = state.finishSample()
		state.mu.Unlock()
		for i := range c.locks {
			c.locks[i].Unlock()
		}
	} else {
		samples = c.samples
	}

	rates := make([]logproto.StreamRate, 0, c.size)

	for i := 0; i < c.size; i++ {
		if state == nil {
			c.locks[i].Lock()
		}

		tenantRates := samples[i]
		for _, tenant := range tenantRates {
			for _, streamRate := range tenant {
				rates = append(rates, logproto.StreamRate{
					Tenant:            streamRate.Tenant,
					StreamHash:        streamRate.StreamHash,
					StreamHashNoShard: streamRate.StreamHashNoShard,
					Rate:              streamRate.Rate,
					Pushes:            streamRate.Pushes,
				})
			}
		}

		if state == nil {
			samples[i] = make(map[string]map[uint64]logproto.StreamRate)
			c.locks[i].Unlock()
		} else {
			// Release each detached stripe after collecting its rates.
			samples[i] = nil
		}
	}

	c.rateLock.Lock()
	defer c.rateLock.Unlock()

	c.allRates = rates
	if state != nil {
		c.kafkaSnapshot = snapshot
	}
}

func (c *StreamRateCalculator) Rates() []logproto.StreamRate {
	c.rateLock.RLock()
	defer c.rateLock.RUnlock()

	return c.allRates
}

// Record accounts for a push's bytes, deferring Kafka contributions until its attempt completes.
func (c *StreamRateCalculator) Record(ctx context.Context, tenant string, streamHash, streamHashNoShard uint64, bytes int) {
	if state := c.kafka; state != nil {
		if observation, ok := ctx.Value(rateObservationKey{}).(*rateObservation); ok {
			*observation = rateObservation{
				tenant: tenant, streamHash: streamHash, streamHashNoShard: streamHashNoShard, bytes: bytes, recorded: true,
			}
			return
		}
		i := streamHash & uint64(c.size-1)
		c.locks[i].Lock()
		c.record(tenant, streamHash, streamHashNoShard, bytes)
		state.mu.Lock()
		// Keep untracked bytes and their invalidation in the same sample.
		state.sampleInvalid = true
		state.mu.Unlock()
		c.locks[i].Unlock()
		return
	}
	c.record(tenant, streamHash, streamHashNoShard, bytes)
}

// record requires Kafka callers to hold the stream's stripe through the subsequent
// Kafka state update, keeping both in the same sample. Native writes acquire the lock here.
func (c *StreamRateCalculator) record(tenant string, streamHash, streamHashNoShard uint64, bytes int) {
	i := streamHash & uint64(c.size-1)

	if c.kafka == nil {
		c.locks[i].Lock()
		defer c.locks[i].Unlock()
	}

	tenantMap := c.getTenant(i, tenant)
	streamRate := tenantMap[streamHash]
	streamRate.StreamHash = streamHash
	streamRate.StreamHashNoShard = streamHashNoShard
	streamRate.Tenant = tenant
	streamRate.Rate += int64(bytes)
	streamRate.Pushes++
	tenantMap[streamHash] = streamRate

	c.samples[i][tenant] = tenantMap
}

func (c *StreamRateCalculator) getTenant(idx uint64, tenant string) map[uint64]logproto.StreamRate {
	if t, ok := c.samples[idx][tenant]; ok {
		return t
	}
	return make(map[uint64]logproto.StreamRate)
}

func (c *StreamRateCalculator) Stop() {
	close(c.stopchan)
}
