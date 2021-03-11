package ingester

import (
	"sync"
	"time"

	"go.uber.org/atomic"
)

// ewmaRate tracks an exponentially weighted moving average of a per-second rate.
type ewmaRate struct {
	newEvents atomic.Int64
	alpha     float64
	interval  time.Duration
	lastRate  float64
	init      bool
	mutex     sync.Mutex
}

func newEWMARate(alpha float64, interval time.Duration) *ewmaRate {
	return &ewmaRate{
		alpha:    alpha,
		interval: interval,
	}
}

// rate returns the per-second rate.
func (r *ewmaRate) rate() float64 {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.lastRate
}

// tick assumes to be called every r.interval.
func (r *ewmaRate) tick() {
	newEvents := r.newEvents.Load()
	r.newEvents.Sub(newEvents)
	instantRate := float64(newEvents) / r.interval.Seconds()

	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.init {
		r.lastRate += r.alpha * (instantRate - r.lastRate)
	} else {
		r.init = true
		r.lastRate = instantRate
	}
}

// inc counts one event.
func (r *ewmaRate) inc() {
	r.newEvents.Inc()
}

func (r *ewmaRate) add(delta int64) {
	r.newEvents.Add(delta)
}
