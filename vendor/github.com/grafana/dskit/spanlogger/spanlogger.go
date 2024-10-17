// Provenance-includes-location: https://github.com/go-kit/log/blob/main/value.go
// Provenance-includes-license: MIT
// Provenance-includes-copyright: Go kit

package spanlogger

import (
	"context"
	"runtime"
	"strconv"
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

// Caller is like github.com/go-kit/log's Caller, but ensures that the caller information is
// that of the caller to SpanLogger (if SpanLogger is being used), not SpanLogger itself.
//
// defaultStackDepth should be the number of stack frames to skip by default, as would be
// passed to github.com/go-kit/log's Caller method.
func Caller(defaultStackDepth int) log.Valuer {
	return func() interface{} {
		stackDepth := defaultStackDepth + 1 // +1 to account for this method.
		seenSpanLogger := false
		pc := make([]uintptr, 1)

		for {
			function, file, line, ok := caller(stackDepth, pc)
			if !ok {
				// We've run out of possible stack frames. Give up.
				return "<unknown>"
			}

			// If we're in a SpanLogger method, we need to continue searching.
			//
			// Matching on the exact function name like this does mean this will break if we rename or refactor SpanLogger, but
			// the tests should catch this. In the worst case scenario, we'll log incorrect caller information, which isn't the
			// end of the world.
			if function == "github.com/grafana/dskit/spanlogger.(*SpanLogger).Log" || function == "github.com/grafana/dskit/spanlogger.(*SpanLogger).DebugLog" {
				seenSpanLogger = true
				stackDepth++
				continue
			}

			// We need to check for go-kit/log stack frames like this because using log.With, log.WithPrefix or log.WithSuffix
			// (including the various level methods like level.Debug, level.Info etc.) to wrap a SpanLogger introduce an
			// additional context.Log stack frame that calls into the SpanLogger. This is because the use of SpanLogger
			// as the logger means the optimisation to avoid creating a new logger in
			// https://github.com/go-kit/log/blob/c7bf81493e581feca11e11a7672b14be3591ca43/log.go#L141-L146 used by those methods
			// can't be used, and so the SpanLogger is wrapped in a new logger.
			if seenSpanLogger && function == "github.com/go-kit/log.(*context).Log" {
				stackDepth++
				continue
			}

			return formatCallerInfoForLog(file, line)
		}
	}
}

// caller is like runtime.Caller, but modified to allow reuse of the uintptr slice and return the function name.
func caller(stackDepth int, pc []uintptr) (function string, file string, line int, ok bool) {
	n := runtime.Callers(stackDepth+1, pc)
	if n < 1 {
		return "", "", 0, false
	}

	frame, _ := runtime.CallersFrames(pc).Next()
	return frame.Function, frame.File, frame.Line, frame.PC != 0
}

// This is based on github.com/go-kit/log's Caller, but modified for use by Caller above.
func formatCallerInfoForLog(file string, line int) string {
	idx := strings.LastIndexByte(file, '/')
	return file[idx+1:] + ":" + strconv.Itoa(line)
}
