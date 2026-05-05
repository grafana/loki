package distributor

import (
	"context"
	"net/http"
	goruntime "runtime"
	"sync/atomic"
	"time"

	"github.com/grafana/dskit/httpgrpc"
	"google.golang.org/grpc/tap"
)

// LoadSheddingHandle implements tap.ServerInHandle and fires after gRPC headers are received but before the request
// body is decoded.
// This allows us the ability to load-shed requests before the request body has been buffered into memory.
type LoadSheddingHandle struct {
	d               *Distributor
	cachedHeapUsed  atomic.Uint64
	lastUpdateNanos atomic.Int64
}

// NewLoadSheddingHandle creates a new LoadSheddingHandle with the given distributor.
func NewLoadSheddingHandle(d *Distributor) *LoadSheddingHandle {
	return &LoadSheddingHandle{
		d: d,
	}
}

// Handle implements tap.ServerInHandle.
func (h *LoadSheddingHandle) Handle(ctx context.Context, _ *tap.Info) (context.Context, error) {
	now := time.Now().UnixNano()
	lastUpdate := h.lastUpdateNanos.Load()

	// Update heap stats at most once per second
	if now-lastUpdate >= int64(time.Second) {
		// Try to claim the update slot
		if h.lastUpdateNanos.CompareAndSwap(lastUpdate, now) {
			m := &goruntime.MemStats{}
			goruntime.ReadMemStats(m)
			h.cachedHeapUsed.Store(m.HeapAlloc)
			h.d.usedMemoryGauge.Set(float64(m.HeapAlloc))
		}
	}

	heapUsed := h.cachedHeapUsed.Load()
	if h.d.cfg.MemoryBasedLoadSheddingThresholdBytes != 0 && heapUsed > h.d.cfg.MemoryBasedLoadSheddingThresholdBytes {
		h.d.loadShedRequestsCounter.Inc()
		return ctx, httpgrpc.Errorf(http.StatusServiceUnavailable, "ServiceUnavailable")
	}
	return ctx, nil
}
