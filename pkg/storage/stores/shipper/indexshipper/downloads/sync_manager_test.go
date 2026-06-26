package downloads

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
)

// blockingSync is an injectable sync func for syncManager tests. It records the
// triggers it was called with and, while gated, blocks until the gate is closed
// so a sync can be held "in progress".
type blockingSync struct {
	mu       sync.Mutex
	triggers []string
	gate     chan struct{} // if non-nil, each call blocks until it is closed
}

func (b *blockingSync) fn(_ context.Context, trigger string) error {
	b.mu.Lock()
	b.triggers = append(b.triggers, trigger)
	gate := b.gate
	b.mu.Unlock()

	if gate != nil {
		<-gate
	}
	return nil
}

func (b *blockingSync) calls() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.triggers...)
}

func newTestSyncManager(fn func(context.Context, string) error) *syncManager {
	return newSyncManager(log.NewNopLogger(), fn)
}

func TestSyncManager_Status(t *testing.T) {
	b := &blockingSync{gate: make(chan struct{})}
	sm := newTestSyncManager(b.fn)

	// Idle: not in progress, no prior sync.
	st := sm.Status()
	require.False(t, st.InProgress)
	require.Zero(t, st.LastDuration)

	// A manual sync is reported in progress while the injected work blocks.
	// TriggerManual returns only once the sync has been marked in progress, so the
	// status reflects it immediately - no polling needed.
	require.True(t, sm.TriggerManual(context.Background()))
	require.True(t, sm.Status().InProgress)
	require.GreaterOrEqual(t, sm.Status().CurrentDuration, time.Duration(0))

	// Release the work; once finished, status returns to idle with a last duration.
	close(b.gate)
	sm.Wait()
	st = sm.Status()
	require.False(t, st.InProgress)
	require.GreaterOrEqual(t, st.LastDuration, time.Duration(0))
	require.Equal(t, syncTriggerManual, st.LastTrigger)
}

func TestSyncManager_TriggerManualSerializes(t *testing.T) {
	b := &blockingSync{gate: make(chan struct{})}
	sm := newTestSyncManager(b.fn)

	// First trigger starts a sync that blocks; concurrent triggers must be no-ops
	// so two syncs never run at once.
	require.True(t, sm.TriggerManual(context.Background()))

	var started atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if sm.TriggerManual(context.Background()) {
				started.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Zero(t, started.Load(), "no manual sync may start while one is in progress")

	// Release the in-flight sync and drain it; with nothing in progress a new
	// trigger starts a sync.
	close(b.gate)
	sm.Wait()
	require.True(t, sm.TriggerManual(context.Background()))
	sm.Wait()

	// Exactly two manual syncs ran; the 20 concurrent triggers were all no-ops.
	require.Equal(t, []string{syncTriggerManual, syncTriggerManual}, b.calls())
}

func TestSyncManager_RunPeriodic(t *testing.T) {
	b := &blockingSync{gate: make(chan struct{})}
	sm := newTestSyncManager(b.fn)

	// Hold a manual sync in progress. TriggerManual returns once it is marked so,
	// so the in-progress state is visible without polling.
	require.True(t, sm.TriggerManual(context.Background()))
	require.True(t, sm.Status().InProgress)

	// A periodic run must skip while a sync is in progress: it returns nil without
	// starting a new sync, leaving the in-progress manual sync untouched.
	require.NoError(t, sm.RunPeriodic(context.Background()))
	require.True(t, sm.Status().InProgress)
	require.Equal(t, syncTriggerManual, sm.Status().LastTrigger)

	// Release the manual sync; once idle, a periodic run executes the work.
	close(b.gate)
	sm.Wait()
	require.NoError(t, sm.RunPeriodic(context.Background()))

	// The work ran exactly twice - the manual sync, then the idle periodic run; the
	// periodic run that overlapped the manual sync skipped.
	require.Equal(t, []string{syncTriggerManual, syncTriggerPeriodic}, b.calls())
	require.Equal(t, syncTriggerPeriodic, sm.Status().LastTrigger)
}

func TestSyncManager_RunPeriodicReturnsError(t *testing.T) {
	wantErr := errors.New("boom")
	sm := newTestSyncManager(func(context.Context, string) error { return wantErr })

	require.ErrorIs(t, sm.RunPeriodic(context.Background()), wantErr)
}

func TestSyncManager_WaitDrains(t *testing.T) {
	var done atomic.Bool
	sm := newTestSyncManager(func(context.Context, string) error {
		time.Sleep(10 * time.Millisecond)
		done.Store(true)
		return nil
	})

	require.True(t, sm.TriggerManual(context.Background()))
	sm.Wait()

	require.True(t, done.Load(), "Wait must block until the in-flight sync finished")
	require.False(t, sm.Status().InProgress)
}
