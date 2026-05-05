package distributor

import (
	"context"
	"net/http"
	goruntime "runtime"

	"github.com/grafana/dskit/httpgrpc"
	"google.golang.org/grpc/tap"
)

// LoadSheddingHandle implements tap.ServerInHandle and fires after gRPC headers are received but before the request
// body is decoded.
// This allows us the ability to load-shed requests before the request body has been buffered into memory.
type LoadSheddingHandle struct {
	d *Distributor
}

// NewLoadSheddingHandle creates a new LoadSheddingHandle with the given distributor.
func NewLoadSheddingHandle(d *Distributor) *LoadSheddingHandle {
	return &LoadSheddingHandle{
		d: d,
	}
}

// Handle implements tap.ServerInHandle.
func (h *LoadSheddingHandle) Handle(ctx context.Context, _ *tap.Info) (context.Context, error) {
	m := &goruntime.MemStats{}
	goruntime.ReadMemStats(m)
	heapUsed := m.HeapAlloc
	h.d.usedMemoryGauge.Set(float64(heapUsed))
	if h.d.cfg.MemoryBasedLoadSheddingThresholdBytes != 0 && heapUsed > h.d.cfg.MemoryBasedLoadSheddingThresholdBytes {
		h.d.loadShedRequestsCounter.Inc()
		return ctx, httpgrpc.Errorf(http.StatusServiceUnavailable, "ServiceUnavailable")
	}
	return ctx, nil
}
