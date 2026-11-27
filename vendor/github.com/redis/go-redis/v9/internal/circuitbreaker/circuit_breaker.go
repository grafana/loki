// Package circuitbreaker provides a circuit breaker implementation for fault tolerance.
package circuitbreaker

import (
	"sync"
	"sync/atomic"
	"time"

	cbq "github.com/redis/go-redis/v9/internal/callbackqueue"
)

// State represents the state of a circuit breaker.
type State int32

const (
	// StateClosed indicates the circuit is closed and requests are allowed.
	StateClosed State = iota
	// StateOpen indicates the circuit is open and requests are blocked.
	StateOpen
	// StateHalfOpen indicates the circuit is testing if the service has recovered.
	StateHalfOpen
)

// String returns the string representation of the circuit state.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Config holds configuration for a circuit breaker.
type Config struct {
	// FailureThreshold is the number of failures before opening the circuit.
	// Default: 5
	FailureThreshold int

	// SuccessThreshold is the number of successes in half-open state before closing.
	// Default: 2
	SuccessThreshold int

	// MaxHalfOpenRequests is the maximum number of requests allowed in half-open state.
	// If 0, uses SuccessThreshold as the limit.
	// Default: 0 (uses SuccessThreshold)
	MaxHalfOpenRequests int

	// OpenTimeout is how long to wait before transitioning from open to half-open.
	// This is the circuit "grace period" that gives a failed database time to
	// self-heal before it is probed again.
	// Default: 60 seconds
	OpenTimeout time.Duration
}

// DefaultConfig returns the default circuit breaker configuration.
func DefaultConfig() Config {
	return Config{
		FailureThreshold:    5,
		SuccessThreshold:    2,
		MaxHalfOpenRequests: 0,
		OpenTimeout:         60 * time.Second,
	}
}

// applyDefaults fills in zero values with defaults.
func (c *Config) applyDefaults() {
	if c.FailureThreshold <= 0 {
		c.FailureThreshold = 5
	}
	if c.SuccessThreshold <= 0 {
		c.SuccessThreshold = 2
	}
	if c.MaxHalfOpenRequests <= 0 {
		c.MaxHalfOpenRequests = c.SuccessThreshold
	}
	if c.OpenTimeout <= 0 {
		c.OpenTimeout = 60 * time.Second
	}
}

// StateChangeCallback is called when the circuit breaker state changes. stats
// is a snapshot taken at the transition, so it reflects the counters that
// triggered the change even though the callback runs asynchronously (see
// cbq): read it instead of calling Stats(), which by delivery time may
// show a later state.
type StateChangeCallback func(oldState, newState State, stats Stats)

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	config Config

	state    atomic.Int32
	failures atomic.Int32
	// externalFailures is the consecutive failure streak of out-of-band
	// signals (RecordFailureForReset), kept apart from the request streak in
	// failures: an external success ends only this one, a successful request
	// ends only the other. Either streak reaching FailureThreshold opens the
	// circuit. See RecordFailureForReset.
	externalFailures atomic.Int32
	successes        atomic.Int32
	requests         atomic.Int32 // Request count in half-open state
	lastFailure      atomic.Int64 // nowNano() stamp (monotonic), 0 = none
	// generation is bumped on every Open->HalfOpen transition, so a reservation
	// (see AllowReserve) taken in one half-open episode can be recognized as
	// stale if it tries to settle after the circuit has cycled through Open and
	// back to a new half-open episode.
	generation atomic.Uint64

	// resetGen is bumped ONLY by Reset (an operator reselect). Out-of-band
	// health probes are not admitted through AllowReserve and hold no reservation,
	// so they gate on this instead: a probe samples resetGen before its checks
	// run and records its verdict only if no Reset intervened. It deliberately
	// does NOT move on Open->HalfOpen — a probe verdict is fresh health info and
	// must still count across a normal recovery transition; only an operator
	// reselect voids it (see RecordFailureForReset).
	resetGen atomic.Uint64

	// transitionMu serializes ALL state transitions and the enqueue of their
	// notifications, so callbacks are delivered in transition (CAS) order. It
	// is held across the counter-clear + CAS + enqueue only, never across the
	// callback itself.
	transitionMu sync.Mutex

	mu        sync.RWMutex
	callbacks []StateChangeCallback

	// cbq delivers state-change callbacks on a single goroutine in FIFO
	// order. Enqueue happens under transitionMu, so the queue order matches the
	// CAS order; the callbacks run OUTSIDE transitionMu, so two concurrent
	// transitions cannot report out of order and a callback may safely re-enter
	// the breaker (RecordFailure/CheckState/Reset) without deadlocking.
	cbq cbq.CallbackQueue
}

// New creates a new circuit breaker with the given configuration.
func New(config Config) *CircuitBreaker {
	config.applyDefaults()
	cb := &CircuitBreaker{
		config: config,
	}
	cb.state.Store(int32(StateClosed))
	return cb
}

// cbEpoch anchors the breaker's clock. time.Since reads Go's monotonic
// clock, so open-timeout math keeps working when the wall clock steps (an
// NTP correction, a VM restore). With wall-clock stamps a backwards step
// made "now - lastFailure" negative and held an open breaker open, with no
// way for probes to recover it, until the wall clock caught up.
var cbEpoch = time.Now()

// nowNano returns the monotonic clock in nanoseconds since cbEpoch. It is
// never 0: lastFailure uses 0 for "no failure recorded". A variable so tests
// can drive the clock without sleeping.
var nowNano = func() int64 { return int64(time.Since(cbEpoch)) + 1 }

// State returns the current state without triggering any transitions.
func (cb *CircuitBreaker) State() State {
	return State(cb.state.Load())
}

// CheckState returns the current state and may trigger state transitions.
// Use this when you need to check if requests should be allowed.
func (cb *CircuitBreaker) CheckState() State {
	state := State(cb.state.Load())
	if state == StateOpen {
		// Guard against a zero timestamp (no failure recorded yet) so we don't
		// treat the Unix epoch as the last failure and transition immediately.
		lastFailure := cb.lastFailure.Load()
		if lastFailure != 0 && nowNano()-lastFailure >= int64(cb.config.OpenTimeout) {
			cb.transitionMu.Lock()
			cb.maybeHalfOpenLocked()
			cb.transitionMu.Unlock()
		}
	}
	return State(cb.state.Load())
}

// maybeHalfOpenLocked performs the Open -> HalfOpen transition when the grace
// period has elapsed. transitionMu MUST be held.
//
// Serializing this transition under the same lock that the half-open
// reservation operations hold (AllowReserve, RecordSuccessFor, ReleaseFor,
// RecordFailureFor) is what makes the generation and the half-open counters
// move atomically with reservation admission and settlement: a reservation
// cannot observe a torn state where the generation has advanced but the counter
// has not (or vice versa), which is the whole class of TOCTOU races the
// lock-free version kept producing.
func (cb *CircuitBreaker) maybeHalfOpenLocked() {
	// Re-read lastFailure under the lock: a failure recorded after the caller's
	// check (e.g. from a request admitted before the circuit opened) must
	// restart the grace period — transitioning off the stale timestamp would
	// probe the endpoint early.
	lastFailure := cb.lastFailure.Load()
	if State(cb.state.Load()) != StateOpen ||
		lastFailure == 0 || nowNano()-lastFailure < int64(cb.config.OpenTimeout) {
		return
	}
	// Clear the half-open counters and advance the generation BEFORE the CAS
	// publishes half-open, so a reservation that observes the new state reads a
	// clean count and the new generation.
	cb.successes.Store(0)
	cb.requests.Store(0)
	cb.generation.Add(1)
	// CAS, not Store: a concurrent Reset may have just published Closed, and
	// overwriting it with HalfOpen would silently undo the reset.
	if cb.state.CompareAndSwap(int32(StateOpen), int32(StateHalfOpen)) {
		// Snapshot + enqueue under the lock so callback order matches CAS order;
		// the callback itself runs on the cbq goroutine.
		stats := cb.Stats()
		cb.cbq.Dispatch(func() { cb.notifyCallbacks(StateOpen, StateHalfOpen, stats) })
	}
}

// IsAllowed returns true if a request should be allowed through.
// This is a convenience method that combines CheckState with half-open request limiting.
func (cb *CircuitBreaker) IsAllowed() bool {
	allowed, _ := cb.Allow()
	return allowed
}

// Allow reports whether a request may proceed and whether the admission
// reserved a bounded half-open probe slot. Closed-state admissions reserve
// nothing, so callers whose operation may outlive a later open -> half-open
// transition (for example a WATCH transaction) must consult reserved before
// calling ReleaseHalfOpen — an unconditional release would free a slot a
// real recovery probe is holding.
func (cb *CircuitBreaker) Allow() (allowed, reserved bool) {
	// Same admission as AllowReserve: the half-open reservation is taken
	// under transitionMu, so no transition can land between the reservation
	// and the decision. The earlier lock-free form reserved, then re-checked
	// the state and rejected on any change, which also rejected a request
	// when a concurrent success had just CLOSED the circuit: a closed circuit
	// must admit. The reservation's generation is not needed here; the legacy
	// settle calls (RecordSuccess, RecordFailure, ReleaseHalfOpen) work on
	// the shared counters.
	ok, r := cb.AllowReserve()
	return ok, r.held
}

// Reservation identifies a single admission returned by AllowReserve. It carries
// the half-open episode (generation) the admission belongs to and a once-only
// settle guard, so that:
//   - a reservation taken in one half-open episode cannot settle against a later
//     one (the admitted request outlived an open -> half-open cycle), and
//   - a reservation shared by several outcomes (e.g. a pipeline batch) settles
//     the half-open slot exactly once, however many outcomes report success.
//
// The zero Reservation (held == false) is what a closed-state admission returns:
// it holds no slot, and RecordSuccessFor treats it as an external success.
type Reservation struct {
	gen     uint64
	held    bool
	settled *atomic.Bool
}

// Generation identifies the breaker episode in which the command was admitted.
func (r Reservation) Generation() uint64 { return r.gen }

// AllowReserve is Allow with an identity-bearing reservation. Prefer it over
// Allow when the admitted operation may outlive a later open -> half-open
// transition, or when several outcomes share one admission: the returned
// Reservation lets RecordSuccessFor / ReleaseFor settle the half-open slot
// exactly once and only within the episode the slot was reserved in.
func (cb *CircuitBreaker) AllowReserve() (allowed bool, r Reservation) {
	// Load the generation before the state: a Reset between the two loads
	// then leaves the admission with the old generation, so its outcome is
	// dropped as stale instead of counting in the fresh episode.
	gen := cb.generation.Load()
	state := State(cb.state.Load())
	if state == StateClosed {
		// Stamp the generation even on a closed (slot-less) admission: the
		// !held settle paths use it to drop an outcome that outlived a
		// reselect/recovery cycle (see RecordSuccessFor/RecordFailureFor).
		return true, Reservation{gen: gen}
	}
	// A plain Open whose grace period has not elapsed is the common rejected
	// case — keep it lock-free. Only the half-open lifecycle takes the lock.
	if state == StateOpen {
		lastFailure := cb.lastFailure.Load()
		if lastFailure == 0 || nowNano()-lastFailure < int64(cb.config.OpenTimeout) {
			return false, Reservation{}
		}
	}
	// HalfOpen, or Open with the grace elapsed: under transitionMu, run any
	// pending Open -> HalfOpen transition and admit atomically. Because the
	// generation, the counter and the state cannot move while the lock is held,
	// there is no admit-then-recheck window: the reservation is bound to exactly
	// the episode it was admitted into (no stale generation, no phantom slot).
	cb.transitionMu.Lock()
	cb.maybeHalfOpenLocked()
	if State(cb.state.Load()) != StateHalfOpen {
		st := State(cb.state.Load())
		gen = cb.generation.Load()
		cb.transitionMu.Unlock()
		// Closed admission (slot-less) carries the generation like the lock-free
		// path above; a non-Closed st means rejected, and its reservation is
		// never settled.
		return st == StateClosed, Reservation{gen: gen}
	}
	if requests := cb.requests.Add(1); int(requests) > cb.config.MaxHalfOpenRequests {
		cb.requests.Add(-1)
		cb.transitionMu.Unlock()
		return false, Reservation{}
	}
	gen = cb.generation.Load()
	cb.transitionMu.Unlock()
	return true, Reservation{gen: gen, held: true, settled: new(atomic.Bool)}
}

// CurrentClosedReservation returns the slot-less reservation a closed-state
// AllowReserve would hand out now, and false when the circuit is not closed.
// It is for owners that admit work with AllowReserve but cannot carry the
// reservation to where the outcome lands (the MultiDB full-duplex router: the
// outcome comes from the engine's reader). Settling with it records the outcome
// in the current closed episode, and nothing while the circuit is open or
// half-open, so a late outcome cannot re-open or close a recovery episode it
// never probed.
//
// The state and generation are read as a seqlock: generation, state,
// generation again. Every transition into a new episode (Reset, Open ->
// HalfOpen) bumps the generation before it publishes the new state, so an
// unchanged generation around a Closed read means the generation belongs to
// that closed episode. Without the re-check, a Closed read followed by a
// Closed -> Open -> HalfOpen cycle would pair the old Closed state with the
// new episode's generation. Lock-free: this runs once per reply.
func (cb *CircuitBreaker) CurrentClosedReservation() (Reservation, bool) {
	gen := cb.generation.Load()
	if State(cb.state.Load()) != StateClosed || cb.generation.Load() != gen {
		return Reservation{}, false
	}
	return Reservation{gen: gen}, true
}

// Held reports whether the reservation holds a half-open probe slot (a
// half-open admission) rather than being a slot-less closed admission.
func (r Reservation) Held() bool {
	return r.held
}

// RecordSuccessFor settles a successful outcome for a reservation from
// AllowReserve. A half-open reservation releases (or, at SuccessThreshold,
// closes on) its slot at most once, and only while it is still the current
// half-open episode; a stale reservation — the circuit cycled open -> half-open
// since it was taken — records nothing. A closed-state reservation (held ==
// false) clears the failure count while the circuit is still closed and
// records nothing once it has moved on: its successes predate the failures
// that opened the circuit and are no evidence of recovery, and counting them
// would let one in-flight batch close a half-open episode it never probed.
// Out-of-band evidence goes through RecordExternalSuccess, which does count.
func (cb *CircuitBreaker) RecordSuccessFor(r Reservation) {
	if !r.held {
		// Clear the failure count only while this is still the SAME closed
		// episode the admission was taken in: the generation must be unchanged
		// AND the circuit still closed. Every Closed -> ... -> Closed round trip
		// bumps the generation (Reset, and HalfOpen->Closed via the bump in
		// maybeHalfOpenLocked), so a stale generation means a reselect or a
		// recovery cycle happened since admission and this success predates it.
		// Fast path, lock-free: nothing to clear. This is the common case
		// on a healthy member, so the lock below costs nothing there.
		if cb.failures.Load() == 0 {
			return
		}
		// Clear under transitionMu, atomically with Reset: a lock-free
		// check-then-store could pass the checks, be descheduled across a
		// Reset, and then wipe failures recorded against the freshly reset
		// member, delaying its opening.
		cb.transitionMu.Lock()
		if r.gen == cb.generation.Load() && State(cb.state.Load()) == StateClosed {
			cb.failures.Store(0)
		}
		cb.transitionMu.Unlock()
		return
	}
	if r.settled.Swap(true) {
		return // already settled by an earlier outcome sharing this reservation
	}
	// Under transitionMu the generation check and the settlement are atomic: the
	// episode cannot cycle between them, so this settles exactly the episode the
	// reservation was admitted into (or nothing, if it is already stale).
	cb.transitionMu.Lock()
	if r.gen == cb.generation.Load() {
		cb.recordSuccessHalfOpenLocked(true)
	}
	cb.transitionMu.Unlock()
}

// ReleaseFor returns a half-open reservation's slot when the outcome was neither
// a recordable success nor failure. Like RecordSuccessFor it settles at most
// once and only within the reservation's own half-open episode; a closed-state
// or already-settled reservation is a no-op.
func (cb *CircuitBreaker) ReleaseFor(r Reservation) {
	if !r.held || r.settled.Swap(true) {
		return
	}
	cb.transitionMu.Lock()
	if r.gen == cb.generation.Load() {
		cb.releaseHalfOpenLocked()
	}
	cb.transitionMu.Unlock()
}

// RecordFailureFor records a failure for a reservation from AllowReserve. A
// half-open reservation records only while it is still the CURRENT half-open
// episode: a failure from an admission that outlived an open -> half-open cycle
// must not re-open (and abort the recovery of) the NEW episode — the symmetric
// guard to RecordSuccessFor. A closed-state reservation (held == false) records
// only while it is still the SAME closed episode it was admitted in: the
// generation must be unchanged AND the circuit still closed. If it has moved to
// open or half-open, or a Reset (reselect) bumped the generation and returned to
// closed, this failure predates the current episode and must not re-open it (a
// stale probe failure aborting a recovery, or reversing an operator reselect).
// Every Closed -> ... -> Closed round trip bumps the generation (Reset, and
// HalfOpen->Closed via maybeHalfOpenLocked's bump), so the generation check is
// what distinguishes "still the admission's closed episode" from "a new one".
// Unlike success/release this does not consume the once-only settle flag — a
// failure opens the circuit rather than freeing a slot, and the batch path
// records one failure per command.
func (cb *CircuitBreaker) RecordFailureFor(r Reservation) {
	if !r.held {
		// Under transitionMu, unlike the success path: a failure MUTATES state,
		// so the generation+state check and the record must be atomic — a Reset
		// landing between an unlocked check and the record would let a stale
		// failure re-open the freshly reset breaker (the exact bug). Closed-state
		// failures are rare, so the lock is cheap here; the hot path is the
		// lock-free success side.
		cb.transitionMu.Lock()
		if r.gen == cb.generation.Load() && State(cb.state.Load()) == StateClosed {
			cb.lastFailure.Store(nowNano())
			if int(cb.failures.Add(1)) >= cb.config.FailureThreshold {
				cb.openFromClosedLocked()
			}
		}
		cb.transitionMu.Unlock()
		return
	}
	// Same-episode failures re-open under the lock, atomically with the
	// generation check; a stale reservation records nothing.
	cb.transitionMu.Lock()
	if r.gen == cb.generation.Load() {
		cb.lastFailure.Store(nowNano())
		cb.recordFailureHalfOpenLocked()
	}
	cb.transitionMu.Unlock()
}

// ReleaseHalfOpen returns a half-open request slot previously reserved by a
// successful IsAllowed call when the operation produced neither a recordable
// success nor failure (for example, it was aborted for an unrelated reason).
// Without this, a reserved-but-never-completed probe could permanently starve
// half-open recovery once MaxHalfOpenRequests slots are exhausted. It only has
// an effect while the breaker is half-open.
func (cb *CircuitBreaker) ReleaseHalfOpen() {
	cb.transitionMu.Lock()
	cb.releaseHalfOpenLocked()
	cb.transitionMu.Unlock()
}

// releaseHalfOpenLocked frees a half-open slot. transitionMu MUST be held.
func (cb *CircuitBreaker) releaseHalfOpenLocked() {
	if State(cb.state.Load()) != StateHalfOpen {
		return
	}
	if cb.requests.Add(-1) < 0 {
		cb.requests.Store(0)
	}
}

// RecordSuccess records a successful operation that was admitted through
// IsAllowed. In half-open state the completed probe's admission slot is
// released when the circuit does not close.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.recordSuccess(true)
}

// RecordExternalSuccess records a successful operation that was NOT admitted
// through IsAllowed (e.g. an out-of-band health check). It counts toward
// closing a half-open circuit but never releases an admission slot it did
// not hold — releasing one would let more than MaxHalfOpenRequests requests
// reach a recovering service.
func (cb *CircuitBreaker) RecordExternalSuccess() {
	cb.recordSuccess(false)
}

// ResetGeneration returns the current reset generation. A health probe samples
// it BEFORE running its checks and passes it to RecordExternalSuccessForReset /
// RecordFailureForReset, which drop the verdict if a Reset (an operator reselect)
// bumped the generation meanwhile. Unlike the reservation generation, this moves
// only on Reset, so a probe verdict still counts across a normal Open->HalfOpen.
func (cb *CircuitBreaker) ResetGeneration() uint64 {
	return cb.resetGen.Load()
}

// RecordExternalSuccessForReset records an out-of-band success (a health probe)
// unless a Reset has run since gen was sampled. Like RecordExternalSuccess it
// holds no admission slot. The generation check and the record run under
// transitionMu — the same lock Reset holds across its counter clear + generation
// bump — so a reselect cannot interleave between them.
func (cb *CircuitBreaker) RecordExternalSuccessForReset(gen uint64) {
	cb.transitionMu.Lock()
	defer cb.transitionMu.Unlock()
	if cb.resetGen.Load() != gen {
		return
	}
	cb.recordExternalSuccessLocked()
}

// recordExternalSuccessLocked applies an out-of-band success with
// transitionMu held. In half-open it counts toward SuccessThreshold like any
// success and never touches an admission slot. In closed it ends the
// EXTERNAL streak only: an external check passing shows the endpoint is
// reachable, not that requests succeed, so it must not clear the request
// streak. Otherwise an endpoint that passes its checks but fails every
// request, at a rate below FailureThreshold per check interval, would have
// its count wiped by every check and never open. Open is a no-op.
func (cb *CircuitBreaker) recordExternalSuccessLocked() {
	switch State(cb.state.Load()) {
	case StateHalfOpen:
		cb.recordSuccessHalfOpenLocked(false)
	case StateClosed:
		cb.externalFailures.Store(0)
	}
}

// RecordFailureForReset records an out-of-band failure (a health probe) unless a
// Reset has run since gen was sampled — the guard that stops a probe verdict
// sampled before an operator reselect from re-opening the freshly selected
// member (at FailureThreshold 1 a single stale failure would). The check and the
// record run under transitionMu, so they are atomic with Reset.
func (cb *CircuitBreaker) RecordFailureForReset(gen uint64) {
	cb.transitionMu.Lock()
	defer cb.transitionMu.Unlock()
	if cb.resetGen.Load() != gen {
		return
	}
	cb.lastFailure.Store(nowNano())
	switch State(cb.state.Load()) {
	case StateClosed:
		// External failures form their own streak. A caller that sends no
		// requests has no request success to clear a shared count, so
		// counting external failures into the request streak let isolated
		// external failures, separated by any number of external successes,
		// add up to an open circuit; the external successes end this streak
		// instead.
		if int(cb.externalFailures.Add(1)) >= cb.config.FailureThreshold {
			cb.openFromClosedLocked()
		}
	case StateHalfOpen:
		cb.recordFailureHalfOpenLocked()
	}
}

func (cb *CircuitBreaker) recordSuccess(heldSlot bool) {
	// Closed is the hot path (every successful command clears the failure
	// count): keep it lock-free. The half-open lifecycle is serialized under
	// transitionMu, the same lock the reservation operations hold.
	if State(cb.state.Load()) == StateClosed {
		cb.failures.Store(0)
		return
	}
	cb.transitionMu.Lock()
	cb.recordSuccessHalfOpenLocked(heldSlot)
	cb.transitionMu.Unlock()
}

// recordSuccessHalfOpenLocked applies a success with transitionMu held. In
// half-open it counts toward SuccessThreshold (closing the circuit) and, for a
// slot-holding admission that does not close it, releases the slot. In closed
// it clears the failure count; Open is a no-op.
func (cb *CircuitBreaker) recordSuccessHalfOpenLocked(heldSlot bool) {
	switch State(cb.state.Load()) {
	case StateHalfOpen:
		successes := cb.successes.Add(1)
		if int(successes) >= cb.config.SuccessThreshold {
			// Clear the failure counters BEFORE Closed becomes visible: they
			// still hold the counts that opened the circuit.
			cb.failures.Store(0)
			cb.externalFailures.Store(0)
			if cb.state.CompareAndSwap(int32(StateHalfOpen), int32(StateClosed)) {
				// Snapshot BEFORE clearing the half-open counters so the callback
				// observes the success count that triggered the close.
				stats := cb.Stats()
				cb.cbq.Dispatch(func() { cb.notifyCallbacks(StateHalfOpen, StateClosed, stats) })
				cb.successes.Store(0)
				cb.requests.Store(0)
			}
			return
		}
		if heldSlot {
			// The probe completed but the circuit is still half-open: give
			// its admission slot back, so MaxHalfOpenRequests bounds
			// CONCURRENT probes rather than a lifetime budget. Without this,
			// a MaxHalfOpenRequests lower than SuccessThreshold could never
			// accumulate enough successes to close the circuit.
			cb.releaseHalfOpenLocked()
		}
	case StateClosed:
		// Reset failure count on success
		cb.failures.Store(0)
	}
}

// RecordFailure records a failed operation.
func (cb *CircuitBreaker) RecordFailure() {
	cb.lastFailure.Store(nowNano())
	switch State(cb.state.Load()) {
	case StateClosed:
		// Hot path: count lock-free, take the lock only to open at the threshold.
		if int(cb.failures.Add(1)) >= cb.config.FailureThreshold {
			cb.transitionMu.Lock()
			// Re-read the count under the lock: a Reset (SetActiveDatabase) may
			// have zeroed it since the lock-free Add crossed the threshold, and
			// opening off that stale reading would immediately undo an operator
			// re-selection. openFromClosedLocked's CAS alone cannot tell the
			// difference — Reset already swapped back to Closed.
			if int(cb.failures.Load()) >= cb.config.FailureThreshold {
				cb.openFromClosedLocked()
			}
			cb.transitionMu.Unlock()
		}
	case StateHalfOpen:
		cb.transitionMu.Lock()
		cb.recordFailureHalfOpenLocked()
		cb.transitionMu.Unlock()
	}
	// Open: the timestamp store above already restarted the grace period.
	//
	// The old lost-CAS retry loop is gone: half-open closes (recordSuccessHalfOpenLocked)
	// and opens (recordFailureHalfOpenLocked) now both run under transitionMu, so
	// they are serialized and no CAS can be lost to a concurrent transition.
}

// openFromClosedLocked performs the Closed -> Open transition. transitionMu MUST
// be held.
func (cb *CircuitBreaker) openFromClosedLocked() {
	if cb.state.CompareAndSwap(int32(StateClosed), int32(StateOpen)) {
		// A Reset that completed between the timestamp store in the caller and
		// this CAS wiped lastFailure; repair it (CAS so a concurrent newer
		// failure's timestamp is kept), or the zero-timestamp guard in
		// CheckState would wedge the circuit open.
		cb.lastFailure.CompareAndSwap(0, nowNano())
		stats := cb.Stats()
		cb.cbq.Dispatch(func() { cb.notifyCallbacks(StateClosed, StateOpen, stats) })
		// successes/requests are 0 in Closed already; reset defensively so the
		// invariant "clean on entry to Open" holds across all transitions.
		cb.successes.Store(0)
		cb.requests.Store(0)
	}
}

// recordFailureHalfOpenLocked opens the circuit from half-open (any failure in
// half-open re-opens). transitionMu MUST be held. If the episode already closed
// (a settling success won under the same lock before this call), the failure is
// treated as a fresh closed-state failure.
func (cb *CircuitBreaker) recordFailureHalfOpenLocked() {
	switch State(cb.state.Load()) {
	case StateHalfOpen:
		if cb.state.CompareAndSwap(int32(StateHalfOpen), int32(StateOpen)) {
			cb.lastFailure.CompareAndSwap(0, nowNano())
			stats := cb.Stats()
			cb.cbq.Dispatch(func() { cb.notifyCallbacks(StateHalfOpen, StateOpen, stats) })
			cb.successes.Store(0)
			cb.requests.Store(0)
		}
	case StateClosed:
		if int(cb.failures.Add(1)) >= cb.config.FailureThreshold {
			cb.openFromClosedLocked()
		}
	}
}

// ForceOpen transitions the breaker straight to Open, regardless of the current
// failure count. For callers that have already determined a member is down (for
// example a failed startup or health probe) and want to open the circuit
// without synthesizing FailureThreshold individual RecordFailure calls.
func (cb *CircuitBreaker) ForceOpen() {
	cb.transitionMu.Lock()
	// Store lastFailure UNDER the lock, before the swap: a concurrent Reset
	// (also under the lock) must not be able to clear it between the store and
	// the swap. Otherwise the swap could publish Open with lastFailure == 0,
	// which CheckState refuses to advance to half-open, wedging the circuit open
	// forever past OpenTimeout.
	cb.lastFailure.Store(nowNano())
	// Swap, capturing the old state for the callback; skip the notify (and the
	// counter clear) if it was already Open, so a redundant ForceOpen is a no-op.
	old := State(cb.state.Swap(int32(StateOpen)))
	if old != StateOpen {
		// successes/requests are meaningless in Open; clear them so the next
		// Open -> HalfOpen episode starts clean, matching the other transitions.
		cb.successes.Store(0)
		cb.requests.Store(0)
		stats := cb.Stats()
		cb.cbq.Dispatch(func() { cb.notifyCallbacks(old, StateOpen, stats) })
	}
	cb.transitionMu.Unlock()
}

// OnStateChange registers a callback to be called when the state changes.
func (cb *CircuitBreaker) OnStateChange(callback StateChangeCallback) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.callbacks = append(cb.callbacks, callback)
}

// notifyCallbacks notifies all registered callbacks of a state change. It runs
// on the cbq goroutine (never under transitionMu), so a callback may
// re-enter the breaker without deadlocking.
func (cb *CircuitBreaker) notifyCallbacks(oldState, newState State, stats Stats) {
	cb.mu.RLock()
	callbacks := make([]StateChangeCallback, len(cb.callbacks))
	copy(callbacks, cb.callbacks)
	cb.mu.RUnlock()

	for _, callback := range callbacks {
		// Recover per callback: the queue only recovers around this whole
		// batch, so a panic here would otherwise unwind past the remaining
		// callbacks and skip them for this transition (and every future one,
		// since a deterministic panic recurs on the same callback each time).
		cbq.RunSafely(func() { callback(oldState, newState, stats) })
	}
}

// Reset resets the circuit breaker to closed state.
// If the circuit was not already closed, callbacks are notified.
func (cb *CircuitBreaker) Reset() {
	// Clear the counters BEFORE Closed becomes visible: a failure recorded
	// right after the swap must count against a fresh counter — off the
	// stale one it could immediately re-open the circuit, and the
	// lastFailure wipe below would then wedge it open past the
	// zero-timestamp guard in CheckState.
	// Hold transitionMu across the clear + swap + enqueue so Reset serializes
	// with the other transitions and its notification keeps CAS order.
	cb.transitionMu.Lock()
	cb.failures.Store(0)
	cb.externalFailures.Store(0)
	cb.successes.Store(0)
	cb.requests.Store(0)
	cb.lastFailure.Store(0)
	// Advance the generation so any in-flight half-open reservation from the
	// episode being reset is now stale: without this, a reservation handed out
	// before the Reset still matches the generation and RecordFailureFor would
	// count its failure against the freshly closed circuit, re-opening the
	// operator-selected member immediately (FailureThreshold == 1).
	cb.generation.Add(1)
	// Bump the probe generation too: a health probe sampled before this reselect
	// must not record its (now stale) verdict against the freshly closed circuit.
	cb.resetGen.Add(1)
	oldState := State(cb.state.Swap(int32(StateClosed)))
	if oldState != StateClosed {
		stats := cb.Stats()
		cb.cbq.Dispatch(func() { cb.notifyCallbacks(oldState, StateClosed, stats) })
	}
	cb.transitionMu.Unlock()
}

// Stats returns current statistics for monitoring.
type Stats struct {
	State State
	// Failures is the consecutive failure streak of admitted requests
	// (RecordFailure, RecordFailureFor); a successful request ends it.
	Failures int32
	// ExternalFailures is the consecutive failure streak of out-of-band
	// signals (RecordFailureForReset, e.g. a health check); an external
	// success ends it. Either streak reaching FailureThreshold opens the
	// circuit.
	ExternalFailures int32
	Successes        int32
	Requests         int32
	LastFailureTime  time.Time
}

// Stats returns current statistics.
func (cb *CircuitBreaker) Stats() Stats {
	lastFailure := cb.lastFailure.Load()
	var lastFailureTime time.Time
	if lastFailure > 0 {
		lastFailureTime = cbEpoch.Add(time.Duration(lastFailure - 1))
	}

	return Stats{
		State:            cb.State(),
		Failures:         cb.failures.Load(),
		ExternalFailures: cb.externalFailures.Load(),
		Successes:        cb.successes.Load(),
		Requests:         cb.requests.Load(),
		LastFailureTime:  lastFailureTime,
	}
}

// Execute runs the given function with circuit breaker protection.
// Returns ErrCircuitOpen if the circuit is open and not ready for testing.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	// Settle through the reservation, so an fn that outlives an open ->
	// half-open cycle settles nothing against the new episode: neither a
	// failure that would re-open it, nor a success that would count toward
	// closing it or free a slot it never held.
	allowed, res := cb.AllowReserve()
	if !allowed {
		return ErrCircuitOpen
	}

	err := fn()
	if err != nil {
		cb.RecordFailureFor(res)
		return err
	}
	cb.RecordSuccessFor(res)
	return nil
}

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = &CircuitOpenError{}

// CircuitOpenError indicates the circuit breaker is open.
type CircuitOpenError struct{}

func (e *CircuitOpenError) Error() string {
	return "circuit breaker is open"
}
