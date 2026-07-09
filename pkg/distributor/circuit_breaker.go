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
	circuitBreakerMaxFailures = prometheus.NewDesc(
		"loki_distributor_circuit_breaker_max_failures",
		"The number of successive failures required to open the circuit breaker.",
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

// A trialCircuitBreaker is a circuit breaker which opens after maxFailures
// successive failures. Once the open period has elapsed, it allows up to
// maxTrials trial requests through in the half-open state. If all of those
// trials succeed it switches back to closed. If maxFailures successive trials
// fail it switches back to open.
type trialCircuitBreaker struct {
	state, totalOpens int
	openPeriod        time.Duration
	lastOpened        time.Time
	isOpenErr         func(err error) bool
	maxTrials         int
	trials            int
	successes         int
	maxFailures       int
	failures          int
	// term is incremented on every state transition. Each done callback is bound
	// to the term in which its request was admitted, so completions that arrive
	// after a transition are ignored rather than counted against an unrelated
	// state.
	term int
	mtx  sync.Mutex
}

// newTrialCircuitBreaker returns a new circuit breaker which opens after
// maxFailures successive failures (as classified by isOpenErr). It remains open
// until the end of the open window, then allows up to maxTrials trial requests
// in the half-open state. It drops all requests in the open state, and drops
// requests over maxTrials in the half-open state. It never drops requests in the
// closed state.
func newTrialCircuitBreaker(
	openPeriod time.Duration,
	isOpenErr func(err error) bool,
	maxTrials int,
	maxFailures int,
) *trialCircuitBreaker {
	return &trialCircuitBreaker{
		state:       circuitBreakerClosed,
		openPeriod:  openPeriod,
		isOpenErr:   isOpenErr,
		maxTrials:   maxTrials,
		maxFailures: maxFailures,
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
	// Bind the callback to the current term so a completion that arrives after a
	// subsequent state transition is ignored.
	term := b.term
	return ok, func(err error) { b.done(term, err) }
}

// Describe implements [prometheus.Collector].
func (b *trialCircuitBreaker) Describe(descs chan<- *prometheus.Desc) {
	descs <- circuitBreakerStateDesc
	descs <- circuitBreakerOpenTotal
	descs <- circuitBreakerMaxRequests
	descs <- circuitBreakerMaxFailures
}

// Collect implements [prometheus.Collector].
func (b *trialCircuitBreaker) Collect(metrics chan<- prometheus.Metric) {
	b.mtx.Lock()
	state := float64(b.state)
	totalOpens := float64(b.totalOpens)
	maxTrials := float64(b.maxTrials)
	maxFailures := float64(b.maxFailures)
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
	metrics <- prometheus.MustNewConstMetric(
		circuitBreakerMaxFailures,
		prometheus.GaugeValue,
		maxFailures,
	)
}

// done is invoked by the callback returned from successful calls to [Allow]. It
// counts successive failures (as classified by isOpenErr), opening the circuit
// breaker once maxFailures is reached. Any non-opening completion resets the
// successive failure count and, in the half-open state, counts as a successful
// trial, switching to closed once all trial requests have succeeded.
//
// term is the term the request was admitted in; completions from an earlier
// term are stale (the breaker has since transitioned) and are ignored, so they
// cannot affect the counters of an unrelated state.
func (b *trialCircuitBreaker) done(term int, err error) {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	if term != b.term {
		return
	}
	if b.isOpenErr(err) {
		b.failures++
		if b.failures >= b.maxFailures {
			b.open()
		}
		return
	}
	// A non-opening completion resets the successive failure count.
	b.failures = 0
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
	b.failures = 0
	b.term++
}

func (b *trialCircuitBreaker) halfOpen() {
	b.state = circuitBreakerHalfOpen
	b.trials = 0
	b.successes = 0
	b.failures = 0
	b.term++
}

func (b *trialCircuitBreaker) close() {
	b.state = circuitBreakerClosed
	b.trials = 0
	b.successes = 0
	b.failures = 0
	b.term++
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
