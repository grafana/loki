package dataset

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
	"github.com/grafana/loki/v3/pkg/memory"
)

// valueDecoderAdapter implements [legacyValueDecoder] for a newer
// [valueDecoder] implementation.
type valueDecoderAdapter struct {
	Alloc *memory.Allocator
	Inner valueDecoder
}

var _ legacyValueDecoder = (*valueDecoderAdapter)(nil)

// PhysicalType returns the type of values supported by the decoder.
func (a *valueDecoderAdapter) PhysicalType() datasetmd.PhysicalType { return a.Inner.PhysicalType() }

// EncodingType returns the encoding type used by the decoder.
func (a *valueDecoderAdapter) EncodingType() datasetmd.EncodingType { return a.Inner.EncodingType() }

// Decode decodes up to len(s) values, storing the results into s. The
// number of decoded values is returned, followed by an error (if any).
// At the end of the stream, Decode returns 0, [io.EOF].
func (a *valueDecoderAdapter) Decode(s []Value) (n int, err error) {
	result, err := a.Inner.Decode(a.Alloc, len(s))
	if result != nil {
		n = a.unpackArray(s, result)
	}
	return n, err
}

func (a *valueDecoderAdapter) unpackArray(dst []Value, result columnar.Array) int {
	switch result := result.(type) {
	case *columnar.UTF8:
		return a.unpackUTF8(dst, result)
	case *columnar.Int64:
		return a.unpackInt64(dst, result.Values())
	default:
		panic(fmt.Sprintf("legacy decoder adapter found unexpected type %T", result))
	}
}

func (a *valueDecoderAdapter) unpackUTF8(dst []Value, result *columnar.UTF8) int {
	if result.Len() > len(dst) {
		panic(fmt.Sprintf("invariant broken: larger src len (%d) than dst (%d)", result.Len(), len(dst)))
	}

	for i := range result.Len() {
		srcBuf := result.Get(i)

		dstBuf := slicegrow.GrowToCap(dst[i].Buffer(), len(srcBuf))
		dstBuf = dstBuf[:len(srcBuf)]
		copy(dstBuf, srcBuf)

		dst[i] = BinaryValue(dstBuf)
	}

	return result.Len()
}

func (a *valueDecoderAdapter) unpackInt64(dst []Value, result []int64) int {
	if len(result) > len(dst) {
		panic(fmt.Sprintf("invariant broken: larger src len (%d) than dst (%d)", len(result), len(dst)))
	}

	for i := range result {
		dst[i] = Int64Value(result[i])
	}
	return len(result)
}

// Reset discards any state and resets the decoder to read from data.
// This permits reusing a decoder rather than allocating a new one.
func (a *valueDecoderAdapter) Reset(data []byte) { a.Inner.Reset(data) }
