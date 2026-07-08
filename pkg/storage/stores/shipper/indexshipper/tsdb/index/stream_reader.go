// SPDX-License-Identifier: AGPL-3.0-only
//
// StreamReader is an alternative implementation of the TSDB index Reader that
// reads bytes from disk on demand via a streamenc.DecbufFactory instead of
// mmap'ing the file. Method-for-method it mirrors the existing Reader
// implementation in this package; internal decoding differs where reads
// operate on a Decbuf (file-backed) rather than a ByteSlice (memory-backed).
//
// The port lands proposal-by-proposal from PLAN-nommap-indexgateway.md's
// Phase 2 Bucket A. Methods still under construction return an error rather
// than a wrong result — callers should assume the reader is not yet ready
// for production traffic.

package index

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc"
)

// StreamReaderOptions configures the streaming reader. All fields are
// optional; zero values produce sensible defaults.
type StreamReaderOptions struct {
	// MaxIdleFileHandles bounds the number of file descriptors kept warm
	// for the underlying file. 0 means no pooling — every read opens and
	// closes an FD.
	MaxIdleFileHandles uint
}

// StreamReader is the file-streaming equivalent of Reader. It intentionally
// mirrors the surface of Reader so it can substitute for it wherever the
// tsdb.IndexReader interface is expected.
type StreamReader struct {
	factory *streamenc.FilePoolDecbufFactory
	path    string
	size    int64

	toc     *TOC
	version int

	// symbols is set by newStreamSymbols once the symbol section has been
	// scanned. It stays nil until P2.A3 fires during construction.
	symbols *streamSymbols

	// streamPostings is the sparse postings-offset-table index for V2+
	// indexes. See stream_postings.go for details on `off` semantics.
	streamPostings map[string][]streamPostingOffset
	// streamPostingsV1 holds the full offset table for V1 indexes.
	streamPostingsV1 map[string]map[string]uint64

	// nameSymbols is a small cache of label-name symbol ordinals populated
	// during construction. Mirrors Reader.nameSymbols.
	nameSymbols map[uint32]string

	// fingerprintOffsets is set by P2.A7. Nil until then; nil is treated as
	// an empty offset table by the sharded-postings logic.
	fingerprintOffsets FingerprintOffsets
}

// NewStreamFileReader opens a TSDB index file for streaming access. The file
// is not mmap'd; individual reads flow through the streamenc factory and can
// be scheduled by the Go runtime.
func NewStreamFileReader(path string) (*StreamReader, error) {
	return NewStreamFileReaderWithOptions(path, StreamReaderOptions{})
}

// NewStreamFileReaderWithOptions is NewStreamFileReader with explicit tuning
// knobs. Phase 2 Bucket A does not yet consume all options — callers should
// treat this as forward-compatible.
func NewStreamFileReaderWithOptions(path string, opts StreamReaderOptions) (*StreamReader, error) {
	// Metrics wiring lives in a later proposal (P2.D1). Nil metrics here is
	// fine — the filepool guards on it.
	factory := streamenc.NewFilePoolDecbufFactory(path, opts.MaxIdleFileHandles, nil)

	size, err := factory.FileSize()
	if err != nil {
		_ = factory.Close()
		if errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		return nil, errors.Wrap(err, "stat index file")
	}

	sr := &StreamReader{
		factory: factory,
		path:    path,
		size:    size,
	}

	if err := sr.readHeader(); err != nil {
		_ = factory.Close()
		return nil, err
	}

	toc, err := sr.readTOC()
	if err != nil {
		_ = factory.Close()
		return nil, errors.Wrap(err, "read TOC")
	}
	sr.toc = toc

	sr.symbols, err = newStreamSymbols(context.Background(), factory, sr.version, int(toc.Symbols))
	if err != nil {
		_ = factory.Close()
		return nil, errors.Wrap(err, "read symbols")
	}

	if err := sr.buildPostingsIndex(context.Background()); err != nil {
		_ = factory.Close()
		return nil, errors.Wrap(err, "read postings table")
	}

	// Warm the nameSymbols cache with reverse lookups for every label name
	// discovered in the postings table. Matches newReader.
	sr.nameSymbols = make(map[uint32]string, len(sr.streamPostings))
	for k := range sr.streamPostings {
		if k == "" {
			continue
		}
		ord, err := sr.symbols.ReverseLookup(k)
		if err != nil {
			_ = factory.Close()
			return nil, errors.Wrap(err, "reverse symbol lookup")
		}
		sr.nameSymbols[ord] = k
	}

	return sr, nil
}

// readHeader validates the magic bytes and captures the file format version.
func (r *StreamReader) readHeader() error {
	if r.size < int64(HeaderLen) {
		return errors.Wrap(streamenc.ErrInvalidSize, "index header")
	}
	d := r.factory.NewRawDecbuf(context.Background())
	if err := d.Err(); err != nil {
		return errors.Wrap(err, "open header decbuf")
	}
	defer func() { _ = d.Close() }()

	magic := d.Be32()
	if magic != MagicIndex {
		return errors.Errorf("invalid magic number %x", magic)
	}
	version := int(d.Byte())
	if err := d.Err(); err != nil {
		return errors.Wrap(err, "read header")
	}
	if version != FormatV1 && version != FormatV2 && version != FormatV3 && version != FormatV4 {
		return errors.Errorf("unknown index file version %d", version)
	}
	r.version = version
	return nil
}

// readTOC reads the fixed-size TOC record from the tail of the file. The TOC
// is 9 uint64 fields followed by a CRC32 over those 72 bytes.
func (r *StreamReader) readTOC() (*TOC, error) {
	if r.size < int64(indexTOCLen) {
		return nil, streamenc.ErrInvalidSize
	}
	d := r.factory.NewRawDecbuf(context.Background())
	if err := d.Err(); err != nil {
		return nil, err
	}
	defer func() { _ = d.Close() }()

	tocStart := int(r.size) - indexTOCLen
	d.ResetAt(tocStart)
	if err := d.Err(); err != nil {
		return nil, err
	}

	// We need to compute the CRC over the 9 uint64s while also decoding
	// them, then compare against the trailing 4-byte CRC. Read the raw
	// content section as a single allocation.
	contentLen := indexTOCLen - 4
	var content [72]byte
	if contentLen != len(content) {
		return nil, fmt.Errorf("unexpected TOC content length %d", contentLen)
	}
	for i := 0; i < 9; i++ {
		v := d.Be64()
		if err := d.Err(); err != nil {
			return nil, err
		}
		binary.BigEndian.PutUint64(content[i*8:(i+1)*8], v)
	}
	expCRC := d.Be32()
	if err := d.Err(); err != nil {
		return nil, err
	}
	actualCRC := crc32Castagnoli(content[:])
	if actualCRC != expCRC {
		return nil, errors.Wrap(streamenc.ErrInvalidChecksum, "read TOC")
	}

	return &TOC{
		Symbols:            binary.BigEndian.Uint64(content[0:8]),
		Series:             binary.BigEndian.Uint64(content[8:16]),
		LabelIndices:       binary.BigEndian.Uint64(content[16:24]),
		LabelIndicesTable:  binary.BigEndian.Uint64(content[24:32]),
		Postings:           binary.BigEndian.Uint64(content[32:40]),
		PostingsTable:      binary.BigEndian.Uint64(content[40:48]),
		FingerprintOffsets: binary.BigEndian.Uint64(content[48:56]),
		Metadata: Metadata{
			From:     int64(binary.BigEndian.Uint64(content[56:64])),
			Through:  int64(binary.BigEndian.Uint64(content[64:72])),
			Checksum: expCRC,
		},
	}, nil
}

// crc32Castagnoli is a package-local shim for the castagnoliTable already
// defined in index.go.
func crc32Castagnoli(b []byte) uint32 {
	h := newCRC32()
	h.Write(b)
	return h.Sum32()
}

// --------- Basic metadata methods (P2.A2) ----------

// Version returns the file format version of the underlying index.
func (r *StreamReader) Version() int { return r.version }

// Bounds returns the earliest and latest timestamps captured in the metadata.
func (r *StreamReader) Bounds() (int64, int64) {
	return r.toc.Metadata.From, r.toc.Metadata.Through
}

// Checksum returns the TOC checksum used as the index-file fingerprint.
func (r *StreamReader) Checksum() uint32 { return r.toc.Metadata.Checksum }

// Size returns the total on-disk size of the index file.
func (r *StreamReader) Size() int64 { return r.size }

// RawFileReader returns an io.ReadSeeker over the raw index file. Each call
// opens a fresh file handle; the caller owns closing it.
//
// This is used by the indexshipper to upload the index file to object
// storage. See P2.B4 in the Phase 2 plan.
func (r *StreamReader) RawFileReader() (io.ReadSeeker, error) {
	f, err := os.Open(r.path)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Close releases the DecbufFactory resources. The file itself is not deleted.
func (r *StreamReader) Close() error {
	return r.factory.Close()
}

// --------- Not-yet-implemented surface (later A proposals) ---------

// Symbols returns an iterator over the symbols in the index.
func (r *StreamReader) Symbols() StringIter {
	return r.symbols.Iter()
}

// SymbolTableSize returns the on-heap footprint of the sparse symbol offsets
// table. Matches Reader.SymbolTableSize() semantics.
func (r *StreamReader) SymbolTableSize() uint64 {
	return uint64(r.symbols.Size())
}

// lookupSymbol resolves a symbol ordinal (or file offset on V1) to its
// string value. Mirrors Reader.lookupSymbol including the nameSymbols cache.
func (r *StreamReader) lookupSymbol(o uint32) (string, error) {
	if s, ok := r.nameSymbols[o]; ok {
		return s, nil
	}
	return r.symbols.Lookup(o)
}

// errStreamReaderNotImplemented is emitted by every method not yet ported.
// It carries the method name so unit tests can pinpoint what's missing.
func errStreamReaderNotImplemented(method string) error {
	return fmt.Errorf("streaming TSDB index reader: %s not implemented (phase 2 in progress)", method)
}

// errStringIter is a StringIter that yields no strings and reports the given
// error from Err(). It's used as a placeholder return value for stubbed
// methods that must satisfy a StringIter-returning signature.
type errStringIter struct {
	err error
}

func (e errStringIter) Next() bool  { return false }
func (e errStringIter) At() string  { return "" }
func (e errStringIter) Err() error  { return e.err }
