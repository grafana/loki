package spanlogger

import (
	"context"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	otlog "github.com/opentracing/opentracing-go/log"

	"github.com/cortexproject/cortex/pkg/util"
)

// SpanLogger unifies tracing and logging, to reduce repetition.
type SpanLogger struct {
	log.Logger
	opentracing.Span
}

// New makes a new SpanLogger.
func New(ctx context.Context, method string, kvps ...interface{}) (*SpanLogger, context.Context) {
	span, ctx := opentracing.StartSpanFromContext(ctx, method)
	logger := &SpanLogger{
		Logger: log.With(util.WithContext(ctx, util.Logger), "method", method),
		Span:   span,
	}
	if len(kvps) > 0 {
		level.Debug(logger).Log(kvps...)
	}
	return logger, ctx
}

// FromContext returns a span logger using the current parent span.
// If there is no parent span, the Spanlogger will only log to stdout.
func FromContext(ctx context.Context) *SpanLogger {
	sp := opentracing.SpanFromContext(ctx)
	if sp == nil {
		return &SpanLogger{
			Logger: util.WithContext(ctx, util.Logger),
			Span:   defaultNoopSpan,
		}
	}
	return &SpanLogger{
		Logger: util.WithContext(ctx, util.Logger),
		Span:   sp,
	}
}

// Log implements gokit's Logger interface; sends logs to underlying logger and
// also puts the on the spans.
func (s *SpanLogger) Log(kvps ...interface{}) error {
	s.Logger.Log(kvps...)
	fields, err := otlog.InterleavedKVToFields(kvps...)
	if err != nil {
		return err
	}
	s.Span.LogFields(fields...)
	return nil
}

// Error sets error flag and logs the error on the span, if non-nil.  Returns the err passed in.
func (s *SpanLogger) Error(err error) error {
	if err == nil {
		return nil
	}
	ext.Error.Set(s.Span, true)
	s.Span.LogFields(otlog.Error(err))
	return err
}
