# Quartz

A Go time testing library for writing deterministic unit tests

Our high level goal is to write unit tests that

1. execute quickly
2. don't flake
3. are straightforward to write and understand

For tests to execute quickly without flakes, we want to focus on _determinism_: the test should run
the same each time, and it should be easy to force the system into a known state (no races) before
executing test assertions. `time.Sleep`, `runtime.Gosched()`, and
polling/[Eventually](https://pkg.go.dev/github.com/stretchr/testify/assert#Eventually) are all
symptoms of an inability to do this easily.

## Usage

### `Clock` interface

In your application code, maintain a reference to a `quartz.Clock` instance to start timers and
tickers, instead of the bare `time` standard library.

```go
import "github.com/coder/quartz"

type Component struct {
	...

	// for testing
	clock quartz.Clock
}
```

Whenever you would call into `time` to start a timer or ticker, call `Component`'s `clock` instead.

In production, set this clock to `quartz.NewReal()` to create a clock that just transparently passes
through to the standard `time` library.

### Mocking

In your tests, you can use a `*Mock` to control the tickers and timers your code under test gets.

```go
import (
	"testing"
	"github.com/coder/quartz"
)

func TestComponent(t *testing.T) {
	mClock := quartz.NewMock(t)
	comp := &Component{
		...
		clock: mClock,
	}
}
```

The `*Mock` clock starts at Jan 1, 2024, 00:00 UTC by default, but you can set any start time you'd like prior to your test.

```go
mClock := quartz.NewMock(t)
mClock.Set(time.Date(2021, 6, 18, 12, 0, 0, 0, time.UTC)) // June 18, 2021 @ 12pm UTC
```

#### Advancing the clock

Once you begin setting timers or tickers, you cannot change the time backward, only advance it
forward. You may continue to use `Set()`, but it is often easier and clearer to use `Advance()`.

For example, with a timer:

```go
fired := false

tmr := mClock.AfterFunc(time.Second, func() {
  fired = true
})
mClock.Advance(time.Second)
```

When you call `Advance()` it immediately moves the clock forward the given amount, and triggers any
tickers or timers that are scheduled to happen at that time. Any triggered events happen on separate
goroutines, so _do not_ immediately assert the results:

```go
fired := false

tmr := mClock.AfterFunc(time.Second, func() {
  fired = true
})
mClock.Advance(time.Second)

// RACE CONDITION, DO NOT DO THIS!
if !fired {
  t.Fatal("didn't fire")
}
```

`Advance()` (and `Set()` for that matter) return an `AdvanceWaiter` object you can use to wait for
all triggered events to complete.

```go
fired := false
// set a test timeout so we don't wait the default `go test` timeout for a failure
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

tmr := mClock.AfterFunc(time.Second, func() {
  fired = true
})

w := mClock.Advance(time.Second)
err := w.Wait(ctx)
if err != nil {
  t.Fatal("AfterFunc f never completed")
}
if !fired {
  t.Fatal("didn't fire")
}
```

The construction of waiting for the triggered events and failing the test if they don't complete is
very common, so there is a shorthand:

```go
w := mClock.Advance(time.Second)
err := w.Wait(ctx)
if err != nil {
  t.Fatal("AfterFunc f never completed")
}
```

is equivalent to:

```go
w := mClock.Advance(time.Second)
w.MustWait(ctx)
```

or even more briefly:

```go
mClock.Advance(time.Second).MustWait(ctx)
```

### Advance only to the next event

One important restriction on advancing the clock is that you may only advance forward to the next
timer or ticker event and no further. The following will result in a test failure:

```go
func TestAdvanceTooFar(t *testing.T) {
	ctx, cancel := context.WithTimeout(10*time.Second)
	defer cancel()
	mClock := quartz.NewMock(t)
	var firedAt time.Time
	mClock.AfterFunc(time.Second, func() {
		firedAt := mClock.Now()
	})
	mClock.Advance(2*time.Second).MustWait(ctx)
}
```

This is a deliberate design decision to allow `Advance()` to immediately and synchronously move the
clock forward (even without calling `Wait()` on returned waiter). This helps meet Quartz's design
goals of writing deterministic and easy to understand unit tests. It also allows the clock to be
advanced, deterministically _during_ the execution of a tick or timer function, as explained in the
next sections on Traps.

Advancing multiple events can be accomplished via looping. E.g. if you have a 1-second ticker

```go
for i := 0; i < 10; i++ {
	mClock.Advance(time.Second).MustWait(ctx)
}
```

will advance 10 ticks.

If you don't know or don't want to compute the time to the next event, you can use `AdvanceNext()`.

```go
d, w := mClock.AdvanceNext()
w.MustWait(ctx)
// d contains the duration we advanced
```

`d, ok := Peek()` returns the duration until the next event, if any (`ok` is `true`). You can use
this to advance a specific time, regardless of the tickers and timer events:

```go
desired := time.Minute // time to advance
for desired > 0 {
	p, ok := mClock.Peek()
	if !ok || p > desired {
		mClock.Advance(desired).MustWait(ctx)
		break
	}
	mClock.Advance(p).MustWait(ctx)
	desired -= p
}
```

### Traps

A trap allows you to match specific calls into the library while mocking, block their return,
inspect their arguments, then release them to allow them to return. They help you write
deterministic unit tests even when the code under test executes asynchronously from the test.

You set your traps prior to executing code under test, and then wait for them to be triggered.

```go
func TestTrap(t *testing.T) {
	ctx, cancel := context.WithTimeout(10*time.Second)
	defer cancel()
	mClock := quartz.NewMock(t)
	trap := mClock.Trap().AfterFunc()
	defer trap.Close() // stop trapping AfterFunc calls

	count := 0
	go mClock.AfterFunc(time.Hour, func(){
		count++
	})
	call := trap.MustWait(ctx)
	call.Release()
	if call.Duration != time.Hour {
		t.Fatal("wrong duration")
	}

	// Now that the async call to AfterFunc has occurred, we can advance the clock to trigger it
	mClock.Advance(call.Duration).MustWait(ctx)
	if count != 1 {
		t.Fatal("wrong count")
	}
}
```

In this test, the trap serves 2 purposes. Firstly, it allows us to capture and assert the duration
passed to the `AfterFunc` call. Secondly, it prevents a race between setting the timer and advancing
it. Since these things happen on different goroutines, if `Advance()` completes before
`AfterFunc()` is called, then the timer never pops in this test.

Any untrapped calls immediately complete using the current time, and calling `Close()` on a trap
causes the mock clock to stop trapping those calls.

You may also `Advance()` the clock between trapping a call and releasing it. The call uses the
current (mocked) time at the moment it is released.

```go
func TestTrap2(t *testing.T) {
	ctx, cancel := context.WithTimeout(10*time.Second)
	defer cancel()
	mClock := quartz.NewMock(t)
	trap := mClock.Trap().Now()
	defer trap.Close() // stop trapping AfterFunc calls

	var logs []string
	done := make(chan struct{})
	go func(clk quartz.Clock){
		defer close(done)
		start := clk.Now()
		phase1()
		p1end := clk.Now()
		logs = append(fmt.Sprintf("Phase 1 took %s", p1end.Sub(start).String()))
		phase2()
		p2end := clk.Now()
		logs = append(fmt.Sprintf("Phase 2 took %s", p2end.Sub(p1end).String()))
	}(mClock)

	// start
	trap.MustWait(ctx).Release()
	// phase 1
	call := trap.MustWait(ctx)
	mClock.Advance(3*time.Second).MustWait(ctx)
	call.Release()
	// phase 2
	call = trap.MustWait(ctx)
	mClock.Advance(5*time.Second).MustWait(ctx)
	call.Release()

	<-done
	// Now logs contains []string{"Phase 1 took 3s", "Phase 2 took 5s"}
}
```

### Tags

When multiple goroutines in the code under test call into the Clock, you can use `tags` to
distinguish them in your traps.

```go
trap := mClock.Trap.Now("foo") // traps any calls that contain "foo"
defer trap.Close()

foo := make(chan time.Time)
go func(){
	foo <- mClock.Now("foo", "bar")
}()
baz := make(chan time.Time)
go func(){
	baz <- mClock.Now("baz")
}()
call := trap.MustWait(ctx)
mClock.Advance(time.Second).MustWait(ctx)
call.Release()
// call.Tags contains []string{"foo", "bar"}

gotFoo := <-foo // 1s after start
gotBaz := <-baz // ?? never trapped, so races with Advance()
```

Tags appear as an optional suffix on all `Clock` methods (type `...string`) and are ignored entirely
by the real clock. They also appear on all methods on returned timers and tickers.

## Recommended Patterns

### Options

We use the Option pattern to inject the mock clock for testing, keeping the call signature in
production clean. The option pattern is compatible with other optional fields as well.

```go
type Option func(*Thing)

// WithTestClock is used in tests to inject a mock Clock
func WithTestClock(clk quartz.Clock) Option {
	return func(t *Thing) {
		t.clock = clk
	}
}

func NewThing(<required args>, opts ...Option) *Thing {
	t := &Thing{
		...
		clock: quartz.NewReal()
	}
	for _, o := range opts {
	  o(t)
	}
	return t
}
```

In tests, this becomes

```go
func TestThing(t *testing.T) {
	mClock := quartz.NewMock(t)
	thing := NewThing(<required args>, WithTestClock(mClock))
	...
}
```

### Tagging convention

Tag your `Clock` method calls as:

```go
func (c *Component) Method() {
	now := c.clock.Now("Component", "Method")
}
```

or

```go
func (c *Component) Method() {
	start := c.clock.Now("Component", "Method", "start")
	...
	end := c.clock.Now("Component", "Method", "end")
}
```

This makes it much less likely that code changes that introduce new components or methods will spoil
existing unit tests.

## Why another time testing library?

Writing good unit tests for components and functions that use the `time` package is difficult, even
though several open source libraries exist. In building Quartz, we took some inspiration from

- [github.com/benbjohnson/clock](https://github.com/benbjohnson/clock)
- Tailscale's [tstest.Clock](https://github.com/coder/tailscale/blob/main/tstest/clock.go)
- [github.com/aspenmesh/tock](https://github.com/aspenmesh/tock)

Quartz shares the high level design of a `Clock` interface that closely resembles the functions in
the `time` standard library, and a "real" clock passes thru to the standard library in production,
while a mock clock gives precise control in testing.

As mentioned in our introduction, our high level goal is to write unit tests that

1. execute quickly
2. don't flake
3. are straightforward to write and understand

For several reasons, this is a tall order when it comes to code that depends on time, and we found
the existing libraries insufficient for our goals.

### Preventing test flakes

The following example comes from the README from benbjohnson/clock:

```go
mock := clock.NewMock()
count := 0

// Kick off a timer to increment every 1 mock second.
go func() {
	ticker := mock.Ticker(1 * time.Second)
	for {
		<-ticker.C
		count++
	}
}()
runtime.Gosched()

// Move the clock forward 10 seconds.
mock.Add(10 * time.Second)

// This prints 10.
fmt.Println(count)
```

The first race condition is fairly obvious: moving the clock forward 10 seconds may generate 10
ticks on the `ticker.C` channel, but there is no guarantee that `count++` executes before
`fmt.Println(count)`.

The second race condition is more subtle, but `runtime.Gosched()` is the tell. Since the ticker
is started on a separate goroutine, there is no guarantee that `mock.Ticker()` executes before
`mock.Add()`. `runtime.Gosched()` is an attempt to get this to happen, but it makes no hard
promises. On a busy system, especially when running tests in parallel, this can flake, advance the
time 10 seconds first, then start the ticker and never generate a tick.

Let's talk about how Quartz tackles these problems.

In our experience, an extremely common use case is creating a ticker then doing a 2-arm `select`
with ticks in one and context expiring in another, i.e.

```go
t := time.NewTicker(duration)
for {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		err := do()
		if err != nil {
			return err
		}
	}
}
```

In Quartz, we refactor this to be more compact and testing friendly:

```go
t := clock.TickerFunc(ctx, duration, do)
return t.Wait()
```

This affords the mock `Clock` the ability to explicitly know when processing of a tick is finished
because it's wrapped in the function passed to `TickerFunc` (`do()` in this example).

In Quartz, when you advance the clock, you are returned an object you can `Wait()` on to ensure all
ticks and timers triggered are finished. This solves the first race condition in the example.

(As an aside, we still support a traditional standard library-style `Ticker`. You may find it useful
if you want to keep your code as close as possible to the standard library, or if you need to use
the channel in a larger `select` block. In that case, you'll have to find some other mechanism to
sync tick processing to your test code.)

To prevent race conditions related to the starting of the ticker, Quartz allows you to set "traps"
for calls that access the clock.

```go
func TestTicker(t *testing.T) {
	mClock := quartz.NewMock(t)
	trap := mClock.Trap().TickerFunc()
	defer trap.Close() // stop trapping at end
	go runMyTicker(mClock) // async calls TickerFunc()
	call := trap.Wait(context.Background()) // waits for a call and blocks its return
	call.Release() // allow the TickerFunc() call to return
	// optionally check the duration using call.Duration
	// Move the clock forward 1 tick
	mClock.Advance(time.Second).MustWait(context.Background())
	// assert results of the tick
}
```

Trapping and then releasing the call to `TickerFunc()` ensures the ticker is started at a
deterministic time, so our calls to `Advance()` will have a predictable effect.

Take a look at `TestExampleTickerFunc` in `example_test.go` for a complete worked example.

### Complex time dependence

Another difficult issue to handle when unit testing is when some code under test makes multiple
calls that depend on the time, and you want to simulate some time passing between them.

A very basic example is measuring how long something took:

```go
var measurement time.Duration
go func(clock quartz.Clock) {
	start := clock.Now()
	doSomething()
	measurement = clock.Since(start)
}(mClock)

// how to get measurement to be, say, 5 seconds?
```

The two calls into the clock happen asynchronously, so we need to be able to advance the clock after
the first call to `Now()` but before the call to `Since()`. Doing this with the libraries we
mentioned above means that you have to be able to mock out or otherwise block the completion of
`doSomething()`.

But, with the trap functionality we mentioned in the previous section, you can deterministically
control the time each call sees.

```go
trap := mClock.Trap().Since()
var measurement time.Duration
go func(clock quartz.Clock) {
	start := clock.Now()
	doSomething()
	measurement = clock.Since(start)
}(mClock)

c := trap.Wait(ctx)
mClock.Advance(5*time.Second)
c.Release()
```

We wait until we trap the `clock.Since()` call, which implies that `clock.Now()` has completed, then
advance the mock clock 5 seconds. Finally, we release the `clock.Since()` call. Any changes to the
clock that happen _before_ we release the call will be included in the time used for the
`clock.Since()` call.

As a more involved example, consider an inactivity timeout: we want something to happen if there is
no activity recorded for some period, say 10 minutes in the following example:

```go
type InactivityTimer struct {
	mu sync.Mutex
	activity time.Time
	clock quartz.Clock
}

func (i *InactivityTimer) Start() {
	i.mu.Lock()
	defer i.mu.Unlock()
	next := i.clock.Until(i.activity.Add(10*time.Minute))
	t := i.clock.AfterFunc(next, func() {
		i.mu.Lock()
		defer i.mu.Unlock()
		next := i.clock.Until(i.activity.Add(10*time.Minute))
		if next == 0 {
			i.timeoutLocked()
			return
		}
		t.Reset(next)
	})
}
```

The actual contents of `timeoutLocked()` doesn't matter for this example, and assume there are other
functions that record the latest `activity`.

We found that some time testing libraries hold a lock on the mock clock while calling the function
passed to `AfterFunc`, resulting in a deadlock if you made clock calls from within.

Others allow this sort of thing, but don't have the flexibility to test edge cases. There is a
subtle bug in our `Start()` function. The timer may pop a little late, and/or some measurable real
time may elapse before `Until()` gets called inside the `AfterFunc`. If there hasn't been activity,
`next` might be negative.

To test this in Quartz, we'll use a trap. We only want to trap the inner `Until()` call, not the
initial one, so to make testing easier we can "tag" the call we want. Like this:

```go
func (i *InactivityTimer) Start() {
	i.mu.Lock()
	defer i.mu.Unlock()
	next := i.clock.Until(i.activity.Add(10*time.Minute))
	t := i.clock.AfterFunc(next, func() {
		i.mu.Lock()
		defer i.mu.Unlock()
		next := i.clock.Until(i.activity.Add(10*time.Minute), "inner")
		if next == 0 {
			i.timeoutLocked()
			return
		}
		t.Reset(next)
	})
}
```

All Quartz `Clock` functions, and functions on returned timers and tickers support zero or more
string tags that allow traps to match on them.

```go
func TestInactivityTimer_Late(t *testing.T) {
	// set a timeout on the test itself, so that if Wait functions get blocked, we don't have to
	// wait for the default test timeout of 10 minutes.
	ctx, cancel := context.WithTimeout(10*time.Second)
	defer cancel()
	mClock := quartz.NewMock(t)
	trap := mClock.Trap.Until("inner")
	defer trap.Close()

	it := &InactivityTimer{
		activity: mClock.Now(),
		clock: mClock,
	}
	it.Start()

	// Trigger the AfterFunc
	w := mClock.Advance(10*time.Minute)
	c := trap.Wait(ctx)
	// Advance the clock a few ms to simulate a busy system
	mClock.Advance(3*time.Millisecond)
	c.Release() // Until() returns
	w.MustWait(ctx) // Wait for the AfterFunc to wrap up

	// Assert that the timeoutLocked() function was called
}
```

This test case will fail with our bugged implementation, since the triggered AfterFunc won't call
`timeoutLocked()` and instead will reset the timer with a negative number. The fix is easy, use
`next <= 0` as the comparison.
