package distributor

import (
	"context"
	"net/http"

	"github.com/grafana/dskit/httpgrpc"
	"google.golang.org/grpc/tap"

	"github.com/grafana/loki/v3/pkg/util/requestlimiter"
)

type contextKey int

const inflightReservationKey contextKey = 0

// LoadSheddingHandle is a gRPC tap handler that fires after headers are received
// but before the request body is decoded. It reserves inflight-byte budget equal
// to the maximum decompressed size of the request, shedding with HTTP 503 if the
// budget is exhausted.
type LoadSheddingHandle struct {
	d              *Distributor
	requestLimiter requestlimiter.RequestLimiter
}

func NewLoadSheddingHandle() *LoadSheddingHandle {
	return &LoadSheddingHandle{}
}

// SetDistributor wires the distributor into the handle. Must be called once
// before Handle is used.
func (h *LoadSheddingHandle) SetDistributor(d *Distributor) {
	h.d = d
	h.requestLimiter = requestlimiter.New(d.cfg.RequestSizeLimiter, d.inflightBytes.Inc)
}

// Handle implements tap.ServerInHandle.
func (h *LoadSheddingHandle) Handle(ctx context.Context, _ *tap.Info) (context.Context, error) {
	if h.d == nil {
		return ctx, httpgrpc.Errorf(http.StatusInternalServerError, "load shedding handle misconfigured: SetDistributor not called")
	}

	reservation, err := h.requestLimiter.Reserve(ctx, h.d.cfg.MaxDecompressedSize)
	if err != nil {
		return ctx, httpgrpc.Errorf(http.StatusServiceUnavailable, "ServiceUnavailable")
	}
	// Safety-net: release if the request fails before PushWithResolver runs.
	// Release is idempotent so a double-release from PushWithResolver is safe.
	go func() {
		<-ctx.Done()
		reservation.Release()
	}()

	return context.WithValue(ctx, inflightReservationKey, reservation), nil
}

// inflightReservation extracts the Reservation stored by Handle, if present.
func inflightReservation(ctx context.Context) (*requestlimiter.Reservation, bool) {
	r, ok := ctx.Value(inflightReservationKey).(*requestlimiter.Reservation)
	return r, ok
}
