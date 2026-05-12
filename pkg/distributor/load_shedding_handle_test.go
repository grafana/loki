package distributor

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/tap"

	util_metric "github.com/grafana/loki/v3/pkg/util/metric"
	"github.com/grafana/loki/v3/pkg/util/requestlimiter"
)

func newTestDistributor(cfg Config) *Distributor {
	return &Distributor{
		cfg:           cfg,
		inflightBytes: util_metric.NewMaxSampleCollector("", ""),
	}
}

func TestLoadSheddingHandle_NilDistributor(t *testing.T) {
	h := NewLoadSheddingHandle()
	_, err := h.Handle(context.Background(), &tap.Info{})
	require.Equal(t, httpgrpc.Errorf(http.StatusInternalServerError, "load shedding handle misconfigured: SetDistributor not called"), err)
}

func TestLoadSheddingHandle_NoLimitConfigured(t *testing.T) {
	// MaxInflightBytes == 0 → noop limiter, all requests pass.
	h := NewLoadSheddingHandle()
	h.SetDistributor(newTestDistributor(Config{MaxDecompressedSize: 1 << 20}))

	outCtx, err := h.Handle(context.Background(), &tap.Info{})
	require.NoError(t, err)

	res, ok := inflightReservation(outCtx)
	require.True(t, ok)
	require.NotNil(t, res)
}

func TestLoadSheddingHandle_InflightBytesSheds(t *testing.T) {
	cfg := Config{
		MaxDecompressedSize: 1 << 20,
		RequestSizeLimiter: requestlimiter.Config{
			MaxInflightBytes: 1, // budget smaller than MaxDecompressedSize
			MaxWait:          10 * time.Millisecond,
		},
	}
	h := NewLoadSheddingHandle()
	h.SetDistributor(newTestDistributor(cfg))

	_, err := h.Handle(context.Background(), &tap.Info{})
	require.Equal(t, httpgrpc.Errorf(http.StatusServiceUnavailable, "ServiceUnavailable"), err)
}

func TestLoadSheddingHandle_InflightBytesLargeLimit(t *testing.T) {
	cfg := Config{
		MaxDecompressedSize: 1 << 20,
		RequestSizeLimiter: requestlimiter.Config{
			MaxInflightBytes: 1 << 50,
			MaxWait:          10 * time.Millisecond,
		},
	}
	h := NewLoadSheddingHandle()
	h.SetDistributor(newTestDistributor(cfg))

	_, err := h.Handle(context.Background(), &tap.Info{})
	require.NoError(t, err)
}

func TestLoadSheddingHandle_ReservationInContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const maxDecompressed = 1 << 20
	cfg := Config{
		MaxDecompressedSize: maxDecompressed,
		RequestSizeLimiter: requestlimiter.Config{
			MaxInflightBytes: 2 * maxDecompressed, // room for two requests
			MaxWait:          10 * time.Millisecond,
		},
	}
	h := NewLoadSheddingHandle()
	h.SetDistributor(newTestDistributor(cfg))

	outCtx, err := h.Handle(ctx, &tap.Info{})
	require.NoError(t, err)

	res, ok := inflightReservation(outCtx)
	require.True(t, ok)
	require.NotNil(t, res)

	res.AdjustToActual(200)
	res.Release()

	// Budget restored — a second request should succeed.
	_, err = h.Handle(ctx, &tap.Info{})
	require.NoError(t, err)
}

func TestLoadSheddingHandle_SafetyNetReleasesOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	const maxDecompressed = 1 << 20
	cfg := Config{
		MaxDecompressedSize: maxDecompressed,
		RequestSizeLimiter: requestlimiter.Config{
			MaxInflightBytes: maxDecompressed, // exactly one request fits
			MaxWait:          50 * time.Millisecond,
		},
	}
	h := NewLoadSheddingHandle()
	h.SetDistributor(newTestDistributor(cfg))

	// First request consumes the entire budget.
	_, err := h.Handle(ctx, &tap.Info{})
	require.NoError(t, err)

	// Cancel the context — the safety-net goroutine releases the reservation.
	cancel()

	require.Eventually(t, func() bool {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel2()
		_, err2 := h.requestLimiter.Reserve(ctx2, maxDecompressed)
		return err2 == nil
	}, 500*time.Millisecond, 10*time.Millisecond)
}
