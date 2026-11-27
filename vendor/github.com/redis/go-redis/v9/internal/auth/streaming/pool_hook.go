package streaming

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9/internal"
	"github.com/redis/go-redis/v9/internal/pool"
)

// ReAuthPoolHook is a pool hook that manages background re-authentication of connections
// when credentials change via a streaming credentials provider.
//
// The hook uses a semaphore-based worker pool to limit concurrent re-authentication
// operations and prevent pool exhaustion. When credentials change, connections are
// marked for re-authentication and processed asynchronously in the background.
//
// The re-authentication process:
//  1. OnPut: When a connection is returned to the pool, check if it needs re-auth
//  2. If yes, schedule it for background processing (move from shouldReAuth to scheduledReAuth)
//  3. A worker goroutine acquires the connection (waits until it's not in use)
//  4. Executes the re-auth function while holding the connection
//  5. Releases the connection back to the pool
//
// Pub/Sub connections never return to the pool, so OnPut never sees them;
// their re-auth is serialized per connection by markPubSubForReAuth instead.
//
// The hook ensures that:
//   - Only one re-auth operation runs per connection at a time
//   - Connections are not used for commands during re-authentication
//   - Re-auth operations timeout if they can't acquire the connection
//   - Resources are properly cleaned up on connection removal
type ReAuthPoolHook struct {
	// shouldReAuth maps connection ID to re-auth function
	// Connections in this map need re-authentication but haven't been scheduled yet
	shouldReAuth     map[uint64]func(error)
	shouldReAuthLock sync.RWMutex

	// workers is a semaphore limiting concurrent re-auth operations
	// Initialized with poolSize tokens to prevent pool exhaustion
	// Uses FastSemaphore for better performance with eventual fairness
	workers *internal.FastSemaphore

	// reAuthTimeout is the maximum time to wait for acquiring a connection for re-auth
	reAuthTimeout time.Duration

	// scheduledReAuth maps connection ID to scheduled status
	// Connections in this map have a background worker attempting re-authentication
	scheduledReAuth map[uint64]bool
	scheduledLock   sync.RWMutex

	// pubSubReAuth tracks in-flight Pub/Sub re-authentication per
	// connection. Entries are dropped by removeConn when the connection
	// closes; an idle entry (no re-auth running or pending) holds no
	// work and is not reported by hasReAuth.
	pubSubReAuth     map[uint64]*pubSubReAuthState
	pubSubReAuthLock sync.Mutex

	// manager is a back-reference for cleanup operations
	manager *Manager
}

// pubSubReAuthState serializes re-authentication for one Pub/Sub
// connection. Rotations run one at a time, in arrival order; a rotation
// that arrives while one is running replaces the pending one, so the
// last AUTH written to the socket always carries the latest credentials.
type pubSubReAuthState struct {
	mu      sync.Mutex
	running bool
	pending func(error)
	closed  bool
}

// NewReAuthPoolHook creates a new re-authentication pool hook.
//
// Parameters:
//   - poolSize: Maximum number of concurrent re-auth operations (typically matches pool size)
//   - reAuthTimeout: Maximum time to wait for acquiring a connection for re-authentication
//
// The poolSize parameter is used to initialize the worker semaphore, ensuring that
// re-auth operations don't exhaust the connection pool.
func NewReAuthPoolHook(poolSize int, reAuthTimeout time.Duration) *ReAuthPoolHook {
	return &ReAuthPoolHook{
		shouldReAuth:    make(map[uint64]func(error)),
		scheduledReAuth: make(map[uint64]bool),
		pubSubReAuth:    make(map[uint64]*pubSubReAuthState),
		workers:         internal.NewFastSemaphore(int32(poolSize)),
		reAuthTimeout:   reAuthTimeout,
	}
}

// MarkForReAuth marks a connection for re-authentication.
//
// This method is called when credentials change and a connection needs to be
// re-authenticated. The actual re-authentication happens asynchronously when
// the connection is returned to the pool (in OnPut).
//
// Parameters:
//   - connID: The connection ID to mark for re-authentication
//   - reAuthFn: Function to call for re-authentication, receives error if acquisition fails
//
// Thread-safe: Can be called concurrently from multiple goroutines.
func (r *ReAuthPoolHook) MarkForReAuth(connID uint64, reAuthFn func(error)) {
	r.shouldReAuthLock.Lock()
	defer r.shouldReAuthLock.Unlock()
	r.shouldReAuth[connID] = reAuthFn
}

// markForReAuthIfOpen is MarkForReAuth for a connection that is not closed.
// The check holds the same lock as removeConn, so a re-auth marked while the
// connection closes is either removed by removeConn or never added.
func (r *ReAuthPoolHook) markForReAuthIfOpen(conn *pool.Conn, reAuthFn func(error)) {
	r.shouldReAuthLock.Lock()
	defer r.shouldReAuthLock.Unlock()
	if conn.IsClosed() {
		return
	}
	r.shouldReAuth[conn.GetID()] = reAuthFn
}

// markPubSubForReAuth schedules re-authentication for a Pub/Sub connection.
// Unlike pooled connections, Pub/Sub connections never return to the pool,
// so there is no OnPut to serialize their re-auth: running each rotation's
// re-auth in its own goroutine lets an older rotation write AUTH last when
// two rotations arrive within milliseconds, and the connection stays on the
// older credentials until the next rotation or a WRONGPASS.
//
// Re-auths for one connection run one at a time, in arrival order; a
// rotation that arrives while one is running replaces any pending re-auth,
// so the last AUTH written always carries the latest credentials.
//
// Thread-safe: Can be called concurrently from multiple goroutines.
func (r *ReAuthPoolHook) markPubSubForReAuth(conn *pool.Conn, reAuthFn func(error)) {
	connID := conn.GetID()
	r.pubSubReAuthLock.Lock()
	// Refuse rotations for a connection that is already closed. The check
	// must hold the same lock as removeConn: otherwise a rotation that
	// slips in after removeConn deleted the entry resurrects tracking
	// state for a dead connection, and nothing ever cleans it up again.
	if conn.IsClosed() {
		r.pubSubReAuthLock.Unlock()
		return
	}
	state, ok := r.pubSubReAuth[connID]
	if !ok {
		state = &pubSubReAuthState{}
		r.pubSubReAuth[connID] = state
	}
	// Hold pubSubReAuthLock while deciding under state.mu: if the lock
	// is released between the lookup and the decision, an older rotation
	// can pause there, a newer one can claim running, and the older one
	// then overwrites pending — the worker would write the older
	// credentials last. removeConn takes the locks in the same order,
	// so this nesting cannot deadlock.
	state.mu.Lock()
	r.pubSubReAuthLock.Unlock()

	if state.closed {
		state.mu.Unlock()
		return
	}
	if state.running {
		// A re-auth is in flight: coalesce, keeping only the latest
		// pending rotation. It runs once the current one finishes.
		state.pending = reAuthFn
		state.mu.Unlock()
		return
	}
	state.running = true
	state.mu.Unlock()

	go func() {
		fn := reAuthFn
		for {
			// A panic in one re-auth must not wedge later rotations
			// for this connection.
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						internal.Logger.Printf(context.Background(), "panic in pubsub reauth worker: %v", rec)
					}
				}()
				if !conn.IsClosed() {
					fn(nil)
				}
			}()

			state.mu.Lock()
			next := state.pending
			state.pending = nil
			if next == nil || state.closed {
				state.running = false
				state.mu.Unlock()
				return
			}
			state.mu.Unlock()
			fn = next
		}
	}()
}

// OnGet is called when a connection is retrieved from the pool.
//
// This hook checks if the connection needs re-authentication or has a scheduled
// re-auth operation. If so, it rejects the connection (returns accept=false),
// causing the pool to try another connection.
//
// Returns:
//   - accept: false if connection needs re-auth, true otherwise
//   - err: always nil (errors are not used in this hook)
//
// Thread-safe: Called concurrently by multiple goroutines getting connections.
func (r *ReAuthPoolHook) OnGet(_ context.Context, conn *pool.Conn, _ bool) (accept bool, err error) {
	connID := conn.GetID()
	r.shouldReAuthLock.RLock()
	_, shouldReAuth := r.shouldReAuth[connID]
	r.shouldReAuthLock.RUnlock()
	// This connection was marked for reauth while in the pool,
	// reject the connection
	if shouldReAuth {
		// simply reject the connection, it will be re-authenticated in OnPut
		return false, nil
	}
	r.scheduledLock.RLock()
	_, hasScheduled := r.scheduledReAuth[connID]
	r.scheduledLock.RUnlock()
	// has scheduled reauth, reject the connection
	if hasScheduled {
		// simply reject the connection, it currently has a reauth scheduled
		// and the worker is waiting for slot to execute the reauth
		return false, nil
	}
	return true, nil
}

// OnPut is called when a connection is returned to the pool.
//
// This hook checks if the connection needs re-authentication. If so, it schedules
// a background goroutine to perform the re-auth asynchronously. The goroutine:
//  1. Waits for a worker slot (semaphore)
//  2. Acquires the connection (waits until not in use)
//  3. Executes the re-auth function
//  4. Releases the connection and worker slot
//
// The connection is always pooled (not removed) since re-auth happens in background.
//
// Returns:
//   - shouldPool: always true (connection stays in pool during background re-auth)
//   - shouldRemove: always false
//   - err: always nil
//
// Thread-safe: Called concurrently by multiple goroutines returning connections.
func (r *ReAuthPoolHook) OnPut(_ context.Context, conn *pool.Conn) (bool, bool, error) {
	if conn == nil {
		// noop
		return true, false, nil
	}
	connID := conn.GetID()
	// Check if reauth is needed and get the function with proper locking
	r.shouldReAuthLock.RLock()
	reAuthFn, ok := r.shouldReAuth[connID]
	r.shouldReAuthLock.RUnlock()

	if ok {
		// Acquire both locks to atomically move from shouldReAuth to scheduledReAuth
		// This prevents race conditions where OnGet might miss the transition
		r.shouldReAuthLock.Lock()
		r.scheduledLock.Lock()
		r.scheduledReAuth[connID] = true
		delete(r.shouldReAuth, connID)
		r.scheduledLock.Unlock()
		r.shouldReAuthLock.Unlock()
		go func() {
			r.workers.AcquireBlocking()
			// safety first
			if conn == nil || (conn != nil && conn.IsClosed()) {
				r.workers.Release()
				return
			}
			defer func() {
				if rec := recover(); rec != nil {
					// once again - safety first
					internal.Logger.Printf(context.Background(), "panic in reauth worker: %v", rec)
				}
				r.scheduledLock.Lock()
				delete(r.scheduledReAuth, connID)
				r.scheduledLock.Unlock()
				r.workers.Release()
			}()

			// Create timeout context for connection acquisition
			// This prevents indefinite waiting if the connection is stuck
			ctx, cancel := context.WithTimeout(context.Background(), r.reAuthTimeout)
			defer cancel()

			// Try to acquire the connection for re-authentication
			// We need to ensure the connection is IDLE (not IN_USE) before transitioning to UNUSABLE
			// This prevents re-authentication from interfering with active commands
			// Use AwaitAndTransition to wait for the connection to become IDLE
			stateMachine := conn.GetStateMachine()
			if stateMachine == nil {
				// No state machine - should not happen, but handle gracefully
				reAuthFn(pool.ErrConnUnusableTimeout)
				return
			}

			// Use predefined slice to avoid allocation
			_, err := stateMachine.AwaitAndTransition(ctx, pool.ValidFromIdle(), pool.StateUnusable)
			if err != nil {
				// Timeout or other error occurred, cannot acquire connection
				reAuthFn(err)
				return
			}

			// safety first
			if !conn.IsClosed() {
				// Successfully acquired the connection, perform reauth
				reAuthFn(nil)
			}

			// Release the connection: UNUSABLE back to IDLE. A conn closed during
			// reAuthFn (e.g. onAuthenticationErr after a rejected AUTH) stays CLOSED.
			_, _ = stateMachine.TryTransition([]pool.ConnState{pool.StateUnusable}, pool.StateIdle)
		}()
	}

	// the reauth will happen in background, as far as the pool is concerned:
	// pool the connection, don't remove it, no error
	return true, false, nil
}

// OnRemove is called when a connection is removed from the pool.
//
// This hook cleans up all state associated with the connection:
//   - Removes from shouldReAuth map (pending re-auth)
//   - Removes from scheduledReAuth map (active re-auth)
//   - Removes credentials listener from manager
//
// This prevents memory leaks and ensures that removed connections don't have
// lingering re-auth operations or listeners.
//
// Thread-safe: Called when connections are removed due to errors, timeouts, or pool closure.
func (r *ReAuthPoolHook) OnRemove(_ context.Context, conn *pool.Conn, _ error) {
	r.removeConn(conn.GetID())
}

// removeConn drops all re-auth state of a connection: its pending and active
// re-auth and its credentials listener.
func (r *ReAuthPoolHook) removeConn(connID uint64) {
	r.shouldReAuthLock.Lock()
	r.scheduledLock.Lock()
	delete(r.scheduledReAuth, connID)
	delete(r.shouldReAuth, connID)
	r.scheduledLock.Unlock()
	r.shouldReAuthLock.Unlock()
	r.pubSubReAuthLock.Lock()
	if state, ok := r.pubSubReAuth[connID]; ok {
		delete(r.pubSubReAuth, connID)
		state.mu.Lock()
		// A worker that already picked up the next rotation drops it;
		// one that is mid-flight finishes and stops.
		state.closed = true
		state.pending = nil
		state.mu.Unlock()
	}
	r.pubSubReAuthLock.Unlock()
	if r.manager != nil {
		r.manager.RemoveListener(connID)
	}
}

// hasReAuth reports whether a re-auth is pending or active for a connection.
func (r *ReAuthPoolHook) hasReAuth(connID uint64) bool {
	r.shouldReAuthLock.RLock()
	_, pending := r.shouldReAuth[connID]
	r.shouldReAuthLock.RUnlock()
	r.scheduledLock.RLock()
	_, scheduled := r.scheduledReAuth[connID]
	r.scheduledLock.RUnlock()
	if pending || scheduled {
		return true
	}
	r.pubSubReAuthLock.Lock()
	state, ok := r.pubSubReAuth[connID]
	r.pubSubReAuthLock.Unlock()
	if !ok {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.running || state.pending != nil
}

var _ pool.PoolHook = (*ReAuthPoolHook)(nil)
