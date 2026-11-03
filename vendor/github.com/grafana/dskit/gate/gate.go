package gate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var ErrMaxConcurrent = errors.New("max concurrent requests inflight")

var ErrGateTimeout = errors.New("timeout waiting for concurrency gate")

// Gate controls the maximum number of concurrently running and waiting queries.
//
// Example of use:
//
//	g := gate.New(r, 5)
//
//	if err := g.Start(ctx); err != nil {
//	   return
//	}
//	defer g.Done()
type Gate interface {
	// Start initiates a new request and waits until it's our turn to fulfill a request.
	Start(ctx context.Context) error
	// Done finishes a query.
	Done()
}

// NewNoop creates a Gate implementation that doesn't enforce any limit.
func NewNoop() Gate {
	return noopGate{}
}

type noopGate struct{}

func (noopGate) Start(context.Context) error { return nil }
func (noopGate) Done()                       {}

// New returns an instrumented gate limiting the number of requests being
// executed concurrently.
//
// The gate implementation is based on the
// github.com/prometheus/prometheus/util/gate package.
//
// It can be called several times but not with the same registerer otherwise it
// will panic when trying to register the same metric multiple times.
func New(reg prometheus.Registerer, maxConcurrent int) Gate {
	return NewInstrumented(reg, maxConcurrent, NewBlocking(maxConcurrent))
}

// NewInstrumented wraps a Gate implementation with one that records max number of inflight
// requests, currently inflight requests, and the duration of calls to the Start method.
func NewInstrumented(reg prometheus.Registerer, maxConcurrent int, gate Gate) Gate {
	g := &instrumentedGate{
		gate: gate,
		max: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "gate_queries_concurrent_max",
			Help: "Number of maximum concurrent queries allowed.",
		}),
		inflight: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "gate_queries_in_flight",
			Help: "Number of queries that are currently in flight.",
		}),
		duration: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name:    "gate_duration_seconds",
			Help:    "How many seconds it took for queries to wait at the gate.",
			Buckets: []float64{0.01, 0.1, 0.3, 0.6, 1, 3, 6, 9, 20, 30, 60, 90, 120, 240, 360, 720},
			// Use defaults recommended by Prometheus for native histograms.
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"outcome"}),
	}

	g.max.Set(float64(maxConcurrent))
	return g
}

type instrumentedGate struct {
	gate Gate

	max      prometheus.Gauge
	inflight prometheus.Gauge
	duration *prometheus.HistogramVec
}

func (g *instrumentedGate) Start(ctx context.Context) error {
	start := time.Now()

	err := g.gate.Start(ctx)
	if err != nil {
		var reason string
		switch {
		case errors.Is(err, context.Canceled):
			reason = "rejected_canceled"
		case errors.Is(err, context.DeadlineExceeded):
			reason = "rejected_deadline_exceeded"
		default:
			reason = "rejected_other"
		}
		g.duration.WithLabelValues(reason).Observe(time.Since(start).Seconds())
		return err
	}

	g.duration.WithLabelValues("permitted").Observe(time.Since(start).Seconds())
	g.inflight.Inc()
	return nil
}

func (g *instrumentedGate) Done() {
	g.inflight.Dec()
	g.gate.Done()
}

// timeoutGate returns ErrGateTimeout when the timeout is reached while still waiting for the delegate gate.
type timeoutGate struct {
	gate    Gate
	timeout time.Duration
}

// NewTimeoutGate wraps a Gate implementation with one that sets a timeout on the context passed to the
// delegated Gate.
func NewTimeoutGate(timeout time.Duration, gate Gate) Gate {
	return &timeoutGate{
		gate:    gate,
		timeout: timeout,
	}
}

func (t *timeoutGate) Start(ctx context.Context) error {
	if t.timeout == 0 {
		return t.gate.Start(ctx)
	}

	// Inject our own error so that we can differentiate between a timeout caused by this gate
	// or a timeout in the original request timeout.
	ctx, cancel := context.WithTimeoutCause(ctx, t.timeout, ErrGateTimeout)
	defer cancel()

	err := t.gate.Start(ctx)
	// Note that we only return an error for a timeout when the delegate has also returned an
	// error. This ensures that when we get a slot in the delegate, our caller will call Done()
	// and release the slot.
	if err != nil && errors.Is(context.Cause(ctx), ErrGateTimeout) {
		err = ErrGateTimeout
	}
	return err
}

func (t *timeoutGate) Done() {
	t.gate.Done()
}

func NewRejecting(maxConcurrent int) Gate {
	return &rejectingGate{
		ch: make(chan struct{}, maxConcurrent),
	}
}

type rejectingGate struct {
	ch chan struct{}
}

func (g *rejectingGate) Start(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case g.ch <- struct{}{}:
		return nil
	default:
		return fmt.Errorf("%w: %d", ErrMaxConcurrent, len(g.ch))
	}
}

func (g *rejectingGate) Done() {
	select {
	case <-g.ch:
	default:
		panic("gate.Done: more operations done than started")
	}
}

func NewBlocking(maxConcurrent int) Gate {
	return &blockingGate{
		ch: make(chan struct{}, maxConcurrent),
	}
}

type blockingGate struct {
	ch chan struct{}
}

func (g *blockingGate) Start(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case g.ch <- struct{}{}:
		return nil
	}
}

func (g *blockingGate) Done() {
	select {
	case <-g.ch:
	default:
		panic("gate.Done: more operations done than started")
	}
}
