// Package obslock provides an observed [sync.RWMutex] that records how long
// callers wait to acquire a lock and how long they hold it. The scheduler and
// worker share it so both emit lock_wait_seconds and lock_hold_seconds with the
// same lock, mode, and reason labels.
package obslock

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds the wait and hold histograms shared by observed mutexes. Wait
// and hold are partitioned by lock (the mutex name), mode (read or write), and
// reason (the acquisition call site).
type Metrics struct {
	waitSeconds *prometheus.HistogramVec
	holdSeconds *prometheus.HistogramVec
}

// NewMetrics registers the wait and hold histograms under the provided metric
// names and returns the Metrics that observed mutexes record into.
func NewMetrics(reg prometheus.Registerer, waitName, holdName string) *Metrics {
	return &Metrics{
		waitSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: waitName,
			Help: "Time spent waiting to acquire a lock before the acquisition succeeded",
		}, []string{"lock", "mode", "reason"}),
		holdSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: holdName,
			Help: "Time a lock was held by a single critical section, from acquisition to release",
		}, []string{"lock", "mode", "reason"}),
	}
}

func newNativeHistogramVec(r prometheus.Registerer, opts prometheus.HistogramOpts, labels []string) *prometheus.HistogramVec {
	opts.NativeHistogramBucketFactor = 1.1
	opts.NativeHistogramMaxBucketNumber = 100
	opts.NativeHistogramMinResetDuration = time.Hour
	return promauto.With(r).NewHistogramVec(opts, labels)
}

// Describe sends the descriptors of the wait and hold histograms to ch so
// callers can enumerate the lock metrics (for example, in a metric-inventory
// test). A nil *Metrics describes nothing.
func (m *Metrics) Describe(ch chan<- *prometheus.Desc) {
	if m == nil {
		return
	}
	m.waitSeconds.Describe(ch)
	m.holdSeconds.Describe(ch)
}

func (m *Metrics) observeWait(lock, mode, reason string, d time.Duration) {
	if m == nil {
		return
	}
	m.waitSeconds.WithLabelValues(lock, mode, reason).Observe(d.Seconds())
}

func (m *Metrics) observeHold(lock, mode, reason string, d time.Duration) {
	if m == nil {
		return
	}
	m.holdSeconds.WithLabelValues(lock, mode, reason).Observe(d.Seconds())
}

// Lock and mode label values.
const (
	modeRead  = "read"
	modeWrite = "write"
)

// RWMutex wraps a [sync.RWMutex] with instrumentation for acquisition wait time
// and critical-section hold time.
//
// The zero value is not usable; call [RWMutex.Init] before first use.
type RWMutex struct {
	mu   sync.RWMutex
	name string
	m    *Metrics
}

// Init binds the mutex to its metrics and lock name. It must be called once
// before the mutex is used and before any goroutine can acquire it.
func (o *RWMutex) Init(name string, m *Metrics) {
	o.name = name
	o.m = m
}

// Mutex wraps a [sync.Mutex] with instrumentation for acquisition wait time and
// critical-section hold time. It is the exclusive-only counterpart to
// [RWMutex] for locks that never need a shared read mode; its acquisitions are
// always recorded with mode "write".
//
// The zero value acquires the lock but records nothing until [Mutex.Init] binds
// it to its metrics and lock name.
type Mutex struct {
	mu   sync.Mutex
	name string
	m    *Metrics
}

// Init binds the mutex to its metrics and lock name. It should be called once
// before the mutex is used and before any goroutine can acquire it.
func (o *Mutex) Init(name string, m *Metrics) {
	o.name = name
	o.m = m
}

// Lock acquires the mutex for the given reason and returns a guard that
// releases it. The guard must be released exactly once with [Guard.Unlock].
func (o *Mutex) Lock(reason string) Guard {
	waitStart := time.Now()
	o.mu.Lock()
	wait := time.Since(waitStart)
	o.m.observeWait(o.name, modeWrite, reason, wait)

	holdStart := time.Now()
	return Guard{
		Wait: wait,
		release: func() {
			hold := time.Since(holdStart)
			o.mu.Unlock()
			o.m.observeHold(o.name, modeWrite, reason, hold)
		},
	}
}

// Guard releases a lock acquired from an [RWMutex]. Wait is the time spent
// waiting to acquire the lock. The guard must be released exactly once with the
// method matching how it was acquired.
type Guard struct {
	Wait    time.Duration
	release func()
}

// Unlock releases a write lock acquired by [RWMutex.Lock].
func (g Guard) Unlock() {
	if g.release != nil {
		g.release()
	}
}

// RUnlock releases a read lock acquired by [RWMutex.RLock].
func (g Guard) RUnlock() { g.Unlock() }

// Lock acquires the write lock for the given reason and returns a guard that
// releases it. The guard must be released exactly once.
func (o *RWMutex) Lock(reason string) Guard {
	waitStart := time.Now()
	o.mu.Lock()
	wait := time.Since(waitStart)
	o.m.observeWait(o.name, modeWrite, reason, wait)

	holdStart := time.Now()
	return Guard{
		Wait: wait,
		release: func() {
			hold := time.Since(holdStart)
			o.mu.Unlock()
			o.m.observeHold(o.name, modeWrite, reason, hold)
		},
	}
}

// RLock acquires the read lock for the given reason and returns a guard that
// releases it. The guard must be released exactly once.
func (o *RWMutex) RLock(reason string) Guard {
	waitStart := time.Now()
	o.mu.RLock()
	wait := time.Since(waitStart)
	o.m.observeWait(o.name, modeRead, reason, wait)

	holdStart := time.Now()
	return Guard{
		Wait: wait,
		release: func() {
			hold := time.Since(holdStart)
			o.mu.RUnlock()
			o.m.observeHold(o.name, modeRead, reason, hold)
		},
	}
}
