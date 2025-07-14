package kgo

import "sync/atomic"

const (
	stateUnstarted = iota
	stateWorking
	stateContinueWorking
)

type workLoop struct{ state atomicU32 }

// maybeBegin returns whether a work loop should begin.
func (l *workLoop) maybeBegin() bool {
	var state uint32
	var done bool
	for !done {
		switch state = l.state.Load(); state {
		case stateUnstarted:
			done = l.state.CompareAndSwap(state, stateWorking)
			state = stateWorking
		case stateWorking:
			done = l.state.CompareAndSwap(state, stateContinueWorking)
			state = stateContinueWorking
		case stateContinueWorking:
			done = true
		}
	}

	return state == stateWorking
}

// maybeFinish demotes loop's internal state and returns whether work should
// keep going. This function should be called before looping to continue
// work.
//
// If again is true, this will avoid demoting from working to not
// working. Again would be true if the loop knows it should continue working;
// calling this function is necessary even in this case to update loop's
// internal state.
//
// This function is a no-op if the loop is already finished, but generally,
// since the loop itself calls MaybeFinish after it has been started, this
// should never be called if the loop is unstarted.
func (l *workLoop) maybeFinish(again bool) bool {
	switch state := l.state.Load(); state {
	// Working:
	// If again, we know we should continue; keep our state.
	// If not again, we try to downgrade state and stop.
	// If we cannot, then something slipped in to say keep going.
	case stateWorking:
		if !again {
			again = !l.state.CompareAndSwap(state, stateUnstarted)
		}
	// Continue: demote ourself and run again no matter what.
	case stateContinueWorking:
		l.state.Store(stateWorking)
		again = true
	}

	return again
}

func (l *workLoop) hardFinish() {
	l.state.Store(stateUnstarted)
}

// lazyI32 is used in a few places where we want atomics _sometimes_.  Some
// uses do not need to be atomic (notably, setup), and we do not want the
// noCopy guard.
//
// Specifically, this is used for a few int32 settings in the config.
type lazyI32 int32

func (v *lazyI32) store(s int32) { atomic.StoreInt32((*int32)(v), s) }
func (v *lazyI32) load() int32   { return atomic.LoadInt32((*int32)(v)) }
