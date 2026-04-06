package gobreaker

// TwoStepCircuitBreaker is like CircuitBreaker but instead of surrounding a function
// with the breaker functionality, it only checks whether a request can proceed and
// expects the caller to report the outcome in a separate step using a callback.
type TwoStepCircuitBreaker[T any] struct {
	cb *CircuitBreaker[T]
}

// NewTwoStepCircuitBreaker returns a new TwoStepCircuitBreaker configured with the given Settings.
func NewTwoStepCircuitBreaker[T any](st Settings) *TwoStepCircuitBreaker[T] {
	return &TwoStepCircuitBreaker[T]{
		cb: NewCircuitBreaker[T](st),
	}
}

// Name returns the name of the TwoStepCircuitBreaker.
func (tscb *TwoStepCircuitBreaker[T]) Name() string {
	return tscb.cb.Name()
}

// State returns the current state of the TwoStepCircuitBreaker.
func (tscb *TwoStepCircuitBreaker[T]) State() State {
	return tscb.cb.State()
}

// Counts returns internal counters
func (tscb *TwoStepCircuitBreaker[T]) Counts() Counts {
	return tscb.cb.Counts()
}

// Allow checks if a new request can proceed.
// If the circuit breaker allow the new request, it returns a callback
// used to register the error object that the request will return in a separate step.
// If the circuit breaker does not allow the request, it returns an error.
func (tscb *TwoStepCircuitBreaker[T]) Allow() (done func(err error), err error) {
	generation, age, err := tscb.cb.beforeRequest()
	if err != nil {
		return nil, err
	}

	return func(err error) {
		tscb.cb.afterRequest(generation, age, err)
	}, nil
}
