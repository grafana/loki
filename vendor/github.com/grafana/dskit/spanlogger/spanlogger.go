package spanlogger

import (
	"context"
	"runtime"
	"strings"

	"go.uber.org/atomic" // Really just need sync/atomic but there is a lint rule preventing it.

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	otlog "github.com/opentracing/opentracing-go/log"

	"github.com/grafana/dskit/tracing"
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
	ctx        context.Context            // context passed in, with logger
	resolver   TenantResolver             // passed in
	baseLogger log.Logger                 // passed in
	logger     atomic.Pointer[log.Logger] // initialized on first use
	opentracing.Span
	sampled      bool
	debugEnabled bool
}

// New makes a new SpanLogger with a log.Logger to send logs to. The provided context will have the logger attached
// to it and can be retrieved with FromContext.
func New(ctx context.Context, logger log.Logger, method string, resolver TenantResolver, kvps ...interface{}) (*SpanLogger, context.Context) {
	span, ctx := opentracing.StartSpanFromContext(ctx, method)
	if ids, err := resolver.TenantIDs(ctx); err == nil && len(ids) > 0 {
		span.SetTag(TenantIDsTagName, ids)
	}
	_, sampled := tracing.ExtractSampledTraceID(ctx)
	l := &SpanLogger{
		ctx:          ctx,
		resolver:     resolver,
		baseLogger:   log.With(logger, "method", method),
		Span:         span,
		sampled:      sampled,
		debugEnabled: debugEnabled(logger),
	}
	if len(kvps) > 0 {
		l.DebugLog(kvps...)
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
	sampled := false
	sp := opentracing.SpanFromContext(ctx)
	if sp == nil {
		sp = opentracing.NoopTracer{}.StartSpan("noop")
	} else {
		_, sampled = tracing.ExtractSampledTraceID(ctx)
	}
	return &SpanLogger{
		ctx:          ctx,
		baseLogger:   logger,
		resolver:     resolver,
		Span:         sp,
		sampled:      sampled,
		debugEnabled: debugEnabled(logger),
	}
}

// Detect whether we should output debug logging.
// false iff the logger says it's not enabled; true if the logger doesn't say.
func debugEnabled(logger log.Logger) bool {
	if x, ok := logger.(interface{ DebugEnabled() bool }); ok && !x.DebugEnabled() {
		return false
	}
	return true
}

// Log implements gokit's Logger interface; sends logs to underlying logger and
// also puts the on the spans.
func (s *SpanLogger) Log(kvps ...interface{}) error {
	s.getLogger().Log(kvps...)
	return s.spanLog(kvps...)
}

// DebugLog is more efficient than level.Debug().Log().
// Also it swallows the error return because nobody checks for errors on debug logs.
func (s *SpanLogger) DebugLog(kvps ...interface{}) {
	if s.debugEnabled {
		// The call to Log() through an interface makes its argument escape, so make a copy here,
		// in the debug-only path, so the function is faster for the non-debug path.
		localCopy := append([]any{}, kvps...)
		level.Debug(s.getLogger()).Log(localCopy...)
	}
	_ = s.spanLog(kvps...)
}

func (s *SpanLogger) spanLog(kvps ...interface{}) error {
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

func (s *SpanLogger) getLogger() log.Logger {
	pLogger := s.logger.Load()
	if pLogger != nil {
		return *pLogger
	}
	// If no logger stored in the pointer, start to make one.
	logger := s.baseLogger
	userID, err := s.resolver.TenantID(s.ctx)
	if err == nil && userID != "" {
		logger = log.With(logger, "user", userID)
	}

	traceID, ok := tracing.ExtractSampledTraceID(s.ctx)
	if ok {
		logger = log.With(logger, "trace_id", traceID)
	}

	// Replace the default valuer for the 'caller' attribute with one that gets the caller of the methods in this file.
	logger = log.With(logger, "caller", spanLoggerAwareCaller())

	// If the value has been set by another goroutine, fetch that other value and discard the one we made.
	if !s.logger.CompareAndSwap(nil, &logger) {
		pLogger := s.logger.Load()
		logger = *pLogger
	}
	return logger
}

// SetSpanAndLogTag sets a tag on the span used by this SpanLogger, and appends a key/value pair to the logger used for
// future log lines emitted by this SpanLogger.
//
// It is not safe to call this method from multiple goroutines simultaneously.
// It is safe to call this method at the same time as calling other SpanLogger methods, however, this may produce
// inconsistent results (eg. some log lines may be emitted with the provided key/value pair, and others may not).
func (s *SpanLogger) SetSpanAndLogTag(key string, value interface{}) {
	s.Span.SetTag(key, value)

	logger := s.getLogger()
	wrappedLogger := log.With(logger, key, value)
	s.logger.Store(&wrappedLogger)
}

// spanLoggerAwareCaller is like log.Caller, but ensures that the caller information is
// that of the caller to SpanLogger, not SpanLogger itself.
func spanLoggerAwareCaller() log.Valuer {
	valuer := atomic.NewPointer[log.Valuer](nil)

	return func() interface{} {
		// If we've already determined the correct stack depth, use it.
		existingValuer := valuer.Load()
		if existingValuer != nil {
			return (*existingValuer)()
		}

		// We haven't been called before, determine the correct stack depth to
		// skip the configured logger's internals and the SpanLogger's internals too.
		//
		// Note that we can't do this in spanLoggerAwareCaller() directly because we
		// need to do this when invoked by the configured logger - otherwise we cannot
		// measure the stack depth of the logger's internals.

		stackDepth := 3 // log.DefaultCaller uses a stack depth of 3, so start searching for the correct stack depth there.

		for {
			_, file, _, ok := runtime.Caller(stackDepth)
			if !ok {
				// We've run out of possible stack frames. Give up.
				valuer.Store(&unknownCaller)
				return unknownCaller()
			}

			if strings.HasSuffix(file, "spanlogger/spanlogger.go") {
				stackValuer := log.Caller(stackDepth + 2) // Add one to skip the stack frame for the SpanLogger method, and another to skip the stack frame for the valuer which we'll invoke below.
				valuer.Store(&stackValuer)
				return stackValuer()
			}

			stackDepth++
		}
	}
}

var unknownCaller log.Valuer = func() interface{} {
	return "<unknown>"
}
