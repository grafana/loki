package ingester

import (
	"time"

	"go.uber.org/atomic"
)

// RateCalculator keeps track of and returns the rate in the last second
type RateCalculator struct {
	sample atomic.Int64
	rate   atomic.Int64

	stopChan chan struct{}
}

func NewRateCalculator() *RateCalculator {
	c := &RateCalculator{
		stopChan: make(chan struct{}),
	}

	go c.recordSample()

	return c
}

func (c *RateCalculator) recordSample() {
	t := time.NewTicker(time.Second)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			sample := c.sample.Swap(0)
			c.rate.Store(sample)
		case <-c.stopChan:
			return
		}
	}
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
