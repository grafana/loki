package pool

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9/internal"
	"github.com/redis/go-redis/v9/internal/maintnotifications/logs"
	"github.com/redis/go-redis/v9/internal/proto"
)

var noDeadline = time.Time{}

// Global atomic counter for connection IDs
var connIDCounter uint64

// HandoffState represents the atomic state for connection handoffs
// This struct is stored atomically to prevent race conditions between
// checking handoff status and reading handoff parameters
type HandoffState struct {
	ShouldHandoff bool   // Whether connection should be handed off
	Endpoint      string // New endpoint for handoff
	SeqID         int64  // Sequence ID from MOVING notification
}

// atomicNetConn is a wrapper to ensure consistent typing in atomic.Value
type atomicNetConn struct {
	conn net.Conn
}

// generateConnID generates a fast unique identifier for a connection with zero allocations
func generateConnID() uint64 {
	return atomic.AddUint64(&connIDCounter, 1)
}

type Conn struct {
	// Connection identifier for unique tracking
	id uint64

	usedAt int64 // atomic

	// Lock-free netConn access using atomic.Value
	// Contains *atomicNetConn wrapper, accessed atomically for better performance
	netConnAtomic atomic.Value // stores *atomicNetConn

	rd *proto.Reader
	bw *bufio.Writer
	wr *proto.Writer

	// Lightweight mutex to protect reader operations during handoff
	// Only used for the brief period during SetNetConn and HasBufferedData/PeekReplyTypeSafe
	readerMu sync.RWMutex

	// Design note:
	// Why have both Usable and Used?
	// _Usable_ is used to mark a connection as safe for use by clients, the connection can still
	// be in the pool but not Usable at the moment (e.g. handoff in progress).
	// _Used_ is used to mark a connection as used when a command is going to be processed on that connection.
	// this is going to happen once the connection is picked from the pool.
	//
	// If a background operation needs to use the connection, it will mark it as Not Usable and only use it when it
	// is not in use. That way, the connection won't be used to send multiple commands at the same time and
	// potentially corrupt the command stream.

	// usable flag to mark connection as safe for use
	// It is false before initialization and after a handoff is marked
	// It will be false during other background operations like re-authentication
	usable atomic.Bool

	// used flag to mark connection as used when a command is going to be
	// processed on that connection. This is used to prevent a race condition with
	// background operations that may execute commands, like re-authentication.
	used atomic.Bool

	// Inited flag to mark connection as initialized, this is almost the same as usable
	// but it is used to make sure we don't initialize a network connection twice
	// On handoff, the network connection is replaced, but the Conn struct is reused
	// this flag will be set to false when the network connection is replaced and
	// set to true after the new network connection is initialized
	Inited atomic.Bool

	pooled    bool
	pubsub    bool
	closed    atomic.Bool
	createdAt time.Time
	expiresAt time.Time

	// maintenanceNotifications upgrade support: relaxed timeouts during migrations/failovers
	// Using atomic operations for lock-free access to avoid mutex contention
	relaxedReadTimeoutNs  atomic.Int64 // time.Duration as nanoseconds
	relaxedWriteTimeoutNs atomic.Int64 // time.Duration as nanoseconds
	relaxedDeadlineNs     atomic.Int64 // time.Time as nanoseconds since epoch

	// Counter to track multiple relaxed timeout setters if we have nested calls
	// will be decremented when ClearRelaxedTimeout is called or deadline is reached
	// if counter reaches 0, we clear the relaxed timeouts
	relaxedCounter atomic.Int32

	// Connection initialization function for reconnections
	initConnFunc func(context.Context, *Conn) error

	// Handoff state - using atomic operations for lock-free access
	handoffRetriesAtomic atomic.Uint32 // Retry counter for handoff attempts

	// Atomic handoff state to prevent race conditions
	// Stores *HandoffState to ensure atomic updates of all handoff-related fields
	handoffStateAtomic atomic.Value // stores *HandoffState

	onClose func() error
}

func NewConn(netConn net.Conn) *Conn {
	return NewConnWithBufferSize(netConn, proto.DefaultBufferSize, proto.DefaultBufferSize)
}

func NewConnWithBufferSize(netConn net.Conn, readBufSize, writeBufSize int) *Conn {
	cn := &Conn{
		createdAt: time.Now(),
		id:        generateConnID(), // Generate unique ID for this connection
	}

	// Use specified buffer sizes, or fall back to 32KiB defaults if 0
	if readBufSize > 0 {
		cn.rd = proto.NewReaderSize(netConn, readBufSize)
	} else {
		cn.rd = proto.NewReader(netConn) // Uses 32KiB default
	}

	if writeBufSize > 0 {
		cn.bw = bufio.NewWriterSize(netConn, writeBufSize)
	} else {
		cn.bw = bufio.NewWriterSize(netConn, proto.DefaultBufferSize)
	}

	// Store netConn atomically for lock-free access using wrapper
	cn.netConnAtomic.Store(&atomicNetConn{conn: netConn})

	// Initialize atomic state
	cn.usable.Store(false)           // false initially, set to true after initialization
	cn.handoffRetriesAtomic.Store(0) // 0 initially

	// Initialize handoff state atomically
	initialHandoffState := &HandoffState{
		ShouldHandoff: false,
		Endpoint:      "",
		SeqID:         0,
	}
	cn.handoffStateAtomic.Store(initialHandoffState)

	cn.wr = proto.NewWriter(cn.bw)
	cn.SetUsedAt(time.Now())
	return cn
}

func (cn *Conn) UsedAt() time.Time {
	unix := atomic.LoadInt64(&cn.usedAt)
	return time.Unix(unix, 0)
}

func (cn *Conn) SetUsedAt(tm time.Time) {
	atomic.StoreInt64(&cn.usedAt, tm.Unix())
}

// Usable

// CompareAndSwapUsable atomically compares and swaps the usable flag (lock-free).
//
// This is used by background operations (handoff, re-auth) to acquire exclusive
// access to a connection. The operation sets usable to false, preventing the pool
// from returning the connection to clients.
//
// Returns true if the swap was successful (old value matched), false otherwise.
func (cn *Conn) CompareAndSwapUsable(old, new bool) bool {
	return cn.usable.CompareAndSwap(old, new)
}

// IsUsable returns true if the connection is safe to use for new commands (lock-free).
//
// A connection is "usable" when it's in a stable state and can be returned to clients.
// It becomes unusable during:
//   - Initialization (before first use)
//   - Handoff operations (network connection replacement)
//   - Re-authentication (credential updates)
//   - Other background operations that need exclusive access
func (cn *Conn) IsUsable() bool {
	return cn.usable.Load()
}

// SetUsable sets the usable flag for the connection (lock-free).
//
// This should be called to mark a connection as usable after initialization or
// to release it after a background operation completes.
//
// Prefer CompareAndSwapUsable() when acquiring exclusive access to avoid race conditions.
func (cn *Conn) SetUsable(usable bool) {
	cn.usable.Store(usable)
}

// Used

// CompareAndSwapUsed atomically compares and swaps the used flag (lock-free).
//
// This is the preferred method for acquiring a connection from the pool, as it
// ensures that only one goroutine marks the connection as used.
//
// Returns true if the swap was successful (old value matched), false otherwise.
func (cn *Conn) CompareAndSwapUsed(old, new bool) bool {
	return cn.used.CompareAndSwap(old, new)
}

// IsUsed returns true if the connection is currently in use (lock-free).
//
// A connection is "used" when it has been retrieved from the pool and is
// actively processing a command. Background operations (like re-auth) should
// wait until the connection is not used before executing commands.
func (cn *Conn) IsUsed() bool {
	return cn.used.Load()
}

// SetUsed sets the used flag for the connection (lock-free).
//
// This should be called when returning a connection to the pool (set to false)
// or when a single-connection pool retrieves its connection (set to true).
//
// Prefer CompareAndSwapUsed() when acquiring from a multi-connection pool to
// avoid race conditions.
func (cn *Conn) SetUsed(val bool) {
	cn.used.Store(val)
}

// getNetConn returns the current network connection using atomic load (lock-free).
// This is the fast path for accessing netConn without mutex overhead.
func (cn *Conn) getNetConn() net.Conn {
	if v := cn.netConnAtomic.Load(); v != nil {
		if wrapper, ok := v.(*atomicNetConn); ok {
			return wrapper.conn
		}
	}
	return nil
}

// setNetConn stores the network connection atomically (lock-free).
// This is used for the fast path of connection replacement.
func (cn *Conn) setNetConn(netConn net.Conn) {
	cn.netConnAtomic.Store(&atomicNetConn{conn: netConn})
}

// getHandoffState returns the current handoff state atomically (lock-free).
func (cn *Conn) getHandoffState() *HandoffState {
	state := cn.handoffStateAtomic.Load()
	if state == nil {
		// Return default state if not initialized
		return &HandoffState{
			ShouldHandoff: false,
			Endpoint:      "",
			SeqID:         0,
		}
	}
	return state.(*HandoffState)
}

// setHandoffState sets the handoff state atomically (lock-free).
func (cn *Conn) setHandoffState(state *HandoffState) {
	cn.handoffStateAtomic.Store(state)
}

// shouldHandoff returns true if connection needs handoff (lock-free).
func (cn *Conn) shouldHandoff() bool {
	return cn.getHandoffState().ShouldHandoff
}

// getMovingSeqID returns the sequence ID atomically (lock-free).
func (cn *Conn) getMovingSeqID() int64 {
	return cn.getHandoffState().SeqID
}

// getNewEndpoint returns the new endpoint atomically (lock-free).
func (cn *Conn) getNewEndpoint() string {
	return cn.getHandoffState().Endpoint
}

// setHandoffRetries sets the retry count atomically (lock-free).
func (cn *Conn) setHandoffRetries(retries int) {
	cn.handoffRetriesAtomic.Store(uint32(retries))
}

// incrementHandoffRetries atomically increments and returns the new retry count (lock-free).
func (cn *Conn) incrementHandoffRetries(delta int) int {
	return int(cn.handoffRetriesAtomic.Add(uint32(delta)))
}

// IsPooled returns true if the connection is managed by a pool and will be pooled on Put.
func (cn *Conn) IsPooled() bool {
	return cn.pooled
}

// IsPubSub returns true if the connection is used for PubSub.
func (cn *Conn) IsPubSub() bool {
	return cn.pubsub
}

func (cn *Conn) IsInited() bool {
	return cn.Inited.Load()
}

// SetRelaxedTimeout sets relaxed timeouts for this connection during maintenanceNotifications upgrades.
// These timeouts will be used for all subsequent commands until the deadline expires.
// Uses atomic operations for lock-free access.
func (cn *Conn) SetRelaxedTimeout(readTimeout, writeTimeout time.Duration) {
	cn.relaxedCounter.Add(1)
	cn.relaxedReadTimeoutNs.Store(int64(readTimeout))
	cn.relaxedWriteTimeoutNs.Store(int64(writeTimeout))
}

// SetRelaxedTimeoutWithDeadline sets relaxed timeouts with an expiration deadline.
// After the deadline, timeouts automatically revert to normal values.
// Uses atomic operations for lock-free access.
func (cn *Conn) SetRelaxedTimeoutWithDeadline(readTimeout, writeTimeout time.Duration, deadline time.Time) {
	cn.SetRelaxedTimeout(readTimeout, writeTimeout)
	cn.relaxedDeadlineNs.Store(deadline.UnixNano())
}

// ClearRelaxedTimeout removes relaxed timeouts, returning to normal timeout behavior.
// Uses atomic operations for lock-free access.
func (cn *Conn) ClearRelaxedTimeout() {
	// Atomically decrement counter and check if we should clear
	newCount := cn.relaxedCounter.Add(-1)
	deadlineNs := cn.relaxedDeadlineNs.Load()
	if newCount <= 0 && (deadlineNs == 0 || time.Now().UnixNano() >= deadlineNs) {
		// Use atomic load to get current value for CAS to avoid stale value race
		current := cn.relaxedCounter.Load()
		if current <= 0 && cn.relaxedCounter.CompareAndSwap(current, 0) {
			cn.clearRelaxedTimeout()
		}
	}
}

func (cn *Conn) clearRelaxedTimeout() {
	cn.relaxedReadTimeoutNs.Store(0)
	cn.relaxedWriteTimeoutNs.Store(0)
	cn.relaxedDeadlineNs.Store(0)
	cn.relaxedCounter.Store(0)
}

// HasRelaxedTimeout returns true if relaxed timeouts are currently active on this connection.
// This checks both the timeout values and the deadline (if set).
// Uses atomic operations for lock-free access.
func (cn *Conn) HasRelaxedTimeout() bool {
	// Fast path: no relaxed timeouts are set
	if cn.relaxedCounter.Load() <= 0 {
		return false
	}

	readTimeoutNs := cn.relaxedReadTimeoutNs.Load()
	writeTimeoutNs := cn.relaxedWriteTimeoutNs.Load()

	// If no relaxed timeouts are set, return false
	if readTimeoutNs <= 0 && writeTimeoutNs <= 0 {
		return false
	}

	deadlineNs := cn.relaxedDeadlineNs.Load()
	// If no deadline is set, relaxed timeouts are active
	if deadlineNs == 0 {
		return true
	}

	// If deadline is set, check if it's still in the future
	return time.Now().UnixNano() < deadlineNs
}

// getEffectiveReadTimeout returns the timeout to use for read operations.
// If relaxed timeout is set and not expired, it takes precedence over the provided timeout.
// This method automatically clears expired relaxed timeouts using atomic operations.
func (cn *Conn) getEffectiveReadTimeout(normalTimeout time.Duration) time.Duration {
	readTimeoutNs := cn.relaxedReadTimeoutNs.Load()

	// Fast path: no relaxed timeout set
	if readTimeoutNs <= 0 {
		return normalTimeout
	}

	deadlineNs := cn.relaxedDeadlineNs.Load()
	// If no deadline is set, use relaxed timeout
	if deadlineNs == 0 {
		return time.Duration(readTimeoutNs)
	}

	nowNs := time.Now().UnixNano()
	// Check if deadline has passed
	if nowNs < deadlineNs {
		// Deadline is in the future, use relaxed timeout
		return time.Duration(readTimeoutNs)
	} else {
		// Deadline has passed, clear relaxed timeouts atomically and use normal timeout
		newCount := cn.relaxedCounter.Add(-1)
		if newCount <= 0 {
			internal.Logger.Printf(context.Background(), logs.UnrelaxedTimeoutAfterDeadline(cn.GetID()))
			cn.clearRelaxedTimeout()
		}
		return normalTimeout
	}
}

// getEffectiveWriteTimeout returns the timeout to use for write operations.
// If relaxed timeout is set and not expired, it takes precedence over the provided timeout.
// This method automatically clears expired relaxed timeouts using atomic operations.
func (cn *Conn) getEffectiveWriteTimeout(normalTimeout time.Duration) time.Duration {
	writeTimeoutNs := cn.relaxedWriteTimeoutNs.Load()

	// Fast path: no relaxed timeout set
	if writeTimeoutNs <= 0 {
		return normalTimeout
	}

	deadlineNs := cn.relaxedDeadlineNs.Load()
	// If no deadline is set, use relaxed timeout
	if deadlineNs == 0 {
		return time.Duration(writeTimeoutNs)
	}

	nowNs := time.Now().UnixNano()
	// Check if deadline has passed
	if nowNs < deadlineNs {
		// Deadline is in the future, use relaxed timeout
		return time.Duration(writeTimeoutNs)
	} else {
		// Deadline has passed, clear relaxed timeouts atomically and use normal timeout
		newCount := cn.relaxedCounter.Add(-1)
		if newCount <= 0 {
			internal.Logger.Printf(context.Background(), logs.UnrelaxedTimeoutAfterDeadline(cn.GetID()))
			cn.clearRelaxedTimeout()
		}
		return normalTimeout
	}
}

func (cn *Conn) SetOnClose(fn func() error) {
	cn.onClose = fn
}

// SetInitConnFunc sets the connection initialization function to be called on reconnections.
func (cn *Conn) SetInitConnFunc(fn func(context.Context, *Conn) error) {
	cn.initConnFunc = fn
}

// ExecuteInitConn runs the stored connection initialization function if available.
func (cn *Conn) ExecuteInitConn(ctx context.Context) error {
	if cn.initConnFunc != nil {
		return cn.initConnFunc(ctx, cn)
	}
	return fmt.Errorf("redis: no initConnFunc set for conn[%d]", cn.GetID())
}

func (cn *Conn) SetNetConn(netConn net.Conn) {
	// Store the new connection atomically first (lock-free)
	cn.setNetConn(netConn)
	// Protect reader reset operations to avoid data races
	// Use write lock since we're modifying the reader state
	cn.readerMu.Lock()
	cn.rd.Reset(netConn)
	cn.readerMu.Unlock()

	cn.bw.Reset(netConn)
}

// GetNetConn safely returns the current network connection using atomic load (lock-free).
// This method is used by the pool for health checks and provides better performance.
func (cn *Conn) GetNetConn() net.Conn {
	return cn.getNetConn()
}

// SetNetConnAndInitConn replaces the underlying connection and executes the initialization.
func (cn *Conn) SetNetConnAndInitConn(ctx context.Context, netConn net.Conn) error {
	// New connection is not initialized yet
	cn.Inited.Store(false)
	// Replace the underlying connection
	cn.SetNetConn(netConn)
	return cn.ExecuteInitConn(ctx)
}

// MarkForHandoff marks the connection for handoff due to MOVING notification (lock-free).
// Returns an error if the connection is already marked for handoff.
// This method uses atomic compare-and-swap to ensure all handoff state is updated atomically.
func (cn *Conn) MarkForHandoff(newEndpoint string, seqID int64) error {
	const maxRetries = 50
	const baseDelay = time.Microsecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		currentState := cn.getHandoffState()

		// Check if already marked for handoff
		if currentState.ShouldHandoff {
			return errors.New("connection is already marked for handoff")
		}

		// Create new state with handoff enabled
		newState := &HandoffState{
			ShouldHandoff: true,
			Endpoint:      newEndpoint,
			SeqID:         seqID,
		}

		// Atomic compare-and-swap to update entire state
		if cn.handoffStateAtomic.CompareAndSwap(currentState, newState) {
			return nil
		}

		// If CAS failed, add exponential backoff to reduce contention
		if attempt < maxRetries-1 {
			delay := baseDelay * time.Duration(1<<uint(attempt%10)) // Cap exponential growth
			time.Sleep(delay)
		}
	}

	return fmt.Errorf("failed to mark connection for handoff after %d attempts due to high contention", maxRetries)
}

func (cn *Conn) MarkQueuedForHandoff() error {
	const maxRetries = 50
	const baseDelay = time.Microsecond

	connAcquired := false
	for attempt := 0; attempt < maxRetries; attempt++ {
		// If CAS failed, add exponential backoff to reduce contention
		// the delay will be 1, 2, 4... up to 512 microseconds
		// Moving this to the top of the loop to avoid "continue" without delay
		if attempt > 0 && attempt < maxRetries-1 {
			delay := baseDelay * time.Duration(1<<uint(attempt%10)) // Cap exponential growth
			time.Sleep(delay)
		}

		// first we need to mark the connection as not usable
		// to prevent the pool from returning it to the caller
		if !connAcquired {
			if !cn.usable.CompareAndSwap(true, false) {
				continue
			}
			connAcquired = true
		}

		currentState := cn.getHandoffState()
		// Check if marked for handoff
		if !currentState.ShouldHandoff {
			return errors.New("connection was not marked for handoff")
		}

		// Create new state with handoff disabled (queued)
		newState := &HandoffState{
			ShouldHandoff: false,
			Endpoint:      currentState.Endpoint, // Preserve endpoint for handoff processing
			SeqID:         currentState.SeqID,    // Preserve seqID for handoff processing
		}

		// Atomic compare-and-swap to update state
		if cn.handoffStateAtomic.CompareAndSwap(currentState, newState) {
			// queue the handoff for processing
			// the connection is now "acquired" (marked as not usable) by the handoff
			// and it won't be returned to any other callers until the handoff is complete
			return nil
		}

	}

	return fmt.Errorf("failed to mark connection as queued for handoff after %d attempts due to high contention", maxRetries)
}

// ShouldHandoff returns true if the connection needs to be handed off (lock-free).
func (cn *Conn) ShouldHandoff() bool {
	return cn.shouldHandoff()
}

// GetHandoffEndpoint returns the new endpoint for handoff (lock-free).
func (cn *Conn) GetHandoffEndpoint() string {
	return cn.getNewEndpoint()
}

// GetMovingSeqID returns the sequence ID from the MOVING notification (lock-free).
func (cn *Conn) GetMovingSeqID() int64 {
	return cn.getMovingSeqID()
}

// GetHandoffInfo returns all handoff information atomically (lock-free).
// This method prevents race conditions by returning all handoff state in a single atomic operation.
// Returns (shouldHandoff, endpoint, seqID).
func (cn *Conn) GetHandoffInfo() (bool, string, int64) {
	state := cn.getHandoffState()
	return state.ShouldHandoff, state.Endpoint, state.SeqID
}

// GetID returns the unique identifier for this connection.
func (cn *Conn) GetID() uint64 {
	return cn.id
}

// ClearHandoffState clears the handoff state after successful handoff (lock-free).
func (cn *Conn) ClearHandoffState() {
	// Create clean state
	cleanState := &HandoffState{
		ShouldHandoff: false,
		Endpoint:      "",
		SeqID:         0,
	}

	// Atomically set clean state
	cn.setHandoffState(cleanState)
	cn.setHandoffRetries(0)
	// Clearing handoff state also means the connection is usable again
	cn.SetUsable(true)
}

// IncrementAndGetHandoffRetries atomically increments and returns handoff retries (lock-free).
func (cn *Conn) IncrementAndGetHandoffRetries(n int) int {
	return cn.incrementHandoffRetries(n)
}

// GetHandoffRetries returns the current handoff retry count (lock-free).
func (cn *Conn) HandoffRetries() int {
	return int(cn.handoffRetriesAtomic.Load())
}

// HasBufferedData safely checks if the connection has buffered data.
// This method is used to avoid data races when checking for push notifications.
func (cn *Conn) HasBufferedData() bool {
	// Use read lock for concurrent access to reader state
	cn.readerMu.RLock()
	defer cn.readerMu.RUnlock()
	return cn.rd.Buffered() > 0
}

// PeekReplyTypeSafe safely peeks at the reply type.
// This method is used to avoid data races when checking for push notifications.
func (cn *Conn) PeekReplyTypeSafe() (byte, error) {
	// Use read lock for concurrent access to reader state
	cn.readerMu.RLock()
	defer cn.readerMu.RUnlock()

	if cn.rd.Buffered() <= 0 {
		return 0, fmt.Errorf("redis: can't peek reply type, no data available")
	}
	return cn.rd.PeekReplyType()
}

func (cn *Conn) Write(b []byte) (int, error) {
	// Lock-free netConn access for better performance
	if netConn := cn.getNetConn(); netConn != nil {
		return netConn.Write(b)
	}
	return 0, net.ErrClosed
}

func (cn *Conn) RemoteAddr() net.Addr {
	// Lock-free netConn access for better performance
	if netConn := cn.getNetConn(); netConn != nil {
		return netConn.RemoteAddr()
	}
	return nil
}

func (cn *Conn) WithReader(
	ctx context.Context, timeout time.Duration, fn func(rd *proto.Reader) error,
) error {
	if timeout >= 0 {
		// Use relaxed timeout if set, otherwise use provided timeout
		effectiveTimeout := cn.getEffectiveReadTimeout(timeout)

		// Get the connection directly from atomic storage
		netConn := cn.getNetConn()
		if netConn == nil {
			return fmt.Errorf("redis: connection not available")
		}

		if err := netConn.SetReadDeadline(cn.deadline(ctx, effectiveTimeout)); err != nil {
			return err
		}
	}
	return fn(cn.rd)
}

func (cn *Conn) WithWriter(
	ctx context.Context, timeout time.Duration, fn func(wr *proto.Writer) error,
) error {
	if timeout >= 0 {
		// Use relaxed timeout if set, otherwise use provided timeout
		effectiveTimeout := cn.getEffectiveWriteTimeout(timeout)

		// Always set write deadline, even if getNetConn() returns nil
		// This prevents write operations from hanging indefinitely
		if netConn := cn.getNetConn(); netConn != nil {
			if err := netConn.SetWriteDeadline(cn.deadline(ctx, effectiveTimeout)); err != nil {
				return err
			}
		} else {
			// If getNetConn() returns nil, we still need to respect the timeout
			// Return an error to prevent indefinite blocking
			return fmt.Errorf("redis: conn[%d] not available for write operation", cn.GetID())
		}
	}

	if cn.bw.Buffered() > 0 {
		if netConn := cn.getNetConn(); netConn != nil {
			cn.bw.Reset(netConn)
		}
	}

	if err := fn(cn.wr); err != nil {
		return err
	}

	return cn.bw.Flush()
}

func (cn *Conn) IsClosed() bool {
	return cn.closed.Load()
}

func (cn *Conn) Close() error {
	cn.closed.Store(true)
	if cn.onClose != nil {
		// ignore error
		_ = cn.onClose()
	}

	// Lock-free netConn access for better performance
	if netConn := cn.getNetConn(); netConn != nil {
		return netConn.Close()
	}
	return nil
}

// MaybeHasData tries to peek at the next byte in the socket without consuming it
// This is used to check if there are push notifications available
// Important: This will work on Linux, but not on Windows
func (cn *Conn) MaybeHasData() bool {
	// Lock-free netConn access for better performance
	if netConn := cn.getNetConn(); netConn != nil {
		return maybeHasData(netConn)
	}
	return false
}

func (cn *Conn) deadline(ctx context.Context, timeout time.Duration) time.Time {
	tm := time.Now()
	cn.SetUsedAt(tm)

	if timeout > 0 {
		tm = tm.Add(timeout)
	}

	if ctx != nil {
		deadline, ok := ctx.Deadline()
		if ok {
			if timeout == 0 {
				return deadline
			}
			if deadline.Before(tm) {
				return deadline
			}
			return tm
		}
	}

	if timeout > 0 {
		return tm
	}

	return noDeadline
}
