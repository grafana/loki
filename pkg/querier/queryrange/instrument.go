package queryrange

import (
	"context"
	"strconv"
	"time"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/instrument"
	"github.com/grafana/dskit/middleware"
	"github.com/opentracing/opentracing-go"

	"github.com/grafana/dskit/server"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

const (
	method = "GET"
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
		inflight := i.InflightRequests.WithLabelValues(method, route)
		inflight.Inc()
		defer inflight.Dec()

		begin := time.Now()
		result, err := next.Do(ctx, r)
		i.observe(ctx, route, err, time.Since(begin))

		return result, err
	})
}

func (i Instrument) observe(ctx context.Context, route string, err error, duration time.Duration) {
	respStatus := "200"
	if err != nil {
		if errResp, ok := httpgrpc.HTTPResponseFromError(err); ok {
			respStatus = strconv.Itoa(int(errResp.Code))
		} else {
			respStatus = "500"
		}
	}
	instrument.ObserveWithExemplar(ctx, i.RequestDuration.WithLabelValues(method, route, respStatus, "false"), duration.Seconds())
}

type Tracer struct{}

var _ queryrangebase.Middleware = Tracer{}

// Wrap implements the queryrangebase.Middleware
func (t Tracer) Wrap(next queryrangebase.Handler) queryrangebase.Handler {
	return queryrangebase.HandlerFunc(func(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
		route := DefaultCodec.Path(r)
		route = middleware.MakeLabelValue(route)
		span, ctx := opentracing.StartSpanFromContext(ctx, route)
		defer span.Finish()
		return next.Do(ctx, r)
	})
}
