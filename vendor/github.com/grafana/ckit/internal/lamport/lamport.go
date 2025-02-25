package lamport

import "sync/atomic"

var globalClock Clock

// Now returns the current Time. The time will not be changed.
func Now() Time { return globalClock.Now() }

// Tick increases the current Time by 1 and returns it.
func Tick() Time { return globalClock.Tick() }

// Observe ensures that the time past t by at least. Must be called when
// receiving a message from a remote machine to roughly synchronize
// clocks between processes.
func Observe(t Time) { globalClock.Observe(t) }

// Clock implements a lamport clock. The current time can be retrieved by
// calling Now. The Clock must be manually incremented either by calling Tick
// or Observe.
type Clock struct {
	time atomic.Uint64
}

// Time is the value of a Clock.
type Time uint64

// Now returns the current Time of c. The time is not changed.
func (c *Clock) Now() Time {
	return Time(c.time.Load())
}

// Tick increases c's Time by 1 and returns the new value.
func (c *Clock) Tick() Time {
	return Time(c.time.Add(1))
}

// Observe ensures that c's time is past t. Observe must be called when
// receiving a message from a remote machine that contains a Time, and is used
// to roughly synchronize clocks between machines.
func (c *Clock) Observe(t Time) {
Retry:
	// If t is behind us, we don't need to do anything.
	now := c.time.Load()
	if uint64(t) < now {
		return
	}

	// Move our clock past t.
	if !c.time.CompareAndSwap(now, uint64(t+1)) {
		// Retry if the CAS failed, which can happen when many observations are
		// happening concurrently. Either this will eventually succeed or another
		// call to Observe will move the current time past t and we'll be able
		// to do the early stop.
		goto Retry
	}
}
