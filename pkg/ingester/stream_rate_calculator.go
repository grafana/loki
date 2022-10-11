package ingester

import (
	"sync"
	"time"
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
	samples  []int64
	rates    []int64
	locks    []stripeLock
	stopchan chan struct{}
}

func NewStreamRateCalculator() *StreamRateCalculator {
	calc := &StreamRateCalculator{
		size:     defaultStripeSize,
		samples:  make([]int64, defaultStripeSize),
		rates:    make([]int64, defaultStripeSize),
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
	for i := 0; i < c.size; i++ {
		c.locks[i].Lock()
		c.rates[i] = c.samples[i]
		c.samples[i] = 0
		c.locks[i].Unlock()
	}
}

func (c *StreamRateCalculator) RateFor(streamHash uint64) int64 {
	i := streamHash & uint64(c.size-1)

	c.locks[i].RLock()
	defer c.locks[i].RUnlock()

	return c.rates[i]
}

func (c *StreamRateCalculator) Record(streamHash uint64, bytes int64) {
	i := streamHash & uint64(c.size-1)

	c.locks[i].Lock()
	defer c.locks[i].Unlock()

	c.samples[i] += bytes
}

func (c *StreamRateCalculator) Stop() {
	close(c.stopchan)
}
