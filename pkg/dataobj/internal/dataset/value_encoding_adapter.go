package dataset

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
	"github.com/grafana/loki/v3/pkg/memory"
)

// stringArray is an Arrow-compatible representation of multiple strings.
type stringArray struct {
	offsets []int32
	data    []byte
}

func (sa *stringArray) Len() int {
	return max(0, len(sa.offsets)-1) // Account for first offset
}

func (sa *stringArray) Get(i int) []byte {
	var (
		start = sa.offsets[i]
		end   = sa.offsets[i+1]
	)

	return sa.data[start:end]
}

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
		n = a.unpackResult(s, result)
	}
	return n, err
}

func (a *valueDecoderAdapter) unpackResult(dst []Value, result any) int {
	switch result := result.(type) {
	case stringArray:
		return a.unpackStringArray(dst, result)
	default:
		panic(fmt.Sprintf("legacy decoder adapter found unexpected type %T", result))
	}
}

func (a *valueDecoderAdapter) unpackStringArray(dst []Value, result stringArray) int {
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

// Reset discards any state and resets the decoder to read from data.
// This permits reusing a decoder rather than allocating a new one.
func (a *valueDecoderAdapter) Reset(data []byte) { a.Inner.Reset(data) }
