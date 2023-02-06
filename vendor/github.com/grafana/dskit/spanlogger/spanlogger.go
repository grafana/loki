package spanlogger

import (
	"context"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/weaveworks/common/tracing"
)

type loggerCtxMarker struct{}

// TenantResolver provides methods for extracting tenant IDs from a context.
type TenantResolver interface {
	// TenantID tries to extract a tenant ID from a context.
	TenantID(context.Context) (string, error)
	// TenantIDs tries to extract tenant IDs from a context.
	TenantIDs(context.Context) ([]string, error)
}

const (
	// TenantIDsTagName is the tenant IDs tag name.
	TenantIDsTagName = "tenant_ids"
)

var (
	loggerCtxKey = &loggerCtxMarker{}
)

// SpanLogger unifies tracing and logging, to reduce repetition.
type SpanLogger struct {
	log.Logger
	opentracing.Span
	sampled bool
}

// New makes a new SpanLogger with a log.Logger to send logs to. The provided context will have the logger attached
// to it and can be retrieved with FromContext.
func New(ctx context.Context, logger log.Logger, method string, resolver TenantResolver, kvps ...interface{}) (*SpanLogger, context.Context) {
	span, ctx := opentracing.StartSpanFromContext(ctx, method)
	if ids, err := resolver.TenantIDs(ctx); err == nil && len(ids) > 0 {
		span.SetTag(TenantIDsTagName, ids)
	}
	lwc, sampled := withContext(ctx, logger, resolver)
	l := &SpanLogger{
		Logger:  log.With(lwc, "method", method),
		Span:    span,
		sampled: sampled,
	}
	if len(kvps) > 0 {
		level.Debug(l).Log(kvps...)
	}

	ctx = context.WithValue(ctx, loggerCtxKey, logger)
	return l, ctx
}

// FromContext returns a span logger using the current parent span.
// If there is no parent span, the SpanLogger will only log to the logger
// within the context. If the context doesn't have a logger, the fallback
// logger is used.
func FromContext(ctx context.Context, fallback log.Logger, resolver TenantResolver) *SpanLogger {
	logger, ok := ctx.Value(loggerCtxKey).(log.Logger)
	if !ok {
		logger = fallback
	}
	sp := opentracing.SpanFromContext(ctx)
	if sp == nil {
		sp = defaultNoopSpan
	}
	lwc, sampled := withContext(ctx, logger, resolver)
	return &SpanLogger{
		Logger:  lwc,
		Span:    sp,
		sampled: sampled,
	}
}

// Log implements gokit's Logger interface; sends logs to underlying logger and
// also puts the on the spans.
func (s *SpanLogger) Log(kvps ...interface{}) error {
	s.Logger.Log(kvps...)
	if !s.sampled {
		return nil
	}
	fields, err := otlog.InterleavedKVToFields(kvps...)
	if err != nil {
		return err
	}
	s.Span.LogFields(fields...)
	return nil
}

// Error sets error flag and logs the error on the span, if non-nil. Returns the err passed in.
func (s *SpanLogger) Error(err error) error {
	if err == nil || !s.sampled {
		return err
	}
	ext.Error.Set(s.Span, true)
	s.Span.LogFields(otlog.Error(err))
	return err
}

func withContext(ctx context.Context, logger log.Logger, resolver TenantResolver) (log.Logger, bool) {
	userID, err := resolver.TenantID(ctx)
	if err == nil && userID != "" {
		logger = log.With(logger, "user", userID)
	}

	traceID, ok := tracing.ExtractSampledTraceID(ctx)
	if !ok {
		return logger, false
	}

	return log.With(logger, "traceID", traceID), true
}
