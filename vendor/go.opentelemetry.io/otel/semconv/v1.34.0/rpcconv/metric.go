// Code generated from semantic convention specification. DO NOT EDIT.

// Package httpconv provides types and functionality for OpenTelemetry semantic
// conventions in the "rpc" namespace.
package rpcconv

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

var (
	addOptPool = &sync.Pool{New: func() any { return &[]metric.AddOption{} }}
	recOptPool = &sync.Pool{New: func() any { return &[]metric.RecordOption{} }}
)

// ClientDuration is an instrument used to record metric values conforming to the
// "rpc.client.duration" semantic conventions. It represents the measures the
// duration of outbound RPC.
type ClientDuration struct {
	metric.Float64Histogram
}

// NewClientDuration returns a new ClientDuration instrument.
func NewClientDuration(
	m metric.Meter,
	opt ...metric.Float64HistogramOption,
) (ClientDuration, error) {
	// Check if the meter is nil.
	if m == nil {
		return ClientDuration{noop.Float64Histogram{}}, nil
	}

	i, err := m.Float64Histogram(
		"rpc.client.duration",
		append([]metric.Float64HistogramOption{
			metric.WithDescription("Measures the duration of outbound RPC."),
			metric.WithUnit("ms"),
		}, opt...)...,
	)
	if err != nil {
	    return ClientDuration{noop.Float64Histogram{}}, err
	}
	return ClientDuration{i}, nil
}

// Inst returns the underlying metric instrument.
func (m ClientDuration) Inst() metric.Float64Histogram {
	return m.Float64Histogram
}

// Name returns the semantic convention name of the instrument.
func (ClientDuration) Name() string {
	return "rpc.client.duration"
}

// Unit returns the semantic convention unit of the instrument
func (ClientDuration) Unit() string {
	return "ms"
}

// Description returns the semantic convention description of the instrument
func (ClientDuration) Description() string {
	return "Measures the duration of outbound RPC."
}

// Record records val to the current distribution.
//
// While streaming RPCs may record this metric as start-of-batch
// to end-of-batch, it's hard to interpret in practice.
//
// **Streaming**: N/A.
func (m ClientDuration) Record(ctx context.Context, val float64, attrs ...attribute.KeyValue) {
	if len(attrs) == 0 {
		m.Float64Histogram.Record(ctx, val)
	}

	o := recOptPool.Get().(*[]metric.RecordOption)
	defer func() {
		*o = (*o)[:0]
		recOptPool.Put(o)
	}()

	*o = append(*o, metric.WithAttributes(attrs...))
	m.Float64Histogram.Record(ctx, val, *o...)
}

// ClientRequestSize is an instrument used to record metric values conforming to
// the "rpc.client.request.size" semantic conventions. It represents the measures
// the size of RPC request messages (uncompressed).
type ClientRequestSize struct {
	metric.Int64Histogram
}

// NewClientRequestSize returns a new ClientRequestSize instrument.
func NewClientRequestSize(
	m metric.Meter,
	opt ...metric.Int64HistogramOption,
) (ClientRequestSize, error) {
	// Check if the meter is nil.
	if m == nil {
		return ClientRequestSize{noop.Int64Histogram{}}, nil
	}

	i, err := m.Int64Histogram(
		"rpc.client.request.size",
		append([]metric.Int64HistogramOption{
			metric.WithDescription("Measures the size of RPC request messages (uncompressed)."),
			metric.WithUnit("By"),
		}, opt...)...,
	)
	if err != nil {
	    return ClientRequestSize{noop.Int64Histogram{}}, err
	}
	return ClientRequestSize{i}, nil
}

// Inst returns the underlying metric instrument.
func (m ClientRequestSize) Inst() metric.Int64Histogram {
	return m.Int64Histogram
}

// Name returns the semantic convention name of the instrument.
func (ClientRequestSize) Name() string {
	return "rpc.client.request.size"
}

// Unit returns the semantic convention unit of the instrument
func (ClientRequestSize) Unit() string {
	return "By"
}

// Description returns the semantic convention description of the instrument
func (ClientRequestSize) Description() string {
	return "Measures the size of RPC request messages (uncompressed)."
}

// Record records val to the current distribution.
//
// **Streaming**: Recorded per message in a streaming batch
func (m ClientRequestSize) Record(ctx context.Context, val int64, attrs ...attribute.KeyValue) {
	if len(attrs) == 0 {
		m.Int64Histogram.Record(ctx, val)
	}

	o := recOptPool.Get().(*[]metric.RecordOption)
	defer func() {
		*o = (*o)[:0]
		recOptPool.Put(o)
	}()

	*o = append(*o, metric.WithAttributes(attrs...))
	m.Int64Histogram.Record(ctx, val, *o...)
}

// ClientRequestsPerRPC is an instrument used to record metric values conforming
// to the "rpc.client.requests_per_rpc" semantic conventions. It represents the
// measures the number of messages received per RPC.
type ClientRequestsPerRPC struct {
	metric.Int64Histogram
}

// NewClientRequestsPerRPC returns a new ClientRequestsPerRPC instrument.
func NewClientRequestsPerRPC(
	m metric.Meter,
	opt ...metric.Int64HistogramOption,
) (ClientRequestsPerRPC, error) {
	// Check if the meter is nil.
	if m == nil {
		return ClientRequestsPerRPC{noop.Int64Histogram{}}, nil
	}

	i, err := m.Int64Histogram(
		"rpc.client.requests_per_rpc",
		append([]metric.Int64HistogramOption{
			metric.WithDescription("Measures the number of messages received per RPC."),
			metric.WithUnit("{count}"),
		}, opt...)...,
	)
	if err != nil {
	    return ClientRequestsPerRPC{noop.Int64Histogram{}}, err
	}
	return ClientRequestsPerRPC{i}, nil
}

// Inst returns the underlying metric instrument.
func (m ClientRequestsPerRPC) Inst() metric.Int64Histogram {
	return m.Int64Histogram
}

// Name returns the semantic convention name of the instrument.
func (ClientRequestsPerRPC) Name() string {
	return "rpc.client.requests_per_rpc"
}

// Unit returns the semantic convention unit of the instrument
func (ClientRequestsPerRPC) Unit() string {
	return "{count}"
}

// Description returns the semantic convention description of the instrument
func (ClientRequestsPerRPC) Description() string {
	return "Measures the number of messages received per RPC."
}

// Record records val to the current distribution.
//
// Should be 1 for all non-streaming RPCs.
//
// **Streaming**: This metric is required for server and client streaming RPCs
func (m ClientRequestsPerRPC) Record(ctx context.Context, val int64, attrs ...attribute.KeyValue) {
	if len(attrs) == 0 {
		m.Int64Histogram.Record(ctx, val)
	}

	o := recOptPool.Get().(*[]metric.RecordOption)
	defer func() {
		*o = (*o)[:0]
		recOptPool.Put(o)
	}()

	*o = append(*o, metric.WithAttributes(attrs...))
	m.Int64Histogram.Record(ctx, val, *o...)
}

// ClientResponseSize is an instrument used to record metric values conforming to
// the "rpc.client.response.size" semantic conventions. It represents the
// measures the size of RPC response messages (uncompressed).
type ClientResponseSize struct {
	metric.Int64Histogram
}

// NewClientResponseSize returns a new ClientResponseSize instrument.
func NewClientResponseSize(
	m metric.Meter,
	opt ...metric.Int64HistogramOption,
) (ClientResponseSize, error) {
	// Check if the meter is nil.
	if m == nil {
		return ClientResponseSize{noop.Int64Histogram{}}, nil
	}

	i, err := m.Int64Histogram(
		"rpc.client.response.size",
		append([]metric.Int64HistogramOption{
			metric.WithDescription("Measures the size of RPC response messages (uncompressed)."),
			metric.WithUnit("By"),
		}, opt...)...,
	)
	if err != nil {
	    return ClientResponseSize{noop.Int64Histogram{}}, err
	}
	return ClientResponseSize{i}, nil
}

// Inst returns the underlying metric instrument.
func (m ClientResponseSize) Inst() metric.Int64Histogram {
	return m.Int64Histogram
}

// Name returns the semantic convention name of the instrument.
func (ClientResponseSize) Name() string {
	return "rpc.client.response.size"
}

// Unit returns the semantic convention unit of the instrument
func (ClientResponseSize) Unit() string {
	return "By"
}

// Description returns the semantic convention description of the instrument
func (ClientResponseSize) Description() string {
	return "Measures the size of RPC response messages (uncompressed)."
}

// Record records val to the current distribution.
//
// **Streaming**: Recorded per response in a streaming batch
func (m ClientResponseSize) Record(ctx context.Context, val int64, attrs ...attribute.KeyValue) {
	if len(attrs) == 0 {
		m.Int64Histogram.Record(ctx, val)
	}

	o := recOptPool.Get().(*[]metric.RecordOption)
	defer func() {
		*o = (*o)[:0]
		recOptPool.Put(o)
	}()

	*o = append(*o, metric.WithAttributes(attrs...))
	m.Int64Histogram.Record(ctx, val, *o...)
}

// ClientResponsesPerRPC is an instrument used to record metric values conforming
// to the "rpc.client.responses_per_rpc" semantic conventions. It represents the
// measures the number of messages sent per RPC.
type ClientResponsesPerRPC struct {
	metric.Int64Histogram
}

// NewClientResponsesPerRPC returns a new ClientResponsesPerRPC instrument.
func NewClientResponsesPerRPC(
	m metric.Meter,
	opt ...metric.Int64HistogramOption,
) (ClientResponsesPerRPC, error) {
	// Check if the meter is nil.
	if m == nil {
		return ClientResponsesPerRPC{noop.Int64Histogram{}}, nil
	}

	i, err := m.Int64Histogram(
		"rpc.client.responses_per_rpc",
		append([]metric.Int64HistogramOption{
			metric.WithDescription("Measures the number of messages sent per RPC."),
			metric.WithUnit("{count}"),
		}, opt...)...,
	)
	if err != nil {
	    return ClientResponsesPerRPC{noop.Int64Histogram{}}, err
	}
	return ClientResponsesPerRPC{i}, nil
}

// Inst returns the underlying metric instrument.
func (m ClientResponsesPerRPC) Inst() metric.Int64Histogram {
	return m.Int64Histogram
}

// Name returns the semantic convention name of the instrument.
func (ClientResponsesPerRPC) Name() string {
	return "rpc.client.responses_per_rpc"
}

// Unit returns the semantic convention unit of the instrument
func (ClientResponsesPerRPC) Unit() string {
	return "{count}"
}

// Description returns the semantic convention description of the instrument
func (ClientResponsesPerRPC) Description() string {
	return "Measures the number of messages sent per RPC."
}

// Record records val to the current distribution.
//
// Should be 1 for all non-streaming RPCs.
//
// **Streaming**: This metric is required for server and client streaming RPCs
func (m ClientResponsesPerRPC) Record(ctx context.Context, val int64, attrs ...attribute.KeyValue) {
	if len(attrs) == 0 {
		m.Int64Histogram.Record(ctx, val)
	}

	o := recOptPool.Get().(*[]metric.RecordOption)
	defer func() {
		*o = (*o)[:0]
		recOptPool.Put(o)
	}()

	*o = append(*o, metric.WithAttributes(attrs...))
	m.Int64Histogram.Record(ctx, val, *o...)
}

// ServerDuration is an instrument used to record metric values conforming to the
// "rpc.server.duration" semantic conventions. It represents the measures the
// duration of inbound RPC.
type ServerDuration struct {
	metric.Float64Histogram
}

// NewServerDuration returns a new ServerDuration instrument.
func NewServerDuration(
	m metric.Meter,
	opt ...metric.Float64HistogramOption,
) (ServerDuration, error) {
	// Check if the meter is nil.
	if m == nil {
		return ServerDuration{noop.Float64Histogram{}}, nil
	}

	i, err := m.Float64Histogram(
		"rpc.server.duration",
		append([]metric.Float64HistogramOption{
			metric.WithDescription("Measures the duration of inbound RPC."),
			metric.WithUnit("ms"),
		}, opt...)...,
	)
	if err != nil {
	    return ServerDuration{noop.Float64Histogram{}}, err
	}
	return ServerDuration{i}, nil
}

// Inst returns the underlying metric instrument.
func (m ServerDuration) Inst() metric.Float64Histogram {
	return m.Float64Histogram
}

// Name returns the semantic convention name of the instrument.
func (ServerDuration) Name() string {
	return "rpc.server.duration"
}

// Unit returns the semantic convention unit of the instrument
func (ServerDuration) Unit() string {
	return "ms"
}

// Description returns the semantic convention description of the instrument
func (ServerDuration) Description() string {
	return "Measures the duration of inbound RPC."
}

// Record records val to the current distribution.
//
// While streaming RPCs may record this metric as start-of-batch
// to end-of-batch, it's hard to interpret in practice.
//
// **Streaming**: N/A.
func (m ServerDuration) Record(ctx context.Context, val float64, attrs ...attribute.KeyValue) {
	if len(attrs) == 0 {
		m.Float64Histogram.Record(ctx, val)
	}

	o := recOptPool.Get().(*[]metric.RecordOption)
	defer func() {
		*o = (*o)[:0]
		recOptPool.Put(o)
	}()

	*o = append(*o, metric.WithAttributes(attrs...))
	m.Float64Histogram.Record(ctx, val, *o...)
}

// ServerRequestSize is an instrument used to record metric values conforming to
// the "rpc.server.request.size" semantic conventions. It represents the measures
// the size of RPC request messages (uncompressed).
type ServerRequestSize struct {
	metric.Int64Histogram
}

// NewServerRequestSize returns a new ServerRequestSize instrument.
func NewServerRequestSize(
	m metric.Meter,
	opt ...metric.Int64HistogramOption,
) (ServerRequestSize, error) {
	// Check if the meter is nil.
	if m == nil {
		return ServerRequestSize{noop.Int64Histogram{}}, nil
	}

	i, err := m.Int64Histogram(
		"rpc.server.request.size",
		append([]metric.Int64HistogramOption{
			metric.WithDescription("Measures the size of RPC request messages (uncompressed)."),
			metric.WithUnit("By"),
		}, opt...)...,
	)
	if err != nil {
	    return ServerRequestSize{noop.Int64Histogram{}}, err
	}
	return ServerRequestSize{i}, nil
}

// Inst returns the underlying metric instrument.
func (m ServerRequestSize) Inst() metric.Int64Histogram {
	return m.Int64Histogram
}

// Name returns the semantic convention name of the instrument.
func (ServerRequestSize) Name() string {
	return "rpc.server.request.size"
}

// Unit returns the semantic convention unit of the instrument
func (ServerRequestSize) Unit() string {
	return "By"
}

// Description returns the semantic convention description of the instrument
func (ServerRequestSize) Description() string {
	return "Measures the size of RPC request messages (uncompressed)."
}

// Record records val to the current distribution.
//
// **Streaming**: Recorded per message in a streaming batch
func (m ServerRequestSize) Record(ctx context.Context, val int64, attrs ...attribute.KeyValue) {
	if len(attrs) == 0 {
		m.Int64Histogram.Record(ctx, val)
	}

	o := recOptPool.Get().(*[]metric.RecordOption)
	defer func() {
		*o = (*o)[:0]
		recOptPool.Put(o)
	}()

	*o = append(*o, metric.WithAttributes(attrs...))
	m.Int64Histogram.Record(ctx, val, *o...)
}

// ServerRequestsPerRPC is an instrument used to record metric values conforming
// to the "rpc.server.requests_per_rpc" semantic conventions. It represents the
// measures the number of messages received per RPC.
type ServerRequestsPerRPC struct {
	metric.Int64Histogram
}

// NewServerRequestsPerRPC returns a new ServerRequestsPerRPC instrument.
func NewServerRequestsPerRPC(
	m metric.Meter,
	opt ...metric.Int64HistogramOption,
) (ServerRequestsPerRPC, error) {
	// Check if the meter is nil.
	if m == nil {
		return ServerRequestsPerRPC{noop.Int64Histogram{}}, nil
	}

	i, err := m.Int64Histogram(
		"rpc.server.requests_per_rpc",
		append([]metric.Int64HistogramOption{
			metric.WithDescription("Measures the number of messages received per RPC."),
			metric.WithUnit("{count}"),
		}, opt...)...,
	)
	if err != nil {
	    return ServerRequestsPerRPC{noop.Int64Histogram{}}, err
	}
	return ServerRequestsPerRPC{i}, nil
}

// Inst returns the underlying metric instrument.
func (m ServerRequestsPerRPC) Inst() metric.Int64Histogram {
	return m.Int64Histogram
}

// Name returns the semantic convention name of the instrument.
func (ServerRequestsPerRPC) Name() string {
	return "rpc.server.requests_per_rpc"
}

// Unit returns the semantic convention unit of the instrument
func (ServerRequestsPerRPC) Unit() string {
	return "{count}"
}

// Description returns the semantic convention description of the instrument
func (ServerRequestsPerRPC) Description() string {
	return "Measures the number of messages received per RPC."
}

// Record records val to the current distribution.
//
// Should be 1 for all non-streaming RPCs.
//
// **Streaming** : This metric is required for server and client streaming RPCs
func (m ServerRequestsPerRPC) Record(ctx context.Context, val int64, attrs ...attribute.KeyValue) {
	if len(attrs) == 0 {
		m.Int64Histogram.Record(ctx, val)
	}

	o := recOptPool.Get().(*[]metric.RecordOption)
	defer func() {
		*o = (*o)[:0]
		recOptPool.Put(o)
	}()

	*o = append(*o, metric.WithAttributes(attrs...))
	m.Int64Histogram.Record(ctx, val, *o...)
}

// ServerResponseSize is an instrument used to record metric values conforming to
// the "rpc.server.response.size" semantic conventions. It represents the
// measures the size of RPC response messages (uncompressed).
type ServerResponseSize struct {
	metric.Int64Histogram
}

// NewServerResponseSize returns a new ServerResponseSize instrument.
func NewServerResponseSize(
	m metric.Meter,
	opt ...metric.Int64HistogramOption,
) (ServerResponseSize, error) {
	// Check if the meter is nil.
	if m == nil {
		return ServerResponseSize{noop.Int64Histogram{}}, nil
	}

	i, err := m.Int64Histogram(
		"rpc.server.response.size",
		append([]metric.Int64HistogramOption{
			metric.WithDescription("Measures the size of RPC response messages (uncompressed)."),
			metric.WithUnit("By"),
		}, opt...)...,
	)
	if err != nil {
	    return ServerResponseSize{noop.Int64Histogram{}}, err
	}
	return ServerResponseSize{i}, nil
}

// Inst returns the underlying metric instrument.
func (m ServerResponseSize) Inst() metric.Int64Histogram {
	return m.Int64Histogram
}

// Name returns the semantic convention name of the instrument.
func (ServerResponseSize) Name() string {
	return "rpc.server.response.size"
}

// Unit returns the semantic convention unit of the instrument
func (ServerResponseSize) Unit() string {
	return "By"
}

// Description returns the semantic convention description of the instrument
func (ServerResponseSize) Description() string {
	return "Measures the size of RPC response messages (uncompressed)."
}

// Record records val to the current distribution.
//
// **Streaming**: Recorded per response in a streaming batch
func (m ServerResponseSize) Record(ctx context.Context, val int64, attrs ...attribute.KeyValue) {
	if len(attrs) == 0 {
		m.Int64Histogram.Record(ctx, val)
	}

	o := recOptPool.Get().(*[]metric.RecordOption)
	defer func() {
		*o = (*o)[:0]
		recOptPool.Put(o)
	}()

	*o = append(*o, metric.WithAttributes(attrs...))
	m.Int64Histogram.Record(ctx, val, *o...)
}

// ServerResponsesPerRPC is an instrument used to record metric values conforming
// to the "rpc.server.responses_per_rpc" semantic conventions. It represents the
// measures the number of messages sent per RPC.
type ServerResponsesPerRPC struct {
	metric.Int64Histogram
}

// NewServerResponsesPerRPC returns a new ServerResponsesPerRPC instrument.
func NewServerResponsesPerRPC(
	m metric.Meter,
	opt ...metric.Int64HistogramOption,
) (ServerResponsesPerRPC, error) {
	// Check if the meter is nil.
	if m == nil {
		return ServerResponsesPerRPC{noop.Int64Histogram{}}, nil
	}

	i, err := m.Int64Histogram(
		"rpc.server.responses_per_rpc",
		append([]metric.Int64HistogramOption{
			metric.WithDescription("Measures the number of messages sent per RPC."),
			metric.WithUnit("{count}"),
		}, opt...)...,
	)
	if err != nil {
	    return ServerResponsesPerRPC{noop.Int64Histogram{}}, err
	}
	return ServerResponsesPerRPC{i}, nil
}

// Inst returns the underlying metric instrument.
func (m ServerResponsesPerRPC) Inst() metric.Int64Histogram {
	return m.Int64Histogram
}

// Name returns the semantic convention name of the instrument.
func (ServerResponsesPerRPC) Name() string {
	return "rpc.server.responses_per_rpc"
}

// Unit returns the semantic convention unit of the instrument
func (ServerResponsesPerRPC) Unit() string {
	return "{count}"
}

// Description returns the semantic convention description of the instrument
func (ServerResponsesPerRPC) Description() string {
	return "Measures the number of messages sent per RPC."
}

// Record records val to the current distribution.
//
// Should be 1 for all non-streaming RPCs.
//
// **Streaming**: This metric is required for server and client streaming RPCs
func (m ServerResponsesPerRPC) Record(ctx context.Context, val int64, attrs ...attribute.KeyValue) {
	if len(attrs) == 0 {
		m.Int64Histogram.Record(ctx, val)
	}

	o := recOptPool.Get().(*[]metric.RecordOption)
	defer func() {
		*o = (*o)[:0]
		recOptPool.Put(o)
	}()

	*o = append(*o, metric.WithAttributes(attrs...))
	m.Int64Histogram.Record(ctx, val, *o...)
}