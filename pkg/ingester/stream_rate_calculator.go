package ingester

import (
	"sync"
	"time"

	"github.com/grafana/loki/pkg/logproto"
)

const (
	// defaultStripeSize is the default number of entries to allocate in the
	// stripeSeries list.
	defaultStripeSize = 1 << 15

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
	samples  []map[string]logproto.StreamRate
	rates    []map[string]logproto.StreamRate
	locks    []stripeLock
	stopchan chan struct{}

	rateLock sync.RWMutex
	allRates []logproto.StreamRate
}

func NewStreamRateCalculator() *StreamRateCalculator {
	calc := &StreamRateCalculator{
		size:     defaultStripeSize,
		samples:  make([]map[string]logproto.StreamRate, defaultStripeSize),
		rates:    make([]map[string]logproto.StreamRate, defaultStripeSize),
		locks:    make([]stripeLock, defaultStripeSize),
		stopchan: make(chan struct{}),
	}

	for i := 0; i < defaultStripeSize; i++ {
		calc.rates[i] = make(map[string]logproto.StreamRate)
		calc.samples[i] = make(map[string]logproto.StreamRate)
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
	rates := make([]logproto.StreamRate, 0, c.size)

	for i := 0; i < c.size; i++ {
		c.locks[i].Lock()
		c.rates[i] = c.samples[i]
		c.samples[i] = make(map[string]logproto.StreamRate)

		sr := c.rates[i]
		for _, v := range sr {
			rates = append(rates, logproto.StreamRate{
				Tenant:            v.Tenant,
				StreamHash:        v.StreamHash,
				StreamHashNoShard: v.StreamHashNoShard,
				Rate:              v.Rate,
			})
		}
		c.locks[i].Unlock()
	}

	c.rateLock.Lock()
	defer c.rateLock.Unlock()

	c.allRates = rates
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

	streamRate := c.samples[i][tenant]
	streamRate.StreamHash = streamHash
	streamRate.StreamHashNoShard = streamHashNoShard
	streamRate.Tenant = tenant
	streamRate.Rate += int64(bytes)

	c.samples[i][tenant] = streamRate
}

func (c *StreamRateCalculator) Stop() {
	close(c.stopchan)
}
