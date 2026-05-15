package distributor

import (
	"fmt"
	"math/rand/v2"
	"sync"
	"time"
)

const (
	circuitBreakerClosed   = "closed"
	circuitBreakerOpen     = "open"
	circuitBreakerHalfOpen = "half-open"
)

// An interface for circuit breakers.
type circuitBreaker interface {
	// An ideal interface would be `Call(func() error) error`, but this
	// requires rewriting pushHandler in http.go. We compromise instead
	// with a "manual" circuit breaker where the caller must call IsPermitted()
	// and Open() themselves instead.
	IsPermitted() bool
	Open()
}

// A linearRampCircuitBreaker is a circuit breaker which, in half-open state,
// accepts more requests for each second that passes in half-opened until
// the half-open period is over.
type linearRampCircuitBreaker struct {
	state                      string
	openPeriod, halfOpenPeriod time.Duration
	lastOpened                 time.Time
	randf64                    func() float64
	mtx                        sync.Mutex
}

// newLinearRampCircuitBreaker returns a new circuit breaker which remains
// open until the end of the open window, and half-open until the end of the
// half-open window.
func newLinearRampCircuitBreaker(openPeriod, halfOpenPeriod time.Duration) *linearRampCircuitBreaker {
	return &linearRampCircuitBreaker{
		state:          circuitBreakerClosed,
		openPeriod:     openPeriod,
		halfOpenPeriod: halfOpenPeriod,
		randf64:        rand.Float64,
	}
}

// IsPermitted returns true if the call is permitted, otherwise false.
func (b *linearRampCircuitBreaker) IsPermitted() bool {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	switch b.state {
	case circuitBreakerClosed:
		return true
	case circuitBreakerOpen:
		return b.handleOpenState()
	case circuitBreakerHalfOpen:
		return b.handleHalfOpenState()
	default:
		// defer will ensure that mtx is unlocked even after a panic.
		panic(fmt.Sprintf("Unknown state: %s", b.state))
	}
}

func (b *linearRampCircuitBreaker) Open() {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	b.state = circuitBreakerOpen
	b.lastOpened = time.Now()
}

func (b *linearRampCircuitBreaker) handleOpenState() bool {
	if time.Since(b.lastOpened) > b.openPeriod {
		b.state = circuitBreakerHalfOpen
		return b.handleHalfOpenState()
	}
	return false
}

func (b *linearRampCircuitBreaker) handleHalfOpenState() bool {
	d := time.Since(b.lastOpened)
	if d > b.openPeriod+b.halfOpenPeriod {
		b.state = circuitBreakerClosed
		return true
	}
	return b.allowRandomHalfOpen(d - b.openPeriod)
}

func (b *linearRampCircuitBreaker) allowRandomHalfOpen(d time.Duration) bool {
	return b.randf64() < float64(d)/float64(b.halfOpenPeriod)
}
