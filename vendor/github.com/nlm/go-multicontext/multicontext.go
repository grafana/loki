// multicontext package provides tools to aggregate the characteristics
// of multiple context.Context objects into one.
package multicontext

import (
	"context"
	"reflect"
	"time"
)

var _ context.Context = MultiContext{}

// WithContexts creates a new context.Context from multiple child contexts.
func WithContexts(ctxs ...context.Context) context.Context {
	return MultiContext{Ctxs: ctxs}
}

// A MultiContext is a struct providing context.Context and aggregating
// the characteritics of multiple child contexts.
type MultiContext struct {
	Ctxs []context.Context
}

// Deadline returns the time when work done on behalf of this context
// should be canceled. Deadline returns ok==false when no deadline is
// set. Successive calls to Deadline return the same results.
//
// MultiContext returns the earliest deadline of all child contexts.
// If no child context have a deadline, it returns a zero-value for
// deadline, and ok==false
func (mc MultiContext) Deadline() (deadline time.Time, ok bool) {
	var mcOk bool
	var mcDeadline time.Time
	for _, ctx := range mc.Ctxs {
		if deadline, ok := ctx.Deadline(); ok {
			if !mcOk {
				mcOk = true
				mcDeadline = deadline
				continue
			}
			if deadline.Before(mcDeadline) {
				mcDeadline = deadline
			}
		}
	}
	return mcDeadline, mcOk
}

// Done returns a channel that's closed when work done on behalf of this
// context should be canceled. Done may return nil if this context can
// never be canceled. Successive calls to Done return the same value.
// The close of the Done channel may happen asynchronously,
// after the cancel function returns.
//
// MultiContext returns a channel that will be closed if any of
// the channels of the child context is closed.
func (mc MultiContext) Done() <-chan struct{} {
	if len(mc.Ctxs) == 0 {
		return nil
	}
	cases := make([]reflect.SelectCase, 0, len(mc.Ctxs))
	for _, ctx := range mc.Ctxs {
		cases = append(cases, reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(ctx.Done()),
		})
	}
	ch := make(chan struct{})
	go func(cases []reflect.SelectCase, ch chan<- struct{}) {
		reflect.Select(cases)
		close(ch)
	}(cases, ch)
	return ch
}

// If Done is not yet closed, Err returns nil.
// If Done is closed, Err returns a non-nil error explaining why:
// Canceled if the context was canceled
// or DeadlineExceeded if the context's deadline passed.
// After Err returns a non-nil error, successive calls to Err return the same error.
//
// MultiContext returns the error from the first child context that
// returns one. If no child context return an error, it returns nil.
func (mc MultiContext) Err() error {
	for _, ctx := range mc.Ctxs {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

// Value returns the value associated with this context for key, or nil
// if no value is associated with key. Successive calls to Value with
// the same key returns the same result.
//
// MultiContext returns the value from the first child context that returns
// a non-nil value. If no child context returns a value, it returns nil.
func (mc MultiContext) Value(key any) any {
	for _, ctx := range mc.Ctxs {
		if v := ctx.Value(key); v != nil {
			return v
		}
	}
	return nil
}
