// Package arrowconv provides helper utilities for converting between Arrow and
// dataset values.
package arrowconv

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

// DatasetType returns the [datasetmd.ValueType] that corresponds to the given
// Arrow type.
//
// - [arrow.INT64] maps to [datasetmd.PHYSICAL_TYPE_INT64].
// - [arrow.UINT64] maps to [datasetmd.PHYSICAL_TYPE_UINT64].
// - [arrow.TIMESTAMP] maps to [datasetmd.PHYSICAL_TYPE_INT64].
// - [arrow.STRING] maps to [datasetmd.PHYSICAL_TYPE_BINARY].
// - [arrow.BINARY] maps to [datasetmd.PHYSICAL_TYPE_BINARY].
//
// DatasetType returns [datasetmd.PHYSICAL_TYPE_UNSPECIFIED], false for
// unsupported Arrow types.
func DatasetType(arrowType arrow.DataType) (datasetmd.PhysicalType, bool) {
	switch arrowType.ID() {
	case arrow.NULL:
		return datasetmd.PHYSICAL_TYPE_UNSPECIFIED, true
	case arrow.INT64:
		return datasetmd.PHYSICAL_TYPE_INT64, true
	case arrow.UINT64:
		return datasetmd.PHYSICAL_TYPE_UINT64, true
	case arrow.TIMESTAMP:
		return datasetmd.PHYSICAL_TYPE_INT64, true
	case arrow.STRING:
		return datasetmd.PHYSICAL_TYPE_BINARY, true
	case arrow.BINARY:
		return datasetmd.PHYSICAL_TYPE_BINARY, true
	}

	return datasetmd.PHYSICAL_TYPE_UNSPECIFIED, false
}

// FromScalar converts a [scalar.Scalar] into a [dataset.Value] of the
// specified type.
//
// The kind of toType and the type of s must be compatible:
//
// - For [datasetmd.PHYSICAL_TYPE_INT64], s must be a [scalar.Int64] or [scalar.Timestamp].
// - For [datasetmd.PHYSICAL_TYPE_UINT64], s must be a [scalar.Uint64].
// - For [datasetmd.PHYSICAL_TYPE_BINARY], s must be a [scalar.Binary] or [scalar.String].
//
// If s references allocated memory, FromScalar will hold a reference to that
// memory. Callers are responsible for releasing the scalar after the returned
// dataset.Value is no longer used.
//
// If s is a null type, it will return a nil [dataset.Value].
func FromScalar(s scalar.Scalar, toType datasetmd.PhysicalType) dataset.Value {
	// IsValid returns false when the scalar is a null value.
	if !s.IsValid() {
		return dataset.Value{}
	}

	switch toType {
	case datasetmd.PHYSICAL_TYPE_UNSPECIFIED:
		return dataset.Value{}

	case datasetmd.PHYSICAL_TYPE_INT64:
		switch s := s.(type) {
		case *scalar.Int64:
			return dataset.Int64Value(s.Value)
		case *scalar.Timestamp:
			return dataset.Int64Value(int64(s.Value))
		default:
			panic(fmt.Sprintf("arrowconv.FromScalar: invalid conversion to INT64; got %T, want *scalar.Int64 or *scalar.Timestamp", s))
		}

	case datasetmd.PHYSICAL_TYPE_UINT64:
		s, ok := s.(*scalar.Uint64)
		if !ok {
			panic(fmt.Sprintf("arrowconv.FromScalar: invalid conversion to UINT64; got %T, want *scalar.Uint64", s))
		}
		return dataset.Uint64Value(s.Value)

	case datasetmd.PHYSICAL_TYPE_BINARY:
		switch s := s.(type) {
		case *scalar.String:
			s.Retain()
			return dataset.BinaryValue(s.Value.Bytes())
		case *scalar.Binary:
			s.Retain()
			return dataset.BinaryValue(s.Value.Bytes())
		default:
			panic(fmt.Sprintf("arrowconv.FromScalar: invalid conversion to BYTE_ARRAY; got %T, want *scalar.Binary", s))
		}

	default:
		panic(fmt.Sprintf("arrowconv.FromScalar: unsupported conversion to dataset.Value type %s", toType))
	}
}

// ToScalar converts a [dataset.Value] into a [scalar.Scalar] of the specified
// type.
//
// The kind of toType and the type of v and toType must be compatible:
//
//   - For [arrow.INT64], v must be a [datasetmd.PHYSICAL_TYPE_INT64].
//   - For [arrow.UINT64], v must be a [datasetmd.PHYSICAL_TYPE_UINT64].
//   - For [arrow.TIMESTAMP], v must be a [datasetmd.PHYSICAL_TYPE_INT64], which
//     will be converted into a nanosecond timestamp.
//   - For [arrow.STRING], v must be a [datasetmd.PHYSICAL_TYPE_BINARY].
//   - For [arrow.BINARY], v must be a [datasetmd.PHYSICAL_TYPE_BINARY].
//
// If v is nil, ToScalar returns a null scalar of the specified type. If toType
// is a Null type, then ToScalar returns a null scalar even if v is non-null.
//
// ToScalar panics if v and toType are not compatible.
func ToScalar(v dataset.Value, toType arrow.DataType) scalar.Scalar {
	if v.IsNil() {
		return scalar.MakeNullScalar(toType)
	}

	switch toType.ID() {
	case arrow.NULL:
		return scalar.MakeNullScalar(toType)

	case arrow.INT64:
		if got, want := v.Type(), datasetmd.PHYSICAL_TYPE_INT64; got != want {
			panic(fmt.Sprintf("arrowconv.ToScalar: invalid conversion to INT64; got %s, want %s", got, want))
		}
		return scalar.NewInt64Scalar(v.Int64())

	case arrow.UINT64:
		if got, want := v.Type(), datasetmd.PHYSICAL_TYPE_UINT64; got != want {
			panic(fmt.Sprintf("arrowconv.ToScalar: invalid conversion to UINT64; got %s, want %s", got, want))
		}
		return scalar.NewUint64Scalar(v.Uint64())

	case arrow.TIMESTAMP:
		if got, want := v.Type(), datasetmd.PHYSICAL_TYPE_INT64; got != want {
			panic(fmt.Sprintf("arrowconv.ToScalar: invalid conversion to TIMESTAMP; got %s, want %s", got, want))
		}
		return scalar.NewTimestampScalar(arrow.Timestamp(v.Int64()), toType)

	case arrow.STRING:
		if got, want := v.Type(), datasetmd.PHYSICAL_TYPE_BINARY; got != want {
			panic(fmt.Sprintf("arrowconv.ToScalar: invalid conversion to STRING; got %s, want %s", got, want))
		}
		return scalar.NewStringScalarFromBuffer(memory.NewBufferBytes(v.Binary()))

	case arrow.BINARY:
		if got, want := v.Type(), datasetmd.PHYSICAL_TYPE_BINARY; got != want {
			panic(fmt.Sprintf("arrowconv.ToScalar: invalid conversion to BINARY; got %s, want %s", got, want))
		}
		return scalar.NewBinaryScalar(memory.NewBufferBytes(v.Binary()), toType)

	default:
		panic(fmt.Sprintf("arrowconv.ToScalar: unsupported conversion to Arrow type %s", toType))
	}
}
