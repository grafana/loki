package receiver

import (
	"context"

	"github.com/weaveworks/common/user"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/model/pdata"

	"github.com/grafana/loki/pkg/util/log"
)

type ConsumeLogsFunc func(context.Context, pdata.Logs) error

func (f ConsumeLogsFunc) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func (f ConsumeLogsFunc) ConsumeLogs(ctx context.Context, td pdata.Logs) error {
	return f(ctx, td)
}

type Middleware interface {
	Wrap(consumer.Logs) consumer.Logs
}

type MiddlewareFunc func(consumer.Logs) consumer.Logs

// Wrap implements Interface
func (tc MiddlewareFunc) Wrap(next consumer.Logs) consumer.Logs {
	return tc(next)
}

// Merge produces a middleware that applies multiple middlewares in turn;
// ie Merge(f,g,h).Wrap(handler) == f.Wrap(g.Wrap(h.Wrap(handler)))
func Merge(middlewares ...Middleware) Middleware {
	return MiddlewareFunc(func(next consumer.Logs) consumer.Logs {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i].Wrap(next)
		}
		return next
	})
}

type fakeTenantMiddleware struct{}

func FakeTenantMiddleware() Middleware {
	return &fakeTenantMiddleware{}
}

func (m *fakeTenantMiddleware) Wrap(next consumer.Logs) consumer.Logs {
	return ConsumeLogsFunc(func(ctx context.Context, td pdata.Logs) error {
		ctx = user.InjectOrgID(ctx, "fake")
		return next.ConsumeLogs(ctx, td)
	})
}

type multiTenancyMiddleware struct{}

func MultiTenancyMiddleware() Middleware {
	return &multiTenancyMiddleware{}
}

func (m *multiTenancyMiddleware) Wrap(next consumer.Logs) consumer.Logs {
	return ConsumeLogsFunc(func(ctx context.Context, td pdata.Logs) error {
		var err error
		_, ctx, err = user.ExtractFromGRPCRequest(ctx)
		if err != nil {
			log.Logger.Log("msg", "failed to extract org id", "err", err)
			return err
		}
		return next.ConsumeLogs(ctx, td)
	})
}
