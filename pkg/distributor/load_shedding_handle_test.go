package distributor

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/tap"
)

func TestLoadSheddingHandle_LoadShedding(t *testing.T) {
	ctx := context.Background()
	info := &tap.Info{}

	tests := []struct {
		testName       string
		thresholdBytes uint64
		shouldLoadShed bool
	}{
		{
			testName:       "zero threshold does not load shed",
			thresholdBytes: 0,
			shouldLoadShed: false,
		},
		{
			testName:       "small threshold does load shed",
			thresholdBytes: 1,
			shouldLoadShed: true,
		},
		{
			testName:       "large threshold does not load shed",
			thresholdBytes: 1 << 50,
			shouldLoadShed: false,
		},
	}

	for _, test := range tests {
		t.Run(test.testName, func(t *testing.T) {
			cfg := Config{
				MemoryBasedLoadSheddingThresholdBytes: test.thresholdBytes,
				MemoryBasedLoadSheddingCacheDuration:  time.Millisecond,
			}
			d := &Distributor{
				cfg:                     cfg,
				usedMemoryGauge:         prometheus.NewGauge(prometheus.GaugeOpts{}),
				loadShedRequestsCounter: prometheus.NewCounter(prometheus.CounterOpts{}),
			}
			h := NewLoadSheddingHandle()
			h.SetDistributor(d)

			_, err := h.Handle(ctx, info)
			if test.shouldLoadShed {
				require.Error(t, err)
				require.Equal(t, httpgrpc.Errorf(http.StatusServiceUnavailable, "ServiceUnavailable"), err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLoadSheddingHandle_Caching(t *testing.T) {
	ctx := context.Background()
	info := &tap.Info{}

	cfg := Config{
		MemoryBasedLoadSheddingThresholdBytes: 0, // Don't actually load shed
		MemoryBasedLoadSheddingCacheDuration:  time.Hour,
	}
	d := &Distributor{
		cfg:                     cfg,
		usedMemoryGauge:         prometheus.NewGauge(prometheus.GaugeOpts{}),
		loadShedRequestsCounter: prometheus.NewCounter(prometheus.CounterOpts{}),
	}
	h := NewLoadSheddingHandle()
	h.SetDistributor(d)

	// Before first call - should be no cached values
	used0 := h.cachedHeapUsed.Load()
	updated0 := h.lastUpdateNanos.Load()
	require.Equal(t, uint64(0), used0)
	require.Equal(t, int64(0), updated0)

	// First call - should update cache
	_, err := h.Handle(ctx, info)
	require.NoError(t, err)
	used1 := h.cachedHeapUsed.Load()
	updated1 := h.lastUpdateNanos.Load()
	require.Greater(t, used1, uint64(0))
	require.Greater(t, updated1, int64(0))

	// Second call - should use cache
	_, err = h.Handle(ctx, info)
	require.NoError(t, err)
	used2 := h.cachedHeapUsed.Load()
	updated2 := h.lastUpdateNanos.Load()
	require.Equal(t, updated1, updated2)
	require.Equal(t, used1, used2)
}

func TestLoadSheddingHandle_ThreadSafety(t *testing.T) {
	ctx := context.Background()
	info := &tap.Info{}

	cfg := Config{
		MemoryBasedLoadSheddingThresholdBytes: 1 << 50, // High threshold
		MemoryBasedLoadSheddingCacheDuration:  10 * time.Millisecond,
	}
	d := &Distributor{
		cfg:                     cfg,
		usedMemoryGauge:         prometheus.NewGauge(prometheus.GaugeOpts{}),
		loadShedRequestsCounter: prometheus.NewCounter(prometheus.CounterOpts{}),
	}
	h := NewLoadSheddingHandle()
	h.SetDistributor(d)

	// Run many concurrent calls
	const numGoroutines = 100
	const callsPerGoroutine = 100
	errChan := make(chan error, numGoroutines*callsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			for j := 0; j < callsPerGoroutine; j++ {
				_, err := h.Handle(ctx, info)
				errChan <- err
			}
		}()
	}

	// Collect all results
	for i := 0; i < numGoroutines*callsPerGoroutine; i++ {
		err := <-errChan
		require.NoError(t, err)
	}
}

func TestLoadSheddingHandle_NilDistributor(t *testing.T) {
	ctx := context.Background()
	info := &tap.Info{}

	h := NewLoadSheddingHandle()

	// Should return error when distributor is nil (misconfiguration)
	_, err := h.Handle(ctx, info)
	require.Error(t, err)
	require.Equal(t, httpgrpc.Errorf(http.StatusInternalServerError, "load shedding handle misconfigured: SetDistributor not called"), err)
}
