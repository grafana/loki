package wal

import (
	"encoding/binary"
	"errors"
	"hash"
	"hash/crc32"
	"io"
	"reflect"
	"sync"
	"unsafe"

	"github.com/golang/snappy"
	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// EncodingType defines the type for encoding enums
type EncodingType byte

// Supported encoding types
const (
	EncodingSnappy EncodingType = iota + 1
)

// Initialize the CRC32 table
var castagnoliTable *crc32.Table

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

// writeChunk writes the log entries to the writer w with the specified encoding type.
func writeChunk(w io.Writer, entries []*logproto.Entry, encoding EncodingType) (int64, error) {
	if len(entries) == 0 {
		return 0, nil
	}

	// Validate encoding type
	if encoding != EncodingSnappy {
		return 0, errors.New("unsupported encoding type")
	}

	var written int64

	// Get a CRC32 hash instance from the pool
	crc := crc32Pool.Get().(hash.Hash32)
	crc.Reset()
	defer crc32Pool.Put(crc)

	// Write encoding byte
	if _, err := w.Write([]byte{byte(encoding)}); err != nil {
		return written, err
	}
	crc.Write([]byte{byte(encoding)})
	written++

	// Write number of entries
	buf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(buf, uint64(len(entries)))
	if _, err := w.Write(buf[:n]); err != nil {
		return written, err
	}
	crc.Write(buf[:n])
	written += int64(n)

	// Write timestamps and lengths
	var prevT, prevDelta, t, delta uint64
	for i, e := range entries {
		t = uint64(e.Timestamp.UnixNano())
		switch i {
		case 0:
			n = binary.PutUvarint(buf, t)
			if _, err := w.Write(buf[:n]); err != nil {
				return written, err
			}
			crc.Write(buf[:n])
			written += int64(n)
		case 1:
			delta = t - prevT
			n = binary.PutUvarint(buf, delta)
			if _, err := w.Write(buf[:n]); err != nil {
				return written, err
			}
			crc.Write(buf[:n])
			written += int64(n)
		default:
			delta = t - prevT
			dod := delta - prevDelta
			n = binary.PutUvarint(buf, dod)
			if _, err := w.Write(buf[:n]); err != nil {
				return written, err
			}
			crc.Write(buf[:n])
			written += int64(n)
		}
		prevT = t
		prevDelta = delta

		// Write length of the line
		lineLen := uint64(len(e.Line))
		n = binary.PutUvarint(buf, lineLen)
		if _, err := w.Write(buf[:n]); err != nil {
			return written, err
		}
		crc.Write(buf[:n])
		written += int64(n)
	}

	// Get the offset for the start of the compressed content
	offset := written

	// Get an S2 writer from the pool and reset it
	s2w := s2WriterPool.Get().(*snappy.Writer)
	s2w.Reset(w)
	defer s2WriterPool.Put(s2w)

	// Write compressed logs
	for _, e := range entries {
		n, err := s2w.Write(unsafeGetBytes(e.Line))
		if err != nil {
			return written, err
		}
		written += int64(n)
	}
	if err := s2w.Close(); err != nil {
		return written, err
	}

	// Reuse the buffer for offset and checksum
	offsetChecksumBuf := make([]byte, 4)

	// Write the offset using BigEndian
	binary.BigEndian.PutUint32(offsetChecksumBuf, uint32(offset))
	if _, err := w.Write(offsetChecksumBuf); err != nil {
		return written, err
	}
	written += 4

	// Calculate and write CRC32 checksum at the end using BigEndian
	checksum := crc.Sum32()
	binary.BigEndian.PutUint32(offsetChecksumBuf, checksum)
	if _, err := w.Write(offsetChecksumBuf); err != nil {
		return written, err
	}
	written += 4

	return written, nil
}

type ChunkReader struct {
	b []byte
}

// Close implements iter.EntryIterator.
func (r *ChunkReader) Close() error {
	panic("unimplemented")
}

// Entry implements iter.EntryIterator.
func (r *ChunkReader) Entry() push.Entry {
	panic("unimplemented")
}

// Error implements iter.EntryIterator.
func (r *ChunkReader) Error() error {
	panic("unimplemented")
}

func (r *ChunkReader) Next() bool {
	panic("implement me")
}

func NewChunkReader(b []byte) *ChunkReader {
	return &ChunkReader{
		b: b,
	}
}

func unsafeGetBytes(s string) []byte {
	var buf []byte
	p := unsafe.Pointer(&buf)
	*(*string)(p) = s
	(*reflect.SliceHeader)(p).Cap = len(s)
	return buf
}
