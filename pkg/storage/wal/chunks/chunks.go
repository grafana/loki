package chunks

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"sync"
	"unsafe"

	"github.com/golang/snappy"
	"github.com/klauspost/compress/s2"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// EncodingType defines the type for encoding enums
type EncodingType byte

// Supported encoding types
const (
	EncodingSnappy EncodingType = iota + 1
)

// Initialize the CRC32 table
var (
	castagnoliTable *crc32.Table
	_               iter.EntryIterator = (*entryBufferedIterator)(nil)
)

func init() {
	castagnoliTable = crc32.MakeTable(crc32.Castagnoli)
}

// newCRC32 initializes a CRC32 hash with the preconfigured polynomial
func newCRC32() hash.Hash32 {
	return crc32.New(castagnoliTable)
}

// CRC32 pool
var crc32Pool = sync.Pool{
	New: func() interface{} {
		return newCRC32()
	},
}

// S2 writer pool
var s2WriterPool = sync.Pool{
	New: func() interface{} {
		return snappy.NewBufferedWriter(nil)
	},
}

// S2 reader pool
var s2ReaderPool = sync.Pool{
	New: func() interface{} {
		return s2.NewReader(nil)
	},
}

type StatsWriter struct {
	io.Writer
	written int64
}

func (w *StatsWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	w.written += int64(n)
	return n, err
}

// WriteChunk writes the log entries to the writer w with the specified encoding type.
func WriteChunk(writer io.Writer, entries []*logproto.Entry, encoding EncodingType) (int64, error) {
	w := &StatsWriter{Writer: writer}

	// Validate encoding type
	if encoding != EncodingSnappy {
		return 0, errors.New("unsupported encoding type")
	}

	// Get a CRC32 hash instance from the pool
	crc := crc32Pool.Get().(hash.Hash32)
	crc.Reset()
	defer crc32Pool.Put(crc)

	// Write encoding byte
	if _, err := w.Write([]byte{byte(encoding)}); err != nil {
		return w.written, err
	}
	crc.Write([]byte{byte(encoding)})

	// Write number of entries
	buf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(buf, uint64(len(entries)))
	if _, err := w.Write(buf[:n]); err != nil {
		return w.written, err
	}
	crc.Write(buf[:n])

	// todo: investigate delta+bitpacking from https://github.com/ronanh/intcomp or prometheus bitstream.
	// Write timestamps and lengths
	var prevT, prevDelta, t, delta uint64
	for i, e := range entries {
		t = uint64(e.Timestamp.UnixNano())
		switch i {
		case 0:
			n = binary.PutUvarint(buf, t)
			if _, err := w.Write(buf[:n]); err != nil {
				return w.written, err
			}
			crc.Write(buf[:n])
		case 1:
			delta = t - prevT
			n = binary.PutUvarint(buf, delta)
			if _, err := w.Write(buf[:n]); err != nil {
				return w.written, err
			}
			crc.Write(buf[:n])
		default:
			delta = t - prevT
			dod := int64(delta - prevDelta)
			n = binary.PutVarint(buf, dod)
			if _, err := w.Write(buf[:n]); err != nil {
				return w.written, err
			}
			crc.Write(buf[:n])
		}
		prevT = t
		prevDelta = delta

		// Write length of the line
		lineLen := uint64(len(e.Line))
		n = binary.PutUvarint(buf, lineLen)
		if _, err := w.Write(buf[:n]); err != nil {
			return w.written, err
		}
		crc.Write(buf[:n])
	}

	// Get the offset for the start of the compressed content
	offset := w.written

	// Get an S2 writer from the pool and reset it
	s2w := s2WriterPool.Get().(*snappy.Writer)
	s2w.Reset(w)
	defer s2WriterPool.Put(s2w)

	// Write compressed logs
	for _, e := range entries {
		n, err := s2w.Write(unsafeGetBytes(e.Line))
		if err != nil {
			return w.written, err
		}
		if n != len(e.Line) {
			return w.written, fmt.Errorf("failed to write all bytes: %d != %d", n, len(e.Line))
		}
	}
	if err := s2w.Close(); err != nil {
		return w.written, err
	}

	// Reuse the buffer for offset and checksum
	offsetChecksumBuf := make([]byte, 4)

	// Write the offset using BigEndian
	binary.BigEndian.PutUint32(offsetChecksumBuf, uint32(offset))
	if _, err := w.Write(offsetChecksumBuf); err != nil {
		return w.written, err
	}

	// Calculate and write CRC32 checksum at the end using BigEndian
	checksum := crc.Sum32()
	binary.BigEndian.PutUint32(offsetChecksumBuf, checksum)
	if _, err := w.Write(offsetChecksumBuf); err != nil {
		return w.written, err
	}

	return w.written, nil
}

// ChunkReader reads chunks from a byte slice
type ChunkReader struct {
	b         []byte
	pos       int
	entries   uint64
	entryIdx  uint64
	dataPos   int
	reader    io.Reader
	prevDelta int64
	err       error
	lineBuf   []byte
	ts        int64
}

// NewChunkReader creates a new ChunkReader and performs CRC verification.
func NewChunkReader(b []byte) (*ChunkReader, error) {
	if len(b) < 8 {
		return nil, errors.New("invalid chunk: too short")
	}

	// Extract the CRC32 checksum at the end
	crcValue := binary.BigEndian.Uint32(b[len(b)-4:])
	// Extract the offset
	offset := binary.BigEndian.Uint32(b[len(b)-8 : len(b)-4])
	if int(offset) > len(b)-8 {
		return nil, errors.New("invalid offset: out of bounds")
	}

	// Verify CRC32 checksum
	if crc32.Checksum(b[:offset], castagnoliTable) != crcValue {
		return nil, errors.New("CRC verification failed")
	}

	// Initialize ChunkReader
	reader := &ChunkReader{
		b: b[:offset],
	}

	// Read the chunk header
	if err := reader.readChunkHeader(); err != nil {
		return nil, err
	}

	// Initialize the decompression reader
	compressedData := b[offset : len(b)-8]
	s2Reader := s2ReaderPool.Get().(*s2.Reader)
	s2Reader.Reset(bytes.NewReader(compressedData))
	reader.reader = s2Reader

	return reader, nil
}

// Close implements iter.EntryIterator.
func (r *ChunkReader) Close() error {
	// Return the S2 reader to the pool
	if r.reader != nil {
		s2ReaderPool.Put(r.reader.(*s2.Reader))
		r.reader = nil
	}
	return nil
}

// Entry implements iter.EntryIterator.
// Currrently the chunk reader returns the timestamp and the line, but it could returns all timestamps or/and all lines.
func (r *ChunkReader) At() (int64, []byte) {
	return r.ts, r.lineBuf
}

// Err implements iter.EntryIterator.
func (r *ChunkReader) Err() error {
	return r.err
}

// Next implements iter.EntryIterator. Reads the next entry from the chunk.
func (r *ChunkReader) Next() bool {
	if r.entryIdx >= r.entries || r.err != nil {
		return false
	}

	// Read timestamp
	switch r.entryIdx {
	case 0:
		ts, n := binary.Uvarint(r.b[r.pos:])
		r.pos += n
		r.ts = int64(ts)
	case 1:
		delta, n := binary.Uvarint(r.b[r.pos:])
		r.pos += n
		r.prevDelta = int64(delta)
		r.ts += r.prevDelta
	default:
		dod, n := binary.Varint(r.b[r.pos:])
		r.pos += n
		r.prevDelta += dod
		r.ts += r.prevDelta
	}

	// Read line length
	l, n := binary.Uvarint(r.b[r.pos:])
	lineLen := int(l)
	r.pos += n

	// If the buffer is not yet initialize or too small, we get a new one.
	if r.lineBuf == nil || lineLen > cap(r.lineBuf) {
		// in case of a replacement we replace back the buffer in the pool
		if r.lineBuf != nil {
			chunkenc.BytesBufferPool.Put(r.lineBuf)
		}
		r.lineBuf = chunkenc.BytesBufferPool.Get(lineLen).([]byte)
		if lineLen > cap(r.lineBuf) {
			r.err = fmt.Errorf("could not get a line buffer of size %d, actual %d", lineLen, cap(r.lineBuf))
			return false
		}
	}
	r.lineBuf = r.lineBuf[:lineLen]
	// Read line from decompressed data
	if _, err := io.ReadFull(r.reader, r.lineBuf); err != nil {
		if err != io.EOF {
			r.err = err
		}
		return false
	}

	r.entryIdx++
	return true
}

// readChunkHeader reads the chunk header and initializes the reader state
func (r *ChunkReader) readChunkHeader() error {
	if len(r.b) < 1 {
		return errors.New("invalid chunk header")
	}

	// Read encoding byte
	encoding := r.b[r.pos]
	r.pos++

	// Read number of entries
	entries, n := binary.Uvarint(r.b[r.pos:])
	r.pos += n
	r.entries = entries

	// Validate encoding (assuming only Snappy is supported)
	if EncodingType(encoding) != EncodingSnappy {
		return errors.New("unsupported encoding type")
	}

	return nil
}

func unsafeGetBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}
