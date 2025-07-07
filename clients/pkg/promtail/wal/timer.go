package wal

import "time"

// backoffTimer is a time.Timer that allows one to move between a minimum and maximum interval, using an exponential backoff
// strategy. It safely re-uses just one time.Timer instance internally.
type backoffTimer struct {
	timer                *time.Timer
	curr, minVal, maxVal time.Duration
	C                    <-chan time.Time
}

func newBackoffTimer(minVal, maxVal time.Duration) *backoffTimer {
	// note that the first timer created will be stopped without ever consuming it, since it's once we can omit it
	// since the timer is recycled, we can keep the channel
	t := time.NewTimer(minVal)
	return &backoffTimer{
		timer:  t,
		minVal: minVal,
		maxVal: maxVal,
		curr:   minVal,
		C:      t.C,
	}
}

func (bt *backoffTimer) backoff() {
	bt.curr = bt.curr * 2
	if bt.curr > bt.maxVal {
		bt.curr = bt.maxVal
	}
	bt.recycle()
}

func (bt *backoffTimer) reset() {
	bt.curr = bt.minVal
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
