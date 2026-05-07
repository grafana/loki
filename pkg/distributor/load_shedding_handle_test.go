package distributor

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/tap"

	util_metric "github.com/grafana/loki/v3/pkg/util/metric"
	"github.com/grafana/loki/v3/pkg/util/requestlimiter"
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
				logger:                  log.NewNopLogger(),
				usedMemoryGauge:         prometheus.NewGauge(prometheus.GaugeOpts{}),
				loadShedRequestsCounter: prometheus.NewCounter(prometheus.CounterOpts{}),
				inflightBytes:           util_metric.NewMaxSampleCollector("", ""),
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
		logger:                  log.NewNopLogger(),
		usedMemoryGauge:         prometheus.NewGauge(prometheus.GaugeOpts{}),
		loadShedRequestsCounter: prometheus.NewCounter(prometheus.CounterOpts{}),
		inflightBytes:           util_metric.NewMaxSampleCollector("", ""),
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
		logger:                  log.NewNopLogger(),
		usedMemoryGauge:         prometheus.NewGauge(prometheus.GaugeOpts{}),
		loadShedRequestsCounter: prometheus.NewCounter(prometheus.CounterOpts{}),
		inflightBytes:           util_metric.NewMaxSampleCollector("", ""),
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

func TestLoadSheddingHandle_InflightBytesLoadShedding(t *testing.T) {
	ctx := context.Background()
	info := &tap.Info{}

	tests := []struct {
		testName       string
		thresholdBytes int64
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
				MaxRecvMsgSize: 100 << 20, // 100MB default
				RequestSizeLimiter: requestlimiter.Config{
					MaxInflightBytes: test.thresholdBytes,
					MaxWait:          10 * time.Millisecond,
				},
			}
			d := &Distributor{
				cfg:                     cfg,
				logger:                  log.NewNopLogger(),
				usedMemoryGauge:         prometheus.NewGauge(prometheus.GaugeOpts{}),
				loadShedRequestsCounter: prometheus.NewCounter(prometheus.CounterOpts{}),
				inflightBytes:           util_metric.NewMaxSampleCollector("", ""),
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

func TestLoadSheddingHandle_ContentLengthEstimation(t *testing.T) {
	ctx := context.Background()

	// MaxInflightBytes sits between 5*contentLength (500) and the default estimate (1_000_000),
	// so the estimated size determines whether the request is shed.
	const maxInflightBytes = 750

	tests := []struct {
		testName             string
		header               metadata.MD
		shouldLoadShed       bool
		expectedCounterValue float64
		expectedLog          string
	}{
		{
			testName:             "no X-Decompressed-Content-Length uses default estimate",
			header:               metadata.MD{"foo": []string{"bar"}},
			shouldLoadShed:       true,
			expectedCounterValue: 0,
			expectedLog:          "level=info msg=\"X-Decompressed-Content-Length header not present - map[foo:[bar]]\"\n",
		},
		{
			testName:             "valid X-Decompressed-Content-Length uses 5x estimate",
			header:               metadata.MD{"x-decompressed-content-length": []string{"100"}},
			shouldLoadShed:       false,
			expectedCounterValue: 1,
			expectedLog:          "",
		},
		{
			testName:             "non-numeric X-Decompressed-Content-Length uses default estimate",
			header:               metadata.MD{"x-decompressed-content-length": []string{"not-a-number"}},
			shouldLoadShed:       true,
			expectedCounterValue: 0,
			expectedLog:          "",
		},
		{
			testName:             "multiple X-Decompressed-Content-Length values uses default estimate",
			header:               metadata.MD{"x-decompressed-content-length": []string{"100", "200"}},
			shouldLoadShed:       true,
			expectedCounterValue: 0,
			expectedLog:          "level=info msg=\"X-Decompressed-Content-Length header not present - map[x-decompressed-content-length:[100 200]]\"\n",
		},
	}

	for _, test := range tests {
		t.Run(test.testName, func(t *testing.T) {
			var logBuf bytes.Buffer
			d := &Distributor{
				cfg: Config{
					RequestSizeLimiter: requestlimiter.Config{
						MaxInflightBytes: maxInflightBytes,
						MaxWait:          10 * time.Millisecond,
					},
				},
				logger:                    log.NewLogfmtLogger(&logBuf),
				usedMemoryGauge:           prometheus.NewGauge(prometheus.GaugeOpts{}),
				loadShedRequestsCounter:   prometheus.NewCounter(prometheus.CounterOpts{}),
				contentLengthPresentCount: prometheus.NewCounter(prometheus.CounterOpts{}),
				inflightBytes:             util_metric.NewMaxSampleCollector("", ""),
			}
			h := NewLoadSheddingHandle()
			h.SetDistributor(d)

			_, err := h.Handle(ctx, &tap.Info{Header: test.header})
			if test.shouldLoadShed {
				require.Error(t, err)
				require.Equal(t, httpgrpc.Errorf(http.StatusServiceUnavailable, "ServiceUnavailable"), err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.expectedCounterValue, testutil.ToFloat64(d.contentLengthPresentCount))
			if test.expectedLog != "" {
				require.Equal(t, test.expectedLog, logBuf.String())
			} else {
				require.Empty(t, logBuf.String())
			}
		})
	}
}
