package columnar

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/memory"
)

// Broadcast returns an Array containing length copies of scalar.
// Broadcast panics if length is negative.
func Broadcast(alloc *memory.Allocator, scalar Scalar, length int) Array {
	if length < 0 {
		panic("length must be non-negative")
	}

	switch scalar := scalar.(type) {
	case *NullScalar:
		builder := NewNullBuilder(alloc)
		builder.AppendNulls(length)
		return builder.Build()

	case *BoolScalar:
		builder := NewBoolBuilder(alloc)
		if scalar.Null {
			builder.AppendNulls(length)
		} else {
			builder.AppendValueCount(scalar.Value, length)
		}
		return builder.Build()

	case *NumberScalar[int32]:
		return makeNumberArrayFromScalar(alloc, scalar, length)
	case *NumberScalar[int64]:
		return makeNumberArrayFromScalar(alloc, scalar, length)
	case *NumberScalar[uint32]:
		return makeNumberArrayFromScalar(alloc, scalar, length)
	case *NumberScalar[uint64]:
		return makeNumberArrayFromScalar(alloc, scalar, length)

	case *UTF8Scalar:
		builder := NewUTF8Builder(alloc)
		if scalar.Null {
			builder.AppendNulls(length)
		} else {
			builder.Grow(length)
			builder.GrowData(len(scalar.Value) * length)
			for range length {
				builder.AppendValue(scalar.Value)
			}
		}
		return builder.Build()

	default:
		panic(fmt.Sprintf("unexpected scalar type %T", scalar))
	}
}

func makeNumberArrayFromScalar[T Numeric](alloc *memory.Allocator, scalar *NumberScalar[T], length int) *Number[T] {
	builder := NewNumberBuilder[T](alloc)
	if scalar.Null {
		builder.AppendNulls(length)
	} else {
		builder.AppendValueCount(scalar.Value, length)
	}
	return builder.Build()
}
