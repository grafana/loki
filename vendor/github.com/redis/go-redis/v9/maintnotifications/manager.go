package maintnotifications

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9/internal"
	"github.com/redis/go-redis/v9/internal/interfaces"
	"github.com/redis/go-redis/v9/internal/maintnotifications/logs"
	"github.com/redis/go-redis/v9/internal/pool"
	"github.com/redis/go-redis/v9/push"
)

// Push notification type constants for maintenance
const (
	NotificationMoving      = "MOVING"       // Per-connection handoff notification
	NotificationMigrating   = "MIGRATING"    // Per-connection migration start notification - relaxes timeouts
	NotificationMigrated    = "MIGRATED"     // Per-connection migration complete notification - clears relaxed timeouts
	NotificationFailingOver = "FAILING_OVER" // Per-connection failover start notification - relaxes timeouts
	NotificationFailedOver  = "FAILED_OVER"  // Per-connection failover complete notification - clears relaxed timeouts
	NotificationSMigrating  = "SMIGRATING"   // Cluster slot migrating notification - relaxes timeouts
	NotificationSMigrated   = "SMIGRATED"    // Cluster slot migrated notification -  unrelaxes timeouts and triggers cluster state reload
)

// maintenanceNotificationTypes contains all notification types that maintenance handles
var maintenanceNotificationTypes = []string{
	NotificationMoving,
	NotificationMigrating,
	NotificationMigrated,
	NotificationFailingOver,
	NotificationFailedOver,
	NotificationSMigrating,
	NotificationSMigrated,
}

// NotificationHook is called before and after notification processing
// PreHook can modify the notification and return false to skip processing
// PostHook is called after successful processing
type NotificationHook interface {
	PreHook(ctx context.Context, notificationCtx push.NotificationHandlerContext, notificationType string, notification []interface{}) ([]interface{}, bool)
	PostHook(ctx context.Context, notificationCtx push.NotificationHandlerContext, notificationType string, notification []interface{}, result error)
}

// MovingOperationKey provides a unique key for tracking MOVING operations
// that combines sequence ID with connection identifier to handle duplicate
// sequence IDs across multiple connections to the same node.
type MovingOperationKey struct {
	SeqID  int64  // Sequence ID from MOVING notification
	ConnID uint64 // Unique connection identifier
}

// String returns a string representation of the key for debugging
func (k MovingOperationKey) String() string {
	return fmt.Sprintf("seq:%d-conn:%d", k.SeqID, k.ConnID)
}

// Manager provides a simplified upgrade functionality with hooks and atomic state.
type Manager struct {
	client  interfaces.ClientInterface
	config  *Config
	options interfaces.OptionsInterface
	pool    pool.Pooler

	// MOVING operation tracking - using sync.Map for better concurrent performance
	activeMovingOps sync.Map // map[MovingOperationKey]*MovingOperation

	// SMIGRATED notification deduplication - tracks processed SeqIDs
	// Multiple connections may receive the same SMIGRATED notification
	processedSMigratedSeqIDs sync.Map // map[int64]bool

	// Atomic state tracking - no locks needed for state queries
	activeOperationCount atomic.Int64 // Number of active operations
	closed               atomic.Bool  // Manager closed state

	// Notification hooks for extensibility
	hooks        []NotificationHook
	hooksMu      sync.RWMutex // Protects hooks slice
	poolHooksRef *PoolHook

	// Cluster state reload callback for SMIGRATED notifications
	clusterStateReloadCallback ClusterStateReloadCallback
}

// MovingOperation tracks an active MOVING operation.
type MovingOperation struct {
	SeqID       int64
	NewEndpoint string
	StartTime   time.Time
	Deadline    time.Time
}

// ClusterStateReloadCallback is a callback function that triggers cluster state reload.
// This is used by node clients to notify their parent ClusterClient about SMIGRATED notifications.
// The hostPort parameter indicates the destination node (e.g., "127.0.0.1:6379").
// The slotRanges parameter contains the migrated slots (e.g., ["1234", "5000-6000"]).
// Currently, implementations typically reload the entire cluster state, but in the future
// this could be optimized to reload only the specific slots.
type ClusterStateReloadCallback func(ctx context.Context, hostPort string, slotRanges []string)

// NewManager creates a new simplified manager.
func NewManager(client interfaces.ClientInterface, pool pool.Pooler, config *Config) (*Manager, error) {
	if client == nil {
		return nil, ErrInvalidClient
	}

	hm := &Manager{
		client:  client,
		pool:    pool,
		options: client.GetOptions(),
		config:  config.Clone(),
		hooks:   make([]NotificationHook, 0),
	}

	// Set up push notification handling
	if err := hm.setupPushNotifications(); err != nil {
		return nil, err
	}

	return hm, nil
}

// GetPoolHook creates a pool hook with a custom dialer.
func (hm *Manager) InitPoolHook(baseDialer func(context.Context, string, string) (net.Conn, error)) {
	poolHook := hm.createPoolHook(baseDialer)
	hm.pool.AddPoolHook(poolHook)
}

// setupPushNotifications sets up push notification handling by registering with the client's processor.
func (hm *Manager) setupPushNotifications() error {
	processor := hm.client.GetPushProcessor()
	if processor == nil {
		return ErrInvalidClient // Client doesn't support push notifications
	}

	// Create our notification handler
	handler := &NotificationHandler{manager: hm, operationsManager: hm}

	// Register handlers for all upgrade notifications with the client's processor
	for _, notificationType := range maintenanceNotificationTypes {
		if err := processor.RegisterHandler(notificationType, handler, true); err != nil {
			return errors.New(logs.FailedToRegisterHandler(notificationType, err))
		}
	}

	return nil
}

// TrackMovingOperationWithConnID starts a new MOVING operation with a specific connection ID.
func (hm *Manager) TrackMovingOperationWithConnID(ctx context.Context, newEndpoint string, deadline time.Time, seqID int64, connID uint64) error {
	// Create composite key
	key := MovingOperationKey{
		SeqID:  seqID,
		ConnID: connID,
	}

	// Create MOVING operation record
	movingOp := &MovingOperation{
		SeqID:       seqID,
		NewEndpoint: newEndpoint,
		StartTime:   time.Now(),
		Deadline:    deadline,
	}

	// Use LoadOrStore for atomic check-and-set operation
	if _, loaded := hm.activeMovingOps.LoadOrStore(key, movingOp); loaded {
		// Duplicate MOVING notification, ignore
		if internal.LogLevel.DebugOrAbove() { // Debug level
			internal.Logger.Printf(context.Background(), logs.DuplicateMovingOperation(connID, newEndpoint, seqID))
		}
		return nil
	}
	if internal.LogLevel.DebugOrAbove() { // Debug level
		internal.Logger.Printf(context.Background(), logs.TrackingMovingOperation(connID, newEndpoint, seqID))
	}

	// Increment active operation count atomically
	hm.activeOperationCount.Add(1)

	return nil
}

// UntrackOperationWithConnID completes a MOVING operation with a specific connection ID.
func (hm *Manager) UntrackOperationWithConnID(seqID int64, connID uint64) {
	// Create composite key
	key := MovingOperationKey{
		SeqID:  seqID,
		ConnID: connID,
	}

	// Remove from active operations atomically
	if _, loaded := hm.activeMovingOps.LoadAndDelete(key); loaded {
		if internal.LogLevel.DebugOrAbove() { // Debug level
			internal.Logger.Printf(context.Background(), logs.UntrackingMovingOperation(connID, seqID))
		}
		// Decrement active operation count only if operation existed
		hm.activeOperationCount.Add(-1)
	} else {
		if internal.LogLevel.DebugOrAbove() { // Debug level
			internal.Logger.Printf(context.Background(), logs.OperationNotTracked(connID, seqID))
		}
	}
}

// GetActiveMovingOperations returns active operations with composite keys.
// WARNING: This method creates a new map and copies all operations on every call.
// Use sparingly, especially in hot paths or high-frequency logging.
func (hm *Manager) GetActiveMovingOperations() map[MovingOperationKey]*MovingOperation {
	result := make(map[MovingOperationKey]*MovingOperation)

	// Iterate over sync.Map to build result
	hm.activeMovingOps.Range(func(key, value interface{}) bool {
		k := key.(MovingOperationKey)
		op := value.(*MovingOperation)

		// Create a copy to avoid sharing references
		result[k] = &MovingOperation{
			SeqID:       op.SeqID,
			NewEndpoint: op.NewEndpoint,
			StartTime:   op.StartTime,
			Deadline:    op.Deadline,
		}
		return true // Continue iteration
	})

	return result
}

// IsHandoffInProgress returns true if any handoff is in progress.
// Uses atomic counter for lock-free operation.
func (hm *Manager) IsHandoffInProgress() bool {
	return hm.activeOperationCount.Load() > 0
}

// GetActiveOperationCount returns the number of active operations.
// Uses atomic counter for lock-free operation.
func (hm *Manager) GetActiveOperationCount() int64 {
	return hm.activeOperationCount.Load()
}

// MarkSMigratedSeqIDProcessed attempts to mark a SMIGRATED SeqID as processed.
// Returns true if this is the first time processing this SeqID (should process),
// false if it was already processed (should skip).
// This prevents duplicate processing when multiple connections receive the same notification.
func (hm *Manager) MarkSMigratedSeqIDProcessed(seqID int64) bool {
	_, alreadyProcessed := hm.processedSMigratedSeqIDs.LoadOrStore(seqID, true)
	return !alreadyProcessed // Return true if NOT already processed
}

// Close closes the manager.
func (hm *Manager) Close() error {
	// Use atomic operation for thread-safe close check
	if !hm.closed.CompareAndSwap(false, true) {
		return nil // Already closed
	}

	// Shutdown the pool hook if it exists
	if hm.poolHooksRef != nil {
		// Use a timeout to prevent hanging indefinitely
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := hm.poolHooksRef.Shutdown(shutdownCtx)
		if err != nil {
			// was not able to close pool hook, keep closed state false
			hm.closed.Store(false)
			return err
		}
		// Remove the pool hook from the pool
		if hm.pool != nil {
			hm.pool.RemovePoolHook(hm.poolHooksRef)
		}
	}

	// Clear all active operations
	hm.activeMovingOps.Range(func(key, value interface{}) bool {
		hm.activeMovingOps.Delete(key)
		return true
	})

	// Reset counter
	hm.activeOperationCount.Store(0)

	return nil
}

// GetState returns current state using atomic counter for lock-free operation.
func (hm *Manager) GetState() State {
	if hm.activeOperationCount.Load() > 0 {
		return StateMoving
	}
	return StateIdle
}

// processPreHooks calls all pre-hooks and returns the modified notification and whether to continue processing.
func (hm *Manager) processPreHooks(ctx context.Context, notificationCtx push.NotificationHandlerContext, notificationType string, notification []interface{}) ([]interface{}, bool) {
	hm.hooksMu.RLock()
	defer hm.hooksMu.RUnlock()

	currentNotification := notification

	for _, hook := range hm.hooks {
		modifiedNotification, shouldContinue := hook.PreHook(ctx, notificationCtx, notificationType, currentNotification)
		if !shouldContinue {
			return modifiedNotification, false
		}
		currentNotification = modifiedNotification
	}

	return currentNotification, true
}

// processPostHooks calls all post-hooks with the processing result.
func (hm *Manager) processPostHooks(ctx context.Context, notificationCtx push.NotificationHandlerContext, notificationType string, notification []interface{}, result error) {
	hm.hooksMu.RLock()
	defer hm.hooksMu.RUnlock()

	for _, hook := range hm.hooks {
		hook.PostHook(ctx, notificationCtx, notificationType, notification, result)
	}
}

// createPoolHook creates a pool hook with this manager already set.
func (hm *Manager) createPoolHook(baseDialer func(context.Context, string, string) (net.Conn, error)) *PoolHook {
	if hm.poolHooksRef != nil {
		return hm.poolHooksRef
	}
	// Get pool size from client options for better worker defaults
	poolSize := 0
	if hm.options != nil {
		poolSize = hm.options.GetPoolSize()
	}

	hm.poolHooksRef = NewPoolHookWithPoolSize(baseDialer, hm.options.GetNetwork(), hm.config, hm, poolSize)
	hm.poolHooksRef.SetPool(hm.pool)

	return hm.poolHooksRef
}

func (hm *Manager) AddNotificationHook(notificationHook NotificationHook) {
	hm.hooksMu.Lock()
	defer hm.hooksMu.Unlock()
	hm.hooks = append(hm.hooks, notificationHook)
}

// SetClusterStateReloadCallback sets the callback function that will be called when a SMIGRATED notification is received.
// This allows node clients to notify their parent ClusterClient to reload cluster state.
func (hm *Manager) SetClusterStateReloadCallback(callback ClusterStateReloadCallback) {
	hm.clusterStateReloadCallback = callback
}

// TriggerClusterStateReload calls the cluster state reload callback if it's set.
// This is called when a SMIGRATED notification is received.
func (hm *Manager) TriggerClusterStateReload(ctx context.Context, hostPort string, slotRanges []string) {
	if hm.clusterStateReloadCallback != nil {
		hm.clusterStateReloadCallback(ctx, hostPort, slotRanges)
	}
}
