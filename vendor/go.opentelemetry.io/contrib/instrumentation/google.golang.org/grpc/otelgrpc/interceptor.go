// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otelgrpc // import "go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"

// gRPC tracing middleware
// https://opentelemetry.io/docs/specs/semconv/rpc/
import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"

	"google.golang.org/grpc"
	grpc_codes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc/internal"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	"go.opentelemetry.io/otel/trace"
)

type messageType attribute.KeyValue

// Event adds an event of the messageType to the span associated with the
// passed context with a message id.
func (m messageType) Event(ctx context.Context, id int, _ interface{}) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.AddEvent("message", trace.WithAttributes(
		attribute.KeyValue(m),
		semconv.RPCMessageIDKey.Int(id),
	))
}

var (
	messageSent     = messageType(semconv.RPCMessageTypeSent)
	messageReceived = messageType(semconv.RPCMessageTypeReceived)
)

// clientStream  wraps around the embedded grpc.ClientStream, and intercepts the RecvMsg and
// SendMsg method call.
type clientStream struct {
	grpc.ClientStream
	desc *grpc.StreamDesc

	span trace.Span

	receivedEvent bool
	sentEvent     bool

	receivedMessageID int
	sentMessageID     int
}

var _ = proto.Marshal

func (w *clientStream) RecvMsg(m interface{}) error {
	err := w.ClientStream.RecvMsg(m)

	if err == nil && !w.desc.ServerStreams {
		w.endSpan(nil)
	} else if errors.Is(err, io.EOF) {
		w.endSpan(nil)
	} else if err != nil {
		w.endSpan(err)
	} else {
		w.receivedMessageID++

		if w.receivedEvent {
			messageReceived.Event(w.Context(), w.receivedMessageID, m)
		}
	}

	return err
}

func (w *clientStream) SendMsg(m interface{}) error {
	err := w.ClientStream.SendMsg(m)

	w.sentMessageID++

	if w.sentEvent {
		messageSent.Event(w.Context(), w.sentMessageID, m)
	}

	if err != nil {
		w.endSpan(err)
	}

	return err
}

func (w *clientStream) Header() (metadata.MD, error) {
	md, err := w.ClientStream.Header()
	if err != nil {
		w.endSpan(err)
	}

	return md, err
}

func (w *clientStream) CloseSend() error {
	err := w.ClientStream.CloseSend()
	if err != nil {
		w.endSpan(err)
	}

	return err
}

func wrapClientStream(s grpc.ClientStream, desc *grpc.StreamDesc, span trace.Span, cfg *config) *clientStream {
	return &clientStream{
		ClientStream:  s,
		span:          span,
		desc:          desc,
		receivedEvent: cfg.ReceivedEvent,
		sentEvent:     cfg.SentEvent,
	}
}

func (w *clientStream) endSpan(err error) {
	if err != nil {
		s, _ := status.FromError(err)
		w.span.SetStatus(codes.Error, s.Message())
		w.span.SetAttributes(statusCodeAttr(s.Code()))
	} else {
		w.span.SetAttributes(statusCodeAttr(grpc_codes.OK))
	}

	w.span.End()
}

// StreamClientInterceptor returns a grpc.StreamClientInterceptor suitable
// for use in a grpc.NewClient call.
//
// Deprecated: Use [NewClientHandler] instead.
func StreamClientInterceptor(opts ...Option) grpc.StreamClientInterceptor {
	cfg := newConfig(opts)
	tracer := cfg.TracerProvider.Tracer(
		ScopeName,
		trace.WithInstrumentationVersion(Version()),
	)

	return func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		callOpts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		i := &InterceptorInfo{
			Method: method,
			Type:   StreamClient,
		}
		if cfg.InterceptorFilter != nil && !cfg.InterceptorFilter(i) {
			return streamer(ctx, desc, cc, method, callOpts...)
		}

		name, attr := telemetryAttributes(method, cc.Target())

		startOpts := append([]trace.SpanStartOption{
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(attr...),
		},
			cfg.SpanStartOptions...,
		)

		ctx, span := tracer.Start(
			ctx,
			name,
			startOpts...,
		)

		ctx = inject(ctx, cfg.Propagators)

		s, err := streamer(ctx, desc, cc, method, callOpts...)
		if err != nil {
			grpcStatus, _ := status.FromError(err)
			span.SetStatus(codes.Error, grpcStatus.Message())
			span.SetAttributes(statusCodeAttr(grpcStatus.Code()))
			span.End()
			return s, err
		}
		stream := wrapClientStream(s, desc, span, cfg)
		return stream, nil
	}
}

// telemetryAttributes returns a span name and span and metric attributes from
// the gRPC method and peer address.
func telemetryAttributes(fullMethod, serverAddr string) (string, []attribute.KeyValue) {
	name, methodAttrs := internal.ParseFullMethod(fullMethod)
	srvAttrs := serverAddrAttrs(serverAddr)

	attrs := make([]attribute.KeyValue, 0, 1+len(methodAttrs)+len(srvAttrs))
	attrs = append(attrs, semconv.RPCSystemGRPC)
	attrs = append(attrs, methodAttrs...)
	attrs = append(attrs, srvAttrs...)
	return name, attrs
}

// serverAddrAttrs returns the server address attributes for the hostport.
func serverAddrAttrs(hostport string) []attribute.KeyValue {
	h, pStr, err := net.SplitHostPort(hostport)
	if err != nil {
		// The server.address attribute is required.
		return []attribute.KeyValue{semconv.ServerAddress(hostport)}
	}
	p, err := strconv.Atoi(pStr)
	if err != nil {
		return []attribute.KeyValue{semconv.ServerAddress(h)}
	}
	return []attribute.KeyValue{
		semconv.ServerAddress(h),
		semconv.ServerPort(p),
	}
}

// statusCodeAttr returns status code attribute based on given gRPC code.
func statusCodeAttr(c grpc_codes.Code) attribute.KeyValue {
	return semconv.RPCGRPCStatusCodeKey.Int64(int64(c))
}

// serverStatus returns a span status code and message for a given gRPC
// status code. It maps specific gRPC status codes to a corresponding span
// status code and message. This function is intended for use on the server
// side of a gRPC connection.
//
// If the gRPC status code is Unknown, DeadlineExceeded, Unimplemented,
// Internal, Unavailable, or DataLoss, it returns a span status code of Error
// and the message from the gRPC status. Otherwise, it returns a span status
// code of Unset and an empty message.
func serverStatus(grpcStatus *status.Status) (codes.Code, string) {
	switch grpcStatus.Code() {
	case grpc_codes.Unknown,
		grpc_codes.DeadlineExceeded,
		grpc_codes.Unimplemented,
		grpc_codes.Internal,
		grpc_codes.Unavailable,
		grpc_codes.DataLoss:
		return codes.Error, grpcStatus.Message()
	default:
		return codes.Unset, ""
	}
}
