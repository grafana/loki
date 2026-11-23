package dataset

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
)

// copyArrayToRow copies an array to a dst []Row at the provided column index.
// Returns the amount of copied rows. Panics if len(dst) < src.Len().
//
// If src is nil, copyArray does nothing and returns 0.
func copyArrayToRow(dst []Row, columnIndex int, src columnar.Array) int {
	if src == nil {
		return 0
	}

	if len(dst) < src.Len() {
		panic(fmt.Sprintf("invariant broken: len(dst) < src.Len(): %d < %d", len(dst), src.Len()))
	}

	switch src := src.(type) {
	case *columnar.UTF8:
		return copyUTF8Array(dst, columnIndex, src)
	case *columnar.Number[int64]:
		return copyInt64Array(dst, columnIndex, src)
	case *columnar.Null:
		return copyNullArray(dst, columnIndex, src)
	default:
		panic(fmt.Sprintf("unexpected array type: %T", src))
	}
}

// copyUTF8Array copies values from src into dst. Returns the amount of copied
// rows.
//
// Should only be called from [copyArray].
func copyUTF8Array(dst []Row, columnIndex int, src *columnar.UTF8) int {
	for i := range src.Len() {
		if src.IsNull(i) {
			dst[i].Values[columnIndex].Zero()
			continue
		}

		srcBuf := src.Get(i)
		dstBuf := slicegrow.GrowToCap(dst[i].Values[columnIndex].Buffer(), len(srcBuf))
		dstBuf = dstBuf[:len(srcBuf)]
		copy(dstBuf, srcBuf)

		dst[i].Values[columnIndex] = BinaryValue(dstBuf)
	}

	return src.Len()
}

// copyUTF8Value copies element i of src into dst and reuses the buffer of dst, so dst does not
// alias src. A null element zeroes dst.
func copyUTF8Value(dst *Value, src *columnar.UTF8, i int) {
	if src.IsNull(i) {
		dst.Zero()
		return
	}

	srcBuf := src.Get(i)
	dstBuf := slicegrow.GrowToCap(dst.Buffer(), len(srcBuf))
	dstBuf = dstBuf[:len(srcBuf)]
	copy(dstBuf, srcBuf)

	*dst = BinaryValue(dstBuf)
}

// copyInt64Array copies values from src into dst. Returns the amount of copied
// rows.
//
// Should only be called from [copyArray].
func copyInt64Array(dst []Row, columnIndex int, src *columnar.Number[int64]) int {
	for i := range src.Len() {
		if src.IsNull(i) {
			dst[i].Values[columnIndex].Zero()
			continue
		}
		dst[i].Values[columnIndex] = Int64Value(src.Get(i))
	}

	return src.Len()
}

// copyNullArray copies values from src into dst. Returns the amount of copied
// rows.
//
// Should only be called from [copyArray].
func copyNullArray(dst []Row, columnIndex int, src *columnar.Null) int {
	for i := range src.Len() {
		dst[i].Values[columnIndex].Zero()
	}
	return src.Len()
}

// scalarOf returns value as a scalar for the compute kernels, or nil if value
// is null.
//
// scalarOf panics on a physical type that it does not know.
func scalarOf(value Value) columnar.Datum {
	switch value.Type() {
	case datasetmd.PHYSICAL_TYPE_INT64:
		return &columnar.NumberScalar[int64]{Value: value.Int64()}
	case datasetmd.PHYSICAL_TYPE_UINT64:
		return &columnar.NumberScalar[uint64]{Value: value.Uint64()}
	case datasetmd.PHYSICAL_TYPE_BINARY:
		return &columnar.UTF8Scalar{Value: value.Binary()}
	case datasetmd.PHYSICAL_TYPE_UNSPECIFIED:
		return nil
	default:
		panic(fmt.Sprintf("dataset.scalarOf: unsupported physical type %s", value.Type()))
	}
}

// valueAt returns the value at index i of arr. A null element gives the nil
// Value. The returned value shares memory with arr.
//
// valueAt panics if arr is not an int64, uint64, binary or null array.
func valueAt(arr columnar.Array, i int) Value {
	switch arr := arr.(type) {
	case *columnar.Number[int64]:
		if arr.IsNull(i) {
			return Value{}
		}
		return Int64Value(arr.Get(i))
	case *columnar.Number[uint64]:
		if arr.IsNull(i) {
			return Value{}
		}
		return Uint64Value(arr.Get(i))
	case *columnar.UTF8:
		if arr.IsNull(i) {
			return Value{}
		}
		return BinaryValue(arr.Get(i))
	case *columnar.ChunkedUTF8:
		return BinaryValue(arr.Get(i))
	case *columnar.Null:
		return Value{}
	default:
		panic(fmt.Sprintf("dataset.valueAt: unsupported array type %T", arr))
	}
}
