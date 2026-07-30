// Provenance-includes-location: https://github.com/grafana/mimir/blob/main/pkg/storage/indexheader/encoding/file_reader.go
// Provenance-includes-location: https://github.com/grafana/mimir/blob/main/pkg/storage/indexheader/encoding/encoding.go
// Provenance-includes-license: AGPL-3.0-only
// Provenance-includes-copyright: The Mimir Authors.

package index

import (
	"bufio"
	"encoding/binary"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"sync"

	tsdb_enc "github.com/prometheus/prometheus/tsdb/encoding"
)

// This file implements a streaming decoder for on-disk index sections, modelled
// on Mimir's indexheader/encoding.FileReader and Decbuf.
//
// Unlike poolByteSlice (see file_pool.go), which materialises a whole section
// into a freshly allocated []byte per read, the streaming decoder pulls bytes
// through a small, fixed-size bufio buffer. Fixed-width primitives (Be32, Be64,
// Uvarint, ...) are served from the buffer with Peek and consumed with Discard,
// so decoding a section allocates nothing on the hot path regardless of the
// section size.
//
// It is used only for the file-backed reader and only for FormatV3+ series
// records. In-memory (RealByteSlice / mmap) readers keep decoding from a
// zero-copy sub-slice, and FormatV2 records keep using the buffered decoder
// because the prior-v3 chunk sampler needs random access to the record bytes.

// streamReaderBufSize is the size of the bufio buffer used to stream index
// sections. It bounds steady-state memory to O(bufSize) per concurrent decode
// instead of O(section size).
const streamReaderBufSize = 4096

// streamDecbufPool recycles streamDecbuf values (and their bufio buffers) across
// section reads so that the streaming decode path allocates nothing on the hot
// path once warm.
var streamDecbufPool = sync.Pool{
	New: func() any { return &streamDecbuf{} },
}

// streamDecbuf decodes big-endian binary data from a file segment by streaming
// it through a pooled bufio.Reader backed by a pooled *os.File handle (see
// filePool). It provides the subset of encoding.Decbuf's methods used by the
// series and chunk decoders and satisfies the decbuf interface (see index.go).
//
// Errors are sticky; callers check Err after a sequence of reads.
type streamDecbuf struct {
	pool *filePool
	f    *os.File
	buf  *bufio.Reader

	// readOff is the absolute file offset of the next underlying read. It backs
	// the io.Reader implementation consumed by buf and is independent of the
	// logical decode cursor (off).
	readOff int64
	fileEnd int64

	length int // logical content length in bytes
	off    int // logical bytes consumed
	e      error
}

// Read implements io.Reader so buf can pull bytes from the file via pread
// (ReadAt) without seeking, which keeps the pooled handle usable concurrently
// with poolByteSlice.readAt.
func (d *streamDecbuf) Read(p []byte) (int, error) {
	if d.readOff >= d.fileEnd {
		return 0, io.EOF
	}
	if int64(len(p)) > d.fileEnd-d.readOff {
		p = p[:d.fileEnd-d.readOff]
	}
	n, err := d.f.ReadAt(p, d.readOff)
	d.readOff += int64(n)
	return n, err
}

// newSeriesStreamDecbuf returns a streaming decoder over the contents of the
// uvarint-length-prefixed record at absolute offset off (the on-disk series
// record layout: [uvarint len][content][crc32]). The returned decoder spans the
// content only and MUST be released with releaseStreamDecbuf once decoding is
// done.
//
// Note: unlike NewDecbufUvarintAt, this does NOT verify the trailing CRC32. The
// series record is on the query hot path and, as with the postings offset table
// (see Reader.Postings), we rely on integrity having been established when the
// file's other sections were validated at open time. Skipping the per-record
// checksum avoids reading the record twice (once to hash, once to decode).
func newSeriesStreamDecbuf(pool *filePool, fileLen, off int) (*streamDecbuf, error) {
	if off >= fileLen {
		return nil, tsdb_enc.ErrInvalidSize
	}
	f, err := pool.get()
	if err != nil {
		return nil, err
	}

	d := streamDecbufPool.Get().(*streamDecbuf)
	d.pool = pool
	d.f = f
	d.readOff = int64(off)
	d.fileEnd = int64(fileLen)
	d.off = 0
	d.length = fileLen - off // provisional; narrowed once the header is read
	d.e = nil
	if d.buf == nil {
		d.buf = bufio.NewReaderSize(d, streamReaderBufSize)
	} else {
		d.buf.Reset(d)
	}

	// Read the uvarint length prefix, then bound the decoder to the content.
	hdr, err := d.peek(binary.MaxVarintLen32)
	if err != nil {
		releaseStreamDecbuf(d)
		return nil, err
	}
	l, n := binary.Uvarint(hdr)
	if n <= 0 {
		releaseStreamDecbuf(d)
		return nil, fmt.Errorf("invalid uvarint reading series record length at offset %d", off)
	}
	if off+n+int(l) > fileLen {
		releaseStreamDecbuf(d)
		return nil, tsdb_enc.ErrInvalidSize
	}
	if _, err := d.buf.Discard(n); err != nil {
		releaseStreamDecbuf(d)
		return nil, err
	}
	d.off = n
	d.length = n + int(l) // so Len() == l

	return d, nil
}

// releaseStreamDecbuf returns the file handle to the pool and the decoder to the
// pool. It is a no-op when d is nil (the buffered/in-memory decode path).
func releaseStreamDecbuf(d *streamDecbuf) {
	if d == nil {
		return
	}
	if d.f != nil {
		_ = d.pool.put(d.f)
		d.f = nil
	}
	d.pool = nil
	streamDecbufPool.Put(d)
}

// peek returns up to n bytes from the buffer without consuming them, treating a
// short read at end-of-file as success (like Mimir's FileReader.Peek).
func (d *streamDecbuf) peek(n int) ([]byte, error) {
	b, err := d.buf.Peek(n)
	if err != nil && !stderrors.Is(err, io.EOF) {
		return nil, err
	}
	return b, nil
}

func (d *streamDecbuf) Err() error { return d.e }
func (d *streamDecbuf) Len() int   { return d.length - d.off }

func (d *streamDecbuf) Skip(l int) {
	if d.e != nil {
		return
	}
	if l > d.Len() {
		d.e = tsdb_enc.ErrInvalidSize
		return
	}
	n, err := d.buf.Discard(l)
	d.off += n
	if err != nil {
		d.e = err
	}
}

func (d *streamDecbuf) Uvarint64() uint64 {
	if d.e != nil {
		return 0
	}
	max := binary.MaxVarintLen64
	if rem := d.Len(); rem < max {
		max = rem
	}
	b, err := d.peek(max)
	if err != nil {
		d.e = err
		return 0
	}
	x, n := binary.Uvarint(b)
	if n < 1 {
		d.e = tsdb_enc.ErrInvalidSize
		return 0
	}
	if cn, err := d.buf.Discard(n); err != nil {
		d.off += cn
		d.e = err
		return 0
	}
	d.off += n
	return x
}

func (d *streamDecbuf) Uvarint() int { return int(d.Uvarint64()) }

func (d *streamDecbuf) Varint64() int64 {
	ux := d.Uvarint64()
	if d.e != nil {
		return 0
	}
	// ZigZag decoding, matching tsdb/encoding.Decbuf.Varint64.
	x := int64(ux >> 1)
	if ux&1 != 0 {
		x = ^x
	}
	return x
}

func (d *streamDecbuf) Be32() uint32 {
	if d.e != nil {
		return 0
	}
	if d.Len() < 4 {
		d.e = tsdb_enc.ErrInvalidSize
		return 0
	}
	b, err := d.peek(4)
	if err != nil {
		d.e = err
		return 0
	}
	if len(b) < 4 {
		d.e = tsdb_enc.ErrInvalidSize
		return 0
	}
	v := binary.BigEndian.Uint32(b)
	if _, err := d.buf.Discard(4); err != nil {
		d.e = err
		return 0
	}
	d.off += 4
	return v
}

func (d *streamDecbuf) Be64() uint64 {
	if d.e != nil {
		return 0
	}
	if d.Len() < 8 {
		d.e = tsdb_enc.ErrInvalidSize
		return 0
	}
	b, err := d.peek(8)
	if err != nil {
		d.e = err
		return 0
	}
	if len(b) < 8 {
		d.e = tsdb_enc.ErrInvalidSize
		return 0
	}
	v := binary.BigEndian.Uint64(b)
	if _, err := d.buf.Discard(8); err != nil {
		d.e = err
		return 0
	}
	d.off += 8
	return v
}
