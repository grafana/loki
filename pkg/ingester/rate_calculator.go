package ingester

import (
	"time"

	"go.uber.org/atomic"
)

// Samples are recorded once every 1000 / bucketCount milliseconds.
const bucketCount = 100

// RateCalculator buckets rate measurements and returns a per-second rate of the recorded values
type RateCalculator struct {
	buckets []int64
	idx     int
	sample  atomic.Int64
	rate    atomic.Int64

	stopChan chan struct{}
}

func NewRateCalculator() *RateCalculator {
	c := &RateCalculator{
		buckets:  make([]int64, bucketCount),
		stopChan: make(chan struct{}),
	}

	go c.recordSample()

	return c
}

func (c *RateCalculator) recordSample() {
	t := time.NewTicker(1000 / bucketCount * time.Millisecond)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			sample := c.sample.Swap(0)
			c.storeNewRate(sample)
		case <-c.stopChan:
			return
		}
	}
}

func (c *RateCalculator) storeNewRate(sample int64) {
	c.buckets[c.idx] = sample
	c.idx = (c.idx + 1) % bucketCount

	var sum int64
	for _, n := range c.buckets {
		sum += n
	}

	c.rate.Store(sum)
}

func (c *RateCalculator) Record(sample int64) {
	c.sample.Add(sample)
}

func (c *RateCalculator) Rate() int64 {
	return c.rate.Load()
}

func (c *RateCalculator) Stop() {
	close(c.stopChan)
}
