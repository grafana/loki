package format

import (
	"fmt"

	"github.com/parquet-go/parquet-go/encoding/thrift"
)

// FooterDecoder decodes Parquet file footers (thrift compact protocol) with
// zero steady-state allocations by retaining its input buffer and decoded
// metadata across calls to Decode.
//
// The zero value is ready to use. FooterDecoder values must not be copied
// after first use and are not safe for concurrent use; to amortize
// allocations across goroutines, keep decoders in a sync.Pool.
//
// A decoder never allocates for inputs whose shape fits the capacity
// retained from previous decodes; larger or differently-shaped inputs grow
// the retained state as needed. The retained state never shrinks: one
// unusually large footer sizes the decoder's buffer and metadata tree for
// its remaining lifetime, so long-lived decoders dedicated to a single file
// may prefer to be discarded after use instead of pooled.
type FooterDecoder struct {
	buf     []byte
	meta    FileMetaData
	reader  thrift.BytesReader
	decoder *thrift.Decoder
}

// protocol is stateless and shared by all decoders.
var footerProtocol thrift.CompactProtocol

// Decode parses the thrift-encoded footer in data and returns the decoded
// metadata along with the number of bytes consumed. Trailing bytes after the
// thrift payload (such as the 28-byte AES-GCM footer signature written by
// encrypted-footer-signing writers) are not an error; callers can detect
// them by comparing the consumed byte count to len(data).
//
// The returned metadata, and all strings and byte slices it references, are
// owned by the decoder and remain valid only until the next call to Decode.
// Callers that need the metadata to outlive the decoder must deep-copy it.
//
// Decode makes a private copy of data; the caller retains ownership of the
// slice and may reuse it immediately.
func (d *FooterDecoder) Decode(data []byte) (*FileMetaData, int, error) {
	// The decoded metadata aliases the retained buffer, so the copy below
	// would corrupt previously returned values in place. Resetting first
	// keeps the "valid until next Decode" contract honest instead of
	// leaving values that are subtly wrong.
	d.meta.Reset()
	d.buf = append(d.buf[:0], data...)

	if d.reader == nil {
		d.reader = footerProtocol.NewReaderFromBytes(nil)
		d.decoder = thrift.NewDecoder(d.reader)
	}
	d.reader.ResetBytes(d.buf)

	if err := d.decoder.Decode(&d.meta); err != nil {
		return nil, 0, fmt.Errorf("decoding parquet footer: %w", err)
	}
	return &d.meta, d.reader.BytesRead(), nil
}
