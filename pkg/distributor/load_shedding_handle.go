package distributor

import (
	"context"
	"net/http"
	goruntime "runtime"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/grafana/dskit/httpgrpc"
	"google.golang.org/grpc/tap"

	"github.com/grafana/loki/v3/pkg/util/requestlimiter"
)

// LoadSheddingHandle implements tap.ServerInHandle and fires after gRPC headers are received but before the request
// body is decoded.
// This allows us the ability to load-shed requests before the request body has been buffered into memory.
type LoadSheddingHandle struct {
	d               *Distributor
	cachedHeapUsed  atomic.Uint64
	lastUpdateNanos atomic.Int64
	requestLimiter  requestlimiter.RequestLimiter
}

// NewLoadSheddingHandle creates a new LoadSheddingHandle. The distributor must be set via SetDistributor
// before Handle is called.
func NewLoadSheddingHandle() *LoadSheddingHandle {
	return &LoadSheddingHandle{}
}

// SetDistributor sets the distributor for the load shedding handle.
// This must be called exactly once before Handle is called.
func (h *LoadSheddingHandle) SetDistributor(d *Distributor) {
	h.d = d
	h.requestLimiter = requestlimiter.New(d.cfg.RequestSizeLimiter, d.inflightBytes)
}

// Handle implements tap.ServerInHandle.
func (h *LoadSheddingHandle) Handle(ctx context.Context, info *tap.Info) (context.Context, error) {
	if h.d == nil {
		return ctx, httpgrpc.Errorf(http.StatusInternalServerError, "load shedding handle misconfigured: SetDistributor not called")
	}

	// Update heap stats at most once per configured cache duration
	now := time.Now().UnixNano()
	lastUpdate := h.lastUpdateNanos.Load()
	if now-lastUpdate >= int64(h.d.cfg.MemoryBasedLoadSheddingCacheDuration) {
		// Try to claim the update slot
		if h.lastUpdateNanos.CompareAndSwap(lastUpdate, now) {
			m := &goruntime.MemStats{}
			goruntime.ReadMemStats(m)
			h.cachedHeapUsed.Store(m.HeapAlloc)
			h.d.usedMemoryGauge.Set(float64(m.HeapAlloc))
		}
	}

	// Load shed based on memory usage if necessary
	heapUsed := h.cachedHeapUsed.Load()
	if h.d.cfg.MemoryBasedLoadSheddingThresholdBytes != 0 && heapUsed > h.d.cfg.MemoryBasedLoadSheddingThresholdBytes {
		h.d.loadShedRequestsCounter.Inc()
		return ctx, httpgrpc.Errorf(http.StatusServiceUnavailable, "ServiceUnavailable")
	}

	// Calculate estimated request size
	estimatedRequestSize := 1_000_000
	contentLength := info.Header.Get("X-Decompressed-Content-Length")
	if len(contentLength) == 1 {
		contentLengthBytes, err := strconv.Atoi(contentLength[0])
		if err == nil {
			h.d.contentLengthPresentCount.Inc()
			estimatedRequestSize = 5 * contentLengthBytes // 5 is an estimate of compression factor from my testing
		}
	}

	// Load shed based on inflight bytes if necessary
	cleanup, err := h.requestLimiter.Limit(ctx, int64(estimatedRequestSize))
	if err != nil {
		h.d.loadShedRequestsCounter.Inc()
		return ctx, httpgrpc.Errorf(http.StatusServiceUnavailable, "ServiceUnavailable")
	}
	go func() {
		<-ctx.Done()
		cleanup()
	}()

	return ctx, nil
}
