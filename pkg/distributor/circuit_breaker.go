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
	circuitBreakerOpenDesc = prometheus.NewDesc(
		"loki_distributor_circuit_breaker_open_total",
		"The number of times the circuit breaker opened.",
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

// A trialCircuitBreaker is a traditional circuit breaker. The circuit breaker
// is opened after a minimum number of successive failures, where it drops all
// requests for the duration of the open period. At the end of the open period
// it transitions to the half-open state where it allows a maximum number of
// "trial" requests. If all trial requests are successful, the circuit breaker
// transitions to closed, otherwise it re-opens and starts again.
type trialCircuitBreaker struct {
	lastOpened                         time.Time
	isOpenErr                          func(err error) bool
	noopDoneFunc                       func(error)
	openPeriod                         time.Duration
	state                              int
	totalOpens                         int
	successes, trials, permittedTrials int
	failures, minFailures              int
	// term is incremented on each state transition. It ensures that a doneFunc
	// is called iff its term matches the current term. Without this, results
	// from previous terms would affect the current term and cause incorrect
	// behavior. A simple example is a request from a previous closed state
	// mistaken as a trial request of the current half-open state.
	term int
	mtx  sync.Mutex
}

// newTrialCircuitBreaker returns a trial new circuit breaker.
func newTrialCircuitBreaker(
	openPeriod time.Duration,
	minFailures int,
	permittedTrials int,
	isOpenErr func(err error) bool,
) *trialCircuitBreaker {
	return &trialCircuitBreaker{
		state:           circuitBreakerClosed,
		openPeriod:      openPeriod,
		minFailures:     minFailures,
		permittedTrials: permittedTrials,
		isOpenErr:       isOpenErr,
		noopDoneFunc:    noopDoneFunc,
	}
}

// Allow implements [circuitBreaker].
func (b *trialCircuitBreaker) Allow() (ok bool, done func(err error)) {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	ok = b.handleAllow()
	if !ok {
		return ok, b.noopDoneFunc
	}
	// Bind the func to the current term.
	term := b.term
	return ok, func(err error) { b.doneFunc(term, err) }
}

// Describe implements [prometheus.Collector].
func (b *trialCircuitBreaker) Describe(descs chan<- *prometheus.Desc) {
	descs <- circuitBreakerStateDesc
	descs <- circuitBreakerOpenDesc
}

// Collect implements [prometheus.Collector].
func (b *trialCircuitBreaker) Collect(metrics chan<- prometheus.Metric) {
	b.mtx.Lock()
	var (
		state      = float64(b.state)
		totalOpens = float64(b.totalOpens)
	)
	b.mtx.Unlock()
	metrics <- prometheus.MustNewConstMetric(
		circuitBreakerStateDesc,
		prometheus.GaugeValue,
		state,
	)
	metrics <- prometheus.MustNewConstMetric(
		circuitBreakerOpenDesc,
		prometheus.CounterValue,
		totalOpens,
	)
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

func (b *trialCircuitBreaker) handleHalfOpenState() bool {
	if b.trials < b.permittedTrials {
		b.trials++
		return true
	}
	return false
}

// A reference to doneFunc is returned in successful calls to [Allow].
func (b *trialCircuitBreaker) doneFunc(term int, err error) {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	// The doneFunc must not run if its for a previous term.
	if term != b.term {
		return
	}
	if b.isOpenErr(err) {
		b.handleOpenErr(err)
		return
	}
	b.handleSuccess()
}

func (b *trialCircuitBreaker) handleOpenErr(_ error) {
	switch b.state {
	case circuitBreakerClosed:
		// Increment the number of successive failures (reset to 0 in [handleSuccess]).
		// If it exceeds the minimum number of failures transition to open state.
		b.failures++
		if b.failures >= b.minFailures {
			b.open()
		}
	case circuitBreakerHalfOpen:
		// A failure in the half-open state re-opens the circuit breaker. This is
		// essential for correctness to avoid deadlocks in the half-open state where
		// all trial requests have completed but neither threshold to close or re-open
		// the circuit breaker was satisifed.
		b.open()
	default:
		// circuitBreakerOpen is unreachable: Allow() returns noopDoneFunc when open,
		// and the term check in doneFunc prevents stale calls.
		panic("Invalid state for handleOpenErr")
	}
}

func (b *trialCircuitBreaker) handleSuccess() {
	// Reset the number of successive failures.
	b.failures = 0
	if b.state == circuitBreakerHalfOpen {
		b.successes++
		// If all trial requests are successful, transition to close state.
		if b.successes >= b.permittedTrials {
			b.close()
		}
	}
}

func (b *trialCircuitBreaker) open() {
	b.newTerm(circuitBreakerOpen)
	b.lastOpened = time.Now()
	b.totalOpens++
}

func (b *trialCircuitBreaker) halfOpen() {
	b.newTerm(circuitBreakerHalfOpen)
}

func (b *trialCircuitBreaker) close() {
	b.newTerm(circuitBreakerClosed)
}

func (b *trialCircuitBreaker) newTerm(state int) {
	b.state = state
	b.term++
	b.trials = 0
	b.successes = 0
	b.failures = 0
}
