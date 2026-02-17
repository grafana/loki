// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otelgrpc // import "go.opentelemetry.io/collector/pdata/internal/otelgrpc"

import (
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/mem"
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

type otelEncoder interface {
	SizeProto() int
	MarshalProto([]byte) int
	UnmarshalProto([]byte) error
}

func (c *codecV2) Marshal(v any) (mem.BufferSlice, error) {
	if m, ok := v.(otelEncoder); ok {
		size := m.SizeProto()
		buf := otelBufferPool.Get(size)
		n := m.MarshalProto((*buf)[:size])
		*buf = (*buf)[:n]
		return []mem.Buffer{mem.NewBuffer(buf, otelBufferPool)}, nil
	}

	return c.delegate.Marshal(v)
}

func (c *codecV2) Unmarshal(data mem.BufferSlice, v any) (err error) {
	if m, ok := v.(otelEncoder); ok {
		// TODO: Upgrade custom Unmarshal logic to support reading from mem.BufferSlice.
		buf := data.MaterializeToBuffer(otelBufferPool)
		defer buf.Free()
		return m.UnmarshalProto(buf.ReadOnlyData())
	}

	return c.delegate.Unmarshal(data, v)
}

func (c *codecV2) Name() string {
	return Name
}
