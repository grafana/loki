package timeutil

import (
	"sync"
	"time"
)

// NewVariableTicker wrap time.Ticker to Reset() the ticker with the next duration (picked from
// input durations) after each tick. The last configured duration is the one that will be preserved
// once previous ones have been applied.
//
// Returns a function for stopping the ticker, and the ticker channel.
func NewVariableTicker(durations ...time.Duration) (func(), <-chan time.Time) {
	if len(durations) == 0 {
		panic("at least 1 duration required")
	}

	// Init the ticker with the 1st duration.
	ticker := time.NewTicker(durations[0])
	durations = durations[1:]

	// If there was only 1 duration we can simply return the built-in ticker.
	if len(durations) == 0 {
		return ticker.Stop, ticker.C
	}

	// Create a channel over which our ticks will be sent.
	ticks := make(chan time.Time, 1)

	// Create a channel used to signal once this ticker is stopped.
	stopped := make(chan struct{})

	go func() {
		for {
			select {
			case ts := <-ticker.C:
				if len(durations) > 0 {
					ticker.Reset(durations[0])
					durations = durations[1:]
				}

				// Non-blocking send to avoid goroutine leak if stopped while consumer is slow.
				select {
				case ticks <- ts:
				case <-stopped:
					return
				}

			case <-stopped:
				// Interrupt the loop once stopped.
				return
			}
		}
	}()

	stopOnce := sync.Once{}
	stop := func() {
		stopOnce.Do(func() {
			ticker.Stop()
			close(stopped)
		})
	}

	return stop, ticks
}
