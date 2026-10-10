package dataset

import (
	"errors"
	"fmt"
	"io"

	"github.com/parquet-go/parquet-go/encoding/delta"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/memory"
)

func init() {
	// Register the encoding so instances of it can be dynamically created.
	registerValueEncoding(
		datasetmd.PHYSICAL_TYPE_INT64,
		datasetmd.ENCODING_TYPE_DELTA,
		registryEntry{
			NewEncoder: func(w streamio.Writer) valueEncoder { return newDeltaEncoder(w) },
			NewDecoder: func(data []byte) valueDecoder { return newDeltaDecoder(data) },
		},
	)
}

var deltaEncoding delta.BinaryPackedEncoding

// deltaEncoder encodes int64s using the Parquet DELTA_BINARY_PACKED encoding
// (github.com/parquet-go/parquet-go/encoding/delta), writing the full
// encoded page to a [streamio.Writer] on Flush.
//
// Values are buffered in memory until Flush is called, since
// DELTA_BINARY_PACKED operates on a full batch of values at once (splitting
// them into blocks and mini-blocks to compute per-block minimum deltas and
// bit widths) rather than incrementally.
type deltaEncoder struct {
	w      streamio.Writer
	values []int64

	encoded []byte // Reused output buffer for Flush; see Flush for why reuse is safe.
}

var _ valueEncoder = (*deltaEncoder)(nil)

// newDeltaEncoder creates a deltaEncoder that writes encoded numbers to w.
func newDeltaEncoder(w streamio.Writer) *deltaEncoder {
	var enc deltaEncoder
	enc.Reset(w)
	return &enc
}

// PhysicalType returns [datasetmd.PHYSICAL_TYPE_INT64].
func (enc *deltaEncoder) PhysicalType() datasetmd.PhysicalType {
	return datasetmd.PHYSICAL_TYPE_INT64
}

// EncodingType returns [datasetmd.ENCODING_TYPE_DELTA].
func (enc *deltaEncoder) EncodingType() datasetmd.EncodingType {
	return datasetmd.ENCODING_TYPE_DELTA
}

// Encode encodes a new value.
func (enc *deltaEncoder) Encode(v Value) error {
	if v.Type() != datasetmd.PHYSICAL_TYPE_INT64 {
		return fmt.Errorf("delta: invalid value type %v", v.Type())
	}
	enc.values = append(enc.values, v.Int64())
	return nil
}

// EstimatedSize returns an estimate of the size of the DELTA_BINARY_PACKED-
// encoded page if Flush were called right now.
//
// DELTA_BINARY_PACKED compresses well for monotonic/near-monotonic data, so
// using the raw (unencoded) int64 size here is a deliberately conservative
// over-estimate: it's safe to cut a page slightly earlier than necessary, but
// not to let one grow far past its configured size hint.
func (enc *deltaEncoder) EstimatedSize() int {
	const int64Size = 8
	return int64Size * len(enc.values)
}

// Flush encodes all buffered values using the DELTA_BINARY_PACKED encoding
// and writes the result to the underlying [streamio.Writer].
func (enc *deltaEncoder) Flush() error {
	if len(enc.values) == 0 {
		return nil
	}

	// enc.encoded retains whatever capacity it grew to on a previous page.
	// Reusing it is safe because the result is written to enc.w and then
	// discarded immediately below -- nothing retains it past this call, so
	// unlike the decoders there's no cross-page aliasing concern.
	encoded, err := deltaEncoding.EncodeInt64(enc.encoded[:0], enc.values)
	if err != nil {
		return fmt.Errorf("delta: encoding values: %w", err)
	}
	enc.encoded = encoded

	n, err := enc.w.Write(encoded)
	if n != len(encoded) {
		return fmt.Errorf("short write; expected %d bytes, wrote %d", len(encoded), n)
	}
	return err
}

// Reset resets the encoder to its initial state.
func (enc *deltaEncoder) Reset(w streamio.Writer) {
	enc.w = w
	enc.values = enc.values[:0]
}

// deltaDecoder decodes int64s encoded with the Parquet DELTA_BINARY_PACKED
// encoding, a block at a time, using [binaryPackedDecoder]. Unlike a batch
// decode of the whole page, this only decodes as many blocks as are needed
// to satisfy each call to Decode, and resumes from where it left off on the
// next one -- so a column that's only read in part (or whose page is read
// across many small batches) doesn't pay to decode values it never uses.
//
// Decoding is deferred to the first call to Decode (rather than happening in
// Reset) because [binaryPackedDecoder.reset] can fail on malformed input,
// and [valueDecoder.Reset] has no error return to report that through.
type deltaDecoder struct {
	data    []byte
	started bool
	stream  binaryPackedDecoder
}

var _ valueDecoder = (*deltaDecoder)(nil)

// newDeltaDecoder creates a deltaDecoder that reads encoded numbers from data.
func newDeltaDecoder(data []byte) *deltaDecoder {
	var dec deltaDecoder
	dec.Reset(data)
	return &dec
}

// PhysicalType returns [datasetmd.PHYSICAL_TYPE_INT64].
func (dec *deltaDecoder) PhysicalType() datasetmd.PhysicalType {
	return datasetmd.PHYSICAL_TYPE_INT64
}

// Type returns [datasetmd.ENCODING_TYPE_DELTA].
func (dec *deltaDecoder) EncodingType() datasetmd.EncodingType {
	return datasetmd.ENCODING_TYPE_DELTA
}

// Decode decodes up to count values, storing the results into a new
// [columnar.Int64] array obtained from the provided allocator. At the end of
// the stream, Decode returns an [io.EOF].
func (dec *deltaDecoder) Decode(alloc *memory.Allocator, count int) (columnar.Array, error) {
	if !dec.started {
		dec.started = true
		if len(dec.data) > 0 {
			if _, err := dec.stream.reset(dec.data); err != nil {
				return nil, fmt.Errorf("delta: decoding values: %w", err)
			}
		}
	}

	valuesBuf := memory.NewBuffer[int64](alloc, count)
	valuesBuf.Resize(count)

	n, err := dec.stream.decodeInt64(valuesBuf.Data())
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("delta: decoding values: %w", err)
	}
	valuesBuf.Resize(n)

	return columnar.NewNumber[int64](valuesBuf.Data(), memory.Bitmap{}), err
}

// Reset resets the deltaDecoder to its initial state.
func (dec *deltaDecoder) Reset(data []byte) {
	dec.data = data
	dec.started = false
}
