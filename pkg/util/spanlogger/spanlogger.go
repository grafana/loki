package spanlogger

import (
	"context"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/spanlogger" //lint:ignore faillint // This package is the wrapper that should be used.
	"github.com/grafana/dskit/tenant"
)

const (
	// TenantIDsTagName is the tenant IDs tag name.
	TenantIDsTagName = spanlogger.TenantIDsTagName
)

type resolverProxy struct{}

func (r *resolverProxy) TenantID(ctx context.Context) (string, error) {
	return tenant.TenantID(ctx)
}

func (r *resolverProxy) TenantIDs(ctx context.Context) ([]string, error) {
	return tenant.TenantIDs(ctx)
}

var (
	resolver = &resolverProxy{}
)

// SpanLogger unifies tracing and logging, to reduce repetition.
type SpanLogger = spanlogger.SpanLogger

// New makes a new SpanLogger with a log.Logger to send logs to. The provided context will have the logger attached
// to it and can be retrieved with FromContext.
func New(ctx context.Context, logger log.Logger, method string, kvps ...interface{}) (*SpanLogger, context.Context) {
	return spanlogger.New(ctx, logger, method, resolver, kvps...)
}

// FromContext returns a span logger using the current parent span.
// If there is no parent span, the SpanLogger will only log to the logger
// within the context. If the context doesn't have a logger, the fallback
// logger is used.
func FromContext(ctx context.Context, fallback log.Logger) *SpanLogger {
	return spanlogger.FromContext(ctx, fallback, resolver)
}
