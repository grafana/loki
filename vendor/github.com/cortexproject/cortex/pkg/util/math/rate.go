package math

import (
	"sync"
	"time"

	"go.uber.org/atomic"
)

// EwmaRate tracks an exponentially weighted moving average of a per-second rate.
type EwmaRate struct {
	newEvents atomic.Int64

	alpha    float64
	interval time.Duration

	mutex    sync.RWMutex
	lastRate float64
	init     bool
}

func NewEWMARate(alpha float64, interval time.Duration) *EwmaRate {
	return &EwmaRate{
		alpha:    alpha,
		interval: interval,
	}
}

// Rate returns the per-second rate.
func (r *EwmaRate) Rate() float64 {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.lastRate
}

// Tick assumes to be called every r.interval.
func (r *EwmaRate) Tick() {
	newEvents := r.newEvents.Swap(0)
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

// Inc counts one event.
func (r *EwmaRate) Inc() {
	r.newEvents.Inc()
}

func (r *EwmaRate) Add(delta int64) {
	r.newEvents.Add(delta)
}
