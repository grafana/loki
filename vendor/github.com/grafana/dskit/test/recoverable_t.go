package test

import (
	"errors"
	"fmt"
	"testing"
)

// RecoverableT wraps a testing.TB so that FailNow, Fatal, and Fatalf panic instead
// of calling runtime.Goexit. This allows using require.* assertions from non-test
// goroutines (e.g., workers in concurrency.ForEachJob) where runtime.Goexit would
// terminate the goroutine without returning, causing the caller to hang.
//
// These are the only testing.TB methods that need overriding: all other methods
// (Errorf, Log, Failed, etc.) are safe to call from any goroutine and delegate
// directly to the wrapped testing.TB.
//
// Usage:
//
//	t := test.NewRecoverableT(t)
//	defer t.Recover()
//
//	require.Equal(t, expected, actual) // panics on failure instead of Goexit
type RecoverableT struct {
	testing.TB
}

type testFailure struct{}

// ErrTestFailed is returned by Recover when a test assertion failure was caught.
var ErrTestFailed = errors.New("test assertion failed")

// NewRecoverableT creates a new RecoverableT wrapping the given testing.TB.
func NewRecoverableT(t testing.TB) *RecoverableT {
	return &RecoverableT{TB: t}
}

// FailNow marks the test as failed and panics with a sentinel value instead of
// calling runtime.Goexit.
func (p *RecoverableT) FailNow() {
	p.Fail()
	panic(testFailure{})
}

// Fatal is equivalent to Log followed by FailNow.
func (p *RecoverableT) Fatal(args ...any) {
	p.Errorf("%s", fmt.Sprint(args...))
	p.FailNow()
}

// Fatalf is equivalent to Logf followed by FailNow.
func (p *RecoverableT) Fatalf(format string, args ...any) {
	p.Errorf(format, args...)
	p.FailNow()
}

// Recover catches the panic from FailNow. It must be called as a deferred function.
// Any non-sentinel panic is re-raised. When a test failure is caught, the enclosing
// function returns its zero values (e.g., nil error). Use RecoverError in functions
// that should return ErrTestFailed to signal the failure to the caller.
func (p *RecoverableT) Recover() {
	if r := recover(); r != nil {
		if _, ok := r.(testFailure); !ok {
			panic(r)
		}
	}
}

// RecoverError catches the panic from FailNow and assigns ErrTestFailed to the
// provided error pointer. It must be called as a deferred function with a pointer
// to a named return value. Any non-sentinel panic is re-raised.
//
// Usage:
//
//	func doWork() (retErr error) {
//	    t := test.NewRecoverableT(t)
//	    defer t.RecoverError(&retErr)
//	    require.Equal(t, expected, actual)
//	    return nil
//	}
func (p *RecoverableT) RecoverError(errPtr *error) {
	if r := recover(); r != nil {
		if _, ok := r.(testFailure); !ok {
			panic(r)
		}
		*errPtr = ErrTestFailed
	}
}
