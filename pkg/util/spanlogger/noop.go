package spanlogger

import (
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
)

type noopTracer struct{}

type noopSpan struct{}
type noopSpanContext struct{}

var (
	defaultNoopSpanContext = noopSpanContext{}
	defaultNoopSpan        = noopSpan{}
	defaultNoopTracer      = noopTracer{}
)

const (
	emptyString = ""
)

func (n noopSpanContext) ForeachBaggageItem(_ func(k, v string) bool) {}

func (n noopSpan) Context() opentracing.SpanContext                { return defaultNoopSpanContext }
func (n noopSpan) SetBaggageItem(_, _ string) opentracing.Span     { return defaultNoopSpan }
func (n noopSpan) BaggageItem(_ string) string                     { return emptyString }
func (n noopSpan) SetTag(_ string, _ interface{}) opentracing.Span { return n }
func (n noopSpan) LogFields(_ ...log.Field)                        {}
func (n noopSpan) LogKV(_ ...interface{})                          {}
func (n noopSpan) Finish()                                         {}
func (n noopSpan) FinishWithOptions(_ opentracing.FinishOptions)   {}
func (n noopSpan) SetOperationName(_ string) opentracing.Span      { return n }
func (n noopSpan) Tracer() opentracing.Tracer                      { return defaultNoopTracer }
func (n noopSpan) LogEvent(_ string)                               {}
func (n noopSpan) LogEventWithPayload(_ string, _ interface{})     {}
func (n noopSpan) Log(_ opentracing.LogData)                       {}

// StartSpan belongs to the Tracer interface.
func (n noopTracer) StartSpan(_ string, _ ...opentracing.StartSpanOption) opentracing.Span {
	return defaultNoopSpan
}

// Inject belongs to the Tracer interface.
func (n noopTracer) Inject(_ opentracing.SpanContext, _ interface{}, _ interface{}) error {
	return nil
}

// Extract belongs to the Tracer interface.
func (n noopTracer) Extract(_ interface{}, _ interface{}) (opentracing.SpanContext, error) {
	return nil, opentracing.ErrSpanContextNotFound
}
