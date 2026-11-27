package redis

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9/internal"
	cbq "github.com/redis/go-redis/v9/internal/callbackqueue"
	"github.com/redis/go-redis/v9/internal/failuredetector"
	"github.com/redis/go-redis/v9/internal/otel"
	"github.com/redis/go-redis/v9/internal/pool"
	"github.com/redis/go-redis/v9/maintnotifications"
	"github.com/redis/go-redis/v9/push"

	imultidb "github.com/redis/go-redis/v9/internal/multidb"
)

// multidbDatabase is one member database: its client, circuit breaker,
// resolved health checks and metadata.
type multidbDatabase struct {
	// exactly one of c / cc is set; client points at the same object through
	// the shared Process/Close surface.
	c  *Client
	cc *ClusterClient

	rec    otel.Recorder // the core's recorder, for this member's health-check metrics
	cb     *imultidb.CircuitBreaker
	weight float64 // guarded by core.dbMu
	fqdn   string
	// id is the database's stable identifier and its key in core.dbs. Assigned
	// once at membership time (before the initial probe), immutable, and never
	// reused, so callbacks and the public API can name a member with an int
	// that a RemoveDatabase never invalidates or renumbers. Read lock-free.
	id int

	checks []MultiDBHealthCheck
	policy MultiDBHealthCheckPolicy

	// removed is set (under core.failoverMu) when the database leaves the
	// membership; a stale probe snapshot must stop recording outcomes and
	// firing callbacks for it.
	removed atomic.Bool

	// noFallbackBefore (multidbNowNano stamp, monotonic) suppresses
	// auto-fallback TO this member until the deadline. It is (re-)armed
	// whenever an automatic failover
	// vacates the member, so auto-fallback cannot immediately undo a
	// detector- or breaker-driven failover — a flaky higher-weight primary
	// whose rate trips the detector (without its breaker ever opening) would
	// otherwise ping-pong. Re-checked lock-free in tryFallbackToPrimary.
	noFallbackBefore atomic.Int64

	// onRemove runs when RemoveDatabase takes the member out, before its
	// client is closed. Full-duplex routers register here to drain and close
	// the private child they built for the member (see multidbFDRouter).
	onRemove onCloseHooks

	// cbq delivers user circuit-state callbacks asynchronously (FIFO). Breaker
	// state changes fire deep inside locked sections (failoverMu is held
	// during manual selection, pre-failover probes and AddDatabase probes);
	// invoking a user callback there would self-deadlock the moment the
	// callback touches a control API that takes the same lock.
	cbq cbq.CallbackQueue
}

// recorder returns where the MultiDB client's metrics go. A hand-built value (tests) leaves
// rec nil, which means the global recorder.
func (c *multidbCore) recorder() otel.Recorder {
	if c.rec == nil {
		return otel.Scoped(nil)
	}
	return c.rec
}

// recorder returns where the member's metrics go. A hand-built value (tests) leaves
// rec nil, which means the global recorder.
func (db *multidbDatabase) recorder() otel.Recorder {
	if db.rec == nil {
		return otel.Scoped(nil)
	}
	return db.rec
}

func (db *multidbDatabase) process(ctx context.Context, cmd Cmder) error {
	if db.cc != nil {
		return db.cc.Process(ctx, cmd)
	}
	return db.c.Process(ctx, cmd)
}

func (db *multidbDatabase) closeClient() error {
	if db.cc != nil {
		return db.cc.Close()
	}
	return db.c.Close()
}

// selectable reports whether the database's circuit permits selecting it,
// WITHOUT reserving a half-open probe slot. IsAllowed consumes one of the
// breaker's bounded half-open requests, so it must only be called right
// before actually executing a command; candidate snapshots, background
// checks and failover re-checks use this instead, or repeated selections
// would exhaust a recovering database's probe budget without ever probing it.
func (db *multidbDatabase) selectable() bool {
	return db.cb.CheckState() != imultidb.CircuitOpen
}

// disabledLocked reports whether the member has weight -1 and must never be
// selected automatically. Caller holds core.dbMu.
func (db *multidbDatabase) disabledLocked() bool {
	return db.weight < 0
}

// failbackOnlyHealthCheck marks health checks whose verdict may only gate
// routing traffic TO a member (candidate probes, auto-fallback, initial
// selection) and must never evict the current active — the lag-aware REST
// check is the canonical case: replication lag on the member already
// serving traffic is not an availability signal, and failover rides on
// traffic signals instead. Satisfied structurally (multidb.LagAwareHealthCheck
// and custom checks alike).
type failbackOnlyHealthCheck interface {
	FailbackOnly() bool
}

// isFailbackOnly reports a check's FailbackOnly marker, recovering a panicking
// custom implementation: this runs on the background loop, and treating a
// panic as "not fail-back-only" keeps the check in the eviction set (the safe
// default — a broken marker cannot silently exempt a member from health gating).
func isFailbackOnly(check MultiDBHealthCheck) (fb bool) {
	marker, ok := check.(failbackOnlyHealthCheck)
	if !ok {
		return false
	}
	defer func() {
		if r := recover(); r != nil {
			internal.Logger.Printf(context.Background(), "multidb: FailbackOnly marker panicked: %v", r)
			fb = false
		}
	}()
	return marker.FailbackOnly()
}

// probe runs the database's health checks under the configured policy,
// bounded by HealthCheckTimeout, and feeds the result into the circuit
// breaker and the OTel recorder.
func (db *multidbDatabase) probe(parent context.Context, timeout time.Duration) bool {
	return db.probeWith(parent, timeout, db.checks)
}

// nonFailbackChecks returns the checks whose verdict may evict the member —
// everything except fail-back-only checks (e.g. the lag-aware REST check),
// which gate routing TO a member but never the one already serving traffic.
// With only fail-back-only checks configured it falls back to the default
// PING: an active member must keep a liveness probe, or an idle or
// Pub/Sub-only client would never notice that the endpoint died (configuring
// any check suppresses the default, and Pub/Sub errors do not feed the
// breaker).
func (db *multidbDatabase) nonFailbackChecks() []MultiDBHealthCheck {
	filtered := make([]MultiDBHealthCheck, 0, len(db.checks))
	for _, check := range db.checks {
		if isFailbackOnly(check) {
			continue
		}
		filtered = append(filtered, check)
	}
	if len(filtered) == 0 {
		return []MultiDBHealthCheck{defaultPingHealthCheck{}}
	}
	return filtered
}

// probeAsActive probes the CURRENTLY ACTIVE member: fail-back-only checks
// are excluded, so a lag breach cannot open the active's breaker and evict
// the member traffic is flowing through. nonFailbackChecks guarantees at
// least the default PING runs, so the recorded verdict is never vacuous.
func (db *multidbDatabase) probeAsActive(parent context.Context, timeout time.Duration) {
	db.probeWith(parent, timeout, db.nonFailbackChecks())
}

// probeExcludingFailbackOnly runs the member's non-fail-back-only checks (at
// least the default PING) and reports whether it is healthy, used to gate a
// re-selection of the current active: a lag breach must not fail selecting the
// member already serving traffic, but a dead endpoint must.
func (db *multidbDatabase) probeExcludingFailbackOnly(parent context.Context, timeout time.Duration) bool {
	return db.probeWith(parent, timeout, db.nonFailbackChecks())
}

// runPolicy executes the health-check policy over checks under ctx, recovering
// a panicking custom policy (treated as unhealthy): probes run on the background
// loop and during construction, where an escaped panic would crash the process.
// (The default policy also recovers each check; see imultidb.RunChecks.)
func (db *multidbDatabase) runPolicy(ctx context.Context, checks []MultiDBHealthCheck) (h bool) {
	defer func() {
		if r := recover(); r != nil {
			internal.Logger.Printf(ctx, "multidb: health-check policy panicked: %v", r)
			h = false
		}
	}()
	if db.cc != nil {
		return db.policy.ExecuteCluster(ctx, checks, db.cc)
	}
	return db.policy.Execute(ctx, checks, db.c)
}

// checkHealthyNoRecord runs checks under a fresh timeout and returns the verdict
// WITHOUT recording it on the breaker or the OTel recorder. Use it when a
// verdict must gate a decision (auto-fallback) but must not feed the breaker:
// recording a fail-back-only check (lag) would let it evict a member from
// failover, the invariant failbackOnlyHealthCheck exists to prevent. A canceled
// parent or an exceeded probe deadline both report unhealthy (don't act).
func (db *multidbDatabase) checkHealthyNoRecord(parent context.Context, timeout time.Duration, checks []MultiDBHealthCheck) bool {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	healthy := db.runPolicy(ctx, checks)
	if parent.Err() != nil || ctx.Err() != nil {
		return false
	}
	return healthy
}

func (db *multidbDatabase) probeWith(parent context.Context, timeout time.Duration, checks []MultiDBHealthCheck) bool {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	// Sample the reset generation BEFORE the checks run: an operator reselect
	// (SetActiveDatabase/ForceActiveDatabase Resets the breaker) that lands while
	// this probe is in flight must void the probe's verdict, so the record below
	// is gated on this generation being unchanged. Probes hold no reservation, so
	// this is their analogue of AllowReserve's generation stamp.
	gen := db.cb.ResetGeneration()
	start := time.Now()
	healthy := db.runPolicy(ctx, checks)

	if parent.Err() != nil {
		// The caller's own context was canceled or expired mid-probe: every
		// caller discards the verdict, so the breaker must not act on it
		// either — a late healthy result could otherwise close an open
		// circuit for an operation that returns context.Canceled.
		return false
	}
	if ctx.Err() != nil {
		// The probe's own deadline (HealthCheckTimeout) expired while a check
		// ignored it and returned late: a late verdict must not be trusted as a
		// success — exceeding the probe timeout is itself an availability
		// signal, so record it as a failure (below), not a pass.
		healthy = false
	}
	db.applyProbeVerdict(ctx, gen, healthy, time.Since(start))
	return healthy
}

// applyProbeVerdict records a probe verdict on the breaker (gated on the reset
// generation sampled before the checks ran) and the OTel recorder. Extracted so
// the background pass can run the checks without recording, revalidate the
// active member, and then apply the verdict — see runHealthChecksOnce. gen must
// be the value ResetGeneration() returned before the checks ran.
func (db *multidbDatabase) applyProbeVerdict(ctx context.Context, gen uint64, healthy bool, took time.Duration) {
	if db.removed.Load() {
		// The database left the membership while the probe was in flight
		// (background snapshot racing RemoveDatabase): its client is closed, so
		// record nothing and fire no callbacks.
		return
	}
	if healthy {
		// CheckState first so an Open circuit past its grace period transitions
		// to HalfOpen and the external success below can count toward closing it.
		// External: probes are not admitted through IsAllowed, so a success must
		// not release a half-open slot a real command probe is holding. Gated on
		// the reset generation: a success sampled before an operator reselect must
		// not count toward closing the freshly reset episode. (CheckState bumps
		// only the reservation generation, not resetGen, so it never voids this.)
		// On a CLOSED breaker a probe success changes nothing: the consecutive
		// command-failure count is command evidence, and a member that answers
		// PING but fails commands must still be able to open.
		db.cb.CheckState()
		db.cb.RecordExternalSuccessForReset(gen)
	} else {
		// Do NOT CheckState on a failed probe: transitioning a grace-elapsed Open
		// breaker to HalfOpen here would briefly admit application traffic to a
		// member the probe just found unhealthy (a spurious Open -> HalfOpen ->
		// Open, and a window where failover could select it). RecordFailureForReset
		// refreshes the open state directly, unless an operator reselect since the
		// sample voids this stale failure.
		db.cb.RecordFailureForReset(gen)
	}
	db.recorder().RecordMultiDBHealthCheck(ctx, db.fqdn, healthy, took)
}

type multidbCore struct {
	opts *MultiDBOptions
	rec  otel.Recorder // see clientRecorder; members carry their own

	dbMu sync.RWMutex
	// dbs is keyed by stable member id (multidbDatabase.id), not by position:
	// membership changes never renumber survivors, so a caller's handle and a
	// queued callback stay valid across a RemoveDatabase.
	dbs map[int]*multidbDatabase

	// active is the id of the active member, or -1 when none is selected. An id,
	// not a position, so removing another member never shifts it.
	active atomic.Int64

	// detectorGen is bumped on every failure-detector reset (resetDetectorSafely:
	// manual reselect, automatic failover, fallback). Commands and batches sample
	// it when they start and skip their detector write if it moved, so an outcome
	// that predates a reset cannot pollute the fresh window — the case where an
	// operator reselects the CURRENT member, which the cur == db identity check
	// cannot see. The per-member breaker has the same guard through its own
	// reset generation.
	detectorGen atomic.Uint64

	// pendingAdds holds the probe-cancel funcs of AddDatabase calls whose
	// startup probe is still running: those members are not in dbs yet, so
	// close() cancels them here instead of through closeAll.
	pendingAddMu sync.Mutex
	pendingAdds  map[*multidbDatabase]context.CancelFunc
	// closing is set when MultiDBClient.Close starts, before its autopipeliner
	// drain; closed is set only after it. Only addDatabase reads it: an add
	// must not publish a member that the running Close then tears down. The
	// drain itself still routes through the core, so closed must stay unset
	// until it ends.
	closing atomic.Bool

	// nextID hands out member ids: monotonic, never decremented, never reset
	// (not even in closeAll). Reuse would let a stale handle or a queued
	// callback resurrect as a different member, so ids are permanent.
	nextID atomic.Int64

	detector MultiDBFailureDetector
	strategy MultiDBFailoverStrategy

	// fallbackInterval is the normalized auto-fallback cadence: always
	// positive so SetAutoFallback(true) works even when the client was
	// constructed with a negative AutoFallbackInterval (which only sets the
	// initial disabled state).
	fallbackInterval time.Duration

	// failoverMu serializes failover attempts and guards the escalation state.
	failoverMu           sync.Mutex
	failoverAttempts     int
	lastFailoverAttempt  time.Time
	autoFallbackDisabled atomic.Bool

	// successSinceFailover breaks the "consecutive failed failover attempts"
	// chain: any successful command sets it, and the next failed failover
	// starts escalating from zero instead of a stale count left over from an
	// earlier, already-recovered outage.
	successSinceFailover atomic.Bool

	pubsubMu sync.Mutex
	pubsubs  map[*PubSub]struct{}
	// reconnectCancel cancels the most recent notifyPubSubs reconnect batch, so
	// a newer active-member change can abandon an older one whose dial is stuck
	// against an already-superseded member. Guarded by pubsubMu.
	reconnectCancel context.CancelFunc

	stopCh chan struct{}
	wg     sync.WaitGroup
	closed atomic.Bool
	// removing counts RemoveDatabase calls that detached a member and are
	// still closing it. Add runs under dbMu in detach, and closeAll waits after
	// taking the membership under dbMu, so Close also waits for those members.
	removing sync.WaitGroup

	// cbq runs the OnFailover / OnActiveDatabaseChanged callbacks on its own
	// goroutine (FIFO). They fire from switchActive's announce closure, which
	// runs on the background loop; running them inline there would deadlock
	// if a callback called Close() (close() waits on that goroutine via
	// wg.Wait). Like the per-db cbq, close() does not wait on this queue.
	cbq cbq.CallbackQueue

	// ready holds queued user callbacks until NewMultiDBClient returns, so a
	// transition during construction (startup breaker reconcile, initial
	// selection) does not reach a callback before the caller has the client.
	// readyOK is false when construction failed: no client is returned, and
	// the held callbacks are dropped. nil when the core was not built by
	// NewMultiDBClient (internal tests): callbacks are not held.
	ready   chan struct{}
	readyOK atomic.Bool
}

// awaitReady blocks a queued callback until construction finished, and
// reports whether the client was returned. It runs on a callback queue
// goroutine, never under a lock.
func (c *multidbCore) awaitReady() bool {
	if c.ready == nil {
		return true
	}
	<-c.ready
	return c.readyOK.Load()
}

// markReady releases the callbacks held by awaitReady. ok reports whether
// NewMultiDBClient returned the client.
func (c *multidbCore) markReady(ok bool) {
	c.readyOK.Store(ok)
	close(c.ready)
}

// multidbEpoch anchors the core's clock for deadlines that must survive a
// wall-clock step (NTP correction, VM restore): time.Since reads Go's
// monotonic clock. A fallback-suppression deadline stamped from the wall
// clock would otherwise outlive a backwards step by the size of the step.
var multidbEpoch = time.Now()

// multidbNowNano returns the monotonic clock in nanoseconds since
// multidbEpoch. A variable so tests can drive the clock without sleeping.
var multidbNowNano = func() int64 { return int64(time.Since(multidbEpoch)) }

// multidbAfterStartupReconcile runs right after startup sets the breakers
// from the final probe pass. Test seam: nil in production.
var multidbAfterStartupReconcile func()

func newMultidbCore(opts *MultiDBOptions) *multidbCore {
	core := &multidbCore{
		opts:     opts,
		rec:      scopedRecorder(opts.OTelRecorder),
		detector: opts.FailureDetector,
		strategy: opts.FailoverStrategy,
		dbs:      make(map[int]*multidbDatabase),
		pubsubs:  make(map[*PubSub]struct{}),
		stopCh:   make(chan struct{}),
	}
	// A stateful default detector must be per-client: writing it back into
	// the caller's options would share one sliding failure window across
	// every client built from the same MultiDBOptions value.
	if core.detector == nil {
		core.detector = failuredetector.NewCommandFailureDetector(
			commandFailureDetectorConfig(opts.FailureDetectorConfig),
		)
	}
	core.fallbackInterval = opts.AutoFallbackInterval
	if core.fallbackInterval <= 0 {
		core.fallbackInterval = defaultMultiDBAutoFallback
	}
	core.active.Store(-1)
	core.autoFallbackDisabled.Store(opts.AutoFallbackInterval < 0)
	return core
}

// buildDatabase constructs the underlying client, circuit breaker and
// resolved health checks for one member database.
func (c *multidbCore) buildDatabase(cfg *MultiDBClientConfig) (*multidbDatabase, error) {
	db := &multidbDatabase{
		rec: c.rec,
		// Stable id assigned here, before the circuit-breaker callback below
		// captures the member: a probe-fired state change then reports the real
		// id. Monotonic and never reused; a failed build just leaves a gap.
		id:     int(c.nextID.Add(1) - 1),
		weight: normalizeMultiDBWeight(cfg.Weight),
		fqdn:   cfg.fqdn(),
	}

	// Recover a panicking member constructor (e.g. NewClient panics on an
	// invalid pool size) and surface it as a construction error instead of
	// crashing the caller / a runtime AddDatabase.
	var buildErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				buildErr = fmt.Errorf("redis: multidb: building member client panicked: %v", r)
			}
		}()
		switch {
		case cfg.Options != nil:
			opt := *cfg.Options
			disableMaintNotificationsIfUnset(&opt)
			if opt.OTelRecorder == nil {
				opt.OTelRecorder = c.opts.OTelRecorder
			}
			// MultiDB owns retries (CommandRetries): each attempt is re-gated
			// and counted by the breaker and detector, and may go to a new
			// active. Member retries would repeat sends the breaker never sees.
			// Cluster members already default to -1.
			if opt.MaxRetries == 0 {
				opt.MaxRetries = -1
			}
			db.c = NewClient(&opt)
			db.c.trackMemberCommandExecution()
			// Derive the FQDN from the normalized address: NewClient defaults an
			// empty Addr to localhost:6379, so capturing it before construction
			// (cfg.fqdn) would record an empty host in logs/metrics.
			db.fqdn = hostOnly(db.c.Options().Addr)
		case cfg.ClusterOptions != nil:
			opt := *cfg.ClusterOptions
			// The cluster client keeps the options pointer and reads Addrs on
			// topology reloads: a private slice keeps a caller mutating its
			// seed list from changing a running member.
			opt.Addrs = append([]string(nil), cfg.ClusterOptions.Addrs...)
			if opt.MaintNotificationsConfig == nil {
				opt.MaintNotificationsConfig = &maintnotifications.Config{Mode: maintnotifications.ModeDisabled}
			}
			if opt.OTelRecorder == nil {
				opt.OTelRecorder = c.opts.OTelRecorder
			}
			db.cc = NewClusterClient(&opt)
			db.cc.trackMemberCommandExecution()
		}
	}()
	if buildErr != nil {
		return nil, buildErr
	}

	db.cb = imultidb.NewCircuitBreaker(imultidb.CircuitBreakerConfig{
		FailureThreshold: c.opts.CircuitBreakerConfig.FailureThreshold,
		SuccessThreshold: c.opts.CircuitBreakerConfig.SuccessThreshold,
		GracePeriod:      c.opts.CircuitBreakerConfig.GracePeriod,
	})

	db.checks = c.memberChecks(cfg)
	db.policy = c.memberPolicy(cfg)

	stateCallback := c.opts.OnCircuitStateChanged
	dbRef := db
	db.cb.OnStateChange(func(oldState, newState imultidb.CircuitState) {
		c.recorder().RecordMultiDBCircuitStateChange(context.Background(), dbRef.fqdn,
			oldState.String(), newState.String())
		if stateCallback != nil && !dbRef.removed.Load() {
			// Deliver asynchronously (FIFO per database): state changes fire
			// under internal locks, and a callback that calls a control API
			// would otherwise self-deadlock. The member id is stable and
			// immutable, so it is captured here and stays correct however
			// membership changes; the removed flag is re-read at DELIVERY time
			// so a member removed while the callback was queued fires nothing.
			from, to := oldState.String(), newState.String()
			id := dbRef.id
			dbRef.cbq.Dispatch(func() {
				if !c.awaitReady() || dbRef.removed.Load() {
					return
				}
				stateCallback(id, from, to)
			})
		}
	})

	return db, nil
}

// memberChecks returns a member's checks. Merge semantics: global checks +
// per-DB checks are additive; a database with no checks from either layer
// gets a single default PING check.
func (c *multidbCore) memberChecks(cfg *MultiDBClientConfig) []MultiDBHealthCheck {
	var checks []MultiDBHealthCheck
	checks = append(checks, c.opts.HealthChecks...)
	checks = append(checks, cfg.HealthChecks...)
	if len(checks) == 0 {
		checks = []MultiDBHealthCheck{defaultPingHealthCheck{}}
	}
	return checks
}

// memberPolicy returns a member's health-check policy.
func (c *multidbCore) memberPolicy(cfg *MultiDBClientConfig) MultiDBHealthCheckPolicy {
	if cfg.HealthCheckPolicy != nil {
		return cfg.HealthCheckPolicy
	}
	return c.opts.HealthCheckPolicy
}

// validateProbeBudget rejects, under the default policy, a member check whose
// probes cannot pass: (Probes-1)*Delay must stay below the check's Timeout and
// HealthCheckTimeout, or every background and startup pass fails. It covers
// the PING floor a member with only fail-back-only checks gets while active.
// name identifies the member in the error.
func (c *multidbCore) validateProbeBudget(cfg *MultiDBClientConfig, name string) error {
	if _, ok := c.memberPolicy(cfg).(defaultMultiDBPolicy); !ok {
		return nil
	}
	checks := c.memberChecks(cfg)
	liveness := false
	for _, hc := range checks {
		if !isFailbackOnly(hc) {
			liveness = true
		}
	}
	if !liveness {
		checks = append(checks, defaultPingHealthCheck{})
	}
	for _, hc := range checks {
		pc := multidbProbeConfig(hc)
		limit := min(pc.Timeout, c.opts.HealthCheckTimeout)
		need := time.Duration(pc.Probes-1) * pc.Delay
		if need < limit {
			continue
		}
		what := fmt.Sprintf("health check %T", hc)
		if _, ok := hc.(defaultPingHealthCheck); ok {
			what = "the default PING check"
		}
		return fmt.Errorf("redis: multidb: database %s: %s waits %v between its %d probes, not below its %v timeout, so it never passes; "+
			"lower the probes or the delay (multidb.WithProbes, multidb.WithDelay) or raise HealthCheckInterval",
			name, what, need, pc.Probes, limit)
	}
	return nil
}

// initHealthErr classifies a failed-initialization exit consistently: a
// deadline means the InitialDBState policy was not satisfied within the
// constructor's deadline, so it returns insufficientHealthyErr (still
// wrapping the deadline) — matching the documented contract and the
// between-passes exit. A caller cancellation is not a health verdict and is
// surfaced as-is. allDown: a completed pass found no enabled member healthy.
func initHealthErr(err error, healthy, required int, allDown bool) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return insufficientHealthyErr(fmt.Sprintf("%d healthy, %d required", healthy, required), err, allDown)
	}
	return err
}

// multidbInitError is a failed start: it matches ErrInsufficientHealthyDatabases,
// the optional cause and, when no member was healthy, ErrPermanentlyNotAvailable.
type multidbInitError struct {
	detail  string
	cause   error
	allDown bool
}

func (e *multidbInitError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%v: %s (%v)", ErrInsufficientHealthyDatabases, e.detail, e.cause)
	}
	return fmt.Sprintf("%v: %s", ErrInsufficientHealthyDatabases, e.detail)
}

func (e *multidbInitError) Unwrap() []error {
	errs := []error{ErrInsufficientHealthyDatabases}
	if e.allDown {
		errs = append(errs, ErrPermanentlyNotAvailable)
	}
	if e.cause != nil {
		errs = append(errs, e.cause)
	}
	return errs
}

// insufficientHealthyErr is the error NewMultiDBClient returns when the
// InitialDBState policy is not met. cause may be nil. allDown reports that no
// member was healthy.
func insufficientHealthyErr(detail string, cause error, allDown bool) error {
	return &multidbInitError{detail: detail, cause: cause, allDown: allDown}
}

// initialize runs the initial health checks, enforces the InitialDBState
// policy and selects the initial active database. When ctx carries a deadline
// it blocks and retries every initialHealthCheckRetryDelay; otherwise a
// single pass is performed.
func (c *multidbCore) initialize(ctx context.Context) error {
	required := c.requiredAvailableCount()
	_, hasDeadline := ctx.Deadline()

	// probeHealthy holds every member's verdict from the pass that satisfied
	// the policy. A member that had not answered when the constructor's
	// deadline arrived is recorded as unhealthy, like a probe that hit
	// HealthCheckTimeout.
	var probeHealthy map[int]bool
	// lastAllDown: the last pass that got every verdict found no enabled member
	// healthy. A pass cut short by the deadline (or none at all) proves nothing,
	// so only a completed pass sets it: ErrPermanentlyNotAvailable means the
	// members answered and were down, not that they did not answer in time.
	lastAllDown := false
	type verdict struct {
		id      int
		healthy bool
	}
	// runPass probes every member once and returns how many passed. It
	// leaves a verdict for every member in probeHealthy.
	//
	// The probes run concurrently. Probing serially let one unreachable
	// member spend the constructor's deadline after enough healthy members
	// had already answered, and the pass then failed on the deadline — in
	// exactly the partial outage OneAvailable and MajorityAvailable exist to
	// tolerate. The pass still waits for every verdict it can get: the
	// verdicts decide which members failover may select, and a member that
	// answers "unhealthy" a moment after the policy is met must still be
	// forced open. Only the deadline cuts the wait short (below). Each probe
	// is bounded by HealthCheckTimeout and by ctx, and the channel is
	// buffered for every member, so a late verdict never blocks anything.
	//
	// Startup probes record NOTHING on the breakers: the reconciliation after
	// the final pass applies the verdicts in one place. A recording probe
	// could otherwise settle after a later pass declared its member healthy
	// (a slow probe that a deadline stopped waiting for), and open the
	// breaker of the member just selected active. Each pass also has its own
	// context, so a probe the pass no longer waits for is stopped instead of
	// left running.
	runPass := func() (int, error) {
		probeHealthy = make(map[int]bool, len(c.dbs))
		passCtx, cancelPass := context.WithCancel(ctx)
		defer cancelPass()
		results := make(chan verdict, len(c.dbs))
		for id, db := range c.dbs {
			go func(id int, db *multidbDatabase) {
				start := time.Now()
				healthy := db.checkHealthyNoRecord(passCtx, c.opts.HealthCheckTimeout, db.checks)
				c.recorder().RecordMultiDBHealthCheck(passCtx, db.fqdn, healthy, time.Since(start))
				results <- verdict{id: id, healthy: healthy}
			}(id, db)
		}
		healthy := 0
		for seen := 0; seen < len(c.dbs); {
			// ctx before verdicts: select picks among ready cases at random,
			// so a verdict sent as ctx ended (from the probe whose check
			// canceled it, or one that noticed the cancellation late) could
			// otherwise be counted, and a canceled construction could meet
			// quorum and succeed. Such a verdict is dropped.
			if ctx.Err() == nil {
				select {
				case v := <-results:
					if ctx.Err() == nil {
						seen++
						probeHealthy[v.id] = v.healthy
						// A disabled member is probed (its breaker state
						// stays current) but never counts toward the policy.
						if v.healthy && !c.dbs[v.id].disabledLocked() {
							healthy++
						}
						continue
					}
				case <-ctx.Done():
				}
			}
			// A caller's cancellation fails: a canceled construction must
			// not return a live client. A DEADLINE that arrives with the
			// policy already met by the verdicts gathered before it
			// succeeds on them. A member still probing then did not
			// answer within the whole constructor deadline, so it is
			// recorded as unhealthy — the same verdict a probe that hit
			// HealthCheckTimeout gets — and forced open below, never
			// selected, and repaired by the background loop once it
			// answers. A verdict landing after expiry is not counted: a
			// probe that notices cancellation late may report healthy for
			// a member the caller stopped waiting on.
			if !errors.Is(ctx.Err(), context.DeadlineExceeded) || healthy < required {
				return healthy, initHealthErr(ctx.Err(), healthy, required, lastAllDown && healthy == 0)
			}
			for id := range c.dbs {
				if _, known := probeHealthy[id]; !known {
					probeHealthy[id] = false
				}
			}
			seen = len(c.dbs)
		}
		// A cancellation right after the last verdict still fails the pass.
		if err := ctx.Err(); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return healthy, initHealthErr(err, healthy, required, false)
		}
		return healthy, nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return initHealthErr(err, 0, required, lastAllDown)
		}
		healthy, err := runPass()
		if err != nil {
			return err
		}
		if healthy >= required {
			break
		}
		// runPass returned without error: every member gave a verdict (or the
		// deadline arrived with the policy met, handled above).
		lastAllDown = healthy == 0
		if !hasDeadline {
			return insufficientHealthyErr(fmt.Sprintf("%d healthy, %d required", healthy, required), nil, lastAllDown)
		}
		select {
		case <-ctx.Done():
			return initHealthErr(ctx.Err(), healthy, required, lastAllDown)
		case <-time.After(initialHealthCheckRetryDelay):
		}
	}

	// Reconcile breaker state with the final probe pass, which is the
	// definitive startup signal. The probes themselves never touch the
	// breakers (see runPass), so this is the only place startup does:
	//  - a member that passed keeps a closed breaker (Reset only repairs a
	//    breaker something else already opened);
	//  - a member that failed its probe gets its breaker opened, so automatic
	//    failover cannot switch to a database already known to be down before
	//    the background checks have had a chance to open it organically.
	// Every member has a verdict here: either its probe answered, or the
	// deadline arrived first and it was recorded as unhealthy.
	for id, db := range c.dbs {
		if probeHealthy[id] {
			if db.cb.CheckState() != imultidb.CircuitClosed {
				db.cb.Reset()
			}
			continue
		}
		db.cb.ForceOpen()
	}
	if multidbAfterStartupReconcile != nil {
		multidbAfterStartupReconcile() // test seam: nil in production
	}

	// Select the highest-weight database among those that passed the final
	// probe pass. Circuit state alone is not enough here: with the default
	// FailureThreshold a database that failed its only startup probe would
	// otherwise still look selectable.
	cands := make([]MultiDBDatabaseState, 0, len(c.dbs))
	for id, db := range c.dbs {
		if db.disabledLocked() {
			continue
		}
		cands = append(cands, MultiDBDatabaseState{
			ID:      id,
			Weight:  db.weight,
			Allowed: probeHealthy[id],
		})
	}
	sortCandidatesByID(cands)
	best := c.selectCandidate(cands)
	if best < 0 {
		return insufficientHealthyErr("no selectable database", nil, false)
	}
	c.active.Store(int64(best))
	return nil
}

// sortCandidatesByID orders candidates by ascending stable id so map iteration
// order never leaks into failover: the strategy sees a deterministic list, and
// equal-weight ties resolve by id instead of arbitrary map order.
func sortCandidatesByID(cands []MultiDBDatabaseState) {
	sort.Slice(cands, func(i, j int) bool { return cands[i].ID < cands[j].ID })
}

// requiredAvailableCount is the number of healthy enabled members the
// InitialDBState policy needs. Disabled members (weight -1) do not count.
func (c *multidbCore) requiredAvailableCount() int {
	n := 0
	for _, db := range c.dbs {
		if !db.disabledLocked() {
			n++
		}
	}
	switch c.opts.InitialDBState {
	case InitialDBStateAllAvailable:
		return n
	case InitialDBStateOneAvailable:
		return 1
	default: // majority: half, rounded up, so a pair starts with one member down
		return (n + 1) / 2
	}
}

// candidates snapshots every enabled database except the one with id
// excludeID (the active) for the failover strategy, ordered by id so
// selection is deterministic despite map iteration order.
func (c *multidbCore) candidates(excludeID int) []MultiDBDatabaseState {
	c.dbMu.RLock()
	defer c.dbMu.RUnlock()
	out := make([]MultiDBDatabaseState, 0, len(c.dbs))
	for id, db := range c.dbs {
		if id == excludeID || db.disabledLocked() {
			continue
		}
		out = append(out, MultiDBDatabaseState{
			ID:      id,
			Weight:  db.weight,
			Allowed: db.selectable(),
		})
	}
	sortCandidatesByID(out)
	return out
}

// selectCandidate runs the failover strategy with panic recovery: strategies
// are user code and also run on the library-owned background loop, where an
// escaped panic would crash the process. A panicking strategy selects
// nothing (logged), which surfaces as temporary unavailability.
func (c *multidbCore) selectCandidate(cands []MultiDBDatabaseState) (best int) {
	defer func() {
		if r := recover(); r != nil {
			internal.Logger.Printf(context.Background(), "multidb: failover strategy panicked: %v", r)
			best = -1
		}
	}()
	owned := false // cands copied — removeCandidate compacts in place
	// Every pass returns or shrinks cands, so this terminates.
	for len(cands) > 0 {
		idx := c.strategy.Select(cands)
		if idx < 0 {
			return -1
		}
		// A custom strategy may return any int; accept it only if it names one
		// of the candidates offered. An out-of-range or non-candidate index
		// would otherwise be stored as the active and later index a wrong/absent
		// member.
		pick := -1
		for i, cand := range cands {
			if cand.ID == idx {
				pick = i
				break
			}
		}
		if pick < 0 {
			internal.Logger.Printf(context.Background(), "multidb: failover strategy returned invalid index %d; ignoring", idx)
			return -1
		}
		if cands[pick].Allowed {
			return idx
		}
		// Every candidate is offered with its Allowed flag so a strategy can
		// prefer a healthy one. A candidate whose breaker is not admitting
		// traffic must not be seated merely because its id was offered: it would
		// make a known-unhealthy member the active (and, on failover, publish an
		// open member and fire callbacks before the gate rejects it). Drop it and
		// ask again with the rest — as a failed target probe does — so one bad
		// pick does not abort a round that still has admissible candidates.
		internal.Logger.Printf(context.Background(), "multidb: failover strategy returned disallowed candidate %d (circuit not admitting traffic); dropping it and reselecting", idx)
		if !owned {
			cands = append([]MultiDBDatabaseState(nil), cands...)
			owned = true
		}
		cands = removeCandidate(cands, idx)
	}
	return -1
}

// shouldFailoverSafely asks the failure detector whether to fail over,
// recovering a panicking custom implementation. ShouldFailover runs on the
// command gate and — via tryFailover — on the library-owned background loop,
// where an escaped panic would end health checking for the client's lifetime.
// A panicking detector is treated as "not tripped": the breaker still drives
// failover, and the panic is logged rather than propagated.
func (c *multidbCore) shouldFailoverSafely() (tripped bool) {
	defer func() {
		if r := recover(); r != nil {
			internal.Logger.Printf(context.Background(), "multidb: failure detector ShouldFailover panicked: %v", r)
			tripped = false
		}
	}()
	d := c.detector
	return d.ShouldFailover()
}

// resetDetectorSafely resets the failure detector, recovering a panicking
// custom implementation: Reset runs on the background loop (auto-failover and
// fallback), where an escaped panic would crash the process.
func (c *multidbCore) resetDetectorSafely() {
	// Every reset opens a fresh outcome window: bump the generation FIRST so an
	// in-flight command that sampled the old one cannot record a pre-reset
	// outcome into the new window, even if it lands between the bump and Reset.
	c.detectorGen.Add(1)
	c.clearDetectorSafely()
}

// clearDetectorSafely resets the detector's state without opening a new
// window (no generation bump). recordDetector* use it to wipe a stale write:
// bumping there would drop recorders that already sampled the current window,
// and each of their cleanups would bump again, so steady traffic could keep
// the detector empty.
func (c *multidbCore) clearDetectorSafely() {
	defer func() {
		if r := recover(); r != nil {
			internal.Logger.Printf(context.Background(), "multidb: failure detector Reset panicked: %v", r)
		}
	}()
	c.detector.Reset()
}

// recordDetectorFailure writes a failure into the detector window the caller
// sampled as gen, and nothing into a later window. The generation check and
// the write are not atomic: a reset that lands between them puts a pre-reset
// failure into the fresh window. So the window is re-checked after the write
// and, if it moved, cleared (same window, no new generation). Only recorders
// that sampled an old generation clear, so this cannot cascade. A stale outcome
// is visible only until its recorder clears it; the clear also wipes the few
// legitimate outcomes recorded in between, which can only delay a failover.
func (c *multidbCore) recordDetectorFailure(gen uint64, err error) {
	if c.detectorGen.Load() != gen {
		return
	}
	c.detector.RecordFailure(err)
	if c.detectorGen.Load() != gen {
		c.clearDetectorSafely()
	}
}

// recordDetectorSuccess is recordDetectorFailure's counterpart: a success
// recorded into a window that moved during the write is wiped the same way,
// so a burst of pre-reset successes cannot dilute the fresh window's failure
// rate or clear state in a custom detector.
func (c *multidbCore) recordDetectorSuccess(gen uint64) {
	if c.detectorGen.Load() != gen {
		return
	}
	c.detector.RecordSuccess()
	if c.detectorGen.Load() != gen {
		c.clearDetectorSafely()
	}
}

// activeDatabaseID returns the stable id of the active database, or -1 when none is
// selected.
func (c *multidbCore) activeDatabaseID() int {
	db, id := c.activeSnapshot()
	if db == nil {
		return -1
	}
	return id
}

// memberCount returns the current number of member databases.
func (c *multidbCore) memberCount() int {
	c.dbMu.RLock()
	defer c.dbMu.RUnlock()
	return len(c.dbs)
}

// activeSnapshot returns the active database and its id, or (nil, id) when none
// is selected or the active id is absent. The id and the map read happen under
// dbMu so they are coherent with a concurrent RemoveDatabase.
func (c *multidbCore) activeSnapshot() (*multidbDatabase, int) {
	c.dbMu.RLock()
	defer c.dbMu.RUnlock()
	id := int(c.active.Load())
	if id < 0 {
		// -1 sentinel: no active selected. A non-negative active is always a
		// live key — removeDatabase refuses to remove the active — so the map
		// lookup below cannot resolve a stale id to a reused member.
		return nil, id
	}
	return c.dbs[id], id
}

// dbByID returns the member with the given stable id, or nil if none.
func (c *multidbCore) dbByID(id int) *multidbDatabase {
	c.dbMu.RLock()
	defer c.dbMu.RUnlock()
	return c.dbs[id]
}

// process is the command hot path: an attempt loop bounded by CommandRetries
// that snapshots the active database, checks its circuit breaker, executes
// the command, and records the outcome on both the breaker and the aggregate
// failure detector. Failures can trigger failover before the next attempt.
//
// When the caller's ctx ends during a command, process returns the member's
// error (e.g. a dial or read timeout); processPipeline and processTxPipeline
// return ctx.Err() and leave the per-command errors on the commands.
func (c *multidbCore) process(ctx context.Context, cmd Cmder) error {
	// A cluster routing decision pinned by one member's autopipeliner (its
	// metadata view and policy) must not be reused by another member.
	cmd.setClusterRoutingDecision(nil)
	if c.closed.Load() {
		cmd.SetErr(ErrClosed)
		return ErrClosed
	}
	if isHImportCmd(cmd) {
		// The typed HImport* methods are overridden to reject the family,
		// but a hand-built HImport*Cmd — or a raw Do(ctx, "himport", ...) —
		// through Process would bypass them and register a fieldset on a
		// single member: the exact failover hazard the rejection prevents.
		cmd.SetErr(errMultiDBHImport)
		return errMultiDBHImport
	}
	attempts := c.opts.CommandRetries + 1
	if c.opts.CommandRetries == math.MaxInt {
		// Guard the +1 against wrapping to a negative attempt count (which
		// would skip the command entirely).
		attempts = math.MaxInt
	}
	// Blocking commands (BLPOP, XREAD BLOCK, WAIT, ...) carry their own read
	// timeout; a local read deadline on those must not be retried because
	// replaying can duplicate blocking side effects — same rule as *Client.
	retryTimeout := cmd.readTimeout() == nil
	var lastErr error

	attempt := 0
	// Bound consecutive gate rejections: when every selectable member is
	// half-open with a full probe budget, failover keeps handing back
	// members the gate then rejects — without a bound this ping-pongs the
	// active index in a busy loop instead of surfacing unavailability.
	gateRejections := 0

	// The current attempt's admission, released on any exit that did not
	// settle it: a panic in a member hook unwinds past the settle paths
	// below, and a leaked half-open slot would wedge the breaker at
	// MaxHalfOpenRequests. ReleaseFor settles at most once and no-ops for a
	// closed admission (same guard as processPipeline and Watch).
	var pendingDB *multidbDatabase
	var pendingRes imultidb.Reservation
	defer func() {
		if pendingDB != nil {
			pendingDB.cb.ReleaseFor(pendingRes)
		}
	}()

	for attempt < attempts {
		if c.closed.Load() {
			// Close landed mid-retry: report the terminal state instead of
			// escalating through the drained membership.
			cmd.SetErr(ErrClosed)
			return ErrClosed
		}
		if err := ctx.Err(); err != nil {
			cmd.SetErr(err)
			return err
		}

		// Detector before the breaker admission: a half-open admission
		// reserves a bounded probe slot, and a tripped detector routes to
		// failover without executing anything — the reservation would leak
		// and eventually starve the recovering active's probe budget.
		db, idx := c.activeSnapshot()
		// Sample the detector window with the member: a reset that lands while
		// this attempt runs voids its detector write (see detectorGen).
		dg := c.detectorGen.Load()
		admitted := false
		var res imultidb.Reservation
		if db != nil && !c.shouldFailoverSafely() {
			admitted, res = db.cb.AllowReserve()
		}
		if !admitted {
			gateRejections++
			// Bound against the CURRENT membership, re-read each rejection: a
			// concurrent AddDatabase raises the member count mid-command, and a
			// cap fixed at entry would trip ErrTemporarilyNotAvailable before
			// the freshly-added member could be tried.
			if gateRejections > c.memberCount()+1 {
				cmd.SetErr(ErrTemporarilyNotAvailable)
				return ErrTemporarilyNotAvailable
			}
			if err := c.tryFailover(ctx, idx); err != nil {
				cmd.SetErr(err)
				return err
			}
			// Re-enter the gate on the newly selected database: its breaker
			// may be half-open and the AllowReserve above is what reserves the
			// probe slot. Re-gating does not consume a retry attempt.
			continue
		}
		gateRejections = 0

		if db.cc != nil {
			if run := clusterControlFor(ctx, cmd); run != nil {
				// A cluster control command that started on a standalone active
				// and failed over to a cluster member: run the typed fan-out,
				// with the typed path's accounting (no outcome recorded).
				db.cb.ReleaseFor(res)
				cmd.SetErr(run(ctx, db.cc))
				rewriteRemovedMemberErr(db, cmd, c.closed.Load())
				return cmd.rawErr()
			}
		}

		if attempt > 0 {
			// Clear the previous attempt's error so a successful retry does
			// not leave the command in a stale error state.
			cmd.SetErr(nil)
		}
		attempt++
		pendingDB, pendingRes = db, res
		var executed atomic.Bool
		err := db.process(context.WithValue(ctx, memberCommandExecutedKey{}, &executed), cmd)
		// Back from the member: every path below settles the admission
		// itself, so the deferred release must stand down.
		pendingDB = nil
		if err == nil && !executed.Load() {
			// A locally answered command is no evidence of member recovery.
			db.cb.ReleaseFor(res)
			return nil
		}
		if callerEnded(ctx, err, db.ctxTimeouts()) {
			// The caller's own context ended (deadline/cancel) while the
			// command ran — typically cutting a dial short, which surfaces as
			// a *net.OpError{Op:"dial"} the classifier would otherwise count
			// as a database failure, or (ContextTimeoutEnabled) a socket
			// timeout armed from the caller's deadline. That is a client-side signal, not a
			// health verdict: recording it would let short caller deadlines
			// open the breaker and drive false failover. Give back any
			// reserved half-open slot (as the neutral branch does) and return
			// without recording an outcome. A genuine unreachable-endpoint
			// dial, where ctx is still alive, still reaches classifyOutcome,
			// and so does a member read/write timeout that lands after ctx
			// expired on a member without ContextTimeoutEnabled (see
			// callerEnded).
			db.cb.ReleaseFor(res)
			return err
		}
		switch classifyOutcome(err, retryTimeout) {
		case outcomeSuccess:
			// Settle the reservation: a closed-state admission holds no
			// half-open slot, and a command that outlives a later open ->
			// half-open transition (a stale reservation) must not free the slot
			// a real recovery probe is holding — RecordSuccessFor enforces both.
			db.cb.RecordSuccessFor(res)
			// Feed the GLOBAL detector only while this command's member is
			// still the active: an in-flight outcome from a member the active
			// already switched away from (or that was removed) would otherwise
			// pollute the current active's failover window. The member's own
			// breaker (above) is always updated — that is member-scoped.
			if cur, _ := c.activeSnapshot(); cur == db {
				// The escalation flag has no window semantics — any success on
				// the active counts — so it is set regardless of the detector
				// generation; only the detector write is window-gated.
				c.successSinceFailover.Store(true)
				c.recordDetectorSuccess(dg)
			}
			return err
		case outcomeNeutral:
			// Not a database-health signal: return to the caller without
			// recording a failure or failing over. Give back the half-open
			// probe slot the admission reserved — recording nothing would
			// otherwise leak it (ReleaseFor is a no-op for a closed admission).
			db.cb.ReleaseFor(res)
			if errors.Is(err, ErrClosed) && db.removed.Load() && !c.closed.Load() {
				// A concurrent control op removed the snapshotted member (closing
				// its client) between the gate and db.process, so its pool returned
				// the terminal ErrClosed even though the MultiDBClient is still
				// open. Do not hand the caller a client-closed sentinel for a live
				// client: surface the retryable ErrTemporarilyNotAvailable instead,
				// so the caller retries exactly as it would for a transport failure
				// and the next attempt lands on the live active. Nothing is replayed
				// here and no health outcome is recorded — the member is gone.
				cmd.SetErr(ErrTemporarilyNotAvailable)
				return ErrTemporarilyNotAvailable
			}
			if attempt < attempts && !cmd.NoRetry() && ctx.Err() == nil && shouldRetry(err, retryTimeout) {
				// A pool wait timeout: the command never left the client.
				// Members run with MaxRetries -1, so retry it here within
				// CommandRetries as *Client does (#3289), still recording no
				// health outcome.
				lastErr = err
				continue
			}
			return err
		case outcomeFailure:
			// Settle the reservation: a failure from a stale half-open episode
			// (this command outlived an open -> half-open cycle) must not re-open
			// the new episode and abort its recovery — RecordFailureFor gates on
			// the reservation's generation, symmetric to the success path.
			db.cb.RecordFailureFor(res)
			// Global detector only while this member is still the active (see
			// the success branch): a late failure from an already-vacated or
			// removed member must not trip failover for the new active.
			// ... and only within the detector window this attempt started in: an
			// operator reselect of this same member resets the detector without
			// changing identity, and a pre-reset failure must not pollute the
			// fresh window (the breaker half is already guarded by its reset
			// generation through RecordFailureFor).
			if cur, _ := c.activeSnapshot(); cur == db {
				c.recordDetectorFailure(dg, err)
			}
			lastErr = err
			if cmd.NoRetry() {
				// Commands that stream into caller-owned writers/buffers
				// must never be replayed after a partial read: the failure
				// is recorded, but the error goes straight to the caller.
				return err
			}
			if ctx.Err() != nil {
				// The caller's ctx ended while the member failed: recorded
				// above, but no retry, and the command keeps the member error
				// (the loop top would overwrite it with ctx.Err()).
				return err
			}
		}
	}
	return lastErr
}

// isRedisReplyError reports whether err is an error reply from the server
// rather than a transport-level failure. It matches the Error marker
// interface, not the concrete proto.RedisError string: the reader parses
// recognized reply prefixes into typed structs (*proto.AuthError,
// *proto.MovedError, ...) that only share the marker.
func isRedisReplyError(err error) bool {
	var redisErr Error
	return errors.As(err, &redisErr)
}

// isRedirectReply reports whether err is a MOVED or ASK redirect. When one
// reaches the multidb layer the active cluster client has already spent its
// redirect budget, so it is an availability signal, not a routine reply.
func isRedirectReply(err error) bool {
	moved, ask, _ := isMovedError(err)
	return moved || ask
}

// outcomeKind classifies a command outcome for breaker/detector recording.
type outcomeKind int

const (
	// outcomeSuccess proves the database served the request (including
	// definitive error replies like WRONGTYPE or redis.Nil).
	outcomeSuccess outcomeKind = iota
	// outcomeFailure is an availability signal: transport-level failures and
	// retryable server replies (LOADING, READONLY, CLUSTERDOWN, ...).
	outcomeFailure
	// outcomeNeutral is not a database-health signal at all: client-side
	// errors (context cancellation, deterministic local rejections) and
	// locally synthesized Redis errors such as ErrCrossSlot.
	outcomeNeutral
)

// classifyOutcome decides how a command outcome feeds the circuit breaker
// and the failure detector. Order matters: retryable server replies (LOADING,
// READONLY, ...) are availability failures even though they are RedisErrors,
// and locally synthesized RedisErrors (ErrCrossSlot) must not count as proof
// of a healthy server because no round trip happened. retryTimeout mirrors
// the *Client rule: false for commands with their own read timeout, whose
// local deadlines must not be treated as retryable failures.
func classifyOutcome(err error, retryTimeout bool) outcomeKind {
	switch {
	case err == nil:
		return outcomeSuccess
	case errors.Is(err, pool.ErrPoolTimeout) || errors.Is(err, pool.ErrPoolExhausted):
		// Local pool saturation: the command never reached the database, so
		// this is capacity pressure on the client, not a health signal —
		// the same classification the failure detector applies.
		return outcomeNeutral
	case errors.Is(err, errMultiDBHImport):
		// Locally synthesized rejection (a RedisError only so batch
		// machinery treats it per-command): no round trip happened, so it
		// must not count as proof of a healthy server. Defensive — the
		// rejection paths all skip outcome recording anyway.
		return outcomeNeutral
	case shouldRetry(err, retryTimeout):
		// Includes broken transport of every flavor: shouldRetry matches
		// any error carrying a Timeout() method (all net.OpErrors — resets,
		// broken pipes) via isTimeoutError, not only io.EOF, so a hard
		// member crash mid-read is recorded and drives failover.
		return outcomeFailure
	case errors.Is(err, ErrCrossSlot):
		return outcomeNeutral
	case errors.Is(err, errClusterNoNodes), errors.Is(err, ErrClusterTopologyUnhealthy):
		// The active member is a cluster client that cannot route the command:
		// no known nodes (topology empty or never loaded), or a slot master
		// still reported fail/loading after the topology-reload retries. An
		// availability failure that must drive failover, not a neutral
		// client-side error.
		return outcomeFailure
	case isRedirectReply(err):
		// A MOVED/ASK that surfaced to this layer means the active cluster
		// client exhausted MaxRedirects and still could not route the command:
		// the member cannot serve traffic, an availability failure — not a
		// healthy reply. (shouldRetry does not cover redirects: the cluster
		// router consumes them internally until the redirect budget is spent.)
		return outcomeFailure
	case isRedisReplyError(err):
		return outcomeSuccess
	default:
		return outcomeNeutral
	}
}

// callerEnded reports whether err is the caller's own context cutting the
// operation short: ctx is done and err is a context error, or a dial the
// expired ctx interrupted (net.OpError Op "dial", same rule as the failure
// detector). Such outcomes are not a health verdict.
//
// ctxTimeouts is the member's ContextTimeoutEnabled. When set, socket deadlines
// follow ctx, so a net timeout after ctx ended is the caller's deadline too.
// When not set, socket I/O does not watch ctx: a read or write timeout (or a
// reset) is a member failure even when ctx has expired.
func callerEnded(ctx context.Context, err error, ctxTimeouts bool) bool {
	if err == nil || ctx == nil {
		return false
	}
	if ctx.Err() == nil {
		// With ctxTimeouts the socket deadline is the ctx deadline itself, so
		// the read can time out just before ctx's own timer marks it done.
		dl, ok := ctx.Deadline()
		if !ctxTimeouts || !ok || time.Now().Before(dl) {
			return false
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return true
	}
	if ctxTimeouts {
		var nerr net.Error
		return errors.As(err, &nerr) && nerr.Timeout()
	}
	return false
}

// ctxTimeouts reports the member's ContextTimeoutEnabled option.
func (db *multidbDatabase) ctxTimeouts() bool {
	switch {
	case db.c != nil:
		return db.c.opt.ContextTimeoutEnabled
	case db.cc != nil:
		return db.cc.opt.ContextTimeoutEnabled
	}
	return false
}

const (
	failoverReasonAutomatic = "automatic"
	failoverReasonManual    = "manual"
	failoverReasonFallback  = "fallback"
)

// tryFailover selects a new active database and switches to it. It is the
// single automatic-failover path shared by the command hot path and the
// background loop; escalation state is shared so the two paths cannot
// double-count.
func (c *multidbCore) tryFailover(ctx context.Context, from int) error {
	c.failoverMu.Lock()
	// announce runs AFTER the unlock: user callbacks may call control APIs
	// that take failoverMu themselves.
	var announce func()
	defer func() {
		c.failoverMu.Unlock()
		if announce != nil {
			announce()
		}
	}()
	// Re-check after the lock wait: a command whose context expired while
	// queued behind another failover must not still switch the active
	// database (without pre-failover probes there is no later check) — and
	// a client closed while this call queued must not mutate anything.
	if c.closed.Load() {
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Re-check under the lock: the active database may already be fine.
	if db, idx := c.activeSnapshot(); db != nil && db.selectable() {
		if idx != from {
			// A concurrent failover already moved to a different selectable
			// member — nothing to do.
			return nil
		}
		// Still on `from`, but it may have been repaired IN PLACE since this
		// failover was queued: SetActiveDatabase re-selecting the current
		// member resets its breaker and detector without changing the active
		// index, and health checks may have closed the breaker. Stay if it is
		// fully available and the detector is clear — switching away would undo
		// a deliberate re-selection on evidence that predates the repair.
		if db.cb.CheckState() == imultidb.CircuitClosed && !c.shouldFailoverSafely() {
			return nil
		}
	}
	// Fail over from the CURRENT active, not the caller's snapshot: a
	// concurrent failover may have moved it since this call queued. Excluding
	// the stale `from` would drop a member that can serve (evicted only by the
	// detector, breaker closed) and offer the unselectable current one, and the
	// switch CAS on a stale `from` would never succeed. Every runtime writer of
	// the active holds failoverMu, so this read is stable until the switch.
	if _, idx := c.activeSnapshot(); idx >= 0 {
		from = idx
	}

	start := time.Now()
	cands := c.candidates(from)
	for {
		best := c.selectCandidate(cands)
		if best < 0 {
			// No alternate candidate: decide what to do with the CURRENT active
			// (live index, not the caller's snapshot — `from` can be stale after
			// a concurrent switch, and the verdict must be about the database
			// traffic actually lands on).
			db, _ := c.activeSnapshot()
			if db != nil {
				switch db.cb.CheckState() {
				case imultidb.CircuitClosed:
					// Fully available again (health checks closed its breaker
					// after the failure burst that tripped the detector): stay
					// on it and clear the tripped state — escalating would strand
					// the client in *NotAvailable forever, because no command can
					// succeed to reset the detector.
					c.resetFailoverEscalationLocked()
					c.resetDetectorSafely()
					return nil
				case imultidb.CircuitHalfOpen:
					// Recovering. A latched custom detector (ShouldFailover stays
					// true across the outage) blocks the gate from ever admitting
					// the bounded half-open probe, and with no alternate the
					// client would wedge half-open forever even after the endpoint
					// recovered. Clear the detector so the caller's next gate
					// admits a probe; a failed probe re-opens the breaker and
					// escalation resumes through the Open path below. The caller's
					// gateRejections cap bounds any spin on an exhausted probe
					// budget. Escalation state is left untouched (unlike Closed) —
					// the member is not proven healthy yet.
					c.resetDetectorSafely()
					return nil
				}
			}
			if c.closed.Load() {
				// close() takes no lock: the candidates may be gone because
				// closeAll drained the membership. Report the terminal state
				// and charge no failover attempt.
				return ErrClosed
			}
			return c.recordFailedFailoverLocked()
		}
		// Commit-time revalidation: candidates() is a snapshot, and background
		// probes can open a candidate's breaker while this round runs
		// (failoverMu does not serialize probes). Never publish a member that is
		// no longer selectable — drop it and re-select, as a failed target probe
		// does. Weight is read at selection time: a SetWeight that lands after
		// the snapshot takes effect at the next selection.
		target := c.dbByID(best)
		if target == nil {
			// removeDatabase holds failoverMu, so under it a missing member can
			// only mean close() drained the membership: report the terminal
			// state instead of dropping every candidate and charging a failover
			// attempt.
			if c.closed.Load() {
				return ErrClosed
			}
			cands = removeCandidate(cands, best)
			continue
		}
		if !target.selectable() {
			cands = removeCandidate(cands, best)
			continue
		}
		if c.opts.ProbeTargetBeforeFailover {
			// One probe under the default policy: a full pass would outlast
			// short command deadlines (see withGatingProbe).
			if !target.probe(withGatingProbe(ctx), c.opts.HealthCheckTimeout) {
				if err := ctx.Err(); err != nil {
					// The caller's context ended mid-probe: the candidate was
					// never proven unhealthy. Surface the context error
					// without dropping the candidate or charging a failover
					// attempt — short command deadlines must not escalate
					// toward ErrPermanentlyNotAvailable.
					return err
				}
				// The probe recorded the failure on the candidate's breaker;
				// drop it from this round and re-select.
				cands = removeCandidate(cands, best)
				continue
			}
			if err := ctx.Err(); err != nil {
				// The probe passed, but the caller's context died while it
				// ran: a canceled attempt must not switch the active state.
				return err
			}
		}
		if c.closed.Load() {
			// Same close-during-probe race as setActiveDatabase: close() takes no
			// lock, so a ProbeTargetBeforeFailover probe can outlast it. Do not
			// switch the active state on an already-closed client.
			return ErrClosed
		}
		// The candidates snapshot predates the probe: never commit to a member
		// disabled since.
		c.dbMu.RLock()
		disabled := target.disabledLocked()
		c.dbMu.RUnlock()
		if disabled {
			cands = removeCandidate(cands, best)
			continue
		}
		if announce = c.switchActive(ctx, from, best, failoverReasonAutomatic, time.Since(start)); announce == nil {
			// The CAS lost against a concurrent switch (the caller's `from`
			// went stale): nothing changed, so nothing may be reset — the
			// caller re-enters its gate against the new active. Escalation
			// state stays untouched too; the concurrent switch's own path
			// already maintains it.
			return nil
		}
		c.resetFailoverEscalationLocked()
		c.resetDetectorSafely()
		return nil
	}
}

func removeCandidate(cands []MultiDBDatabaseState, index int) []MultiDBDatabaseState {
	out := cands[:0]
	for _, cand := range cands {
		if cand.ID != index {
			out = append(out, cand)
		}
	}
	return out
}

// backgroundFailoverOnce is the background twin of the command gate: the
// active must move even with no command traffic, so one pass of the
// background loop fails over when the active is not selectable OR the
// detector is tripped. The detector term matters on its own: a no-retry or
// non-retryable command can trip a latched custom detector while the breaker
// stays closed and exit without another gate pass, and an idle or Pub/Sub-only
// client would otherwise never switch. tryFailover resets the detector when it
// switches, so a tripped detector does not ping-pong the active.
func (c *multidbCore) backgroundFailoverOnce(ctx context.Context) {
	if db, idx := c.activeSnapshot(); db != nil && (!db.selectable() || c.shouldFailoverSafely()) {
		_ = c.tryFailover(ctx, idx)
	}
}

// trackPendingAdd registers the cancel func of an in-flight AddDatabase probe.
// The member is not in c.dbs yet, so closeAll cannot reach it: close() cancels
// these instead, and the add's own closed re-check then discards the member.
// Re-checking closed after registering covers a close() that ran between the
// add's first closed check and this registration.
func (c *multidbCore) trackPendingAdd(db *multidbDatabase, cancel context.CancelFunc) {
	c.pendingAddMu.Lock()
	if c.pendingAdds == nil {
		c.pendingAdds = map[*multidbDatabase]context.CancelFunc{}
	}
	c.pendingAdds[db] = cancel
	c.pendingAddMu.Unlock()
	if c.addsClosed() {
		cancel()
	}
}

// beginClose stops new members from being added and cancels the in-flight
// add probes. MultiDBClient.Close calls it before its autopipeliner drain.
func (c *multidbCore) beginClose() {
	c.closing.Store(true)
	c.cancelPendingAdds()
}

// addsClosed reports whether Close has started (see closing).
func (c *multidbCore) addsClosed() bool {
	return c.closing.Load() || c.closed.Load()
}

func (c *multidbCore) untrackPendingAdd(db *multidbDatabase) {
	c.pendingAddMu.Lock()
	delete(c.pendingAdds, db)
	c.pendingAddMu.Unlock()
}

// cancelPendingAdds unblocks every in-flight AddDatabase probe (see
// trackPendingAdd). A custom check that ignores its context still runs to
// HealthCheckTimeout; that is the check's bug, not something close can force.
func (c *multidbCore) cancelPendingAdds() {
	c.pendingAddMu.Lock()
	defer c.pendingAddMu.Unlock()
	for _, cancel := range c.pendingAdds {
		cancel()
	}
}

// resetFailoverEscalationLocked starts a fresh escalation chain: the next
// failed failover counts from zero. Both the counter and the timestamp must
// clear — leaving lastFailoverAttempt set would make recordFailedFailoverLocked's
// FailoverAttemptDelay gate fold the new chain's first failure into the old
// burst and skip the increment. failoverMu must be held.
func (c *multidbCore) resetFailoverEscalationLocked() {
	c.failoverAttempts = 0
	c.lastFailoverAttempt = time.Time{}
}

// recordFailedFailoverLocked implements the escalation chain: attempts are
// rate-limited by FailoverAttemptDelay (a burst within the window counts as
// one attempt), ErrTemporarilyNotAvailable is returned until
// MaxFailoverAttempts consecutive attempts have failed, then
// ErrPermanentlyNotAvailable (escalated, not terminal: a later successful
// failover resets the chain). failoverMu must be held.
func (c *multidbCore) recordFailedFailoverLocked() error {
	if c.successSinceFailover.Swap(false) {
		// Successful traffic since the last failed attempt: the chain of
		// consecutive failures is broken, so a fresh outage escalates from
		// zero instead of a stale count.
		c.resetFailoverEscalationLocked()
	}
	now := time.Now()
	if c.lastFailoverAttempt.IsZero() || now.Sub(c.lastFailoverAttempt) >= c.opts.FailoverAttemptDelay {
		c.failoverAttempts++
		c.lastFailoverAttempt = now
	}
	if c.failoverAttempts >= c.opts.MaxFailoverAttempts {
		return ErrPermanentlyNotAvailable
	}
	return ErrTemporarilyNotAvailable
}

// switchActive is the single point for every active-index change: automatic
// failover, auto-fallback and manual selection all funnel through it, so
// callbacks, metrics and PubSub fire once per real change. It returns a
// non-nil announce closure when the switch happened; callers MUST invoke it
// AFTER releasing failoverMu. Metrics and the PubSub nudge run synchronously;
// the user callbacks go through cbq.
// from and to are stable member ids. Because ids are immutable, the ids
// captured here stay correct in the async callbacks however membership changes,
// and the CAS now fails only on a genuine concurrent switch (a RemoveDatabase of
// another member no longer moves `active`).
func (c *multidbCore) switchActive(ctx context.Context, from, to int, reason string, took time.Duration) (announce func()) {
	if !c.active.CompareAndSwap(int64(from), int64(to)) {
		return nil
	}

	fromFQDN, toFQDN := "", ""
	if db := c.dbByID(from); db != nil {
		fromFQDN = db.fqdn
	}
	if db := c.dbByID(to); db != nil {
		toFQDN = db.fqdn
	}

	internal.Logger.Printf(ctx, "multidb: active database changed %d (%s) -> %d (%s), reason=%s",
		from, fromFQDN, to, toFQDN, reason)

	// An automatic failover just vacated `from`: suppress auto-fallback back to
	// it for one fallback interval, re-armed on every such vacate. Otherwise
	// the next fallback check would return to a member the detector evicted on
	// a rate signal (its breaker may never have opened), undoing the failover
	// and ping-ponging a flaky higher-weight primary. Manual selection and
	// fallback itself (which does not gate on this) are unaffected.
	if reason == failoverReasonAutomatic {
		if fromDB := c.dbByID(from); fromDB != nil {
			fromDB.noFallbackBefore.Store(multidbNowNano() + int64(c.fallbackInterval))
		}
	}

	// Callbacks run later on cbq, so detach from the caller ctx (a manual
	// SetActiveDatabase ctx dies on return). Keep trace/values, drop cancel.
	cbCtx := context.WithoutCancel(ctx)

	// Enqueue the user callbacks HERE, under failoverMu (every caller holds it
	// across this CAS), so their queue order matches the switch order. Doing it
	// in the announce closure instead — which runs after the caller unlocks —
	// lets two back-to-back switches (A->B then B->C) enqueue B->C before A->B.
	// Dispatch only appends and never runs user code, so holding the lock here
	// is safe; the callbacks still execute off-lock on the cbq goroutine, which
	// RunSafely-wraps each and lets a callback re-enter control APIs. closed is
	// re-read at delivery so a callback never fires on an already-closed client.
	if reason == failoverReasonAutomatic || reason == failoverReasonManual {
		if c.opts.OnFailover != nil {
			c.cbq.Dispatch(func() {
				if !c.awaitReady() || c.closed.Load() {
					return
				}
				c.opts.OnFailover(cbCtx, from, to)
			})
		}
	}
	if c.opts.OnActiveDatabaseChanged != nil {
		c.cbq.Dispatch(func() {
			if !c.awaitReady() || c.closed.Load() {
				return
			}
			c.opts.OnActiveDatabaseChanged(from, to)
		})
	}

	// The announce closure runs AFTER the caller releases failoverMu: it only
	// does off-lock work (metrics, PubSub re-dial), no ordering-sensitive
	// callback dispatch.
	return func() {
		c.recorder().RecordMultiDBActiveDatabaseChange(ctx, fromFQDN, toFQDN)
		if reason == failoverReasonAutomatic || reason == failoverReasonManual {
			c.recorder().RecordMultiDBFailover(ctx, fromFQDN, toFQDN, reason, took)
		}
		c.notifyPubSubs(ctx)
	}
}

// setActiveDatabase implements manual failover. With probe=true it is the safe
// probe-then-switch path (SetActiveDatabase); with probe=false it is the
// unconditional operator override (ForceActiveDatabase).
func (c *multidbCore) setActiveDatabase(ctx context.Context, index int, probe bool) error {
	if c.closed.Load() {
		// Consistent with the command paths: the drained membership would
		// otherwise surface as a misleading out-of-range error.
		return ErrClosed
	}
	// An already-done context must not reach the probe below: a probe that
	// fails only because the caller's context ended would otherwise record
	// breaker failures against a perfectly healthy target.
	if err := ctx.Err(); err != nil {
		return err
	}
	// Hold failoverMu across the whole operation — lookup, probe and switch.
	// Membership changes (RemoveDatabase) also serialize on it, so the probed
	// database cannot be removed and the slice cannot shift between the probe
	// and the switch. The probe is bounded by HealthCheckTimeout.
	c.failoverMu.Lock()
	var announce func()
	defer func() {
		c.failoverMu.Unlock()
		if announce != nil {
			announce()
		}
	}()
	// Re-check after the lock wait: the client may have closed and the
	// operator's context may have expired while this call queued behind
	// another failover/membership operation, and the probe-less force path
	// has no later chance to notice.
	if c.closed.Load() {
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	db := c.dbByID(index)
	if db == nil {
		// Close does not take failoverMu: it may have drained the membership
		// since the check above. closed is set before the drain, so a miss it
		// caused sees it.
		if c.closed.Load() {
			return ErrClosed
		}
		return fmt.Errorf("%w: %d", ErrDatabaseNotFound, index)
	}
	// Weights are written under dbMu, so read them under it.
	disabled := func() bool {
		c.dbMu.RLock()
		defer c.dbMu.RUnlock()
		return db.disabledLocked()
	}
	if probe && disabled() {
		return fmt.Errorf("%w: %d", ErrDatabaseDisabled, index)
	}
	start := time.Now()
	if probe {
		// Re-selecting the CURRENT active must not be gated by fail-back-only
		// checks (e.g. lag): those decide routing TO a member, never the one
		// already serving traffic, so a lag breach must not fail the operation
		// or record a breaker failure against the active. Switching to a
		// DIFFERENT member runs the full check set (fail-back-to gating).
		// One probe under the default policy (see withGatingProbe).
		reselectingActive := index == int(c.active.Load())
		probeCtx := withGatingProbe(ctx)
		healthy := false
		if reselectingActive {
			healthy = db.probeExcludingFailbackOnly(probeCtx, c.opts.HealthCheckTimeout)
		} else {
			healthy = db.probe(probeCtx, c.opts.HealthCheckTimeout)
		}
		if !healthy {
			if err := ctx.Err(); err != nil {
				// The context ended mid-probe: report the caller's error,
				// not a verdict about the target's health.
				return err
			}
			// A genuinely unhealthy DIFFERENT target: open its breaker outright
			// (like initialize and AddDatabase) so a failed manual selection
			// does not leave a known-down member selectable for the next
			// automatic failover. Re-selecting the active is never opened here —
			// a fail-back-only breach must not evict the member serving traffic.
			if !reselectingActive {
				db.cb.ForceOpen()
			}
			return ErrTargetUnhealthy
		}
		if err := ctx.Err(); err != nil {
			// The probe passed, but the caller's context died while it ran:
			// a canceled control operation must not switch the active state.
			return err
		}
		// Re-check at commit time. SetWeight(-1) takes failoverMu, so this
		// holds; it keeps the switch safe if that ever changes.
		if disabled() {
			return fmt.Errorf("%w: %d", ErrDatabaseDisabled, index)
		}
	}
	if c.closed.Load() {
		// close() takes no lock, so it can have drained the membership while
		// the probe above ran (up to HealthCheckTimeout). Re-check before
		// mutating breaker/detector/active state or reporting success on an
		// already-closed client.
		return ErrClosed
	}
	// The operator explicitly selected this database — either a fresh probe
	// just passed, or ForceActiveDatabase is an unconditional override. Reset
	// its breaker in both cases so a still-open circuit does not immediately
	// fail the switch away; a genuinely dead forced target re-opens it
	// organically on the next failures.
	db.cb.Reset()
	// A fresh detector window as well: after an explicit operator selection
	// a previously tripped detector must not immediately fail away — also
	// when the selected database is already the active one.
	c.resetDetectorSafely()
	// The operator explicitly selected a healthy member: the chain of failed
	// failover attempts is over — also when no index change happens, or a
	// later unrelated outage would escalate from the stale count.
	c.resetFailoverEscalationLocked()
	from := int(c.active.Load())
	if from == index {
		return nil
	}
	if announce = c.switchActive(ctx, from, index, failoverReasonManual, time.Since(start)); announce == nil {
		return errors.New("redis: multidb: active database changed concurrently, retry")
	}
	return nil
}

func (c *multidbCore) addDatabase(ctx context.Context, cfg MultiDBClientConfig) (int, error) {
	if err := cfg.validate(); err != nil {
		return -1, err
	}
	if err := c.validateProbeBudget(&cfg, cfg.fqdn()); err != nil {
		return -1, err
	}
	if c.addsClosed() {
		return -1, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	db, err := c.buildDatabase(&cfg)
	if err != nil {
		return -1, err
	}
	// Probe BEFORE taking failoverMu and BEFORE publishing the member.
	// checkHealthyNoRecord runs the checks and returns a verdict WITHOUT recording
	// on the breaker or firing OnCircuitStateChanged. Recording here would open
	// the breaker (at the default threshold a single failure does) and dispatch a
	// callback for a member not yet in c.dbs, so a SetWeight / AddDatabaseHook
	// made from that callback would get ErrDatabaseNotFound. Running it off
	// failoverMu also stops a slow (or uncooperative custom) health check from
	// blocking an urgent failover, which needs the same lock. The verdict is
	// applied after the member is published, below.
	healthy := true
	if !cfg.SkipInitialHealthCheck {
		start := time.Now()
		// close() cancels in-flight add probes (trackPendingAdd), so a shutdown
		// does not wait out HealthCheckTimeout on a member it will discard; the
		// closed re-check below then closes the built client.
		probeCtx, cancelProbe := context.WithCancel(ctx)
		c.trackPendingAdd(db, cancelProbe)
		healthy = db.checkHealthyNoRecord(probeCtx, c.opts.HealthCheckTimeout, db.checks)
		// Read before cancelProbe below makes probeCtx.Err() unconditionally set.
		canceledByClose := probeCtx.Err() != nil && ctx.Err() == nil
		c.untrackPendingAdd(db)
		cancelProbe()
		if err := ctx.Err(); err != nil {
			// The caller's context ended while the probe ran: a canceled control
			// operation must not mutate the membership. No callbacks were fired,
			// so the built client just needs closing.
			db.removed.Store(true)
			_ = db.closeClient()
			return -1, err
		}
		if !canceledByClose {
			// A probe close() cut short is not a health verdict for a member
			// that is never added; the closed re-check below discards it.
			c.recorder().RecordMultiDBHealthCheck(ctx, db.fqdn, healthy, time.Since(start))
		}
	}
	// Serialize with the other membership paths (RemoveDatabase, manual failover),
	// which also hold failoverMu, and publish under it so the ForceOpen callback
	// below reports a stable member id.
	c.failoverMu.Lock()
	// The abort branches below close the built client. That runs code outside
	// this package (an OTel pool registrar's UnregisterPool, for one) which may
	// call a control operation taking failoverMu, so they release the lock
	// first; the success path keeps it through the publish and ForceOpen.
	unlocked := false
	unlock := func() {
		if !unlocked {
			unlocked = true
			c.failoverMu.Unlock()
		}
	}
	defer unlock()
	// Re-check after the lock wait: the caller's context may have expired while
	// the call queued behind another control operation.
	if err := ctx.Err(); err != nil {
		db.removed.Store(true)
		unlock()
		_ = db.closeClient()
		return -1, err
	}

	c.dbMu.Lock()
	if c.addsClosed() {
		// Close started while the member was being prepared: closeAll has
		// drained c.dbs or is about to, so inserting here would leak the
		// client or hand the caller an id for a member Close tears down.
		c.dbMu.Unlock()
		db.removed.Store(true)
		unlock()
		_ = db.closeClient()
		return -1, ErrClosed
	}
	c.dbs[db.id] = db
	c.dbMu.Unlock()

	// Open the breaker AFTER publishing the member. ForceOpen dispatches
	// OnCircuitStateChanged asynchronously, and a callback that uses this
	// member's id through a non-failoverMu control path (SetWeight,
	// AddDatabaseHook) must find it in c.dbs. The member is briefly selectable
	// with a still-closed breaker until this runs, but that self-heals on the
	// first command or the next probe and is strictly smaller than handing the
	// callback an id that is not yet a member.
	// Close takes no lock, so it can have flipped closed since the dbMu re-check
	// above; closeAll then drains this member like any other. Do not hand the
	// caller a success id for a member that is closed or about to be — report
	// the terminal state, and skip the ForceOpen below so no state-change
	// callback fires for a member reported as not added. The residual window
	// (closed flips right after this check) is the inherent Close-vs-AddDatabase
	// race.
	if c.addsClosed() {
		return -1, ErrClosed
	}
	if !healthy {
		db.cb.ForceOpen()
	}
	return db.id, nil
}

func (c *multidbCore) removeDatabase(ctx context.Context, id int) error {
	if c.closed.Load() {
		// Consistent with the other control paths: the drained membership
		// would otherwise surface as a misleading not-found error.
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// detach takes the member out of the membership under the locks. The
	// client is closed AFTER both locks are released: closing a client runs
	// code outside this package (an OTel pool registrar's UnregisterPool, for
	// one) that may call back into a control operation taking failoverMu, and
	// a slow pool close under failoverMu would also block an urgent failover.
	detach := func() (*multidbDatabase, error) {
		// Hold failoverMu so the active-id check below cannot race a concurrent
		// switchActive (all active transitions happen under it).
		c.failoverMu.Lock()
		defer c.failoverMu.Unlock()
		// Re-check after the lock wait: a client closed or an operator request
		// whose context died while queued must not still delete a member.
		if c.closed.Load() {
			return nil, ErrClosed
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.dbMu.Lock()
		defer c.dbMu.Unlock()
		db, ok := c.dbs[id]
		if !ok {
			// A concurrent Close drained the membership (see setActiveDatabase).
			if c.closed.Load() {
				return nil, ErrClosed
			}
			return nil, fmt.Errorf("%w: %d", ErrDatabaseNotFound, id)
		}
		if int(c.active.Load()) == id {
			return nil, errors.New("redis: multidb: cannot remove the active database")
		}
		if !db.disabledLocked() && c.enabledExceptLocked(id) == 0 {
			// The active may be a disabled member ForceActiveDatabase selected:
			// keep at least one enabled member, as construction and SetWeight do.
			return nil, fmt.Errorf("redis: multidb: cannot remove database %d; it is the last enabled member", id)
		}
		// Mark before the client is closed: a background probe holding a stale
		// snapshot must stop recording outcomes for this member.
		db.removed.Store(true)
		// Ids are stable and `active` is an id, so removing a non-active member
		// renumbers nothing and never shifts the active — just drop the key.
		delete(c.dbs, id)
		c.removing.Add(1)
		return db, nil
	}
	db, err := detach()
	if err != nil {
		return err
	}
	defer c.removing.Done()
	// Drain what was built on the member (full-duplex children) while its
	// client is still open, then close the client.
	_ = db.onRemove.run()
	return db.closeClient()
}

func (c *multidbCore) setWeight(id int, weight float64) error {
	// Reject before touching membership (see validateMultiDBWeight).
	if err := validateMultiDBWeight(weight); err != nil {
		return err
	}
	weight = normalizeMultiDBWeight(weight)
	if c.closed.Load() {
		// After Close drains the membership the id would surface as a
		// misleading not-found error; report the terminal state instead,
		// consistent with the other control APIs.
		return ErrClosed
	}
	if weight < 0 {
		// Disabling takes failoverMu (lock order failoverMu -> dbMu, as in
		// RemoveDatabase): every active switch happens under it, so the
		// "not active" check below cannot race a switch to this member that
		// is probing. A switch in flight commits first, and this call is then
		// rejected because the member is active.
		c.failoverMu.Lock()
		defer c.failoverMu.Unlock()
	}
	c.dbMu.Lock()
	defer c.dbMu.Unlock()
	// Re-check under the lock: Close sets closed before draining the membership
	// under dbMu, so a call that passed the entry check and was then descheduled
	// past a concurrent Close would otherwise report ErrDatabaseNotFound for an
	// id that was valid when it began. Report the terminal state instead,
	// consistent with the entry check and the other control APIs.
	if c.closed.Load() {
		return ErrClosed
	}
	db, ok := c.dbs[id]
	if !ok {
		return fmt.Errorf("%w: %d", ErrDatabaseNotFound, id)
	}
	if weight < 0 && db.disabledLocked() {
		// Already disabled (e.g. an active ForceActiveDatabase selected): no-op.
		return nil
	}
	if weight < 0 && id == int(c.active.Load()) {
		// Like RemoveDatabase: the active stays selectable until the caller
		// switches away from it.
		return fmt.Errorf("redis: multidb: cannot disable the active database %d; switch away first", id)
	}
	if weight < 0 && c.enabledExceptLocked(id) == 0 {
		// Construction rejects an all-disabled membership too.
		return fmt.Errorf("redis: multidb: cannot disable database %d; it is the last enabled member", id)
	}
	db.weight = weight
	return nil
}

// enabledExceptLocked counts the enabled members other than id. dbMu MUST be
// held.
func (c *multidbCore) enabledExceptLocked(id int) int {
	n := 0
	for other, db := range c.dbs {
		if other != id && !db.disabledLocked() {
			n++
		}
	}
	return n
}

func (c *multidbCore) setAutoFallback(enabled bool) {
	c.autoFallbackDisabled.Store(!enabled)
}

func (c *multidbCore) addDatabaseHook(id int, hook Hook) error {
	// Resolve the member under dbMu and re-check closed there (not via dbByID),
	// so a Close racing this call reports the terminal ErrClosed rather than a
	// misleading ErrDatabaseNotFound — consistent with setWeight and the entry
	// checks elsewhere. AddHook itself runs outside the lock: it calls the
	// hook's methods to rebuild the chain, and that is user code.
	c.dbMu.RLock()
	if c.closed.Load() {
		c.dbMu.RUnlock()
		return ErrClosed
	}
	db := c.dbs[id]
	c.dbMu.RUnlock()
	if db == nil {
		return fmt.Errorf("%w: %d", ErrDatabaseNotFound, id)
	}
	if db.cc != nil {
		db.cc.AddHook(hook)
	} else {
		db.c.AddHook(hook)
	}
	// A RemoveDatabase or Close that ran during AddHook closed the member:
	// the hook will never see traffic, so do not report it as installed.
	if db.removed.Load() {
		if c.closed.Load() {
			return ErrClosed
		}
		return fmt.Errorf("%w: %d", ErrDatabaseNotFound, id)
	}
	return nil
}

// startBackgroundLoop starts the loop that keeps every database's circuit
// breaker fed by health checks, fails the active database over when its
// breaker no longer allows traffic (so idle and PubSub-only workloads still
// fail over), and performs auto-fallback to a recovered higher-weight
// database.
func (c *multidbCore) startBackgroundLoop() {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		ticker := time.NewTicker(c.opts.HealthCheckInterval)
		defer ticker.Stop()

		// Start the fallback clock now, not at the zero time: otherwise the
		// first tick's time.Since is effectively infinite and always runs
		// fallback immediately (before any member has had a chance to settle).
		lastFallbackCheck := time.Now()

		// Bind the probe context to shutdown: close() closes stopCh, so
		// canceling here interrupts an in-flight probe for members whose clients
		// honor context (ContextTimeoutEnabled, or bounded by a finite
		// ReadTimeout), keeping close() from waiting a full probe pass. A member
		// with no socket timeout and no ContextTimeoutEnabled can still block a
		// raw read past the cancel — that remains a documented misconfiguration.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			<-c.stopCh
			cancel()
		}()

		for {
			select {
			case <-c.stopCh:
				return
			case <-ticker.C:
			}

			c.runHealthChecksOnce(ctx)

			c.backgroundFailoverOnce(ctx)

			if !c.autoFallbackDisabled.Load() &&
				time.Since(lastFallbackCheck) >= c.fallbackInterval {
				lastFallbackCheck = time.Now()
				c.tryFallbackToPrimary(ctx)
			}
		}
	}()
}

func (c *multidbCore) runHealthChecksOnce(ctx context.Context) {
	c.dbMu.RLock()
	dbs := make([]*multidbDatabase, 0, len(c.dbs))
	for _, db := range c.dbs {
		dbs = append(dbs, db)
	}
	c.dbMu.RUnlock()

	// Probe the active first. Its verdict is the one that can call for a
	// failover, and a client with no command traffic (idle, or Pub/Sub only)
	// learns about a dead active only from this pass: probing it last would
	// make such a client wait for every passive's probe (up to
	// HealthCheckTimeout each) before it could switch.
	if active, _ := c.activeSnapshot(); active != nil {
		for i, db := range dbs {
			if db == active {
				dbs[0], dbs[i] = dbs[i], dbs[0]
				break
			}
		}
	}

	// Passives are probed concurrently, after the active: one slow or
	// unreachable passive (up to HealthCheckTimeout each) must not push back the
	// next pass, and with it the active's next probe, by the sum of all their
	// timeouts. Each passive's probe touches only its own member.
	var passives sync.WaitGroup
	defer passives.Wait()
	for _, db := range dbs {
		select {
		case <-c.stopCh:
			return
		default:
		}
		// Re-read the active each iteration, not once before the loop: a
		// concurrent failover during the (up to HealthCheckTimeout per member)
		// pass would otherwise leave the freshly-selected active probed with
		// fail-back-only checks (e.g. lag) that could open its breaker and
		// evict it right after selection.
		active, _ := c.activeSnapshot()
		if db == active {
			// The active is probed without fail-back-only checks (e.g. the
			// lag-aware REST check): their verdicts gate routing traffic TO
			// a member, never evicting the one traffic flows through.
			before := db.cb.State()
			db.probeAsActive(ctx, c.opts.HealthCheckTimeout)
			// A probe that closes the active's breaker is recovery evidence,
			// like a successful command: it breaks the chain of consecutive
			// failed failovers. A client that sends no commands (idle, or
			// Pub/Sub only) has no command success to do that, so without
			// this a member that recovered in place kept the old attempt
			// count and the next outage could report permanent
			// unavailability after a single failed attempt.
			if before != imultidb.CircuitClosed && db.cb.State() == imultidb.CircuitClosed {
				c.successSinceFailover.Store(true)
			}
			// Decide failover now, not after the rest of the pass: an active
			// this probe found unhealthy must not keep traffic (and an idle
			// client's Pub/Sub) on it while the passives are probed.
			c.backgroundFailoverOnce(ctx)
			continue
		}
		// Passive member: run the FULL check set but do NOT record yet, then
		// revalidate the active. A failover can make this member the active while
		// the probe runs (up to HealthCheckTimeout); recording the full verdict
		// then could let a fail-back-only failure (lag) evict the member now
		// serving traffic. If it went active, record only the active-safe subset
		// (probeAsActive); otherwise apply the full verdict. This narrows the
		// window from the whole probe to the revalidation instant — the residual
		// (a failover between the recheck and the record) is not fully closed
		// without per-check verdict granularity; a stale verdict is additionally
		// dropped by the reset generation if an operator reselect intervened.
		passives.Add(1)
		go func(db *multidbDatabase) {
			defer passives.Done()
			gen := db.cb.ResetGeneration()
			start := time.Now()
			healthy := db.checkHealthyNoRecord(ctx, c.opts.HealthCheckTimeout, db.checks)
			if ctx.Err() != nil {
				// The pass was canceled (Close stops the loop this way): the
				// probe was cut short, not failed. Record nothing, as probeWith
				// does for a canceled parent.
				return
			}
			if again, _ := c.activeSnapshot(); again == db {
				db.probeAsActive(ctx, c.opts.HealthCheckTimeout)
				return
			}
			db.applyProbeVerdict(ctx, gen, healthy, time.Since(start))
		}(db)
	}
}

// tryFallbackToPrimary switches back to a strictly-higher-weight database
// whose circuit is closed again. Candidate collection and the final switch
// happen under failoverMu (so a concurrent RemoveDatabase, which also holds it,
// cannot remove the selected member or shift membership in between), but the
// candidate PROBES run off-lock — see below.
func (c *multidbCore) tryFallbackToPrimary(ctx context.Context) {
	// Yield to a real failover instead of blocking it: fallback is a background
	// cadence nicety, so skipping a cycle while a failover (or another fallback)
	// holds failoverMu costs nothing.
	if !c.failoverMu.TryLock() {
		return
	}
	active, idx := c.activeSnapshot()
	if active == nil {
		c.failoverMu.Unlock()
		return
	}

	now := multidbNowNano()
	// Collect every higher-weight, breaker-closed candidate not in its
	// post-failover suppression window, then RELEASE failoverMu BEFORE probing.
	// Each probe waits up to HealthCheckTimeout; holding failoverMu across
	// them would block a command-path emergency tryFailover (which needs the
	// same lock) and let command contexts expire. Weights are captured here under dbMu
	// (SetWeight mutates them under dbMu).
	type fbCand struct {
		id     int
		weight float64
		db     *multidbDatabase
	}
	c.dbMu.RLock()
	activeWeight := active.weight
	var cands []fbCand
	for id, db := range c.dbs {
		if id == idx {
			continue
		}
		// Skip a member still in its post-failover fallback-suppression window:
		// an automatic failover recently vacated it, so returning now would
		// likely just be undone again.
		if now < db.noFallbackBefore.Load() {
			continue
		}
		if !db.disabledLocked() && db.weight > activeWeight && db.cb.CheckState() == imultidb.CircuitClosed {
			cands = append(cands, fbCand{id, db.weight, db})
		}
	}
	c.dbMu.RUnlock()
	c.failoverMu.Unlock()

	// Probe every candidate concurrently with the full check set (incl
	// fail-back-only lag), WITHOUT recording and WITHOUT failoverMu held: the
	// breaker's consecutive-failure model leaves an intermittently-laggy member
	// "closed", so CheckState alone would fall back onto it. Concurrent, like
	// the passive probes: this runs on the background loop, and serial probes
	// would delay the active's next probe by up to N x HealthCheckTimeout.
	// The highest-weight candidate that passes wins, so a laggy top-weight
	// member cannot shadow a healthy lower-weight one; recording nothing means
	// the probe never evicts a member from failover.
	passed := make([]bool, len(cands))
	var probes sync.WaitGroup
	for i, cand := range cands {
		probes.Add(1)
		go func(i int, db *multidbDatabase) {
			defer probes.Done()
			passed[i] = db.checkHealthyNoRecord(ctx, c.opts.HealthCheckTimeout, db.checks)
		}(i, cand.db)
	}
	probes.Wait()
	if ctx.Err() != nil {
		return
	}
	top := -1
	for i, cand := range cands {
		if !passed[i] {
			continue
		}
		// Highest weight wins; ties break by lowest id so map iteration order
		// never decides which member fallback picks.
		if top < 0 || cand.weight > cands[top].weight ||
			(cand.weight == cands[top].weight && cand.id < cands[top].id) {
			top = i
		}
	}
	if top < 0 {
		return
	}
	best, bestDB := cands[top].id, cands[top].db

	// Reacquire failoverMu and REVALIDATE before switching: the probes ran
	// off-lock, so the active may have moved (another failover/fallback) or the
	// winner's weight, breaker state or membership may have changed. TryLock
	// again to keep yielding to a real failover, and abandon the fallback if
	// anything shifted.
	if !c.failoverMu.TryLock() {
		return
	}
	var announce func()
	defer func() {
		c.failoverMu.Unlock()
		if announce != nil {
			announce()
		}
	}()
	// Close may have started during the probes. Fallback is optional, so
	// stop on closing too: the members are about to be torn down.
	if c.addsClosed() {
		return
	}
	cur, curIdx := c.activeSnapshot()
	if cur == nil || curIdx != idx {
		return
	}
	c.dbMu.RLock()
	db, ok := c.dbs[best]
	stillGood := ok && db == bestDB &&
		!c.autoFallbackDisabled.Load() && // SetAutoFallback(false) may have raced the off-lock probe
		!db.disabledLocked() &&
		db.weight > cur.weight &&
		multidbNowNano() >= db.noFallbackBefore.Load() &&
		db.cb.CheckState() == imultidb.CircuitClosed
	c.dbMu.RUnlock()
	if !stillGood {
		return
	}
	if announce = c.switchActive(ctx, idx, best, failoverReasonFallback, 0); announce != nil {
		// The detector window still holds outcomes recorded against the old
		// active; left tripped, the very next command would immediately fail
		// away from the just-recovered primary. Clear it, as the automatic
		// and manual failover paths do — and end the failed-failover chain:
		// a successful fallback IS a recovery, and a later unrelated outage
		// must escalate from a clean slate.
		c.resetFailoverEscalationLocked()
		c.resetDetectorSafely()
	}
}

// hasStandaloneMember reports whether any member database is served by a
// standalone client, i.e. whether PubSub can ever be dialed.
func (c *multidbCore) hasStandaloneMember() bool {
	c.dbMu.RLock()
	defer c.dbMu.RUnlock()
	for _, db := range c.dbs {
		if db.c != nil {
			return true
		}
	}
	return false
}

// standaloneForPubSub returns the standalone member client whose options a new
// PubSub should adopt when the active member at creation is a cluster member:
// the subscription can only ever be served by a standalone (after failover), so
// its creation-time PubSub knobs (write timeout, Protocol gate) must come from a
// real standalone rather than the zero Options. Lowest id for determinism;
// mixed-Protocol members are the documented caveat in newPubSub. Returns nil
// when no standalone member exists.
func (c *multidbCore) standaloneForPubSub() *Client {
	c.dbMu.RLock()
	defer c.dbMu.RUnlock()
	best := -1
	var cl *Client
	for id, db := range c.dbs {
		if db.c != nil && (best < 0 || id < best) {
			best, cl = id, db.c
		}
	}
	return cl
}

// newPubSub creates a PubSub whose connections always target the currently
// active database: every (re-)dial resolves the active snapshot, and
// notifyPubSubs forces a re-dial on every active-database change.
func (c *multidbCore) newPubSub() *PubSub {
	// Connections may be dialed against different members over the PubSub's
	// lifetime; remember each connection's owner so closeConn can untrack it
	// on the right member's pool.
	var ownersMu sync.Mutex
	owners := make(map[*pool.Conn]*Client)
	// handoffAddrs holds the endpoint a member's connection was moved to by a
	// maintenance handoff (MOVING). Keyed by member so a failover never dials
	// another member's endpoint. The addr argument of newConn is not used: it
	// is the PubSub's single opt.Addr, which does not follow the member.
	handoffAddrs := make(map[*Client]string)

	// Terminality of "a cluster member is active and no standalone can serve the
	// subscription" is decided by the config at creation, not by live
	// membership: a passive standalone can be removed after a failover and a new
	// one added later, so "no standalone right now" is transient. Only a config
	// that was all-cluster when this PubSub was created — and still has no
	// standalone — is a permanent mismatch worth the terminal ErrClosed.
	staticAllCluster := !c.hasStandaloneMember()

	pubsub := &PubSub{
		rec: c.rec,
		newConn: func(ctx context.Context, _ string, channels []string, onInit ...func(*pool.Conn)) (*pool.Conn, error) {
			if c.closed.Load() {
				// ErrClosed (== pool.ErrClosed) is the only error the PubSub
				// channel loop treats as terminal; anything else would make
				// a post-close subscription retry forever.
				return nil, ErrClosed
			}
			db, _ := c.activeSnapshot()
			if db == nil {
				return nil, ErrTemporarilyNotAvailable
			}
			if db.c == nil {
				// A cluster member is active and cannot serve the subscription.
				// Terminal only for an all-cluster-at-creation config that still
				// has no standalone member (fail fast so Channel loops exit);
				// otherwise the mismatch is transient — a later
				// failover/fallback to, or an AddDatabase of, a standalone member
				// lets the re-dial succeed — so return the retryable error and
				// keep the channel loop polling.
				if staticAllCluster && !c.hasStandaloneMember() {
					return nil, ErrClosed
				}
				return nil, errPubSubRequiresStandalone
			}
			// newPubSubConn dials, runs the handshake and tracks the conn as one
			// Limiter admission, and closes the conn on failure. A member
			// removed meanwhile answers with the pool's ErrClosed, which
			// pubSubDialErr keeps terminal for the channel loops.
			addr := db.c.opt.Addr
			ownersMu.Lock()
			if a := handoffAddrs[db.c]; a != "" {
				addr = a
			}
			ownersMu.Unlock()
			cn, err := db.c.newPubSubConn(ctx, addr, channels, onInit...)
			if err != nil {
				return nil, pubSubDialErr(db, err, c.closed.Load())
			}
			// Removal (and Close) marks the member before closing its PubSub
			// pool, which checks closed only before dialing: re-check after
			// tracking, so either the pool close sees this conn or this sees
			// the mark. A conn to a removed member would otherwise stay open.
			if db.removed.Load() {
				db.c.pubSubPool.UntrackConn(cn)
				_ = cn.Close()
				return nil, pubSubDialErr(db, pool.ErrClosed, c.closed.Load())
			}
			ownersMu.Lock()
			owners[cn] = db.c
			ownersMu.Unlock()
			return cn, nil
		},
		closeConn: func(cn *pool.Conn) error {
			ownersMu.Lock()
			owner := owners[cn]
			// PubSub.reconnect closes a connection marked for handoff before
			// re-dialing: remember the new endpoint for its member.
			if ep := cn.GetHandoffEndpoint(); owner != nil && cn.ShouldHandoff() && ep != "" && ep != internal.RedisNull {
				handoffAddrs[owner] = ep
			}
			ownersMu.Unlock()
			if owner != nil {
				owner.pubSubPool.UntrackConn(cn)
			}
			// Close first, forget the owner after: a receive that is still
			// processing a buffered push on this connection looks the owner
			// up lock-free, and must not find it gone while the connection
			// is alive. After Close a miss means "closed", and pushes on a
			// closed connection are dropped (see processorFor).
			err := cn.Close()
			ownersMu.Lock()
			delete(owners, cn)
			ownersMu.Unlock()
			return err
		},
	}
	// A push is handled by the processor of the member that owns the
	// connection it arrived on, not by the creation-time member's: after a
	// failover the follower re-dials through another member, and that
	// member's handlers (maintenance notifications, for one) must see its
	// pushes. The owner map is the same one closeConn uses; a member's
	// processor never changes after the client is built.
	// The owner decides both halves of the push gate: whether the connection
	// speaks RESP3 at all, and which processor handles its pushes. A PubSub
	// created while a RESP2 member was active must still dispatch the pushes
	// of a RESP3 member it re-dials to after a failover, and the reverse
	// must not decode RESP2 traffic as pushes.
	pubsub.processorFor = func(cn *pool.Conn) push.NotificationProcessor {
		ownersMu.Lock()
		owner := owners[cn]
		ownersMu.Unlock()
		if owner == nil {
			// Every connection is registered before newConn returns, so a
			// miss means closeConn has finished with it: a push read after
			// that belongs to a member that is no longer serving this
			// subscription and is dropped rather than dispatched through
			// another member's processor.
			return nil
		}
		if owner.opt.Protocol != 3 {
			return nil
		}
		return owner.pushProcessor
	}

	// opt must always be non-nil (PubSub reads it unconditionally). Clone the
	// active standalone member's options rather than sharing the pointer: the
	// owning client mutates its own opt (e.g. maintnotifications handoffs).
	//
	// These fields are captured ONCE, from the member active at Subscribe
	// time, and deliberately not refreshed on failover: PubSub reads opt
	// outside its mutex (e.g. the RESP3 push gate on the receive path), so
	// swapping it at re-dial time would be a data race. Each connection is
	// still initialized through the owning member's own initConn/handshake,
	// and push processing (the RESP3 gate and the processor) follows the
	// connection's owner through processorFor below; only the PubSub-level
	// write timeout on subscribe frames keeps the creation-time value.
	if db, _ := c.activeSnapshot(); db != nil && db.c != nil {
		optCopy := *db.c.opt
		pubsub.opt = &optCopy
		pubsub.pushProcessor = db.c.pushProcessor
	} else if sd := c.standaloneForPubSub(); sd != nil {
		// Active is a cluster member (or none), but a standalone member exists:
		// the subscription lands on it after failover. Adopt its options now
		// rather than the zero Options — otherwise subscribe frames would use a
		// zero WriteTimeout and the wrong Protocol gate until (and after) the
		// failover, even though the connection itself handshakes correctly.
		optCopy := *sd.opt
		pubsub.opt = &optCopy
		pubsub.pushProcessor = sd.pushProcessor
	} else {
		pubsub.opt = &Options{}
		pubsub.pushProcessor = push.NewVoidProcessor()
	}
	pubsub.onClose = func() { c.removePubSub(pubsub) }
	pubsub.init()

	c.pubsubMu.Lock()
	if c.closed.Load() {
		// Racing Close: it already snapshotted+cleared the registry, so a
		// PubSub registered now would never be closed by close(). Close it here
		// instead of leaking its connection; the caller's Subscribe then errors.
		c.pubsubMu.Unlock()
		_ = pubsub.Close()
		return pubsub
	}
	c.pubsubs[pubsub] = struct{}{}
	c.pubsubMu.Unlock()
	return pubsub
}

// removePubSub deregisters a subscription closed by the caller so the
// registry does not grow unbounded and notifyPubSubs stops touching it.
func (c *multidbCore) removePubSub(ps *PubSub) {
	c.pubsubMu.Lock()
	delete(c.pubsubs, ps)
	c.pubsubMu.Unlock()
}

// notifyPubSubs forces every registered subscription to re-dial so it lands
// on the new active database immediately instead of waiting for the next
// read error.
func (c *multidbCore) notifyPubSubs(ctx context.Context) {
	c.pubsubMu.Lock()
	subs := make([]*PubSub, 0, len(c.pubsubs))
	for ps := range c.pubsubs {
		subs = append(subs, ps)
	}
	if len(subs) == 0 {
		c.pubsubMu.Unlock()
		return
	}

	// Reconnect dials and resubscribes synchronously; running it inline would
	// bill every subscription's recovery to whichever command triggered the
	// failover. Detach from the caller's context — its cancellation must not
	// abort recovery — but bind cancellation to client shutdown so close() can
	// interrupt an in-flight dial instead of blocking on a subscription's lock
	// (PubSub.Reconnect holds it across the dial). Reconnect each subscription
	// on its own goroutine so one slow dial cannot stall the others; each
	// resolves the active member at dial time, so a late reconnect still lands
	// on the current active.
	//
	// Supersede the previous switch's reconnect batch: with rapid A->B->C
	// changes, the A->B reconnect can block dialing an unreachable B while
	// holding each subscription's mutex, stalling the B->C reconnect behind it
	// until B's dial timeout. A newer switch wins, so record this batch's cancel
	// and cancel the previous one — freeing those mutexes for this batch. The
	// prior cancel is invoked AFTER unlocking: it can wake a Reconnect that
	// calls back into removePubSub, which takes pubsubMu.
	rctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	prevCancel := c.reconnectCancel
	c.reconnectCancel = cancel
	c.pubsubMu.Unlock()
	if prevCancel != nil {
		prevCancel()
	}

	reason := errors.New("multidb: active database changed")
	var wg sync.WaitGroup
	for _, ps := range subs {
		wg.Add(1)
		go func(ps *PubSub) {
			defer wg.Done()
			ps.forceReconnect(rctx, reason)
		}(ps)
	}
	go func() {
		defer cancel()
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-c.stopCh: // client closing: unblock any in-flight reconnect dial
		}
	}()
}

func (c *multidbCore) close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	// Members whose startup probe is still running are not in dbs yet; unblock
	// those adds so they discard their member promptly (see trackPendingAdd).
	c.beginClose()
	close(c.stopCh)
	c.wg.Wait()

	// Snapshot and clear the registry first, close outside the lock:
	// PubSub.Close fires onClose (which calls removePubSub → pubsubMu), so
	// closing under pubsubMu would self-deadlock.
	c.pubsubMu.Lock()
	subs := make([]*PubSub, 0, len(c.pubsubs))
	for ps := range c.pubsubs {
		subs = append(subs, ps)
	}
	c.pubsubs = map[*PubSub]struct{}{}
	c.pubsubMu.Unlock()
	for _, ps := range subs {
		_ = ps.Close()
	}

	return c.closeAll()
}

func (c *multidbCore) closeAll() error {
	// Take the members out of the membership under dbMu, then close their
	// clients with the lock released. Closing a client runs code outside
	// this package (an OTel pool registrar's UnregisterPool, for one), and
	// that code may call back into MultiDB; holding dbMu across it would
	// deadlock such a call.
	c.dbMu.Lock()
	dbs := c.dbs
	c.dbs = nil
	for _, db := range dbs {
		// Mark removed before closing: a circuit-state change fired during
		// teardown (or already queued) checks this flag at delivery, and
		// close() does not wait on the callback queue — without it an
		// OnCircuitStateChanged could fire after Close returns.
		db.removed.Store(true)
	}
	c.dbMu.Unlock()

	var firstErr error
	for _, db := range dbs {
		if err := db.closeClient(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// A RemoveDatabase that detached a member before the dbMu section above is
	// still closing it: wait, so no member client outlives Close. Later calls
	// find dbs nil and detach nothing.
	c.removing.Wait()
	return firstErr
}

// defaultPingHealthCheck is the built-in health check used when a database
// has no checks configured: a plain PING (universally supported, works
// against OSS replicas and clusters). Richer checks live in the multidb
// package.
type defaultPingHealthCheck struct{}

func (defaultPingHealthCheck) CheckHealth(ctx context.Context, client *Client) (bool, error) {
	if err := client.Ping(ctx).Err(); err != nil {
		return false, err
	}
	return true, nil
}

func (defaultPingHealthCheck) CheckClusterHealth(ctx context.Context, client *ClusterClient) (bool, error) {
	var masters atomic.Int32
	// Ping every master. Fail on the first error.
	//
	// We do this by hand so the check does not depend on cluster policy
	// routing. It still matches PING's Redis policy (all_shards +
	// all_succeeded: hit one node per shard, all must pass), but a plain
	// client.Ping() would rely on that policy.
	err := client.ForEachMaster(ctx, func(ctx context.Context, master *Client) error {
		masters.Add(1)
		return master.Ping(ctx).Err()
	})
	if err != nil {
		return false, err
	}
	if masters.Load() == 0 {
		// An empty topology pinged nothing: that is not proof of health.
		return false, errors.New("redis: multidb: cluster reported no masters to health-check")
	}
	return true, nil
}

// pubSubDialErr classifies a PubSub dial failure against a snapshotted member.
// A member that a concurrent control op removed mid-dial has a closed pool that
// returns pool.ErrClosed; the channel receive loops treat that as terminal and
// stop delivery for good, even though the MultiDBClient is still open. Return
// the retryable ErrTemporarilyNotAvailable instead so the loop re-dials and the
// next snapshot lands on the live active. Every other error passes through.
//
// closed is the core's own closed flag, read at the dial: close() marks every
// member removed, so without it a dial racing shutdown would hand the channel
// loop a retryable error for a client that has shut down, and the loop would
// keep re-dialing instead of exiting.
func pubSubDialErr(db *multidbDatabase, err error, closed bool) error {
	if closed {
		return err
	}
	if db.removed.Load() && errors.Is(err, pool.ErrClosed) {
		return ErrTemporarilyNotAvailable
	}
	return err
}
