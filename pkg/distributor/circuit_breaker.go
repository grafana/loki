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
	circuitBreakerOpenTotal = prometheus.NewDesc(
		"loki_distributor_circuit_breaker_open_total",
		"The number of times the circuit breaker opened.",
		nil,
		nil,
	)
	circuitBreakerMaxRequests = prometheus.NewDesc(
		"loki_distributor_circuit_breaker_max_requests",
		"The current number of max requests.",
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

// A trialCircuitBreaker is a circuit breaker which, once the open period has
// elapsed, allows up to maxTrials trial requests through in the half-open
// state. If all of those trials succeed it switches back to closed. If any
// trial fails it switches back to open.
type trialCircuitBreaker struct {
	state, totalOpens int
	openPeriod        time.Duration
	lastOpened        time.Time
	isOpenErr         func(err error) bool
	maxTrials         int
	trials            int
	successes         int
	mtx               sync.Mutex
}

// newTrialCircuitBreaker returns a new circuit breaker which remains open until
// the end of the open window, then allows up to maxTrials trial requests in the
// half-open state. It drops all requests in the open state, and drops requests
// over maxTrials in the half-open state. It never drops requests in the closed
// state.
func newTrialCircuitBreaker(
	openPeriod time.Duration,
	isOpenErr func(err error) bool,
	maxTrials int,
) *trialCircuitBreaker {
	return &trialCircuitBreaker{
		state:      circuitBreakerClosed,
		openPeriod: openPeriod,
		isOpenErr:  isOpenErr,
		maxTrials:  maxTrials,
	}
}

// Allow implements [circuitBreaker].
func (b *trialCircuitBreaker) Allow() (ok bool, done func(err error)) {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	ok = b.handleAllow()
	if !ok {
		return ok, noopDoneFunc
	}
	return ok, b.doneFunc
}

// Describe implements [prometheus.Collector].
func (b *trialCircuitBreaker) Describe(descs chan<- *prometheus.Desc) {
	descs <- circuitBreakerStateDesc
	descs <- circuitBreakerOpenTotal
	descs <- circuitBreakerMaxRequests
}

// Collect implements [prometheus.Collector].
func (b *trialCircuitBreaker) Collect(metrics chan<- prometheus.Metric) {
	b.mtx.Lock()
	state := float64(b.state)
	totalOpens := float64(b.totalOpens)
	maxTrials := float64(b.maxTrials)
	b.mtx.Unlock()
	metrics <- prometheus.MustNewConstMetric(
		circuitBreakerStateDesc,
		prometheus.GaugeValue,
		state,
	)
	metrics <- prometheus.MustNewConstMetric(
		circuitBreakerOpenTotal,
		prometheus.CounterValue,
		totalOpens,
	)
	metrics <- prometheus.MustNewConstMetric(
		circuitBreakerMaxRequests,
		prometheus.GaugeValue,
		maxTrials,
	)
}

// A reference to doneFunc is returned in successful calls to [Allow]. It checks
// if the (optional) error should open the circuit breaker, and otherwise counts
// successful trials in the half-open state, switching to closed once all trial
// requests have succeeded.
func (b *trialCircuitBreaker) doneFunc(err error) {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	if b.isOpenErr(err) {
		b.open()
		return
	}
	if b.state == circuitBreakerHalfOpen {
		b.successes++
		if b.successes >= b.maxTrials {
			b.close()
		}
	}
}

func (b *trialCircuitBreaker) open() {
	if b.state == circuitBreakerOpen {
		return
	}
	b.state = circuitBreakerOpen
	b.totalOpens++
	b.lastOpened = time.Now()
}

func (b *trialCircuitBreaker) halfOpen() {
	b.state = circuitBreakerHalfOpen
	b.trials = 0
	b.successes = 0
}

func (b *trialCircuitBreaker) close() {
	b.state = circuitBreakerClosed
	b.trials = 0
	b.successes = 0
}

func (b *trialCircuitBreaker) handleAllow() bool {
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

func (b *trialCircuitBreaker) handleOpenState() bool {
	if time.Since(b.lastOpened) > b.openPeriod {
		b.halfOpen()
		return b.handleHalfOpenState()
	}
	return false
}

// handleHalfOpenState allows up to maxTrials trial requests through. Requests
// over maxTrials are dropped until the outstanding trials either all succeed
// (switching to closed) or one fails (switching to open).
func (b *trialCircuitBreaker) handleHalfOpenState() bool {
	if b.trials < b.maxTrials {
		b.trials++
		return true
	}
	return false
}
