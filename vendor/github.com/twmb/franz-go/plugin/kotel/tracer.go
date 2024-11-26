package kotel

import (
	"context"
	"unicode/utf8"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.18.0"
	"go.opentelemetry.io/otel/trace"
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

func (t *Tracer) maybeKeyAttr(attrs []attribute.KeyValue, r *kgo.Record) []attribute.KeyValue {
	if r.Key == nil {
		return attrs
	}
	var keykey string
	if t.keyFormatter != nil {
		k, err := t.keyFormatter(r)
		if err != nil || !utf8.ValidString(k) {
			return attrs
		}
		keykey = k
	} else {
		if !utf8.Valid(r.Key) {
			return attrs
		}
		keykey = string(r.Key)
	}
	return append(attrs, semconv.MessagingKafkaMessageKeyKey.String(keykey))
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
	attrs := []attribute.KeyValue{
		semconv.MessagingSystemKey.String("kafka"),
		semconv.MessagingSourceKindTopic,
		semconv.MessagingSourceName(r.Topic),
		semconv.MessagingOperationProcess,
		semconv.MessagingKafkaSourcePartition(int(r.Partition)),
		semconv.MessagingKafkaMessageOffset(int(r.Offset)),
	}
	attrs = t.maybeKeyAttr(attrs, r)
	if t.clientID != "" {
		attrs = append(attrs, semconv.MessagingKafkaClientIDKey.String(t.clientID))
	}
	if t.consumerGroup != "" {
		attrs = append(attrs, semconv.MessagingKafkaConsumerGroupKey.String(t.consumerGroup))
	}
	if r.Key != nil && r.Value == nil {
		attrs = append(attrs, semconv.MessagingKafkaMessageTombstoneKey.Bool(true))
	}
	opts := []trace.SpanStartOption{
		trace.WithAttributes(attrs...),
		trace.WithSpanKind(trace.SpanKindConsumer),
	}

	if r.Context == nil {
		r.Context = context.Background()
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
	attrs := []attribute.KeyValue{
		semconv.MessagingSystemKey.String("kafka"),
		semconv.MessagingDestinationKindTopic,
		semconv.MessagingDestinationName(r.Topic),
		semconv.MessagingOperationPublish,
	}
	attrs = t.maybeKeyAttr(attrs, r)
	if t.clientID != "" {
		attrs = append(attrs, semconv.MessagingKafkaClientIDKey.String(t.clientID))
	}
	if r.Key != nil && r.Value == nil {
		attrs = append(attrs, semconv.MessagingKafkaMessageTombstoneKey.Bool(true))
	}
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
	attrs := []attribute.KeyValue{
		semconv.MessagingSystemKey.String("kafka"),
		semconv.MessagingSourceKindTopic,
		semconv.MessagingSourceName(r.Topic),
		semconv.MessagingOperationReceive,
		semconv.MessagingKafkaSourcePartition(int(r.Partition)),
	}
	attrs = t.maybeKeyAttr(attrs, r)
	if t.clientID != "" {
		attrs = append(attrs, semconv.MessagingKafkaClientIDKey.String(t.clientID))
	}
	if t.consumerGroup != "" {
		attrs = append(attrs, semconv.MessagingKafkaConsumerGroupKey.String(t.consumerGroup))
	}
	if r.Key != nil && r.Value == nil {
		attrs = append(attrs, semconv.MessagingKafkaMessageTombstoneKey.Bool(true))
	}
	opts := []trace.SpanStartOption{
		trace.WithAttributes(attrs...),
		trace.WithSpanKind(trace.SpanKindConsumer),
	}

	if r.Context == nil {
		r.Context = context.Background()
	}
	// Extract the span context from the record.
	ctx := t.propagators.Extract(r.Context, NewRecordCarrier(r))
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
