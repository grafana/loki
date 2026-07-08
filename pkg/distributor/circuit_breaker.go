package distributor

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	circuitBreakerClosed = iota
	circuitBreakerOpen
	circuitBreakerHalfOpen
)

var (
	circuitBreakerStateDesc = prometheus.NewDesc(
		"loki_distributor_circuit_breaker_state",
		"The state of the circuit breaker.",
		nil,
		nil,
	)
)

var (
	noopDoneFunc = func(_ error) {}
)

// An interface for circuit breakers.
type circuitBreaker interface {
	// Allow returns true if the request can proceed, otherwise false. It returns
	// a done callback that MUST be called when the request is finished.
	Allow() (bool, func(err error))
}

// A linearRampCircuitBreaker is a circuit breaker which, in half-open state,
// accepts more requests for each second that passes in half-opened until
// the half-open period is over.
type linearRampCircuitBreaker struct {
	state                      int
	openPeriod, halfOpenPeriod time.Duration
	lastOpened                 time.Time
	isOpenErr                  func(err error) bool
	maxRequests                int
	requests                   int
	mtx                        sync.Mutex
}

// newLinearRampCircuitBreaker returns a new circuit breaker which remains
// open until the end of the open window, and half-open until the end of the
// half-open window. It drops all requests in the open state, and drops all
// requests over max requests in half open state. It never drops requests
// in closed state.
func newLinearRampCircuitBreaker(
	openPeriod, halfOpenPeriod time.Duration,
	isOpenErr func(err error) bool,
) *linearRampCircuitBreaker {
	return &linearRampCircuitBreaker{
		state:          circuitBreakerClosed,
		openPeriod:     openPeriod,
		halfOpenPeriod: halfOpenPeriod,
		isOpenErr:      isOpenErr,
	}
}

// Allow implements [circuitBreaker].
func (b *linearRampCircuitBreaker) Allow() (ok bool, done func(err error)) {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	ok = b.handleAllow()
	if !ok {
		return ok, noopDoneFunc
	}
	b.requests++
	return ok, b.doneFunc
}

// Describe implements [prometheus.Collector].
func (b *linearRampCircuitBreaker) Describe(descs chan<- *prometheus.Desc) {
	descs <- circuitBreakerStateDesc
}

// Collect implements [prometheus.Collector].
func (b *linearRampCircuitBreaker) Collect(metrics chan<- prometheus.Metric) {
	b.mtx.Lock()
	state := float64(b.state)
	b.mtx.Unlock()
	metrics <- prometheus.MustNewConstMetric(
		circuitBreakerStateDesc,
		prometheus.GaugeValue,
		state,
	)
}

// A reference to doneFunc is returned in successful calls to [Allow]. It subtracts
// 1 from the requests counter and also checks if the (optional) error should open
// the circuit breaker.
func (b *linearRampCircuitBreaker) doneFunc(err error) {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	b.requests--
	if b.isOpenErr(err) {
		b.open()
	}
}

func (b *linearRampCircuitBreaker) open() {
	if b.state == circuitBreakerOpen {
		return
	}
	b.state = circuitBreakerOpen
	b.lastOpened = time.Now()
	b.maxRequests = max(1, b.maxRequests/2)
}

func (b *linearRampCircuitBreaker) handleAllow() bool {
	switch b.state {
	case circuitBreakerClosed:
		return true
	case circuitBreakerOpen:
		return b.handleOpenState()
	case circuitBreakerHalfOpen:
		return b.handleHalfOpenState()
	default:
		// defer will ensure that mtx is unlocked even after a panic.
		panic("Unknown state")
	}
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
	return b.allowHalfOpen(d - b.openPeriod)
}

// allowHalfOpen returns true if a request is allowed d seconds into the
// half-open period. The number of allowed requests increases from 0 to
// maxRequests in equal increments over the half-open period.
func (b *linearRampCircuitBreaker) allowHalfOpen(d time.Duration) bool {
	ramp := d.Seconds() / b.halfOpenPeriod.Seconds()
	rampMax := int(float64(b.maxRequests) * ramp)
	return b.requests < min(b.maxRequests, rampMax)
}
