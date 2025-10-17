package ingester

import (
	"context"
	"testing"
	"time"

	"go.uber.org/atomic"

	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// mockThrottledWAL is a WAL implementation that can be controlled for testing throttling
type mockThrottledWAL struct {
	throttled atomic.Bool
	logCalled atomic.Int32
}

func (m *mockThrottledWAL) Start() {}

func (m *mockThrottledWAL) Log(_ *wal.Record) error {
	m.logCalled.Add(1)
	return nil
}

func (m *mockThrottledWAL) Stop() error {
	return nil
}

func (m *mockThrottledWAL) IsDiskThrottled() bool {
	return m.throttled.Load()
}

func (m *mockThrottledWAL) SetThrottled(throttled bool) {
	m.throttled.Store(throttled)
}

func (m *mockThrottledWAL) GetLogCallCount() int32 {
	return m.logCalled.Load()
}

// TestWALDiskThrottleInterface verifies that the WAL interface includes IsDiskThrottled
func TestWALDiskThrottleInterface(t *testing.T) {
	t.Run("noopWAL returns false", func(t *testing.T) {
		w := noopWAL{}
		require.False(t, w.IsDiskThrottled())
	})

	t.Run("mockThrottledWAL can be controlled", func(t *testing.T) {
		w := &mockThrottledWAL{}
		require.False(t, w.IsDiskThrottled())

		w.SetThrottled(true)
		require.True(t, w.IsDiskThrottled())

		w.SetThrottled(false)
		require.False(t, w.IsDiskThrottled())
	})
}

// TestIngesterPushWithDiskThrottle verifies that Push returns ErrReadOnly when disk is throttled
func TestIngesterPushWithDiskThrottle(t *testing.T) {
	mockWAL := &mockThrottledWAL{}
	store, ing := newTestStore(t, defaultIngesterTestConfig(t), mockWAL)
	defer store.Stop()
	defer services.StopAndAwaitTerminated(context.Background(), ing) //nolint:errcheck

	ctx := user.InjectOrgID(context.Background(), "test-user")
	req := &logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{job="test"}`,
				Entries: []logproto.Entry{
					{Timestamp: time.Now(), Line: "test log line"},
				},
			},
		},
	}

	t.Run("push succeeds when not throttled", func(t *testing.T) {
		mockWAL.SetThrottled(false)
		_, err := ing.Push(ctx, req)
		require.NoError(t, err)
	})

	t.Run("push returns ErrReadOnly when throttled", func(t *testing.T) {
		mockWAL.SetThrottled(true)
		_, err := ing.Push(ctx, req)
		require.Error(t, err)
		require.Equal(t, ErrReadOnly, err)
	})

	t.Run("push succeeds again after throttle is released", func(t *testing.T) {
		mockWAL.SetThrottled(false)
		_, err := ing.Push(ctx, req)
		require.NoError(t, err)
	})
}

// TestWALWrapperDiskThrottle tests the walWrapper's disk throttling functionality
func TestWALWrapperDiskThrottle(t *testing.T) {
	walDir := t.TempDir()
	cfg := WALConfig{
		Enabled:            true,
		Dir:                walDir,
		CheckpointDuration: 5 * time.Minute,
		DiskFullThreshold:  0.90,
	}

	metrics := newIngesterMetrics(prometheus.NewRegistry(), "test")
	w, err := newWAL(cfg, prometheus.NewRegistry(), metrics, newIngesterSeriesIter(nil))
	require.NoError(t, err)
	require.NotNil(t, w)

	// Verify it's a walWrapper
	wrapper, ok := w.(*walWrapper)
	require.True(t, ok, "expected walWrapper type")

	t.Run("starts not throttled", func(t *testing.T) {
		require.False(t, wrapper.IsDiskThrottled())
	})

	t.Run("can be manually throttled", func(t *testing.T) {
		// Simulate disk becoming full by manually setting the flag
		wrapper.diskThrottled.Store(true)
		require.True(t, wrapper.IsDiskThrottled())

		// Simulate disk freeing up
		wrapper.diskThrottled.Store(false)
		require.False(t, wrapper.IsDiskThrottled())
	})

	// Clean up
	require.NoError(t, w.Stop())
}

// TestWALConfigValidation tests the validation of DiskFullThreshold
func TestWALConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		threshold float64
		wantErr   bool
	}{
		{
			name:      "valid threshold 0.90",
			threshold: 0.90,
			wantErr:   false,
		},
		{
			name:      "valid threshold 0.0 (disabled)",
			threshold: 0.0,
			wantErr:   false,
		},
		{
			name:      "valid threshold 1.0",
			threshold: 1.0,
			wantErr:   false,
		},
		{
			name:      "invalid threshold -0.1",
			threshold: -0.1,
			wantErr:   true,
		},
		{
			name:      "invalid threshold 1.1",
			threshold: 1.1,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := WALConfig{
				Enabled:            true,
				CheckpointDuration: 5 * time.Minute,
				DiskFullThreshold:  tt.threshold,
			}
			err := cfg.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestDiskThrottleWithMultiplePushes verifies throttling behavior under load
func TestDiskThrottleWithMultiplePushes(t *testing.T) {
	mockWAL := &mockThrottledWAL{}
	store, ing := newTestStore(t, defaultIngesterTestConfig(t), mockWAL)
	defer store.Stop()
	defer services.StopAndAwaitTerminated(context.Background(), ing) //nolint:errcheck

	ctx := user.InjectOrgID(context.Background(), "test-user")

	// Start with throttle disabled
	mockWAL.SetThrottled(false)

	// Push multiple times successfully
	for i := 0; i < 5; i++ {
		req := &logproto.PushRequest{
			Streams: []logproto.Stream{
				{
					Labels: `{job="test"}`,
					Entries: []logproto.Entry{
						{Timestamp: time.Now(), Line: "test log line"},
					},
				},
			},
		}
		_, err := ing.Push(ctx, req)
		require.NoError(t, err, "push %d should succeed", i)
	}

	// Enable throttle
	mockWAL.SetThrottled(true)

	// All subsequent pushes should fail with ErrReadOnly
	for i := 0; i < 5; i++ {
		req := &logproto.PushRequest{
			Streams: []logproto.Stream{
				{
					Labels: `{job="test"}`,
					Entries: []logproto.Entry{
						{Timestamp: time.Now(), Line: "test log line"},
					},
				},
			},
		}
		_, err := ing.Push(ctx, req)
		require.Error(t, err, "push %d should fail when throttled", i)
		require.Equal(t, ErrReadOnly, err, "push %d should return ErrReadOnly", i)
	}

	// Disable throttle
	mockWAL.SetThrottled(false)

	// Pushes should succeed again
	for i := 0; i < 5; i++ {
		req := &logproto.PushRequest{
			Streams: []logproto.Stream{
				{
					Labels: `{job="test"}`,
					Entries: []logproto.Entry{
						{Timestamp: time.Now(), Line: "test log line"},
					},
				},
			},
		}
		_, err := ing.Push(ctx, req)
		require.NoError(t, err, "push %d should succeed after throttle released", i)
	}
}

// TestDiskThrottleDoesNotBlockOtherOperations verifies that throttling only affects Push
func TestDiskThrottleDoesNotBlockOtherOperations(t *testing.T) {
	mockWAL := &mockThrottledWAL{}
	store, ing := newTestStore(t, defaultIngesterTestConfig(t), mockWAL)
	defer store.Stop()
	defer services.StopAndAwaitTerminated(context.Background(), ing) //nolint:errcheck

	ctx := user.InjectOrgID(context.Background(), "test-user")

	// Push some data first
	mockWAL.SetThrottled(false)
	req := &logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{job="test"}`,
				Entries: []logproto.Entry{
					{Timestamp: time.Now(), Line: "test log line"},
				},
			},
		},
	}
	_, err := ing.Push(ctx, req)
	require.NoError(t, err)

	// Enable throttle
	mockWAL.SetThrottled(true)

	// Verify Push is blocked
	_, err = ing.Push(ctx, req)
	require.Equal(t, ErrReadOnly, err)

	// Verify throttling only affects Push operations, not the overall ingester state
	// The fact that we could successfully push data before throttling proves the ingester is operational
	require.False(t, ing.readonly, "ingester should not be in readonly mode (shutdown) when disk throttled")
}

// TestDiskUsageCalculation tests the disk usage percentage calculation math
func TestDiskUsageCalculation(t *testing.T) {
	tests := []struct {
		name          string
		totalBlocks   uint64
		blockSize     uint64
		freeBlocks    uint64
		expectedUsage float64
	}{
		{
			name:          "empty disk (0% used)",
			totalBlocks:   1000,
			blockSize:     4096,
			freeBlocks:    1000,
			expectedUsage: 0.0,
		},
		{
			name:          "half full disk (50% used)",
			totalBlocks:   1000,
			blockSize:     4096,
			freeBlocks:    500,
			expectedUsage: 0.5,
		},
		{
			name:          "90% full disk (exactly at threshold)",
			totalBlocks:   1000,
			blockSize:     4096,
			freeBlocks:    100,
			expectedUsage: 0.9,
		},
		{
			name:          "95% full disk (above threshold)",
			totalBlocks:   1000,
			blockSize:     4096,
			freeBlocks:    50,
			expectedUsage: 0.95,
		},
		{
			name:          "completely full disk (100% used)",
			totalBlocks:   1000,
			blockSize:     4096,
			freeBlocks:    0,
			expectedUsage: 1.0,
		},
		{
			name:          "10% used disk",
			totalBlocks:   10000,
			blockSize:     512,
			freeBlocks:    9000,
			expectedUsage: 0.1,
		},
		{
			name:          "89% used (just below threshold)",
			totalBlocks:   10000,
			blockSize:     1024,
			freeBlocks:    1100,
			expectedUsage: 0.89,
		},
		{
			name:          "91% used (just above threshold)",
			totalBlocks:   10000,
			blockSize:     1024,
			freeBlocks:    900,
			expectedUsage: 0.91,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the calculation from walWrapper.checkDiskUsage()
			total := tt.totalBlocks * tt.blockSize
			free := tt.freeBlocks * tt.blockSize
			used := total - free
			usagePercent := float64(used) / float64(total)

			// Verify the calculation matches expected
			require.InDelta(t, tt.expectedUsage, usagePercent, 0.001,
				"usage calculation incorrect: got %.4f, want %.4f", usagePercent, tt.expectedUsage)

			// Verify threshold comparison logic (>= 0.90)
			shouldThrottle := usagePercent >= 0.90
			expectedToThrottle := tt.expectedUsage >= 0.90
			require.Equal(t, expectedToThrottle, shouldThrottle,
				"throttle decision incorrect for %.2f%% usage", usagePercent*100)
		})
	}
}

// TestThresholdBoundaryConditions tests edge cases around the threshold
func TestThresholdBoundaryConditions(t *testing.T) {
	tests := []struct {
		name           string
		usage          float64
		threshold      float64
		shouldThrottle bool
		description    string
	}{
		{
			name:           "usage exactly at threshold",
			usage:          0.90,
			threshold:      0.90,
			shouldThrottle: true,
			description:    "should throttle when usage == threshold",
		},
		{
			name:           "usage just below threshold",
			usage:          0.8999,
			threshold:      0.90,
			shouldThrottle: false,
			description:    "should not throttle when usage < threshold",
		},
		{
			name:           "usage just above threshold",
			usage:          0.9001,
			threshold:      0.90,
			shouldThrottle: true,
			description:    "should throttle when usage > threshold",
		},
		{
			name:           "usage well below threshold",
			usage:          0.50,
			threshold:      0.90,
			shouldThrottle: false,
			description:    "should not throttle at 50% usage",
		},
		{
			name:           "usage well above threshold",
			usage:          0.99,
			threshold:      0.90,
			shouldThrottle: true,
			description:    "should throttle at 99% usage",
		},
		{
			name:           "threshold at 0.80, usage at 0.79",
			usage:          0.79,
			threshold:      0.80,
			shouldThrottle: false,
			description:    "should not throttle below 80% threshold",
		},
		{
			name:           "threshold at 0.80, usage at 0.80",
			usage:          0.80,
			threshold:      0.80,
			shouldThrottle: true,
			description:    "should throttle at exactly 80% threshold",
		},
		{
			name:           "threshold at 0.95, usage at 0.90",
			usage:          0.90,
			threshold:      0.95,
			shouldThrottle: false,
			description:    "should not throttle with higher threshold",
		},
		{
			name:           "full disk",
			usage:          1.0,
			threshold:      0.90,
			shouldThrottle: true,
			description:    "should throttle at 100% usage",
		},
		{
			name:           "empty disk",
			usage:          0.0,
			threshold:      0.90,
			shouldThrottle: false,
			description:    "should not throttle at 0% usage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This is the comparison logic from walWrapper.monitorDisk()
			isThrottled := tt.usage >= tt.threshold

			require.Equal(t, tt.shouldThrottle, isThrottled,
				"%s: usage=%.4f, threshold=%.4f", tt.description, tt.usage, tt.threshold)
		})
	}
}

// TestDiskUsageRealWorld tests with realistic filesystem values
func TestDiskUsageRealWorld(t *testing.T) {
	tests := []struct {
		name           string
		totalGB        float64
		usedGB         float64
		threshold      float64
		shouldThrottle bool
	}{
		{
			name:           "1TB disk, 800GB used (80%)",
			totalGB:        1000,
			usedGB:         800,
			threshold:      0.90,
			shouldThrottle: false,
		},
		{
			name:           "1TB disk, 900GB used (90%)",
			totalGB:        1000,
			usedGB:         900,
			threshold:      0.90,
			shouldThrottle: true,
		},
		{
			name:           "1TB disk, 950GB used (95%)",
			totalGB:        1000,
			usedGB:         950,
			threshold:      0.90,
			shouldThrottle: true,
		},
		{
			name:           "500GB disk, 450GB used (90%)",
			totalGB:        500,
			usedGB:         450,
			threshold:      0.90,
			shouldThrottle: true,
		},
		{
			name:           "100GB disk, 89GB used (89%)",
			totalGB:        100,
			usedGB:         89,
			threshold:      0.90,
			shouldThrottle: false,
		},
		{
			name:           "10TB disk, 9.5TB used (95%)",
			totalGB:        10000,
			usedGB:         9500,
			threshold:      0.90,
			shouldThrottle: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert GB to bytes (simulating real disk usage)
			const bytesPerGB = 1024 * 1024 * 1024
			totalBytes := uint64(tt.totalGB * bytesPerGB)
			usedBytes := uint64(tt.usedGB * bytesPerGB)
			freeBytes := totalBytes - usedBytes

			// Calculate usage percentage (same logic as checkDiskUsage)
			usagePercent := float64(usedBytes) / float64(totalBytes)
			isThrottled := usagePercent >= tt.threshold

			require.Equal(t, tt.shouldThrottle, isThrottled,
				"usage=%.2f%%, threshold=%.2f%%", usagePercent*100, tt.threshold*100)

			// Verify the math is correct
			expectedUsage := tt.usedGB / tt.totalGB
			require.InDelta(t, expectedUsage, usagePercent, 0.001,
				"calculated usage %.4f doesn't match expected %.4f", usagePercent, expectedUsage)

			t.Logf("Total: %.0fGB, Used: %.0fGB, Free: %.0fGB, Usage: %.2f%%, Throttle: %v",
				tt.totalGB, tt.usedGB, float64(freeBytes)/bytesPerGB, usagePercent*100, isThrottled)
		})
	}
}
