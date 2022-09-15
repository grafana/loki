package ingester

import (
	"sync"
	"time"

	"go.uber.org/atomic"
)

// Samples are recorded once every 1000 / bucketCount milliseconds.
const bucketCount = 100

// RateCalculator buckets rate measurements and returns a per-second rate of the recorded values
type RateCalculator struct {
	lock    sync.RWMutex
	buckets []int64
	idx     int
	sample  atomic.Int64
}

func NewRateCalculator() *RateCalculator {
	c := &RateCalculator{
		buckets: make([]int64, bucketCount),
	}

	go c.recordSample()

	return c
}

func (c *RateCalculator) recordSample() {
	t := time.NewTicker(1000 / bucketCount * time.Millisecond)
	for range t.C {
		rate := c.sample.Swap(0)
		c.storeNewRate(rate)
	}
}

func (c *RateCalculator) storeNewRate(newRate int64) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.buckets[c.idx] = newRate
	c.idx = (c.idx + 1) % bucketCount
}

func (c *RateCalculator) Record(sample int64) {
	c.sample.Add(sample)
}

func (c *RateCalculator) Rate() int64 {
	c.lock.RLock()
	defer c.lock.RUnlock()

	var sum int64
	for _, n := range c.buckets {
		sum += n
	}
	return sum
}
