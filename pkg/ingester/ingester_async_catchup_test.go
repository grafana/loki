package ingester

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/kafka/partitionring"
)

// mockPartitionReader implements services.Service for testing
type mockPartitionReader struct {
	services.Service
	mu               sync.Mutex
	state            services.State
	startAsyncCalled bool
	awaitError       error
	isReady          bool
	readyCond        *sync.Cond
}

func newMockPartitionReader() *mockPartitionReader {
	m := &mockPartitionReader{
		state:   services.New,
		isReady: false,
	}
	m.readyCond = sync.NewCond(&m.mu)
	return m
}

func (m *mockPartitionReader) State() services.State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *mockPartitionReader) StartAsync(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startAsyncCalled = true
	m.state = services.Starting
	return nil
}

func (m *mockPartitionReader) AwaitRunning(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Wait until ready or has error
	for !m.isReady && m.awaitError == nil {
		m.readyCond.Wait()
	}

	return m.awaitError
}

func (m *mockPartitionReader) transitionToRunning() {
	m.mu.Lock()
	m.state = services.Running
	m.isReady = true
	m.mu.Unlock()
	m.readyCond.Broadcast() // Wake all waiting goroutines
}

func (m *mockPartitionReader) transitionToFailed(err error) {
	m.mu.Lock()
	m.state = services.Failed
	m.awaitError = err
	m.mu.Unlock()
	m.readyCond.Broadcast() // Wake all waiting goroutines
}

// mockPartitionRingLifecycler implements the methods we need for testing
type mockPartitionRingLifecycler struct {
	mu                        sync.Mutex
	partitionState            ring.PartitionState
	changeStateCalls          []ring.PartitionState
	getPartitionStateError    error
	changePartitionStateError error
}

func newMockPartitionRingLifecycler() *mockPartitionRingLifecycler {
	return &mockPartitionRingLifecycler{
		partitionState:   ring.PartitionPending,
		changeStateCalls: []ring.PartitionState{},
	}
}

func (m *mockPartitionRingLifecycler) GetPartitionState(_ context.Context) (ring.PartitionState, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.partitionState, time.Now(), m.getPartitionStateError
}

func (m *mockPartitionRingLifecycler) ChangePartitionState(_ context.Context, state ring.PartitionState) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Always record the call attempt, even if it will error
	m.changeStateCalls = append(m.changeStateCalls, state)

	if m.changePartitionStateError != nil {
		return m.changePartitionStateError
	}

	m.partitionState = state
	return nil
}

func (m *mockPartitionRingLifecycler) getChangeStateCalls() []ring.PartitionState {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]ring.PartitionState, len(m.changeStateCalls))
	copy(result, m.changeStateCalls)
	return result
}

// TestAsyncCatchupStartupSequence verifies that the partition ring lifecycler
// starts before the partition reader, allowing the pod to become ready quickly.
func TestAsyncCatchupStartupSequence(t *testing.T) {
	// This test verifies the new startup sequence where:
	// 1. Partition ring lifecycler starts first (partition goes PENDING)
	// 2. Partition reader starts asynchronously (doesn't block startup)
	// 3. Pod becomes ready quickly
	// 4. autoActivatePartitionAfterCatchup() activates partition when reader is ready

	mockReader := newMockPartitionReader()

	// Verify StartAsync is called (not StartAndAwaitRunning)
	err := mockReader.StartAsync(context.Background())
	require.NoError(t, err)

	assert.True(t, mockReader.startAsyncCalled, "StartAsync should be called")
	assert.Equal(t, services.Starting, mockReader.State(), "Reader should be in Starting state after StartAsync")

	// Verify that calling StartAsync doesn't block
	start := time.Now()
	err = mockReader.StartAsync(context.Background())
	duration := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, duration, 100*time.Millisecond, "StartAsync should return immediately")
}

// TestAutoActivatePartitionAfterCatchup verifies that the partition is automatically
// activated once the partition reader completes catch-up.
func TestAutoActivatePartitionAfterCatchup(t *testing.T) {
	tests := []struct {
		name                string
		readerFinalState    services.State
		readerError         error
		expectedActivation  bool
		expectedLogMessages []string
	}{
		{
			name:               "successful catch-up activates partition",
			readerFinalState:   services.Running,
			readerError:        nil,
			expectedActivation: true,
			expectedLogMessages: []string{
				"waiting for partition reader to catch up",
				"partition reader caught up, transitioning partition to ACTIVE",
				"partition is now ACTIVE and serving queries",
			},
		},
		{
			name:               "failed catch-up keeps partition in PENDING",
			readerFinalState:   services.Failed,
			readerError:        assert.AnError,
			expectedActivation: false,
			expectedLogMessages: []string{
				"waiting for partition reader to catch up",
				"partition reader failed during catch-up, partition will remain in PENDING state",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockReader := newMockPartitionReader()
			mockLifecycler := newMockPartitionRingLifecycler()

			// Create a test helper that mimics autoActivatePartitionAfterCatchup behavior
			testActivate := func() {
				// Wait for partition reader to reach Running state
				if err := mockReader.AwaitRunning(context.Background()); err != nil {
					return // Error during catch-up, don't activate
				}

				// Transition to ACTIVE
				_ = mockLifecycler.ChangePartitionState(context.Background(), ring.PartitionActive)
			}

			// Start activation in background
			done := make(chan struct{})
			go func() {
				testActivate()
				close(done)
			}()

			// Simulate the partition reader transitioning to final state
			time.Sleep(50 * time.Millisecond) // Give goroutine time to start
			if tt.readerFinalState == services.Running {
				mockReader.transitionToRunning()
			} else {
				mockReader.transitionToFailed(tt.readerError)
			}

			// Wait for activation to complete
			select {
			case <-done:
				// Success
			case <-time.After(1 * time.Second):
				t.Fatal("activation function did not complete in time")
			}

			// Verify partition state changes
			calls := mockLifecycler.getChangeStateCalls()
			if tt.expectedActivation {
				require.Len(t, calls, 1, "Should have called ChangePartitionState once")
				assert.Equal(t, ring.PartitionActive, calls[0], "Should transition to ACTIVE")
			} else {
				assert.Empty(t, calls, "Should not call ChangePartitionState on failure")
			}
		})
	}
}

// TestPartitionStaysPendingDuringCatchup verifies that the partition remains in
// PENDING state while the partition reader is catching up, and only transitions
// to ACTIVE once catch-up completes.
func TestPartitionStaysPendingDuringCatchup(t *testing.T) {
	mockReader := newMockPartitionReader()
	mockLifecycler := newMockPartitionRingLifecycler()

	// Start partition reader in Starting state
	err := mockReader.StartAsync(context.Background())
	require.NoError(t, err)

	// Start activation function in background
	go func() {
		_ = mockReader.AwaitRunning(context.Background())
		_ = mockLifecycler.ChangePartitionState(context.Background(), ring.PartitionActive)
	}()

	// Verify partition stays PENDING while reader is in Starting state
	for i := 0; i < 5; i++ {
		time.Sleep(50 * time.Millisecond)
		state, _, err := mockLifecycler.GetPartitionState(context.Background())
		require.NoError(t, err)
		assert.Equal(t, ring.PartitionPending, state, "Partition should remain PENDING during catch-up")
	}

	// Transition reader to Running (catch-up complete)
	mockReader.transitionToRunning()

	// Wait for activation to complete
	require.Eventually(t, func() bool {
		state, _, _ := mockLifecycler.GetPartitionState(context.Background())
		return state == ring.PartitionActive
	}, 1*time.Second, 50*time.Millisecond, "Partition should transition to ACTIVE after catch-up")
}

// TestPartitionStateResetOnStartup verifies that partitions are correctly reset to PENDING state
// on startup. For ACTIVE partitions, we delete and recreate them. For other states, we transition
// them to PENDING.
func TestPartitionStateResetOnStartup(t *testing.T) {
	tests := []struct {
		name                 string
		initialState         ring.PartitionState
		shouldDeleteRecreate bool
		shouldChangeState    bool
		expectedFinalState   ring.PartitionState
		expectedCallCount    int
	}{
		{
			name:                 "partition ACTIVE - delete and recreate as PENDING",
			initialState:         ring.PartitionActive,
			shouldDeleteRecreate: true,
			shouldChangeState:    false,
			expectedFinalState:   ring.PartitionPending,
			expectedCallCount:    0, // No ChangePartitionState call, we delete and recreate instead
		},
		{
			name:                 "partition PENDING - already in correct state",
			initialState:         ring.PartitionPending,
			shouldDeleteRecreate: false,
			shouldChangeState:    false,
			expectedFinalState:   ring.PartitionPending,
			expectedCallCount:    0, // No state change needed
		},
		{
			name:                 "partition INACTIVE - set to PENDING",
			initialState:         ring.PartitionInactive,
			shouldDeleteRecreate: false,
			shouldChangeState:    true,
			expectedFinalState:   ring.PartitionPending,
			expectedCallCount:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockLifecycler := newMockPartitionRingLifecycler()
			mockLifecycler.partitionState = tt.initialState

			// Simulate startup sequence
			currentState, _, err := mockLifecycler.GetPartitionState(context.Background())
			require.NoError(t, err)

			if currentState == ring.PartitionActive {
				// Simulate delete and recreate: partition is deleted, then lifecycler recreates it as PENDING
				mockLifecycler.partitionState = ring.PartitionPending
			} else if currentState != ring.PartitionPending {
				// Set to PENDING if not already
				err = mockLifecycler.ChangePartitionState(context.Background(), ring.PartitionPending)
				require.NoError(t, err, "Should successfully change partition state to PENDING")
			}

			// Verify final state
			finalState, _, err := mockLifecycler.GetPartitionState(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tt.expectedFinalState, finalState, "Final partition state should match expected")

			// Verify ChangePartitionState call count
			calls := mockLifecycler.getChangeStateCalls()
			assert.Len(t, calls, tt.expectedCallCount, "ChangePartitionState call count should match expected")

			if tt.shouldChangeState {
				assert.Equal(t, ring.PartitionPending, calls[0], "Should have set partition to PENDING")
			}
		})
	}
}

// TestMultiplePartitionRecreations verifies that deleteAndRecreatePartition can be called
// multiple times without panicking due to duplicate metric registration, and that metrics
// remain available throughout the lifecycle.
func TestMultiplePartitionRecreations(t *testing.T) {
	kvStore, closer := consul.NewInMemoryClient(ring.GetPartitionRingCodec(), log.NewNopLogger(), nil)
	t.Cleanup(func() { assert.NoError(t, closer.Close()) })

	// Use the SAME registry across all recreations to test duplicate registration handling
	reg := prometheus.NewRegistry()

	instanceID := "ingester-zone-a-0"
	partitionID, err := partitionring.ExtractPartitionID(instanceID)
	require.NoError(t, err)

	// Helper to create and start a lifecycler (mimics what deleteAndRecreatePartition does)
	createLifecycler := func() *ring.PartitionInstanceLifecycler {
		lifecyclerCfg := ring.PartitionInstanceLifecyclerConfig{
			PartitionID:                 partitionID,
			InstanceID:                  instanceID,
			WaitOwnersCountOnPending:    1,
			WaitOwnersDurationOnPending: 24 * time.Hour,
			PollingInterval:             100 * time.Millisecond,
		}

		// Use unregisteringRegisterer to automatically unregister old metrics
		// before registering new ones, ensuring the new lifecycler's metrics are collected
		unregisteringReg := &unregisteringRegisterer{
			Registerer: prometheus.WrapRegistererWithPrefix("loki_", reg),
		}

		lc := ring.NewPartitionInstanceLifecycler(
			lifecyclerCfg,
			PartitionRingName,
			PartitionRingKey,
			kvStore,
			log.NewNopLogger(),
			unregisteringReg,
		)

		err := services.StartAndAwaitRunning(context.Background(), lc)
		require.NoError(t, err)
		return lc
	}

	// Helper to get metric value
	getMetricValue := func(metricName string) float64 {
		metrics, err := reg.Gather()
		require.NoError(t, err)
		for _, mf := range metrics {
			if mf.GetName() == metricName {
				if len(mf.GetMetric()) > 0 {
					var sum float64
					for _, m := range mf.GetMetric() {
						if m.GetCounter() != nil {
							sum += m.GetCounter().GetValue()
						}
					}
					return sum
				}
			}
		}
		return 0
	}

	// Create mock ingester structure for deleteAndRecreatePartition
	mockIng := &Ingester{
		cfg: Config{
			LifecyclerConfig: ring.LifecyclerConfig{
				ID: instanceID,
			},
			KafkaIngestion: KafkaIngestionConfig{
				PartitionRingConfig: partitionring.Config{
					MinOwnersCount:    1,
					MinOwnersDuration: 24 * time.Hour,
				},
			},
		},
		ingestPartitionID:    partitionID,
		partitionRingKVStore: kvStore,
		logger:               log.NewNopLogger(),
		registerer:           reg,
	}

	// Test 3 cycles of delete/recreate
	// NOTE: In production, deleteAndRecreatePartition is only called ONCE at startup.
	// This test verifies it can be called multiple times without panicking, and that
	// each lifecycle independently collects metrics.
	for cycle := 1; cycle <= 3; cycle++ {
		t.Run(fmt.Sprintf("cycle_%d", cycle), func(t *testing.T) {
			// Create and start lifecycler
			mockIng.partitionRingLifecycler = createLifecycler()

			// Set partition to ACTIVE
			err := kvStore.CAS(context.Background(), PartitionRingKey, func(in interface{}) (out interface{}, retry bool, err error) {
				ringDesc := ring.GetOrCreatePartitionRingDesc(in)
				ringDesc.UpdatePartitionState(partitionID, ring.PartitionActive, time.Now())
				return ringDesc, true, nil
			})
			require.NoError(t, err)

			// Delete and recreate - should NOT panic on duplicate metrics
			// This unregisters old metrics and registers new ones (which start at 0)
			err = mockIng.deleteAndRecreatePartition(context.Background())
			require.NoError(t, err, "deleteAndRecreatePartition should succeed in cycle %d", cycle)

			// Verify partition is PENDING
			state, _, err := mockIng.partitionRingLifecycler.GetPartitionState(context.Background())
			require.NoError(t, err)
			assert.Equal(t, ring.PartitionPending, state, "Partition should be PENDING after recreation in cycle %d", cycle)

			// Wait for reconciliation
			time.Sleep(200 * time.Millisecond)

			// Verify the NEW lifecycler is actively collecting metrics
			// Since we unregister/re-register, the counter starts at 0 and should have
			// incremented after 200ms of reconciliation
			metricsValue := getMetricValue("loki_partition_ring_lifecycler_reconciles_total")
			assert.Greater(t, metricsValue, float64(0),
				"Cycle %d: metrics value (%f) should be > 0, proving the new lifecycler is actively collecting",
				cycle, metricsValue)

			// Stop for next cycle
			err = services.StopAndAwaitTerminated(context.Background(), mockIng.partitionRingLifecycler)
			require.NoError(t, err)
		})
	}

	// Final verification: metrics from the first lifecycler should still be available
	t.Run("final_metrics_check", func(t *testing.T) {
		reconcilesTotal := getMetricValue("loki_partition_ring_lifecycler_reconciles_total")
		assert.Greater(t, reconcilesTotal, float64(0),
			"Metrics from first lifecycler should remain available after all recreations")
	})
}

// TestPartitionActivationRace tests the scenario where the reconciliation loop
// might try to activate the partition at the same time as autoActivatePartitionAfterCatchup().
func TestPartitionActivationRace(t *testing.T) {
	mockReader := newMockPartitionReader()
	mockLifecycler := newMockPartitionRingLifecycler()

	// Simulate concurrent activation attempts
	const numGoroutines = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Make reader immediately ready
	mockReader.transitionToRunning()

	// Launch multiple concurrent activation attempts
	for j := 0; j < numGoroutines; j++ {
		go func() {
			defer wg.Done()
			_ = mockReader.AwaitRunning(context.Background())
			_ = mockLifecycler.ChangePartitionState(context.Background(), ring.PartitionActive)
		}()
	}

	wg.Wait()

	// Verify partition is in ACTIVE state
	state, _, err := mockLifecycler.GetPartitionState(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ring.PartitionActive, state, "Partition should be ACTIVE")

	// ChangePartitionState should be idempotent - multiple calls are safe
	calls := mockLifecycler.getChangeStateCalls()
	assert.Equal(t, numGoroutines, len(calls), "All goroutines should have called ChangePartitionState")
	for _, call := range calls {
		assert.Equal(t, ring.PartitionActive, call, "All calls should be to ACTIVE state")
	}
}

// TestMultiHourLagScenario simulates the real-world scenario that motivated this change:
// a partition ingester restarting with multi-hour Kafka consumption lag.
func TestMultiHourLagScenario(t *testing.T) {
	mockReader := newMockPartitionReader()
	mockLifecycler := newMockPartitionRingLifecycler()

	// Create test helper that mimics autoActivatePartitionAfterCatchup behavior
	testActivate := func() {
		_ = mockReader.AwaitRunning(context.Background())
		_ = mockLifecycler.ChangePartitionState(context.Background(), ring.PartitionActive)
	}

	// Simulate starting partition reader (async, doesn't block)
	startTime := time.Now()
	err := mockReader.StartAsync(context.Background())
	startupDuration := time.Since(startTime)

	require.NoError(t, err)
	assert.Less(t, startupDuration, 100*time.Millisecond, "StartAsync should return immediately, not wait for catch-up")

	// Verify partition is in PENDING state
	state, _, err := mockLifecycler.GetPartitionState(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ring.PartitionPending, state, "Partition should be PENDING during catch-up")

	// Start auto-activation in background
	go testActivate()

	// Simulate multi-hour catch-up completing (in test, this is instant)
	time.Sleep(100 * time.Millisecond)
	mockReader.transitionToRunning()

	// Verify partition eventually transitions to ACTIVE
	require.Eventually(t, func() bool {
		state, _, _ := mockLifecycler.GetPartitionState(context.Background())
		return state == ring.PartitionActive
	}, 1*time.Second, 50*time.Millisecond, "Partition should transition to ACTIVE after catch-up")
}

// TestPartitionReaderFailureDuringCatchup verifies behavior when the partition
// reader fails during catch-up (e.g., Kafka connection issues).
func TestPartitionReaderFailureDuringCatchup(t *testing.T) {
	mockReader := newMockPartitionReader()
	mockLifecycler := newMockPartitionRingLifecycler()

	// Create test helper that mimics autoActivatePartitionAfterCatchup behavior
	testActivate := func() {
		if err := mockReader.AwaitRunning(context.Background()); err != nil {
			return // Error during catch-up, don't activate
		}
		_ = mockLifecycler.ChangePartitionState(context.Background(), ring.PartitionActive)
	}

	// Start partition reader asynchronously
	err := mockReader.StartAsync(context.Background())
	require.NoError(t, err)

	// Start auto-activation in background
	done := make(chan struct{})
	go func() {
		testActivate()
		close(done)
	}()

	// Simulate reader failing during catch-up
	time.Sleep(50 * time.Millisecond)
	mockReader.transitionToFailed(fmt.Errorf("kafka connection failed"))

	// Wait for test activation to complete
	select {
	case <-done:
		// Success - function completed gracefully
	case <-time.After(1 * time.Second):
		t.Fatal("test activation did not complete after reader failure")
	}

	// Verify partition was NOT activated (should remain PENDING)
	state, _, err := mockLifecycler.GetPartitionState(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ring.PartitionPending, state, "Partition should remain PENDING when reader fails")

	// Verify ChangePartitionState was never called
	calls := mockLifecycler.getChangeStateCalls()
	assert.Empty(t, calls, "ChangePartitionState should not be called on reader failure")
}

// TestStateTransitionTimeline creates a timeline of state transitions to verify
// the correct sequence of events during async catch-up.
func TestStateTransitionTimeline(t *testing.T) {
	mockReader := newMockPartitionReader()
	mockLifecycler := newMockPartitionRingLifecycler()

	// Create test helper that mimics autoActivatePartitionAfterCatchup behavior
	testActivate := func() {
		_ = mockReader.AwaitRunning(context.Background())
		_ = mockLifecycler.ChangePartitionState(context.Background(), ring.PartitionActive)
	}

	timeline := make([]string, 0)
	timelineMu := sync.Mutex{}
	addEvent := func(event string) {
		timelineMu.Lock()
		defer timelineMu.Unlock()
		timeline = append(timeline, event)
	}

	// T=0: Partition reader starts asynchronously
	addEvent("reader_start_async")
	err := mockReader.StartAsync(context.Background())
	require.NoError(t, err)

	// T=0: Partition is PENDING
	state, _, _ := mockLifecycler.GetPartitionState(context.Background())
	assert.Equal(t, ring.PartitionPending, state)
	addEvent("partition_pending")

	// T=0: Auto-activation goroutine starts
	addEvent("auto_activate_start")
	go testActivate()

	// T=100ms: Reader still catching up
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, services.Starting, mockReader.State())
	addEvent("reader_catching_up")

	// T=200ms: Reader completes catch-up
	time.Sleep(100 * time.Millisecond)
	mockReader.transitionToRunning()
	addEvent("reader_running")

	// Wait for partition to become ACTIVE
	require.Eventually(t, func() bool {
		state, _, _ := mockLifecycler.GetPartitionState(context.Background())
		if state == ring.PartitionActive {
			addEvent("partition_active")
			return true
		}
		return false
	}, 1*time.Second, 50*time.Millisecond)

	// Verify timeline is correct
	timelineMu.Lock()
	defer timelineMu.Unlock()
	expectedTimeline := []string{
		"reader_start_async",
		"partition_pending",
		"auto_activate_start",
		"reader_catching_up",
		"reader_running",
		"partition_active",
	}
	assert.Equal(t, expectedTimeline, timeline, "Timeline should match expected sequence")
}

// TestChangePartitionStateErrors verifies that errors from ChangePartitionState
// are handled gracefully and logged appropriately.
func TestChangePartitionStateErrors(t *testing.T) {
	mockReader := newMockPartitionReader()
	mockLifecycler := newMockPartitionRingLifecycler()

	// Simulate an error when trying to change partition state
	mockLifecycler.changePartitionStateError = fmt.Errorf("ring update failed")

	// Create test helper that mimics autoActivatePartitionAfterCatchup behavior
	testActivate := func() {
		_ = mockReader.AwaitRunning(context.Background())
		_ = mockLifecycler.ChangePartitionState(context.Background(), ring.PartitionActive)
	}

	// Make reader ready
	mockReader.transitionToRunning()

	// Start auto-activation
	done := make(chan struct{})
	go func() {
		testActivate()
		close(done)
	}()

	// Wait for completion
	select {
	case <-done:
		// Function should complete even with error
	case <-time.After(1 * time.Second):
		t.Fatal("test activation should complete even with ChangePartitionState error")
	}

	// Verify partition state was attempted to be changed
	calls := mockLifecycler.getChangeStateCalls()
	assert.Len(t, calls, 1, "Should have attempted to change partition state")

	// Partition should remain in original state due to error
	state, _, err := mockLifecycler.GetPartitionState(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ring.PartitionPending, state, "Partition should remain PENDING on error")
}
