// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package grpcencoding // import "go.opentelemetry.io/collector/pdata/internal/grpcencoding"

import (
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/mem"

	"go.opentelemetry.io/collector/pdata/internal"
	otlpcollectorlogs "go.opentelemetry.io/collector/pdata/internal/data/protogen/collector/logs/v1"
	otlpcollectormetrics "go.opentelemetry.io/collector/pdata/internal/data/protogen/collector/metrics/v1"
	otlpcollectorprofile "go.opentelemetry.io/collector/pdata/internal/data/protogen/collector/profiles/v1development"
	otlpcollectortraces "go.opentelemetry.io/collector/pdata/internal/data/protogen/collector/trace/v1"
)

var (
	defaultBufferPoolSizes = []int{
		256,
		4 << 10,   // 4KB (go page size)
		16 << 10,  // 16KB (max HTTP/2 frame size used by gRPC)
		32 << 10,  // 32KB (default buffer size for io.Copy)
		512 << 10, // 512KB
		1 << 20,   // 1MB
		4 << 20,   // 4MB
		16 << 20,  // 16MB
	}
	otelBufferPool = mem.NewTieredBufferPool(defaultBufferPoolSizes...)
)

// DefaultBufferPool returns the current default buffer pool. It is a BufferPool
// created with mem.NewTieredBufferPool that uses a set of default sizes optimized for
// expected telemetry workflows.
func DefaultBufferPool() mem.BufferPool {
	return otelBufferPool
}

// Name is the name registered for the proto compressor.
const Name = "proto"

func init() {
	encoding.RegisterCodecV2(&codecV2{delegate: encoding.GetCodecV2(Name)})
}

// codecV2 is a custom proto encoding that uses a different tier schema for the TieredBufferPool as well
// as it call into the custom marshal/unmarshal logic that works with memory pooling.
// If not an otlp payload fallback on the default grpc/proto encoding.
type codecV2 struct {
	delegate encoding.CodecV2
}

func (c *codecV2) Marshal(v any) (mem.BufferSlice, error) {
	switch req := v.(type) {
	case *otlpcollectorlogs.ExportLogsServiceRequest:
		size := internal.SizeProtoOrigExportLogsServiceRequest(req)
		buf := otelBufferPool.Get(size)
		n := internal.MarshalProtoOrigExportLogsServiceRequest(req, (*buf)[:size])
		*buf = (*buf)[:n]
		return []mem.Buffer{mem.NewBuffer(buf, otelBufferPool)}, nil
	case *otlpcollectormetrics.ExportMetricsServiceRequest:
		size := internal.SizeProtoOrigExportMetricsServiceRequest(req)
		buf := otelBufferPool.Get(size)
		n := internal.MarshalProtoOrigExportMetricsServiceRequest(req, (*buf)[:size])
		*buf = (*buf)[:n]
		return []mem.Buffer{mem.NewBuffer(buf, otelBufferPool)}, nil
	case *otlpcollectortraces.ExportTraceServiceRequest:
		size := internal.SizeProtoOrigExportTraceServiceRequest(req)
		buf := otelBufferPool.Get(size)
		n := internal.MarshalProtoOrigExportTraceServiceRequest(req, (*buf)[:size])
		*buf = (*buf)[:n]
		return []mem.Buffer{mem.NewBuffer(buf, otelBufferPool)}, nil
	case *otlpcollectorprofile.ExportProfilesServiceRequest:
		size := internal.SizeProtoOrigExportProfilesServiceRequest(req)
		buf := otelBufferPool.Get(size)
		n := internal.MarshalProtoOrigExportProfilesServiceRequest(req, (*buf)[:size])
		*buf = (*buf)[:n]
		return []mem.Buffer{mem.NewBuffer(buf, otelBufferPool)}, nil
	}

	return c.delegate.Marshal(v)
}

func (c *codecV2) Unmarshal(data mem.BufferSlice, v any) (err error) {
	switch req := v.(type) {
	case *otlpcollectorlogs.ExportLogsServiceRequest:
		// TODO: Upgrade custom Unmarshal logic to support reading from mem.BufferSlice.
		buf := data.MaterializeToBuffer(otelBufferPool)
		defer buf.Free()
		return internal.UnmarshalProtoOrigExportLogsServiceRequest(req, buf.ReadOnlyData())
	case *otlpcollectormetrics.ExportMetricsServiceRequest:
		// TODO: Upgrade custom Unmarshal logic to support reading from mem.BufferSlice.
		buf := data.MaterializeToBuffer(otelBufferPool)
		defer buf.Free()
		return internal.UnmarshalProtoOrigExportMetricsServiceRequest(req, buf.ReadOnlyData())
	case *otlpcollectortraces.ExportTraceServiceRequest:
		// TODO: Upgrade custom Unmarshal logic to support reading from mem.BufferSlice.
		buf := data.MaterializeToBuffer(otelBufferPool)
		defer buf.Free()
		return internal.UnmarshalProtoOrigExportTraceServiceRequest(req, buf.ReadOnlyData())
	case *otlpcollectorprofile.ExportProfilesServiceRequest:
		// TODO: Upgrade custom Unmarshal logic to support reading from mem.BufferSlice.
		buf := data.MaterializeToBuffer(otelBufferPool)
		defer buf.Free()
		return internal.UnmarshalProtoOrigExportProfilesServiceRequest(req, buf.ReadOnlyData())
	}

	return c.delegate.Unmarshal(data, v)
}

func (c *codecV2) Name() string {
	return Name
}
