package spanlogger

import (
	"context"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/spanlogger"

	util_log "github.com/cortexproject/cortex/pkg/util/log"

	"github.com/grafana/loki/pkg/tenant"
)

const (
	// TenantIDsTagName is the tenant IDs tag name.
	TenantIDsTagName = spanlogger.TenantIDsTagName
)

// SpanLogger unifies tracing and logging, to reduce repetition.
type SpanLogger = spanlogger.SpanLogger

// New makes a new SpanLogger with a log.Logger to send logs to. The provided context will have the logger attached
// to it and can be retrieved with FromContext.
func New(ctx context.Context, method string, kvps ...interface{}) (*SpanLogger, context.Context) {
	return spanlogger.New(ctx, util_log.Logger, method, tenant.DefaultResolver, kvps...)
}

// NewWithLogger is like New but allows to pass a logger.
func NewWithLogger(ctx context.Context, logger log.Logger, method string, kvps ...interface{}) (*SpanLogger, context.Context) {
	return spanlogger.New(ctx, logger, method, tenant.DefaultResolver, kvps...)
}

// FromContext returns a SpanLogger using the current parent span.
// If there is no parent span, the SpanLogger will only log to the logger
// within the context. If the context doesn't have a logger, the fallback
// logger is used.
func FromContext(ctx context.Context) *SpanLogger {
	return spanlogger.FromContext(ctx, util_log.Logger, tenant.DefaultResolver)
}
