package queryrange

import (
	"context"
	"strconv"
	"time"

	"github.com/grafana/dskit/grpcutil"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/instrument"
	"github.com/grafana/dskit/middleware"

	"github.com/grafana/dskit/server"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
)

const (
	gRPC = "gRPC"
)

type Instrument struct {
	*server.Metrics
}

var _ queryrangebase.Middleware = Instrument{}

// Wrap implements the queryrangebase.Middleware
func (i Instrument) Wrap(next queryrangebase.Handler) queryrangebase.Handler {
	return queryrangebase.HandlerFunc(func(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
		route := DefaultCodec.Path(r)
		route = middleware.MakeLabelValue(route)
		inflight := i.InflightRequests.WithLabelValues(gRPC, route)
		inflight.Inc()
		defer inflight.Dec()

		begin := time.Now()
		result, err := next.Do(ctx, r)
		i.observe(ctx, route, err, time.Since(begin))

		return result, err
	})
}

func (i Instrument) observe(ctx context.Context, method string, err error, duration time.Duration) {
	respStatus := "success"
	if err != nil {
		if errResp, ok := httpgrpc.HTTPResponseFromError(err); ok {
			respStatus = strconv.Itoa(int(errResp.Code))
		} else if grpcutil.IsCanceled(err) {
			respStatus = "cancel"
		} else {
			respStatus = "error"
		}
	}
	instrument.ObserveWithExemplar(ctx, i.RequestDuration.WithLabelValues(gRPC, method, respStatus, "false"), duration.Seconds())
}
