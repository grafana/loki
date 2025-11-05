package quartz

import (
	"context"
	"time"
)

type realClock struct{}

func NewReal() Clock {
	return realClock{}
}

func (realClock) NewTicker(d time.Duration, _ ...string) *Ticker {
	tkr := time.NewTicker(d)
	return &Ticker{ticker: tkr, C: tkr.C}
}

func (realClock) TickerFunc(ctx context.Context, d time.Duration, f func() error, _ ...string) Waiter {
	ct := &realContextTicker{
		ctx: ctx,
		tkr: time.NewTicker(d),
		f:   f,
		err: make(chan error, 1),
	}
	go ct.run()
	return ct
}

type realContextTicker struct {
	ctx context.Context
	tkr *time.Ticker
	f   func() error
	err chan error
}

func (t *realContextTicker) Wait(_ ...string) error {
	return <-t.err
}

func (t *realContextTicker) run() {
	defer t.tkr.Stop()
	for {
		select {
		case <-t.ctx.Done():
			t.err <- t.ctx.Err()
			return
		case <-t.tkr.C:
			err := t.f()
			if err != nil {
				t.err <- err
				return
			}
		}
	}
}

func (realClock) NewTimer(d time.Duration, _ ...string) *Timer {
	rt := time.NewTimer(d)
	return &Timer{C: rt.C, timer: rt}
}

func (realClock) AfterFunc(d time.Duration, f func(), _ ...string) *Timer {
	rt := time.AfterFunc(d, f)
	return &Timer{C: rt.C, timer: rt}
}

func (realClock) Now(_ ...string) time.Time {
	return time.Now()
}

func (realClock) Since(t time.Time, _ ...string) time.Duration {
	return time.Since(t)
}

func (realClock) Until(t time.Time, _ ...string) time.Duration {
	return time.Until(t)
}

var _ Clock = realClock{}
