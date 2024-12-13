// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pubsub

import (
	"context"
	"fmt"
	"log"
	"sync"

	pb "cloud.google.com/go/pubsub/apiv1/pubsubpb"
	"cloud.google.com/go/pubsub/internal"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// The following keys are used to tag requests with a specific topic/subscription ID.
var (
	keyTopic        = tag.MustNewKey("topic")
	keySubscription = tag.MustNewKey("subscription")
)

// In the following, errors are used if status is not "OK".
var (
	keyStatus = tag.MustNewKey("status")
	keyError  = tag.MustNewKey("error")
)

const statsPrefix = "cloud.google.com/go/pubsub/"

// The following are measures recorded in publish/subscribe flows.
var (
	// PublishedMessages is a measure of the number of messages published, which may include errors.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	PublishedMessages = stats.Int64(statsPrefix+"published_messages", "Number of PubSub message published", stats.UnitDimensionless)

	// PublishLatency is a measure of the number of milliseconds it took to publish a bundle,
	// which may consist of one or more messages.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	PublishLatency = stats.Float64(statsPrefix+"publish_roundtrip_latency", "The latency in milliseconds per publish batch", stats.UnitMilliseconds)

	// PullCount is a measure of the number of messages pulled.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	PullCount = stats.Int64(statsPrefix+"pull_count", "Number of PubSub messages pulled", stats.UnitDimensionless)

	// AckCount is a measure of the number of messages acked.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	AckCount = stats.Int64(statsPrefix+"ack_count", "Number of PubSub messages acked", stats.UnitDimensionless)

	// NackCount is a measure of the number of messages nacked.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	NackCount = stats.Int64(statsPrefix+"nack_count", "Number of PubSub messages nacked", stats.UnitDimensionless)

	// ModAckCount is a measure of the number of messages whose ack-deadline was modified.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	ModAckCount = stats.Int64(statsPrefix+"mod_ack_count", "Number of ack-deadlines modified", stats.UnitDimensionless)

	// ModAckTimeoutCount is a measure of the number ModifyAckDeadline RPCs that timed out.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	ModAckTimeoutCount = stats.Int64(statsPrefix+"mod_ack_timeout_count", "Number of ModifyAckDeadline RPCs that timed out", stats.UnitDimensionless)

	// StreamOpenCount is a measure of the number of times a streaming-pull stream was opened.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	StreamOpenCount = stats.Int64(statsPrefix+"stream_open_count", "Number of calls opening a new streaming pull", stats.UnitDimensionless)

	// StreamRetryCount is a measure of the number of times a streaming-pull operation was retried.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	StreamRetryCount = stats.Int64(statsPrefix+"stream_retry_count", "Number of retries of a stream send or receive", stats.UnitDimensionless)

	// StreamRequestCount is a measure of the number of requests sent on a streaming-pull stream.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	StreamRequestCount = stats.Int64(statsPrefix+"stream_request_count", "Number gRPC StreamingPull request messages sent", stats.UnitDimensionless)

	// StreamResponseCount is a measure of the number of responses received on a streaming-pull stream.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	StreamResponseCount = stats.Int64(statsPrefix+"stream_response_count", "Number of gRPC StreamingPull response messages received", stats.UnitDimensionless)

	// OutstandingMessages is a measure of the number of outstanding messages held by the client before they are processed.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	OutstandingMessages = stats.Int64(statsPrefix+"outstanding_messages", "Number of outstanding Pub/Sub messages", stats.UnitDimensionless)

	// OutstandingBytes is a measure of the number of bytes all outstanding messages held by the client take up.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	OutstandingBytes = stats.Int64(statsPrefix+"outstanding_bytes", "Number of outstanding bytes", stats.UnitDimensionless)

	// PublisherOutstandingMessages is a measure of the number of published outstanding messages held by the client before they are processed.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	PublisherOutstandingMessages = stats.Int64(statsPrefix+"publisher_outstanding_messages", "Number of outstanding publish messages", stats.UnitDimensionless)

	// PublisherOutstandingBytes is a measure of the number of bytes all outstanding publish messages held by the client take up.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	PublisherOutstandingBytes = stats.Int64(statsPrefix+"publisher_outstanding_bytes", "Number of outstanding publish bytes", stats.UnitDimensionless)
)

var (
	// PublishedMessagesView is a cumulative sum of PublishedMessages.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	PublishedMessagesView *view.View

	// PublishLatencyView is a distribution of PublishLatency.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	PublishLatencyView *view.View

	// PullCountView is a cumulative sum of PullCount.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	PullCountView *view.View

	// AckCountView is a cumulative sum of AckCount.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	AckCountView *view.View

	// NackCountView is a cumulative sum of NackCount.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	NackCountView *view.View

	// ModAckCountView is a cumulative sum of ModAckCount.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	ModAckCountView *view.View

	// ModAckTimeoutCountView is a cumulative sum of ModAckTimeoutCount.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	ModAckTimeoutCountView *view.View

	// StreamOpenCountView is a cumulative sum of StreamOpenCount.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	StreamOpenCountView *view.View

	// StreamRetryCountView is a cumulative sum of StreamRetryCount.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	StreamRetryCountView *view.View

	// StreamRequestCountView is a cumulative sum of StreamRequestCount.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	StreamRequestCountView *view.View

	// StreamResponseCountView is a cumulative sum of StreamResponseCount.
	// It is EXPERIMENTAL and subject to change or removal without notice.
	StreamResponseCountView *view.View

	// OutstandingMessagesView is the last value of OutstandingMessages
	// It is EXPERIMENTAL and subject to change or removal without notice.
	OutstandingMessagesView *view.View

	// OutstandingBytesView is the last value of OutstandingBytes
	// It is EXPERIMENTAL and subject to change or removal without notice.
	OutstandingBytesView *view.View

	// PublisherOutstandingMessagesView is the last value of OutstandingMessages
	// It is EXPERIMENTAL and subject to change or removal without notice.
	PublisherOutstandingMessagesView *view.View

	// PublisherOutstandingBytesView is the last value of OutstandingBytes
	// It is EXPERIMENTAL and subject to change or removal without notice.
	PublisherOutstandingBytesView *view.View
)

func init() {
	PublishedMessagesView = createCountView(stats.Measure(PublishedMessages), keyTopic, keyStatus, keyError)
	PublishLatencyView = createDistView(PublishLatency, keyTopic, keyStatus, keyError)
	PublisherOutstandingMessagesView = createLastValueView(PublisherOutstandingMessages, keyTopic)
	PublisherOutstandingBytesView = createLastValueView(PublisherOutstandingBytes, keyTopic)
	PullCountView = createCountView(PullCount, keySubscription)
	AckCountView = createCountView(AckCount, keySubscription)
	NackCountView = createCountView(NackCount, keySubscription)
	ModAckCountView = createCountView(ModAckCount, keySubscription)
	ModAckTimeoutCountView = createCountView(ModAckTimeoutCount, keySubscription)
	StreamOpenCountView = createCountView(StreamOpenCount, keySubscription)
	StreamRetryCountView = createCountView(StreamRetryCount, keySubscription)
	StreamRequestCountView = createCountView(StreamRequestCount, keySubscription)
	StreamResponseCountView = createCountView(StreamResponseCount, keySubscription)
	OutstandingMessagesView = createLastValueView(OutstandingMessages, keySubscription)
	OutstandingBytesView = createLastValueView(OutstandingBytes, keySubscription)

	DefaultPublishViews = []*view.View{
		PublishedMessagesView,
		PublishLatencyView,
		PublisherOutstandingMessagesView,
		PublisherOutstandingBytesView,
	}

	DefaultSubscribeViews = []*view.View{
		PullCountView,
		AckCountView,
		NackCountView,
		ModAckCountView,
		ModAckTimeoutCountView,
		StreamOpenCountView,
		StreamRetryCountView,
		StreamRequestCountView,
		StreamResponseCountView,
		OutstandingMessagesView,
		OutstandingBytesView,
	}
}

// These arrays hold the default OpenCensus views that keep track of publish/subscribe operations.
// It is EXPERIMENTAL and subject to change or removal without notice.
var (
	DefaultPublishViews   []*view.View
	DefaultSubscribeViews []*view.View
)

func createCountView(m stats.Measure, keys ...tag.Key) *view.View {
	return &view.View{
		Name:        m.Name(),
		Description: m.Description(),
		TagKeys:     keys,
		Measure:     m,
		Aggregation: view.Sum(),
	}
}

func createDistView(m stats.Measure, keys ...tag.Key) *view.View {
	return &view.View{
		Name:        m.Name(),
		Description: m.Description(),
		TagKeys:     keys,
		Measure:     m,
		Aggregation: view.Distribution(0, 25, 50, 75, 100, 200, 400, 600, 800, 1000, 2000, 4000, 6000),
	}
}

func createLastValueView(m stats.Measure, keys ...tag.Key) *view.View {
	return &view.View{
		Name:        m.Name(),
		Description: m.Description(),
		TagKeys:     keys,
		Measure:     m,
		Aggregation: view.LastValue(),
	}
}

var logOnce sync.Once

// withSubscriptionKey returns a new context modified with the subscriptionKey tag map.
func withSubscriptionKey(ctx context.Context, subName string) context.Context {
	ctx, err := tag.New(ctx, tag.Upsert(keySubscription, subName))
	if err != nil {
		logOnce.Do(func() {
			log.Printf("pubsub: error creating tag map for 'subscribe' key: %v", err)
		})
	}
	return ctx
}

func recordStat(ctx context.Context, m *stats.Int64Measure, n int64) {
	stats.Record(ctx, m.M(n))
}

const defaultTracerName = "cloud.google.com/go/pubsub"

func tracer() trace.Tracer {
	return otel.Tracer(defaultTracerName, trace.WithInstrumentationVersion(internal.Version))
}

var _ propagation.TextMapCarrier = (*messageCarrier)(nil)

// messageCarrier injects and extracts traces from pubsub.Message attributes.
type messageCarrier struct {
	attributes map[string]string
}

const googclientPrefix string = "googclient_"

// newMessageCarrier creates a new PubsubMessageCarrier.
func newMessageCarrier(msg *Message) messageCarrier {
	return messageCarrier{attributes: msg.Attributes}
}

// NewMessageCarrierFromPB creates a propagation.TextMapCarrier that can be used to extract the trace
// context from a protobuf PubsubMessage.
//
// Example:
// ctx = propagation.TraceContext{}.Extract(ctx, pubsub.NewMessageCarrierFromPB(msg))
func NewMessageCarrierFromPB(msg *pb.PubsubMessage) propagation.TextMapCarrier {
	return messageCarrier{attributes: msg.Attributes}
}

// Get retrieves a single value for a given key.
func (c messageCarrier) Get(key string) string {
	return c.attributes[googclientPrefix+key]
}

// Set sets an attribute.
func (c messageCarrier) Set(key, val string) {
	c.attributes[googclientPrefix+key] = val
}

// Keys returns a slice of all keys in the carrier.
func (c messageCarrier) Keys() []string {
	i := 0
	out := make([]string, len(c.attributes))
	for k := range c.attributes {
		out[i] = k
		i++
	}
	return out
}

// injectPropagation injects context data into the Pub/Sub message's Attributes field.
func injectPropagation(ctx context.Context, msg *Message) {
	// only inject propagation if a valid span context was detected.
	if trace.SpanFromContext(ctx).SpanContext().IsValid() {
		if msg.Attributes == nil {
			msg.Attributes = make(map[string]string)
		}
		propagation.TraceContext{}.Inject(ctx, newMessageCarrier(msg))
	}
}

const (
	// publish span names
	createSpanName     = "create"
	publishFCSpanName  = "publisher flow control"
	batcherSpanName    = "publisher batching"
	publishRPCSpanName = "publish"

	// subscribe span names
	subscribeSpanName = "subscribe"
	ccSpanName        = "subscriber concurrency control"
	processSpanName   = "process"
	scheduleSpanName  = "subscribe scheduler"
	modackSpanName    = "modack"
	ackSpanName       = "ack"
	nackSpanName      = "nack"

	// event names
	eventPublishStart = "publish start"
	eventPublishEnd   = "publish end"
	eventModackStart  = "modack start"
	eventModackEnd    = "modack end"
	eventAckStart     = "ack start"
	eventAckEnd       = "ack end"
	eventNackStart    = "nack start"
	eventNackEnd      = "nack end"
	eventAckCalled    = "ack called"
	eventNackCalled   = "nack called"

	resultAcked   = "acked"
	resultNacked  = "nacked"
	resultExpired = "expired"

	// custom pubsub specific attributes
	gcpProjectIDAttribute  = "gcp.project_id"
	pubsubPrefix           = "messaging.gcp_pubsub."
	eosAttribute           = pubsubPrefix + "exactly_once_delivery"
	resultAttribute        = pubsubPrefix + "result"
	receiptModackAttribute = pubsubPrefix + "is_receipt_modack"
)

func startSpan(ctx context.Context, spanType, resourceID string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	spanName := spanType
	if resourceID != "" {
		spanName = fmt.Sprintf("%s %s", resourceID, spanType)
	}
	return tracer().Start(ctx, spanName, opts...)
}

func getPublishSpanAttributes(project, dst string, msg *Message, attrs ...attribute.KeyValue) []trace.SpanStartOption {
	opts := []trace.SpanStartOption{
		trace.WithAttributes(
			semconv.MessagingMessageID(msg.ID),
			semconv.MessagingMessageBodySize(len(msg.Data)),
			semconv.MessagingGCPPubsubMessageOrderingKey(msg.OrderingKey),
		),
		trace.WithAttributes(attrs...),
		trace.WithSpanKind(trace.SpanKindProducer),
	}
	opts = append(opts, getCommonOptions(project, dst)...)
	return opts
}

func getSubscriberOpts(project, dst string, msg *Message, attrs ...attribute.KeyValue) []trace.SpanStartOption {
	opts := []trace.SpanStartOption{
		trace.WithAttributes(
			semconv.MessagingMessageID(msg.ID),
			semconv.MessagingMessageBodySize(len(msg.Data)),
			semconv.MessagingGCPPubsubMessageOrderingKey(msg.OrderingKey),
		),
		trace.WithAttributes(attrs...),
		trace.WithSpanKind(trace.SpanKindConsumer),
	}
	if msg.DeliveryAttempt != nil {
		opts = append(opts, trace.WithAttributes(semconv.MessagingGCPPubsubMessageDeliveryAttempt(*msg.DeliveryAttempt)))
	}
	opts = append(opts, getCommonOptions(project, dst)...)
	return opts
}

func getCommonOptions(projectID, destination string) []trace.SpanStartOption {
	opts := []trace.SpanStartOption{
		trace.WithAttributes(
			attribute.String(gcpProjectIDAttribute, projectID),
			semconv.MessagingSystemGCPPubsub,
			semconv.MessagingDestinationName(destination),
		),
	}
	return opts

}

// spanRecordError records the error, sets the status to error, and ends the span.
// This is recommended by https://opentelemetry.io/docs/instrumentation/go/manual/#record-errors
// since RecordError doesn't set the status of a span.
func spanRecordError(span trace.Span, err error) {
	if span != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, err.Error())
		span.End()
	}
}
