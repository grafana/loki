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

	// workers is a semaphore channel limiting concurrent re-auth operations
	// Initialized with poolSize tokens to prevent pool exhaustion
	workers chan struct{}

	// reAuthTimeout is the maximum time to wait for acquiring a connection for re-auth
	reAuthTimeout time.Duration

	// scheduledReAuth maps connection ID to scheduled status
	// Connections in this map have a background worker attempting re-authentication
	scheduledReAuth map[uint64]bool
	scheduledLock   sync.RWMutex

	// manager is a back-reference for cleanup operations
	manager *Manager
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
	workers := make(chan struct{}, poolSize)
	// Initialize the workers channel with tokens (semaphore pattern)
	for i := 0; i < poolSize; i++ {
		workers <- struct{}{}
	}

	return &ReAuthPoolHook{
		shouldReAuth:    make(map[uint64]func(error)),
		scheduledReAuth: make(map[uint64]bool),
		workers:         workers,
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
			<-r.workers
			// safety first
			if conn == nil || (conn != nil && conn.IsClosed()) {
				r.workers <- struct{}{}
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
				r.workers <- struct{}{}
			}()

			var err error
			timeout := time.After(r.reAuthTimeout)

			// Try to acquire the connection
			// We need to ensure the connection is both Usable and not Used
			// to prevent data races with concurrent operations
			const baseDelay = 10 * time.Microsecond
			acquired := false
			attempt := 0
			for !acquired {
				select {
				case <-timeout:
					// Timeout occurred, cannot acquire connection
					err = pool.ErrConnUnusableTimeout
					reAuthFn(err)
					return
				default:
					// Try to acquire: set Usable=false, then check Used
					if conn.CompareAndSwapUsable(true, false) {
						if !conn.IsUsed() {
							acquired = true
						} else {
							// Release Usable and retry with exponential backoff
							// todo(ndyakov): think of a better way to do this without the need
							// to release the connection, but just wait till it is not used
							conn.SetUsable(true)
						}
					}
					if !acquired {
						// Exponential backoff: 10, 20, 40, 80... up to 5120 microseconds
						delay := baseDelay * time.Duration(1<<uint(attempt%10)) // Cap exponential growth
						time.Sleep(delay)
						attempt++
					}
				}
			}

			// safety first
			if !conn.IsClosed() {
				// Successfully acquired the connection, perform reauth
				reAuthFn(nil)
			}

			// Release the connection
			conn.SetUsable(true)
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
	connID := conn.GetID()
	r.shouldReAuthLock.Lock()
	r.scheduledLock.Lock()
	delete(r.scheduledReAuth, connID)
	delete(r.shouldReAuth, connID)
	r.scheduledLock.Unlock()
	r.shouldReAuthLock.Unlock()
	if r.manager != nil {
		r.manager.RemoveListener(connID)
	}
}

var _ pool.PoolHook = (*ReAuthPoolHook)(nil)
