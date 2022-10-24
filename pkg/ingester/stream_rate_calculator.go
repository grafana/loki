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
	samples  []logproto.StreamRate
	rates    []logproto.StreamRate
	locks    []stripeLock
	stopchan chan struct{}

	rateLock sync.RWMutex
	allRates []logproto.StreamRate
}

func NewStreamRateCalculator() *StreamRateCalculator {
	calc := &StreamRateCalculator{
		size:     defaultStripeSize,
		samples:  make([]logproto.StreamRate, defaultStripeSize),
		rates:    make([]logproto.StreamRate, defaultStripeSize),
		locks:    make([]stripeLock, defaultStripeSize),
		stopchan: make(chan struct{}),
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
		c.samples[i] = logproto.StreamRate{}

		sr := c.rates[i]
		if sr.StreamHash != 0 {
			rates = append(rates, logproto.StreamRate{
				StreamHash:        sr.StreamHash,
				StreamHashNoShard: sr.StreamHashNoShard,
				Rate:              sr.Rate,
			})
		}
		c.locks[i].Unlock()
	}

	c.rateLock.Lock()
	defer c.rateLock.Unlock()

	c.allRates = rates
}

func (c *StreamRateCalculator) Rates() []*logproto.StreamRate {
	c.rateLock.RLock()
	defer c.rateLock.RUnlock()

	rates := make([]*logproto.StreamRate, 0, len(c.allRates))
	for _, r := range c.allRates {
		rates = append(rates, &logproto.StreamRate{
			StreamHash:        r.StreamHash,
			StreamHashNoShard: r.StreamHashNoShard,
			Rate:              r.Rate,
		})
	}

	return rates
}

func (c *StreamRateCalculator) Record(streamHash, streamHashNoShard uint64, bytes int64) {
	i := streamHash & uint64(c.size-1)

	c.locks[i].Lock()
	defer c.locks[i].Unlock()

	streamRate := c.samples[i]
	streamRate.StreamHash = streamHash
	streamRate.StreamHashNoShard = streamHashNoShard
	streamRate.Rate += bytes
	c.samples[i] = streamRate
}

func (c *StreamRateCalculator) Stop() {
	close(c.stopchan)
}
