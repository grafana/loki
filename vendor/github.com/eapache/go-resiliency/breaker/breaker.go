// Package breaker implements the circuit-breaker resiliency pattern for Go.
package breaker

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// ErrBreakerOpen is the error returned from Run() when the function is not executed
// because the breaker is currently open.
var ErrBreakerOpen = errors.New("circuit breaker is open")

// State is a type representing the possible states of a circuit breaker.
type State uint32

const (
	Closed State = iota
	Open
	HalfOpen
)

// Breaker implements the circuit-breaker resiliency pattern
type Breaker struct {
	errorThreshold, successThreshold int
	timeout                          time.Duration

	lock              sync.Mutex
	state             State
	errors, successes int
	lastError         time.Time
}

// New constructs a new circuit-breaker that starts closed.
// From closed, the breaker opens if "errorThreshold" errors are seen
// without an error-free period of at least "timeout". From open, the
// breaker half-closes after "timeout". From half-open, the breaker closes
// after "successThreshold" consecutive successes, or opens on a single error.
func New(errorThreshold, successThreshold int, timeout time.Duration) *Breaker {
	return &Breaker{
		errorThreshold:   errorThreshold,
		successThreshold: successThreshold,
		timeout:          timeout,
	}
}

// Run will either return ErrBreakerOpen immediately if the circuit-breaker is
// already open, or it will run the given function and pass along its return
// value. It is safe to call Run concurrently on the same Breaker.
func (b *Breaker) Run(work func() error) error {
	state := b.GetState()

	if state == Open {
		return ErrBreakerOpen
	}

	return b.doWork(state, work)
}

// Go will either return ErrBreakerOpen immediately if the circuit-breaker is
// already open, or it will run the given function in a separate goroutine.
// If the function is run, Go will return nil immediately, and will *not* return
// the return value of the function. It is safe to call Go concurrently on the
// same Breaker.
func (b *Breaker) Go(work func() error) error {
	state := b.GetState()

	if state == Open {
		return ErrBreakerOpen
	}

	// errcheck complains about ignoring the error return value, but
	// that's on purpose; if you want an error from a goroutine you have to
	// get it over a channel or something
	go b.doWork(state, work)

	return nil
}

// GetState returns the current State of the circuit-breaker at the moment
// that it is called.
func (b *Breaker) GetState() State {
	return (State)(atomic.LoadUint32((*uint32)(&b.state)))
}

func (b *Breaker) doWork(state State, work func() error) error {
	var panicValue interface{}

	result := func() error {
		defer func() {
			panicValue = recover()
		}()
		return work()
	}()

	if result == nil && panicValue == nil && state == Closed {
		// short-circuit the normal, success path without contending
		// on the lock
		return nil
	}

	// oh well, I guess we have to contend on the lock
	b.processResult(result, panicValue)

	if panicValue != nil {
		// as close as Go lets us come to a "rethrow" although unfortunately
		// we lose the original panicing location
		panic(panicValue)
	}

	return result
}

func (b *Breaker) processResult(result error, panicValue interface{}) {
	b.lock.Lock()
	defer b.lock.Unlock()

	if result == nil && panicValue == nil {
		if b.state == HalfOpen {
			b.successes++
			if b.successes == b.successThreshold {
				b.closeBreaker()
			}
		}
	} else {
		if b.errors > 0 {
			expiry := b.lastError.Add(b.timeout)
			if time.Now().After(expiry) {
				b.errors = 0
			}
		}

		switch b.state {
		case Closed:
			b.errors++
			if b.errors == b.errorThreshold {
				b.openBreaker()
			} else {
				b.lastError = time.Now()
			}
		case HalfOpen:
			b.openBreaker()
		}
	}
}

func (b *Breaker) openBreaker() {
	b.changeState(Open)
	go b.timer()
}

func (b *Breaker) closeBreaker() {
	b.changeState(Closed)
}

func (b *Breaker) timer() {
	time.Sleep(b.timeout)

	b.lock.Lock()
	defer b.lock.Unlock()

	b.changeState(HalfOpen)
}

func (b *Breaker) changeState(newState State) {
	b.errors = 0
	b.successes = 0
	atomic.StoreUint32((*uint32)(&b.state), (uint32)(newState))
}
