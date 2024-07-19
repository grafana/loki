package quartz

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

// Mock is the testing implementation of Clock.  It tracks a time that monotonically increases
// during a test, triggering any timers or tickers automatically.
type Mock struct {
	tb testing.TB
	mu sync.Mutex

	// cur is the current time
	cur time.Time

	all        []event
	nextTime   time.Time
	nextEvents []event
	traps      []*Trap
}

type event interface {
	next() time.Time
	fire(t time.Time)
}

func (m *Mock) TickerFunc(ctx context.Context, d time.Duration, f func() error, tags ...string) Waiter {
	if d <= 0 {
		panic("TickerFunc called with negative or zero duration")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c := newCall(clockFunctionTickerFunc, tags, withDuration(d))
	m.matchCallLocked(c)
	defer close(c.complete)
	t := &mockTickerFunc{
		ctx:  ctx,
		d:    d,
		f:    f,
		nxt:  m.cur.Add(d),
		mock: m,
		cond: sync.NewCond(&m.mu),
	}
	m.all = append(m.all, t)
	m.recomputeNextLocked()
	go t.waitForCtx()
	return t
}

func (m *Mock) NewTicker(d time.Duration, tags ...string) *Ticker {
	if d <= 0 {
		panic("NewTicker called with negative or zero duration")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c := newCall(clockFunctionNewTicker, tags, withDuration(d))
	m.matchCallLocked(c)
	defer close(c.complete)
	// 1 element buffer follows standard library implementation
	ticks := make(chan time.Time, 1)
	t := &Ticker{
		C:    ticks,
		c:    ticks,
		d:    d,
		nxt:  m.cur.Add(d),
		mock: m,
	}
	m.addEventLocked(t)
	return t
}

func (m *Mock) NewTimer(d time.Duration, tags ...string) *Timer {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := newCall(clockFunctionNewTimer, tags, withDuration(d))
	defer close(c.complete)
	m.matchCallLocked(c)
	ch := make(chan time.Time, 1)
	t := &Timer{
		C:    ch,
		c:    ch,
		nxt:  m.cur.Add(d),
		mock: m,
	}
	if d <= 0 {
		// zero or negative duration timer means we should immediately fire
		// it, rather than add it.
		go t.fire(t.mock.cur)
		return t
	}
	m.addEventLocked(t)
	return t
}

func (m *Mock) AfterFunc(d time.Duration, f func(), tags ...string) *Timer {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := newCall(clockFunctionAfterFunc, tags, withDuration(d))
	defer close(c.complete)
	m.matchCallLocked(c)
	t := &Timer{
		nxt:  m.cur.Add(d),
		fn:   f,
		mock: m,
	}
	if d <= 0 {
		// zero or negative duration timer means we should immediately fire
		// it, rather than add it.
		go t.fire(t.mock.cur)
		return t
	}
	m.addEventLocked(t)
	return t
}

func (m *Mock) Now(tags ...string) time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := newCall(clockFunctionNow, tags)
	defer close(c.complete)
	m.matchCallLocked(c)
	return m.cur
}

func (m *Mock) Since(t time.Time, tags ...string) time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := newCall(clockFunctionSince, tags, withTime(t))
	defer close(c.complete)
	m.matchCallLocked(c)
	return m.cur.Sub(t)
}

func (m *Mock) Until(t time.Time, tags ...string) time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := newCall(clockFunctionUntil, tags, withTime(t))
	defer close(c.complete)
	m.matchCallLocked(c)
	return t.Sub(m.cur)
}

func (m *Mock) addEventLocked(e event) {
	m.all = append(m.all, e)
	m.recomputeNextLocked()
}

func (m *Mock) recomputeNextLocked() {
	var best time.Time
	var events []event
	for _, e := range m.all {
		if best.IsZero() || e.next().Before(best) {
			best = e.next()
			events = []event{e}
			continue
		}
		if e.next().Equal(best) {
			events = append(events, e)
			continue
		}
	}
	m.nextTime = best
	m.nextEvents = events
}

func (m *Mock) removeTimer(t *Timer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeTimerLocked(t)
}

func (m *Mock) removeTimerLocked(t *Timer) {
	t.stopped = true
	m.removeEventLocked(t)
}

func (m *Mock) removeEventLocked(e event) {
	defer m.recomputeNextLocked()
	for i := range m.all {
		if m.all[i] == e {
			m.all = append(m.all[:i], m.all[i+1:]...)
			return
		}
	}
}

func (m *Mock) matchCallLocked(c *Call) {
	var traps []*Trap
	for _, t := range m.traps {
		if t.matches(c) {
			traps = append(traps, t)
		}
	}
	if len(traps) == 0 {
		return
	}
	c.releases.Add(len(traps))
	m.mu.Unlock()
	for _, t := range traps {
		go t.catch(c)
	}
	c.releases.Wait()
	m.mu.Lock()
}

// AdvanceWaiter is returned from Advance and Set calls and allows you to wait for ticks and timers
// to complete.  In the case of functions passed to AfterFunc or TickerFunc, it waits for the
// functions to return.  For other ticks & timers, it just waits for the tick to be delivered to
// the channel.
//
// If multiple timers or tickers trigger simultaneously, they are all run on separate
// go routines.
type AdvanceWaiter struct {
	tb testing.TB
	ch chan struct{}
}

// Wait for all timers and ticks to complete, or until context expires.
func (w AdvanceWaiter) Wait(ctx context.Context) error {
	select {
	case <-w.ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// MustWait waits for all timers and ticks to complete, and fails the test immediately if the
// context completes first.  MustWait must be called from the goroutine running the test or
// benchmark, similar to `t.FailNow()`.
func (w AdvanceWaiter) MustWait(ctx context.Context) {
	w.tb.Helper()
	select {
	case <-w.ch:
		return
	case <-ctx.Done():
		w.tb.Fatalf("context expired while waiting for clock to advance: %s", ctx.Err())
	}
}

// Done returns a channel that is closed when all timers and ticks complete.
func (w AdvanceWaiter) Done() <-chan struct{} {
	return w.ch
}

// Advance moves the clock forward by d, triggering any timers or tickers.  The returned value can
// be used to wait for all timers and ticks to complete.  Advance sets the clock forward before
// returning, and can only advance up to the next timer or tick event. It will fail the test if you
// attempt to advance beyond.
//
// If you need to advance exactly to the next event, and don't know or don't wish to calculate it,
// consider AdvanceNext().
func (m *Mock) Advance(d time.Duration) AdvanceWaiter {
	m.tb.Helper()
	w := AdvanceWaiter{tb: m.tb, ch: make(chan struct{})}
	m.mu.Lock()
	fin := m.cur.Add(d)
	// nextTime.IsZero implies no events scheduled.
	if m.nextTime.IsZero() || fin.Before(m.nextTime) {
		m.cur = fin
		m.mu.Unlock()
		close(w.ch)
		return w
	}
	if fin.After(m.nextTime) {
		m.tb.Errorf(fmt.Sprintf("cannot advance %s which is beyond next timer/ticker event in %s",
			d.String(), m.nextTime.Sub(m.cur)))
		m.mu.Unlock()
		close(w.ch)
		return w
	}

	m.cur = m.nextTime
	go m.advanceLocked(w)
	return w
}

func (m *Mock) advanceLocked(w AdvanceWaiter) {
	defer close(w.ch)
	wg := sync.WaitGroup{}
	for i := range m.nextEvents {
		e := m.nextEvents[i]
		t := m.cur
		wg.Add(1)
		go func() {
			e.fire(t)
			wg.Done()
		}()
	}
	// release the lock and let the events resolve.  This allows them to call back into the
	// Mock to query the time or set new timers.  Each event should remove or reschedule
	// itself from nextEvents.
	m.mu.Unlock()
	wg.Wait()
}

// Set the time to t.  If the time is after the current mocked time, then this is equivalent to
// Advance() with the difference.  You may only Set the time earlier than the current time before
// starting tickers and timers (e.g. at the start of your test case).
func (m *Mock) Set(t time.Time) AdvanceWaiter {
	m.tb.Helper()
	w := AdvanceWaiter{tb: m.tb, ch: make(chan struct{})}
	m.mu.Lock()
	if t.Before(m.cur) {
		defer close(w.ch)
		defer m.mu.Unlock()
		// past
		if !m.nextTime.IsZero() {
			m.tb.Error("Set mock clock to the past after timers/tickers started")
		}
		m.cur = t
		return w
	}
	// future
	// nextTime.IsZero implies no events scheduled.
	if m.nextTime.IsZero() || t.Before(m.nextTime) {
		defer close(w.ch)
		defer m.mu.Unlock()
		m.cur = t
		return w
	}
	if t.After(m.nextTime) {
		defer close(w.ch)
		defer m.mu.Unlock()
		m.tb.Errorf("cannot Set time to %s which is beyond next timer/ticker event at %s",
			t.String(), m.nextTime)
		return w
	}

	m.cur = m.nextTime
	go m.advanceLocked(w)
	return w
}

// AdvanceNext advances the clock to the next timer or tick event.  It fails the test if there are
// none scheduled.  It returns the duration the clock was advanced and a waiter that can be used to
// wait for the timer/tick event(s) to finish.
func (m *Mock) AdvanceNext() (time.Duration, AdvanceWaiter) {
	m.mu.Lock()
	m.tb.Helper()
	w := AdvanceWaiter{tb: m.tb, ch: make(chan struct{})}
	if m.nextTime.IsZero() {
		defer close(w.ch)
		defer m.mu.Unlock()
		m.tb.Error("cannot AdvanceNext because there are no timers or tickers running")
	}
	d := m.nextTime.Sub(m.cur)
	m.cur = m.nextTime
	go m.advanceLocked(w)
	return d, w
}

// Peek returns the duration until the next ticker or timer event and the value
// true, or, if there are no running tickers or timers, it returns zero and
// false.
func (m *Mock) Peek() (d time.Duration, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.nextTime.IsZero() {
		return 0, false
	}
	return m.nextTime.Sub(m.cur), true
}

// Trapper allows the creation of Traps
type Trapper struct {
	// mock is the underlying Mock.  This is a thin wrapper around Mock so that
	// we can have our interface look like mClock.Trap().NewTimer("foo")
	mock *Mock
}

func (t Trapper) NewTimer(tags ...string) *Trap {
	return t.mock.newTrap(clockFunctionNewTimer, tags)
}

func (t Trapper) AfterFunc(tags ...string) *Trap {
	return t.mock.newTrap(clockFunctionAfterFunc, tags)
}

func (t Trapper) TimerStop(tags ...string) *Trap {
	return t.mock.newTrap(clockFunctionTimerStop, tags)
}

func (t Trapper) TimerReset(tags ...string) *Trap {
	return t.mock.newTrap(clockFunctionTimerReset, tags)
}

func (t Trapper) TickerFunc(tags ...string) *Trap {
	return t.mock.newTrap(clockFunctionTickerFunc, tags)
}

func (t Trapper) TickerFuncWait(tags ...string) *Trap {
	return t.mock.newTrap(clockFunctionTickerFuncWait, tags)
}

func (t Trapper) NewTicker(tags ...string) *Trap {
	return t.mock.newTrap(clockFunctionNewTicker, tags)
}

func (t Trapper) TickerStop(tags ...string) *Trap {
	return t.mock.newTrap(clockFunctionTickerStop, tags)
}

func (t Trapper) TickerReset(tags ...string) *Trap {
	return t.mock.newTrap(clockFunctionTickerReset, tags)
}

func (t Trapper) Now(tags ...string) *Trap {
	return t.mock.newTrap(clockFunctionNow, tags)
}

func (t Trapper) Since(tags ...string) *Trap {
	return t.mock.newTrap(clockFunctionSince, tags)
}

func (t Trapper) Until(tags ...string) *Trap {
	return t.mock.newTrap(clockFunctionUntil, tags)
}

func (m *Mock) Trap() Trapper {
	return Trapper{m}
}

func (m *Mock) newTrap(fn clockFunction, tags []string) *Trap {
	m.mu.Lock()
	defer m.mu.Unlock()
	tr := &Trap{
		fn:    fn,
		tags:  tags,
		mock:  m,
		calls: make(chan *Call),
		done:  make(chan struct{}),
	}
	m.traps = append(m.traps, tr)
	return tr
}

// NewMock creates a new Mock with the time set to midnight UTC on Jan 1, 2024.
// You may re-set the time earlier than this, but only before timers or tickers
// are created.
func NewMock(tb testing.TB) *Mock {
	cur, err := time.Parse(time.RFC3339, "2024-01-01T00:00:00Z")
	if err != nil {
		panic(err)
	}
	return &Mock{
		tb:  tb,
		cur: cur,
	}
}

var _ Clock = &Mock{}

type mockTickerFunc struct {
	ctx  context.Context
	d    time.Duration
	f    func() error
	nxt  time.Time
	mock *Mock

	// cond is a condition Locked on the main Mock.mu
	cond *sync.Cond
	// done is true when the ticker exits
	done bool
	// err holds the error when the ticker exits
	err error
}

func (m *mockTickerFunc) next() time.Time {
	return m.nxt
}

func (m *mockTickerFunc) fire(_ time.Time) {
	m.mock.mu.Lock()
	defer m.mock.mu.Unlock()
	if m.done {
		return
	}
	m.nxt = m.nxt.Add(m.d)
	m.mock.recomputeNextLocked()

	m.mock.mu.Unlock()
	err := m.f()
	m.mock.mu.Lock()
	if err != nil {
		m.exitLocked(err)
	}
}

func (m *mockTickerFunc) exitLocked(err error) {
	if m.done {
		return
	}
	m.done = true
	m.err = err
	m.mock.removeEventLocked(m)
	m.cond.Broadcast()
}

func (m *mockTickerFunc) waitForCtx() {
	<-m.ctx.Done()
	m.mock.mu.Lock()
	defer m.mock.mu.Unlock()
	m.exitLocked(m.ctx.Err())
}

func (m *mockTickerFunc) Wait(tags ...string) error {
	m.mock.mu.Lock()
	defer m.mock.mu.Unlock()
	c := newCall(clockFunctionTickerFuncWait, tags)
	m.mock.matchCallLocked(c)
	defer close(c.complete)
	for !m.done {
		m.cond.Wait()
	}
	return m.err
}

var _ Waiter = &mockTickerFunc{}

type clockFunction int

const (
	clockFunctionNewTimer clockFunction = iota
	clockFunctionAfterFunc
	clockFunctionTimerStop
	clockFunctionTimerReset
	clockFunctionTickerFunc
	clockFunctionTickerFuncWait
	clockFunctionNewTicker
	clockFunctionTickerReset
	clockFunctionTickerStop
	clockFunctionNow
	clockFunctionSince
	clockFunctionUntil
)

type callArg func(c *Call)

type Call struct {
	Time     time.Time
	Duration time.Duration
	Tags     []string

	fn       clockFunction
	releases sync.WaitGroup
	complete chan struct{}
}

func (c *Call) Release() {
	c.releases.Done()
	<-c.complete
}

func withTime(t time.Time) callArg {
	return func(c *Call) {
		c.Time = t
	}
}

func withDuration(d time.Duration) callArg {
	return func(c *Call) {
		c.Duration = d
	}
}

func newCall(fn clockFunction, tags []string, args ...callArg) *Call {
	c := &Call{
		fn:       fn,
		Tags:     tags,
		complete: make(chan struct{}),
	}
	for _, a := range args {
		a(c)
	}
	return c
}

type Trap struct {
	fn    clockFunction
	tags  []string
	mock  *Mock
	calls chan *Call
	done  chan struct{}
}

func (t *Trap) catch(c *Call) {
	select {
	case t.calls <- c:
	case <-t.done:
		c.Release()
	}
}

func (t *Trap) matches(c *Call) bool {
	if t.fn != c.fn {
		return false
	}
	for _, tag := range t.tags {
		if !slices.Contains(c.Tags, tag) {
			return false
		}
	}
	return true
}

func (t *Trap) Close() {
	t.mock.mu.Lock()
	defer t.mock.mu.Unlock()
	for i, tr := range t.mock.traps {
		if t == tr {
			t.mock.traps = append(t.mock.traps[:i], t.mock.traps[i+1:]...)
		}
	}
	close(t.done)
}

var ErrTrapClosed = errors.New("trap closed")

func (t *Trap) Wait(ctx context.Context) (*Call, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.done:
		return nil, ErrTrapClosed
	case c := <-t.calls:
		return c, nil
	}
}

// MustWait calls Wait() and then if there is an error, immediately fails the
// test via tb.Fatalf()
func (t *Trap) MustWait(ctx context.Context) *Call {
	t.mock.tb.Helper()
	c, err := t.Wait(ctx)
	if err != nil {
		t.mock.tb.Fatalf("context expired while waiting for trap: %s", err.Error())
	}
	return c
}
