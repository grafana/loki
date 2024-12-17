package mocktracer

import (
	"sync"

	"github.com/opentracing/opentracing-go"
)

// New returns a MockTracer opentracing.Tracer implementation that's intended
// to facilitate tests of OpenTracing instrumentation.
func New() *MockTracer {
	t := &MockTracer{
		finishedSpans: []*MockSpan{},
		startedSpans:  []*MockSpan{},
		injectors:     make(map[interface{}]Injector),
		extractors:    make(map[interface{}]Extractor),
	}

	// register default injectors/extractors
	textPropagator := new(TextMapPropagator)
	t.RegisterInjector(opentracing.TextMap, textPropagator)
	t.RegisterExtractor(opentracing.TextMap, textPropagator)

	httpPropagator := &TextMapPropagator{HTTPHeaders: true}
	t.RegisterInjector(opentracing.HTTPHeaders, httpPropagator)
	t.RegisterExtractor(opentracing.HTTPHeaders, httpPropagator)

	return t
}

// MockTracer is only intended for testing OpenTracing instrumentation.
//
// It is entirely unsuitable for production use, but appropriate for tests
// that want to verify tracing behavior in other frameworks/applications.
type MockTracer struct {
	sync.RWMutex
	finishedSpans []*MockSpan
	startedSpans  []*MockSpan
	injectors     map[interface{}]Injector
	extractors    map[interface{}]Extractor
}

// UnfinishedSpans returns all spans that have been started and not finished since the
// MockTracer was constructed or since the last call to its Reset() method.
func (t *MockTracer) UnfinishedSpans() []*MockSpan {
	t.RLock()
	defer t.RUnlock()

	spans := make([]*MockSpan, len(t.startedSpans))
	copy(spans, t.startedSpans)
	return spans
}

// FinishedSpans returns all spans that have been Finish()'ed since the
// MockTracer was constructed or since the last call to its Reset() method.
func (t *MockTracer) FinishedSpans() []*MockSpan {
	t.RLock()
	defer t.RUnlock()
	spans := make([]*MockSpan, len(t.finishedSpans))
	copy(spans, t.finishedSpans)
	return spans
}

// Reset clears the internally accumulated finished spans. Note that any
// extant MockSpans will still append to finishedSpans when they Finish(),
// even after a call to Reset().
func (t *MockTracer) Reset() {
	t.Lock()
	defer t.Unlock()
	t.startedSpans = []*MockSpan{}
	t.finishedSpans = []*MockSpan{}
}

// StartSpan belongs to the Tracer interface.
func (t *MockTracer) StartSpan(operationName string, opts ...opentracing.StartSpanOption) opentracing.Span {
	sso := opentracing.StartSpanOptions{}
	for _, o := range opts {
		o.Apply(&sso)
	}

	span := newMockSpan(t, operationName, sso)
	t.recordStartedSpan(span)
	return span
}

// RegisterInjector registers injector for given format
func (t *MockTracer) RegisterInjector(format interface{}, injector Injector) {
	t.injectors[format] = injector
}

// RegisterExtractor registers extractor for given format
func (t *MockTracer) RegisterExtractor(format interface{}, extractor Extractor) {
	t.extractors[format] = extractor
}

// Inject belongs to the Tracer interface.
func (t *MockTracer) Inject(sm opentracing.SpanContext, format interface{}, carrier interface{}) error {
	spanContext, ok := sm.(MockSpanContext)
	if !ok {
		return opentracing.ErrInvalidSpanContext
	}
	injector, ok := t.injectors[format]
	if !ok {
		return opentracing.ErrUnsupportedFormat
	}
	return injector.Inject(spanContext, carrier)
}

// Extract belongs to the Tracer interface.
func (t *MockTracer) Extract(format interface{}, carrier interface{}) (opentracing.SpanContext, error) {
	extractor, ok := t.extractors[format]
	if !ok {
		return nil, opentracing.ErrUnsupportedFormat
	}
	return extractor.Extract(carrier)
}

func (t *MockTracer) recordStartedSpan(span *MockSpan) {
	t.Lock()
	defer t.Unlock()
	t.startedSpans = append(t.startedSpans, span)
}

func (t *MockTracer) recordFinishedSpan(span *MockSpan) {
	t.Lock()
	defer t.Unlock()
	t.finishedSpans = append(t.finishedSpans, span)

	for i := range t.startedSpans {
		if t.startedSpans[i].SpanContext.SpanID == span.SpanContext.SpanID &&
			t.startedSpans[i].SpanContext.TraceID == span.SpanContext.TraceID {
			t.startedSpans = append(t.startedSpans[:i], t.startedSpans[i+1:]...)
			return
		}
	}
}
