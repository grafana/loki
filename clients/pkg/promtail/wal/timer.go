package wal

import "time"

// backoffTimer is a time.Timer that allows one to move between a minimum and maximum interval, using an exponential backoff
// strategy. It safely re-uses just one time.Timer instance internally.
type backoffTimer struct {
	timer          *time.Timer
	curr, min, max time.Duration
	C              <-chan time.Time
}

func newBackoffTimer(min, max time.Duration) *backoffTimer {
	// note that the first timer created will be stopped without ever consuming it, since it's once we can omit it
	// since the timer is recycled, we can keep the channel
	t := time.NewTimer(min)
	return &backoffTimer{
		timer: t,
		min:   min,
		max:   max,
		curr:  min,
		C:     t.C,
	}
}

func (bt *backoffTimer) backoff() {
	bt.curr = bt.curr * 2
	if bt.curr > bt.max {
		bt.curr = bt.max
	}
	bt.recycle()
}

func (bt *backoffTimer) reset() {
	bt.curr = bt.min
	bt.recycle()
}

// recycle stops and attempts to drain the time.Timer underlying channel, in order to fully recycle the instance.
func (bt *backoffTimer) recycle() {
	if !bt.timer.Stop() {
		// attempt to drain timer's channel if it has expired
		select {
		case <-bt.timer.C:
		default:
		}
	}
	// safe to call reset after checking stopping and draining the timer
	bt.timer.Reset(bt.curr)
}
