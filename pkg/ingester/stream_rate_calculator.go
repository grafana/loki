package ingester

import (
	"sync"
	"time"

	"github.com/grafana/loki/pkg/logproto"
)

const (
	// defaultStripeSize is the default number of entries to allocate in the
	// stripeSeries list.
	defaultStripeSize = 1 << 10

	// The intent is for a per-second rate so this is hard coded
	updateInterval = time.Second

	// The factor used to weight the moving average. Must be in the range [0, 1.0].
	// A larger factor weights recent samples more heavily while a smaller
	// factor weights historic samples more heavily.
	smoothingFactor = .2

	// A threshold for when to reset the average without smoothing. Calculated
	// by currentValue >= (lastValue * burstThreshold). This allows us to set
	// the smoothing factor fairly small to preserve historic samples but still
	// be able to react to sudden bursts in load
	burstThreshold = 1.5
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

	rateLock sync.RWMutex
	allRates []logproto.StreamRate
}

func NewStreamRateCalculator() *StreamRateCalculator {
	calc := &StreamRateCalculator{
		size: defaultStripeSize,
		// Lookup pattern: tenant -> fingerprint -> rate
		samples:  make([]map[string]map[uint64]logproto.StreamRate, defaultStripeSize),
		locks:    make([]stripeLock, defaultStripeSize),
		stopchan: make(chan struct{}),
	}

	for i := 0; i < defaultStripeSize; i++ {
		calc.samples[i] = make(map[string]map[uint64]logproto.StreamRate)
	}

	go calc.updateLoop()

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
	rates := c.rates()
	samples := c.currentSamplesPerTenant()
	samples = updateSamples(rates, samples)

	updatedRates := make([]logproto.StreamRate, 0, c.size)
	for _, tenantRates := range samples {
		for _, streamRates := range tenantRates {
			updatedRates = append(updatedRates, streamRates)
		}
	}

	c.rateLock.Lock()
	defer c.rateLock.Unlock()

	c.allRates = updatedRates
}

func (c *StreamRateCalculator) rates() []logproto.StreamRate {
	c.rateLock.RLock()
	defer c.rateLock.RUnlock()

	return append([]logproto.StreamRate(nil), c.allRates...)
}

func (c *StreamRateCalculator) currentSamplesPerTenant() map[string]map[uint64]logproto.StreamRate {
	rates := make(map[string]map[uint64]logproto.StreamRate, c.size)

	for i := 0; i < c.size; i++ {
		c.locks[i].Lock()

		for tenantID, tenant := range c.samples[i] {
			existingRates := ratesForTenant(rates, tenantID)

			for streamHash, streamRate := range tenant {
				existingRates[streamHash] = logproto.StreamRate{
					Tenant:            streamRate.Tenant,
					StreamHash:        streamRate.StreamHash,
					StreamHashNoShard: streamRate.StreamHashNoShard,
					Rate:              streamRate.Rate,
				}
			}

			rates[tenantID] = existingRates
		}

		c.samples[i] = make(map[string]map[uint64]logproto.StreamRate)
		c.locks[i].Unlock()
	}

	return rates
}

func updateSamples(rates []logproto.StreamRate, samples map[string]map[uint64]logproto.StreamRate) map[string]map[uint64]logproto.StreamRate {
	for _, streamRate := range rates {
		tenantRates := ratesForTenant(samples, streamRate.Tenant)

		if rate, ok := tenantRates[streamRate.StreamHash]; ok {
			rate.Rate = weightedMovingAverage(rate.Rate, streamRate.Rate)
			tenantRates[streamRate.StreamHash] = rate
		} else {
			streamRate.Rate = weightedMovingAverage(0, streamRate.Rate)
			tenantRates[streamRate.StreamHash] = streamRate
		}

		samples[streamRate.Tenant] = tenantRates
	}

	return samples
}

func weightedMovingAverage(n, l int64) int64 {
	next, last := float64(n), float64(l)

	// If we see a sudden spike use the new value without smoothing
	if next >= (last * burstThreshold) {
		return n
	}

	// https://en.wikipedia.org/wiki/Moving_average#Exponential_moving_average
	return int64((smoothingFactor * next) + ((1 - smoothingFactor) * last))
}

func (c *StreamRateCalculator) Rates() []logproto.StreamRate {
	c.rateLock.RLock()
	defer c.rateLock.RUnlock()

	return c.allRates
}

func (c *StreamRateCalculator) Record(tenant string, streamHash, streamHashNoShard uint64, bytes int) {
	i := streamHash & uint64(c.size-1)

	c.locks[i].Lock()
	defer c.locks[i].Unlock()

	tenantMap := c.tenantMapFromSamples(i, tenant)
	streamRate := tenantMap[streamHash]
	streamRate.StreamHash = streamHash
	streamRate.StreamHashNoShard = streamHashNoShard
	streamRate.Tenant = tenant
	streamRate.Rate += int64(bytes)
	tenantMap[streamHash] = streamRate

	c.samples[i][tenant] = tenantMap
}

func (c *StreamRateCalculator) Remove(tenant string, streamHash uint64) {
	i := streamHash & uint64(c.size-1)

	c.locks[i].Lock()
	tenantMap := c.tenantMapFromSamples(i, tenant)
	delete(tenantMap, streamHash)
	c.samples[i][tenant] = tenantMap
	c.locks[i].Unlock()

	c.rateLock.Lock()
	defer c.rateLock.Unlock()

	for i, rate := range c.allRates {
		if rate.Tenant == tenant && rate.StreamHash == streamHash {
			c.allRates = append(c.allRates[:i], c.allRates[i+1:]...)
			break
		}
	}
}

func (c *StreamRateCalculator) tenantMapFromSamples(idx uint64, tenant string) map[uint64]logproto.StreamRate {
	if t, ok := c.samples[idx][tenant]; ok {
		return t
	}
	return make(map[uint64]logproto.StreamRate)
}

func ratesForTenant(rates map[string]map[uint64]logproto.StreamRate, tenant string) map[uint64]logproto.StreamRate {
	if t, ok := rates[tenant]; ok {
		return t
	}
	return make(map[uint64]logproto.StreamRate)
}

func (c *StreamRateCalculator) Stop() {
	close(c.stopchan)
}
