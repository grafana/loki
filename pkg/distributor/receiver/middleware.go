package receiver

import (
	"context"

	"github.com/weaveworks/common/user"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/model/pdata"
)

type ConsumeTracesFunc func(context.Context, pdata.Traces) error

func (f ConsumeTracesFunc) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func (f ConsumeTracesFunc) ConsumeTraces(ctx context.Context, td pdata.Traces) error {
	return f(ctx, td)
}

type Middleware interface {
	Wrap(consumer.Traces) consumer.Traces
}

type MiddlewareFunc func(consumer.Traces) consumer.Traces

// Wrap implements Interface
func (tc MiddlewareFunc) Wrap(next consumer.Traces) consumer.Traces {
	return tc(next)
}

// Merge produces a middleware that applies multiple middlesware in turn;
// ie Merge(f,g,h).Wrap(handler) == f.Wrap(g.Wrap(h.Wrap(handler)))
func Merge(middlesware ...Middleware) Middleware {
	return MiddlewareFunc(func(next consumer.Traces) consumer.Traces {
		for i := len(middlesware) - 1; i >= 0; i-- {
			next = middlesware[i].Wrap(next)
		}
		return next
	})
}

type fakeTenantMiddleware struct{}

func FakeTenantMiddleware() Middleware {
	return &fakeTenantMiddleware{}
}

func (m *fakeTenantMiddleware) Wrap(next consumer.Traces) consumer.Traces {
	return ConsumeTracesFunc(func(ctx context.Context, td pdata.Traces) error {
		ctx = user.InjectOrgID(ctx, "fake")
		return next.ConsumeTraces(ctx, td)
	})
}

type multiTenancyMiddleware struct{}

func MultiTenancyMiddleware() Middleware {
	return &multiTenancyMiddleware{}
}

func (m *multiTenancyMiddleware) Wrap(next consumer.Traces) consumer.Traces {
	return ConsumeTracesFunc(func(ctx context.Context, td pdata.Traces) error {
		var err error
		_, ctx, err = user.ExtractFromGRPCRequest(ctx)
		if err != nil {
			return err
		}
		return next.ConsumeTraces(ctx, td)
	})
}
