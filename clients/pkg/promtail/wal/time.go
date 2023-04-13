package wal

import "time"

// backoffTimer is a time.Timer that allows one to move between a minimum and maximum interval, using an exponential backoff
// strategy. It safely re-uses just one time.Timer instance internally.
type backoffTimer struct {
	timer          *time.Timer
	curr, min, max time.Duration
}

func newBackoffTimer(min, max time.Duration) *backoffTimer {
	// note that the first timer created will be stopped without ever consuming it, since it's once we can omit it
	return &backoffTimer{
		timer: time.NewTimer(min),
		min:   min,
		max:   max,
		curr:  min,
	}
}

func (bt *backoffTimer) backoff() {
	bt.curr = bt.curr * 2
	if bt.curr > bt.max {
		bt.curr = bt.max
	}
}

func (bt *backoffTimer) reset() {
	bt.curr = bt.min
}

// C follows the same pattern time.Timer and time.Ticker uses, but it's a function to reset the timer in each call, so that
// it uses the appropriate interval.
func (bt *backoffTimer) C() <-chan time.Time {
	if !bt.timer.Stop() {
		// attempt to drain timer's channel if it has expired
		select {
		case <-bt.timer.C:
		default:
		}
	}
	// safe to call reset after checking stopping and draining the timer
	bt.timer.Reset(bt.curr)
	return bt.timer.C
}
