package kotel

import (
	"context"
	"unicode/utf8"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.18.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/twmb/franz-go/pkg/kgo"
)

var ( // interface checks to ensure we implement the hooks properly.
	_ kgo.HookProduceRecordBuffered   = new(Tracer)
	_ kgo.HookProduceRecordUnbuffered = new(Tracer)
	_ kgo.HookFetchRecordBuffered     = new(Tracer)
	_ kgo.HookFetchRecordUnbuffered   = new(Tracer)
)

type Tracer struct {
	tracerProvider trace.TracerProvider
	propagators    propagation.TextMapPropagator
	tracer         trace.Tracer
	clientID       string
	consumerGroup  string
	keyFormatter   func(*kgo.Record) (string, error)
	linkSpans      bool
}

// TracerOpt interface used for setting optional config properties.
type TracerOpt interface{ apply(*Tracer) }

type tracerOptFunc func(*Tracer)

func (o tracerOptFunc) apply(t *Tracer) { o(t) }

// TracerProvider takes a trace.TracerProvider and applies it to the Tracer.
// If none is specified, the global provider is used.
func TracerProvider(provider trace.TracerProvider) TracerOpt {
	return tracerOptFunc(func(t *Tracer) { t.tracerProvider = provider })
}

// TracerPropagator takes a propagation.TextMapPropagator and applies it to the
// Tracer.
//
// If none is specified, the global Propagator is used.
func TracerPropagator(propagator propagation.TextMapPropagator) TracerOpt {
	return tracerOptFunc(func(t *Tracer) { t.propagators = propagator })
}

// ClientID sets the optional client_id attribute value.
func ClientID(id string) TracerOpt {
	return tracerOptFunc(func(t *Tracer) { t.clientID = id })
}

// ConsumerGroup sets the optional group attribute value.
func ConsumerGroup(group string) TracerOpt {
	return tracerOptFunc(func(t *Tracer) { t.consumerGroup = group })
}

// KeyFormatter formats a Record's key for use in a span's attributes,
// overriding the default of string(Record.Key).
//
// This option can be used to parse binary data and return a canonical string
// representation. If the returned string is not valid UTF-8 or if the
// formatter returns an error, the key is not attached to the span.
func KeyFormatter(fn func(*kgo.Record) (string, error)) TracerOpt {
	return tracerOptFunc(func(t *Tracer) { t.keyFormatter = fn })
}

// LinkSpans enables consumer spans to be linked to the parent span,
// instead of creating a child relationship.
func LinkSpans() TracerOpt {
	return tracerOptFunc(func(t *Tracer) { t.linkSpans = true })
}

// NewTracer returns a Tracer, used as option for kotel to instrument franz-go
// with tracing.
func NewTracer(opts ...TracerOpt) *Tracer {
	t := &Tracer{}
	for _, opt := range opts {
		opt.apply(t)
	}
	if t.tracerProvider == nil {
		t.tracerProvider = otel.GetTracerProvider()
	}
	if t.propagators == nil {
		t.propagators = otel.GetTextMapPropagator()
	}
	t.tracer = t.tracerProvider.Tracer(
		instrumentationName,
		trace.WithInstrumentationVersion(semVersion()),
		trace.WithSchemaURL(semconv.SchemaURL),
	)
	return t
}

func (t *Tracer) keyAttr(r *kgo.Record) (attribute.KeyValue, bool) {
	if r.Key == nil {
		return attribute.KeyValue{}, false
	}
	if t.keyFormatter != nil {
		key, err := t.keyFormatter(r)
		if err != nil || !utf8.ValidString(key) {
			return attribute.KeyValue{}, false
		}
		return semconv.MessagingKafkaMessageKeyKey.String(key), true
	}
	if !utf8.Valid(r.Key) {
		return attribute.KeyValue{}, false
	}
	return semconv.MessagingKafkaMessageKeyKey.String(string(r.Key)), true
}

func (t *Tracer) publishAttrs(r *kgo.Record) []attribute.KeyValue {
	topic := r.Topic
	key, hasKey := t.keyAttr(r)
	tombstone := r.Key != nil && r.Value == nil
	n := 4
	if hasKey {
		n++
	}
	if t.clientID != "" {
		n++
	}
	if tombstone {
		n++
	}

	attrs := make([]attribute.KeyValue, 0, n)
	attrs = append(attrs,
		semconv.MessagingSystemKey.String("kafka"),
		semconv.MessagingDestinationKindTopic,
		semconv.MessagingDestinationName(topic),
		semconv.MessagingOperationPublish,
	)
	if hasKey {
		attrs = append(attrs, key)
	}
	if t.clientID != "" {
		attrs = append(attrs, semconv.MessagingKafkaClientIDKey.String(t.clientID))
	}
	if tombstone {
		attrs = append(attrs, semconv.MessagingKafkaMessageTombstoneKey.Bool(true))
	}
	return attrs
}

func (t *Tracer) consumerAttrs(r *kgo.Record, operation attribute.KeyValue) []attribute.KeyValue {
	topic := r.Topic
	partition := r.Partition
	offset := r.Offset
	key, hasKey := t.keyAttr(r)
	tombstone := r.Key != nil && r.Value == nil
	n := 6
	if hasKey {
		n++
	}
	if t.clientID != "" {
		n++
	}
	if t.consumerGroup != "" {
		n++
	}
	if tombstone {
		n++
	}

	attrs := make([]attribute.KeyValue, 0, n)
	attrs = append(attrs,
		semconv.MessagingSystemKey.String("kafka"),
		semconv.MessagingSourceKindTopic,
		semconv.MessagingSourceName(topic),
		operation,
		semconv.MessagingKafkaSourcePartition(int(partition)),
		semconv.MessagingKafkaMessageOffsetKey.Int64(offset),
	)
	if hasKey {
		attrs = append(attrs, key)
	}
	if t.clientID != "" {
		attrs = append(attrs, semconv.MessagingKafkaClientIDKey.String(t.clientID))
	}
	if t.consumerGroup != "" {
		attrs = append(attrs, semconv.MessagingKafkaConsumerGroupKey.String(t.consumerGroup))
	}
	if tombstone {
		attrs = append(attrs, semconv.MessagingKafkaMessageTombstoneKey.Bool(true))
	}
	return attrs
}

// WithProcessSpan starts a new span for the "process" operation on a consumer
// record.
//
// It sets up the span options. The user's application code is responsible for
// ending the span.
//
// This should only ever be called within a polling loop of a consumed record and
// not a record which has been created for producing, so call this at the start of each
// iteration of your processing for the record.
func (t *Tracer) WithProcessSpan(r *kgo.Record) (context.Context, trace.Span) {
	// Set up the span options.
	attrs := t.consumerAttrs(r, semconv.MessagingOperationProcess)
	opts := []trace.SpanStartOption{
		trace.WithAttributes(attrs...),
		trace.WithSpanKind(trace.SpanKindConsumer),
	}

	if r.Context == nil {
		r.Context = context.Background()
	}

	if t.linkSpans {
		opts = append(opts, trace.WithNewRoot())
		if s := trace.SpanContextFromContext(r.Context); s.IsValid() {
			opts = append(opts, trace.WithLinks(trace.Link{SpanContext: s}))
		}
	}

	// Start a new span using the provided context and options.
	return t.tracer.Start(r.Context, r.Topic+" process", opts...)
}

// Hooks ----------------------------------------------------------------------

// OnProduceRecordBuffered starts a new span for the "publish" operation on a
// buffered record.
//
// It sets span options and injects the span context into record and updates
// the record's context, so it can be ended in the OnProduceRecordUnbuffered
// hook.
func (t *Tracer) OnProduceRecordBuffered(r *kgo.Record) {
	// Set up span options.
	attrs := t.publishAttrs(r)
	opts := []trace.SpanStartOption{
		trace.WithAttributes(attrs...),
		trace.WithSpanKind(trace.SpanKindProducer),
	}
	// Start the "publish" span.
	ctx, _ := t.tracer.Start(r.Context, r.Topic+" publish", opts...)
	// Inject the span context into the record.
	t.propagators.Inject(ctx, NewRecordCarrier(r))
	// Update the record context.
	r.Context = ctx
}

// OnProduceRecordUnbuffered continues and ends the "publish" span for an
// unbuffered record.
//
// It sets attributes with values unset when producing and records any error
// that occurred during the publish operation.
func (t *Tracer) OnProduceRecordUnbuffered(r *kgo.Record, err error) {
	span := trace.SpanFromContext(r.Context)
	defer span.End()
	span.SetAttributes(
		semconv.MessagingKafkaDestinationPartition(int(r.Partition)),
		semconv.MessagingKafkaMessageOffsetKey.Int64(r.Offset),
	)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
	}
}

// OnFetchRecordBuffered starts a new span for the "receive" operation on a
// buffered record.
//
// It sets the span options and extracts the span context from the record,
// updates the record's context to ensure it can be ended in the
// OnFetchRecordUnbuffered hook and can be used in downstream consumer
// processing.
func (t *Tracer) OnFetchRecordBuffered(r *kgo.Record) {
	// Set up the span options.
	attrs := t.consumerAttrs(r, semconv.MessagingOperationReceive)
	opts := []trace.SpanStartOption{
		trace.WithAttributes(attrs...),
		trace.WithSpanKind(trace.SpanKindConsumer),
	}

	if r.Context == nil {
		r.Context = context.Background()
	}
	// Extract the span context from the record.
	ctx := t.propagators.Extract(r.Context, NewRecordCarrier(r))

	if t.linkSpans {
		opts = append(opts, trace.WithNewRoot())
		if s := trace.SpanContextFromContext(ctx); s.IsValid() {
			opts = append(opts, trace.WithLinks(trace.Link{SpanContext: s}))
		}
	}

	// Start the "receive" span.
	newCtx, _ := t.tracer.Start(ctx, r.Topic+" receive", opts...)
	// Update the record context.
	r.Context = newCtx
}

// OnFetchRecordUnbuffered continues and ends the "receive" span for an
// unbuffered record.
func (t *Tracer) OnFetchRecordUnbuffered(r *kgo.Record, _ bool) {
	span := trace.SpanFromContext(r.Context)
	defer span.End()
}
