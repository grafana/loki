// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package indexheader

import (
	"bufio"
	"context"
	"encoding/binary"
	"hash"
	"hash/crc32"
	"io"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
	"unsafe"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/tsdb/encoding"
	"github.com/prometheus/prometheus/tsdb/fileutil"
	"github.com/prometheus/prometheus/tsdb/index"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/objstore"
	"github.com/thanos-io/thanos/pkg/runutil"
)

const (
	// BinaryFormatV1 represents first version of index-header file.
	BinaryFormatV1 = 1

	indexTOCLen  = 6*8 + crc32.Size
	binaryTOCLen = 2*8 + crc32.Size
	// headerLen represents number of bytes reserved of index header for header.
	headerLen = 4 + 1 + 1 + 8

	// MagicIndex are 4 bytes at the head of an index-header file.
	MagicIndex = 0xBAAAD792

	postingLengthFieldSize = 4
)

// The table gets initialized with sync.Once but may still cause a race
// with any other use of the crc32 package anywhere. Thus we initialize it
// before.
var castagnoliTable *crc32.Table

func init() {
	castagnoliTable = crc32.MakeTable(crc32.Castagnoli)
}

// newCRC32 initializes a CRC32 hash with a preconfigured polynomial, so the
// polynomial may be easily changed in one location at a later time, if necessary.
func newCRC32() hash.Hash32 {
	return crc32.New(castagnoliTable)
}

// BinaryTOC is a table of content for index-header file.
type BinaryTOC struct {
	// Symbols holds start to the same symbols section as index related to this index header.
	Symbols uint64
	// PostingsOffsetTable holds start to the the same Postings Offset Table section as index related to this index header.
	PostingsOffsetTable uint64
}

// WriteBinary build index-header file from the pieces of index in object storage.
func WriteBinary(ctx context.Context, bkt objstore.BucketReader, id ulid.ULID, filename string) (err error) {
	ir, indexVersion, err := newChunkedIndexReader(ctx, bkt, id)
	if err != nil {
		return errors.Wrap(err, "new index reader")
	}
	tmpFilename := filename + ".tmp"

	// Buffer for copying and encbuffers.
	// This also will control the size of file writer buffer.
	buf := make([]byte, 32*1024)
	bw, err := newBinaryWriter(tmpFilename, buf)
	if err != nil {
		return errors.Wrap(err, "new binary index header writer")
	}
	defer runutil.CloseWithErrCapture(&err, bw, "close binary writer for %s", tmpFilename)

	if err := bw.AddIndexMeta(indexVersion, ir.toc.PostingsTable); err != nil {
		return errors.Wrap(err, "add index meta")
	}

	if err := ir.CopySymbols(bw.SymbolsWriter(), buf); err != nil {
		return err
	}

	if err := bw.f.Flush(); err != nil {
		return errors.Wrap(err, "flush")
	}

	if err := ir.CopyPostingsOffsets(bw.PostingOffsetsWriter(), buf); err != nil {
		return err
	}

	if err := bw.f.Flush(); err != nil {
		return errors.Wrap(err, "flush")
	}

	if err := bw.WriteTOC(); err != nil {
		return errors.Wrap(err, "write index header TOC")
	}

	if err := bw.f.Flush(); err != nil {
		return errors.Wrap(err, "flush")
	}

	if err := bw.f.f.Sync(); err != nil {
		return errors.Wrap(err, "sync")
	}

	// Create index-header in atomic way, to avoid partial writes (e.g during restart or crash of store GW).
	return os.Rename(tmpFilename, filename)
}

type chunkedIndexReader struct {
	ctx  context.Context
	path string
	size uint64
	bkt  objstore.BucketReader
	toc  *index.TOC
}

func newChunkedIndexReader(ctx context.Context, bkt objstore.BucketReader, id ulid.ULID) (*chunkedIndexReader, int, error) {
	indexFilepath := filepath.Join(id.String(), block.IndexFilename)
	attrs, err := bkt.Attributes(ctx, indexFilepath)
	if err != nil {
		return nil, 0, errors.Wrapf(err, "get object attributes of %s", indexFilepath)
	}

	rc, err := bkt.GetRange(ctx, indexFilepath, 0, index.HeaderLen)
	if err != nil {
		return nil, 0, errors.Wrapf(err, "get TOC from object storage of %s", indexFilepath)
	}

	b, err := ioutil.ReadAll(rc)
	if err != nil {
		runutil.CloseWithErrCapture(&err, rc, "close reader")
		return nil, 0, errors.Wrapf(err, "get header from object storage of %s", indexFilepath)
	}

	if err := rc.Close(); err != nil {
		return nil, 0, errors.Wrap(err, "close reader")
	}

	if m := binary.BigEndian.Uint32(b[0:4]); m != index.MagicIndex {
		return nil, 0, errors.Errorf("invalid magic number %x for %s", m, indexFilepath)
	}

	version := int(b[4:5][0])

	if version != index.FormatV1 && version != index.FormatV2 {
		return nil, 0, errors.Errorf("not supported index file version %d of %s", version, indexFilepath)
	}

	ir := &chunkedIndexReader{
		ctx:  ctx,
		path: indexFilepath,
		size: uint64(attrs.Size),
		bkt:  bkt,
	}

	toc, err := ir.readTOC()
	if err != nil {
		return nil, 0, err
	}
	ir.toc = toc

	return ir, version, nil
}

func (r *chunkedIndexReader) readTOC() (*index.TOC, error) {
	rc, err := r.bkt.GetRange(r.ctx, r.path, int64(r.size-indexTOCLen-crc32.Size), indexTOCLen+crc32.Size)
	if err != nil {
		return nil, errors.Wrapf(err, "get TOC from object storage of %s", r.path)
	}

	tocBytes, err := ioutil.ReadAll(rc)
	if err != nil {
		runutil.CloseWithErrCapture(&err, rc, "close toc reader")
		return nil, errors.Wrapf(err, "get TOC from object storage of %s", r.path)
	}

	if err := rc.Close(); err != nil {
		return nil, errors.Wrap(err, "close toc reader")
	}

	toc, err := index.NewTOCFromByteSlice(realByteSlice(tocBytes))
	if err != nil {
		return nil, errors.Wrap(err, "new TOC")
	}
	return toc, nil
}

func (r *chunkedIndexReader) CopySymbols(w io.Writer, buf []byte) (err error) {
	rc, err := r.bkt.GetRange(r.ctx, r.path, int64(r.toc.Symbols), int64(r.toc.Series-r.toc.Symbols))
	if err != nil {
		return errors.Wrapf(err, "get symbols from object storage of %s", r.path)
	}
	defer runutil.CloseWithErrCapture(&err, rc, "close symbol reader")

	if _, err := io.CopyBuffer(w, rc, buf); err != nil {
		return errors.Wrap(err, "copy symbols")
	}

	return nil
}

func (r *chunkedIndexReader) CopyPostingsOffsets(w io.Writer, buf []byte) (err error) {
	rc, err := r.bkt.GetRange(r.ctx, r.path, int64(r.toc.PostingsTable), int64(r.size-r.toc.PostingsTable))
	if err != nil {
		return errors.Wrapf(err, "get posting offset table from object storage of %s", r.path)
	}
	defer runutil.CloseWithErrCapture(&err, rc, "close posting offsets reader")

	if _, err := io.CopyBuffer(w, rc, buf); err != nil {
		return errors.Wrap(err, "copy posting offsets")
	}

	return nil
}

// TODO(bwplotka): Add padding for efficient read.
type binaryWriter struct {
	f *FileWriter

	toc BinaryTOC

	// Reusable memory.
	buf encoding.Encbuf

	crc32 hash.Hash
}

func newBinaryWriter(fn string, buf []byte) (w *binaryWriter, err error) {
	dir := filepath.Dir(fn)

	df, err := fileutil.OpenDir(dir)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return nil, err
		}
		df, err = fileutil.OpenDir(dir)
	}
	if err != nil {

		return nil, err
	}

	defer runutil.CloseWithErrCapture(&err, df, "dir close")

	if err := os.RemoveAll(fn); err != nil {
		return nil, errors.Wrap(err, "remove any existing index at path")
	}

	// We use file writer for buffers not larger than reused one.
	f, err := NewFileWriter(fn, len(buf))
	if err != nil {
		return nil, err
	}
	if err := df.Sync(); err != nil {
		return nil, errors.Wrap(err, "sync dir")
	}

	w = &binaryWriter{
		f: f,

		// Reusable memory.
		buf:   encoding.Encbuf{B: buf},
		crc32: newCRC32(),
	}

	w.buf.Reset()
	w.buf.PutBE32(MagicIndex)
	w.buf.PutByte(BinaryFormatV1)

	return w, w.f.Write(w.buf.Get())
}

type FileWriter struct {
	f    *os.File
	fbuf *bufio.Writer
	pos  uint64
	name string
}

// TODO(bwplotka): Added size to method, upstream this.
func NewFileWriter(name string, size int) (*FileWriter, error) {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		return nil, err
	}
	return &FileWriter{
		f:    f,
		fbuf: bufio.NewWriterSize(f, size),
		pos:  0,
		name: name,
	}, nil
}

func (fw *FileWriter) Pos() uint64 {
	return fw.pos
}

func (fw *FileWriter) Write(bufs ...[]byte) error {
	for _, b := range bufs {
		n, err := fw.fbuf.Write(b)
		fw.pos += uint64(n)
		if err != nil {
			return err
		}
		// For now the index file must not grow beyond 64GiB. Some of the fixed-sized
		// offset references in v1 are only 4 bytes large.
		// Once we move to compressed/varint representations in those areas, this limitation
		// can be lifted.
		if fw.pos > 16*math.MaxUint32 {
			return errors.Errorf("%q exceeding max size of 64GiB", fw.name)
		}
	}
	return nil
}

func (fw *FileWriter) Flush() error {
	return fw.fbuf.Flush()
}

func (fw *FileWriter) WriteAt(buf []byte, pos uint64) error {
	if err := fw.Flush(); err != nil {
		return err
	}
	_, err := fw.f.WriteAt(buf, int64(pos))
	return err
}

// AddPadding adds zero byte padding until the file size is a multiple size.
func (fw *FileWriter) AddPadding(size int) error {
	p := fw.pos % uint64(size)
	if p == 0 {
		return nil
	}
	p = uint64(size) - p

	if err := fw.Write(make([]byte, p)); err != nil {
		return errors.Wrap(err, "add padding")
	}
	return nil
}

func (fw *FileWriter) Close() error {
	if err := fw.Flush(); err != nil {
		return err
	}
	if err := fw.f.Sync(); err != nil {
		return err
	}
	return fw.f.Close()
}

func (fw *FileWriter) Remove() error {
	return os.Remove(fw.name)
}

func (w *binaryWriter) AddIndexMeta(indexVersion int, indexPostingOffsetTable uint64) error {
	w.buf.Reset()
	w.buf.PutByte(byte(indexVersion))
	w.buf.PutBE64(indexPostingOffsetTable)
	return w.f.Write(w.buf.Get())
}

func (w *binaryWriter) SymbolsWriter() io.Writer {
	w.toc.Symbols = w.f.Pos()
	return w
}

func (w *binaryWriter) PostingOffsetsWriter() io.Writer {
	w.toc.PostingsOffsetTable = w.f.Pos()
	return w
}

func (w *binaryWriter) WriteTOC() error {
	w.buf.Reset()

	w.buf.PutBE64(w.toc.Symbols)
	w.buf.PutBE64(w.toc.PostingsOffsetTable)

	w.buf.PutHash(w.crc32)

	return w.f.Write(w.buf.Get())
}

func (w *binaryWriter) Write(p []byte) (int, error) {
	n := w.f.Pos()
	err := w.f.Write(p)
	return int(w.f.Pos() - n), err
}

func (w *binaryWriter) Close() error {
	return w.f.Close()
}

type postingValueOffsets struct {
	offsets       []postingOffset
	lastValOffset int64
}

type postingOffset struct {
	// label value.
	value string
	// offset of this entry in posting offset table in index-header file.
	tableOff int
}

const valueSymbolsCacheSize = 1024

type BinaryReader struct {
	b   index.ByteSlice
	toc *BinaryTOC

	// Close that releases the underlying resources of the byte slice.
	c io.Closer

	// Map of LabelName to a list of some LabelValues's position in the offset table.
	// The first and last values for each name are always present, we keep only 1/postingOffsetsInMemSampling of the rest.
	postings map[string]*postingValueOffsets
	// For the v1 format, labelname -> labelvalue -> offset.
	postingsV1 map[string]map[string]index.Range

	// Symbols struct that keeps only 1/postingOffsetsInMemSampling in the memory, then looks up the rest via mmap.
	symbols *index.Symbols
	// Cache of the label name symbol lookups,
	// as there are not many and they are half of all lookups.
	nameSymbols map[uint32]string
	// Direct cache of values. This is much faster than an LRU cache and still provides
	// a reasonable cache hit ratio.
	valueSymbolsMx sync.Mutex
	valueSymbols   [valueSymbolsCacheSize]struct {
		index  uint32
		symbol string
	}

	dec *index.Decoder

	version             int
	indexVersion        int
	indexLastPostingEnd int64

	postingOffsetsInMemSampling int
}

// NewBinaryReader loads or builds new index-header if not present on disk.
func NewBinaryReader(ctx context.Context, logger log.Logger, bkt objstore.BucketReader, dir string, id ulid.ULID, postingOffsetsInMemSampling int) (*BinaryReader, error) {
	binfn := filepath.Join(dir, id.String(), block.IndexHeaderFilename)
	br, err := newFileBinaryReader(binfn, postingOffsetsInMemSampling)
	if err == nil {
		return br, nil
	}

	level.Debug(logger).Log("msg", "failed to read index-header from disk; recreating", "path", binfn, "err", err)

	start := time.Now()
	if err := WriteBinary(ctx, bkt, id, binfn); err != nil {
		return nil, errors.Wrap(err, "write index header")
	}

	level.Debug(logger).Log("msg", "built index-header file", "path", binfn, "elapsed", time.Since(start))
	return newFileBinaryReader(binfn, postingOffsetsInMemSampling)
}

func newFileBinaryReader(path string, postingOffsetsInMemSampling int) (bw *BinaryReader, err error) {
	f, err := fileutil.OpenMmapFile(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			runutil.CloseWithErrCapture(&err, f, "index header close")
		}
	}()

	r := &BinaryReader{
		b:                           realByteSlice(f.Bytes()),
		c:                           f,
		postings:                    map[string]*postingValueOffsets{},
		postingOffsetsInMemSampling: postingOffsetsInMemSampling,
	}

	// Verify header.
	if r.b.Len() < headerLen {
		return nil, errors.Wrap(encoding.ErrInvalidSize, "index header's header")
	}
	if m := binary.BigEndian.Uint32(r.b.Range(0, 4)); m != MagicIndex {
		return nil, errors.Errorf("invalid magic number %x", m)
	}
	r.version = int(r.b.Range(4, 5)[0])
	r.indexVersion = int(r.b.Range(5, 6)[0])

	r.indexLastPostingEnd = int64(binary.BigEndian.Uint64(r.b.Range(6, headerLen)))

	if r.version != BinaryFormatV1 {
		return nil, errors.Errorf("unknown index header file version %d", r.version)
	}

	r.toc, err = newBinaryTOCFromByteSlice(r.b)
	if err != nil {
		return nil, errors.Wrap(err, "read index header TOC")
	}

	// TODO(bwplotka): Consider contributing to Prometheus to allow specifying custom number for symbolsFactor.
	r.symbols, err = index.NewSymbols(r.b, r.indexVersion, int(r.toc.Symbols))
	if err != nil {
		return nil, errors.Wrap(err, "read symbols")
	}

	var lastKey []string
	if r.indexVersion == index.FormatV1 {
		// Earlier V1 formats don't have a sorted postings offset table, so
		// load the whole offset table into memory.
		r.postingsV1 = map[string]map[string]index.Range{}

		var prevRng index.Range
		if err := index.ReadOffsetTable(r.b, r.toc.PostingsOffsetTable, func(key []string, off uint64, _ int) error {
			if len(key) != 2 {
				return errors.Errorf("unexpected key length for posting table %d", len(key))
			}

			if lastKey != nil {
				prevRng.End = int64(off - crc32.Size)
				r.postingsV1[lastKey[0]][lastKey[1]] = prevRng
			}

			if _, ok := r.postingsV1[key[0]]; !ok {
				r.postingsV1[key[0]] = map[string]index.Range{}
				r.postings[key[0]] = nil // Used to get a list of labelnames in places.
			}

			lastKey = key
			prevRng = index.Range{Start: int64(off + postingLengthFieldSize)}
			return nil
		}); err != nil {
			return nil, errors.Wrap(err, "read postings table")
		}
		if lastKey != nil {
			prevRng.End = r.indexLastPostingEnd - crc32.Size
			r.postingsV1[lastKey[0]][lastKey[1]] = prevRng
		}
	} else {
		lastTableOff := 0
		valueCount := 0

		// For the postings offset table we keep every label name but only every nth
		// label value (plus the first and last one), to save memory.
		if err := index.ReadOffsetTable(r.b, r.toc.PostingsOffsetTable, func(key []string, off uint64, tableOff int) error {
			if len(key) != 2 {
				return errors.Errorf("unexpected key length for posting table %d", len(key))
			}

			if _, ok := r.postings[key[0]]; !ok {
				// Not seen before label name.
				r.postings[key[0]] = &postingValueOffsets{}
				if lastKey != nil {
					// Always include last value for each label name, unless it was just added in previous iteration based
					// on valueCount.
					if (valueCount-1)%postingOffsetsInMemSampling != 0 {
						r.postings[lastKey[0]].offsets = append(r.postings[lastKey[0]].offsets, postingOffset{value: lastKey[1], tableOff: lastTableOff})
					}
					r.postings[lastKey[0]].lastValOffset = int64(off - crc32.Size)
					lastKey = nil
				}
				valueCount = 0
			}

			lastKey = key
			lastTableOff = tableOff
			valueCount++

			if (valueCount-1)%postingOffsetsInMemSampling == 0 {
				r.postings[key[0]].offsets = append(r.postings[key[0]].offsets, postingOffset{value: key[1], tableOff: tableOff})
			}

			return nil
		}); err != nil {
			return nil, errors.Wrap(err, "read postings table")
		}
		if lastKey != nil {
			if (valueCount-1)%postingOffsetsInMemSampling != 0 {
				// Always include last value for each label name if not included already based on valueCount.
				r.postings[lastKey[0]].offsets = append(r.postings[lastKey[0]].offsets, postingOffset{value: lastKey[1], tableOff: lastTableOff})
			}
			// In any case lastValOffset is unknown as don't have next posting anymore. Guess from TOC table.
			// In worst case we will overfetch a few bytes.
			r.postings[lastKey[0]].lastValOffset = r.indexLastPostingEnd - crc32.Size
		}
		// Trim any extra space in the slices.
		for k, v := range r.postings {
			l := make([]postingOffset, len(v.offsets))
			copy(l, v.offsets)
			r.postings[k].offsets = l
		}
	}

	r.nameSymbols = make(map[uint32]string, len(r.postings))
	for k := range r.postings {
		if k == "" {
			continue
		}
		off, err := r.symbols.ReverseLookup(k)
		if err != nil {
			return nil, errors.Wrap(err, "reverse symbol lookup")
		}
		r.nameSymbols[off] = k
	}

	r.dec = &index.Decoder{LookupSymbol: r.LookupSymbol}

	return r, nil
}

// newBinaryTOCFromByteSlice return parsed TOC from given index header byte slice.
func newBinaryTOCFromByteSlice(bs index.ByteSlice) (*BinaryTOC, error) {
	if bs.Len() < binaryTOCLen {
		return nil, encoding.ErrInvalidSize
	}
	b := bs.Range(bs.Len()-binaryTOCLen, bs.Len())

	expCRC := binary.BigEndian.Uint32(b[len(b)-4:])
	d := encoding.Decbuf{B: b[:len(b)-4]}

	if d.Crc32(castagnoliTable) != expCRC {
		return nil, errors.Wrap(encoding.ErrInvalidChecksum, "read index header TOC")
	}

	if err := d.Err(); err != nil {
		return nil, err
	}

	return &BinaryTOC{
		Symbols:             d.Be64(),
		PostingsOffsetTable: d.Be64(),
	}, nil
}

func (r *BinaryReader) IndexVersion() (int, error) {
	return r.indexVersion, nil
}

// TODO(bwplotka): Get advantage of multi value offset fetch.
func (r *BinaryReader) PostingsOffset(name string, value string) (index.Range, error) {
	rngs, err := r.postingsOffset(name, value)
	if err != nil {
		return index.Range{}, err
	}
	if len(rngs) != 1 {
		return index.Range{}, NotFoundRangeErr
	}
	return rngs[0], nil
}

func skipNAndName(d *encoding.Decbuf, buf *int) {
	if *buf == 0 {
		// Keycount+LabelName are always the same number of bytes,
		// and it's faster to skip than parse.
		*buf = d.Len()
		d.Uvarint()      // Keycount.
		d.UvarintBytes() // Label name.
		*buf -= d.Len()
		return
	}
	d.Skip(*buf)
}
func (r *BinaryReader) postingsOffset(name string, values ...string) ([]index.Range, error) {
	rngs := make([]index.Range, 0, len(values))
	if r.indexVersion == index.FormatV1 {
		e, ok := r.postingsV1[name]
		if !ok {
			return nil, nil
		}
		for _, v := range values {
			rng, ok := e[v]
			if !ok {
				continue
			}
			rngs = append(rngs, rng)
		}
		return rngs, nil
	}

	e, ok := r.postings[name]
	if !ok {
		return nil, nil
	}

	if len(values) == 0 {
		return nil, nil
	}

	buf := 0
	valueIndex := 0
	for valueIndex < len(values) && values[valueIndex] < e.offsets[0].value {
		// Discard values before the start.
		valueIndex++
	}

	var newSameRngs []index.Range // The start, end offsets in the postings table in the original index file.
	for valueIndex < len(values) {
		wantedValue := values[valueIndex]

		i := sort.Search(len(e.offsets), func(i int) bool { return e.offsets[i].value >= wantedValue })
		if i == len(e.offsets) {
			// We're past the end.
			break
		}
		if i > 0 && e.offsets[i].value != wantedValue {
			// Need to look from previous entry.
			i--
		}

		// Don't Crc32 the entire postings offset table, this is very slow
		// so hope any issues were caught at startup.
		d := encoding.NewDecbufAt(r.b, int(r.toc.PostingsOffsetTable), nil)
		d.Skip(e.offsets[i].tableOff)

		// Iterate on the offset table.
		newSameRngs = newSameRngs[:0]
		for d.Err() == nil {
			// Posting format entry is as follows:
			// │ ┌────────────────────────────────────────┐ │
			// │ │  n = 2 <1b>                            │ │
			// │ ├──────────────────────┬─────────────────┤ │
			// │ │ len(name) <uvarint>  │ name <bytes>    │ │
			// │ ├──────────────────────┼─────────────────┤ │
			// │ │ len(value) <uvarint> │ value <bytes>   │ │
			// │ ├──────────────────────┴─────────────────┤ │
			// │ │  offset <uvarint64>                    │ │
			// │ └────────────────────────────────────────┘ │
			// First, let's skip n and name.
			skipNAndName(&d, &buf)
			value := d.UvarintBytes() // Label value.
			postingOffset := int64(d.Uvarint64())

			if len(newSameRngs) > 0 {
				// We added some ranges in previous iteration. Use next posting offset as end of all our new ranges.
				for j := range newSameRngs {
					newSameRngs[j].End = postingOffset - crc32.Size
				}
				rngs = append(rngs, newSameRngs...)
				newSameRngs = newSameRngs[:0]
			}

			for string(value) >= wantedValue {
				// If wantedValue is equals of greater than current value, loop over all given wanted values in the values until
				// this is no longer true or there are no more values wanted.
				// This ensures we cover case when someone asks for postingsOffset(name, value1, value1, value1).

				// Record on the way if wanted value is equal to the current value.
				if string(value) == wantedValue {
					newSameRngs = append(newSameRngs, index.Range{Start: postingOffset + postingLengthFieldSize})
				}
				valueIndex++
				if valueIndex == len(values) {
					break
				}
				wantedValue = values[valueIndex]
			}

			if i+1 == len(e.offsets) {
				// No more offsets for this name.
				// Break this loop and record lastOffset on the way for ranges we just added if any.
				for j := range newSameRngs {
					newSameRngs[j].End = e.lastValOffset
				}
				rngs = append(rngs, newSameRngs...)
				break
			}

			if valueIndex != len(values) && wantedValue <= e.offsets[i+1].value {
				// wantedValue is smaller or same as the next offset we know about, let's iterate further to add those.
				continue
			}

			// Nothing wanted or wantedValue is larger than next offset we know about.
			// Let's exit and do binary search again / exit if nothing wanted.

			if len(newSameRngs) > 0 {
				// We added some ranges in this iteration. Use next posting offset as the end of our ranges.
				// We know it exists as we never go further in this loop than e.offsets[i, i+1].

				skipNAndName(&d, &buf)
				d.UvarintBytes() // Label value.
				postingOffset := int64(d.Uvarint64())

				for j := range newSameRngs {
					newSameRngs[j].End = postingOffset - crc32.Size
				}
				rngs = append(rngs, newSameRngs...)
			}
			break
		}
		if d.Err() != nil {
			return nil, errors.Wrap(d.Err(), "get postings offset entry")
		}
	}

	return rngs, nil
}

func (r *BinaryReader) LookupSymbol(o uint32) (string, error) {
	cacheIndex := o % valueSymbolsCacheSize
	r.valueSymbolsMx.Lock()
	if cached := r.valueSymbols[cacheIndex]; cached.index == o && cached.symbol != "" {
		v := cached.symbol
		r.valueSymbolsMx.Unlock()
		return v, nil
	}
	r.valueSymbolsMx.Unlock()

	if s, ok := r.nameSymbols[o]; ok {
		return s, nil
	}

	if r.indexVersion == index.FormatV1 {
		// For v1 little trick is needed. Refs are actual offset inside index, not index-header. This is different
		// of the header length difference between two files.
		o += headerLen - index.HeaderLen
	}

	s, err := r.symbols.Lookup(o)
	if err != nil {
		return s, err
	}

	r.valueSymbolsMx.Lock()
	r.valueSymbols[cacheIndex].index = o
	r.valueSymbols[cacheIndex].symbol = s
	r.valueSymbolsMx.Unlock()

	return s, nil
}

func (r *BinaryReader) LabelValues(name string) ([]string, error) {
	if r.indexVersion == index.FormatV1 {
		e, ok := r.postingsV1[name]
		if !ok {
			return nil, nil
		}
		values := make([]string, 0, len(e))
		for k := range e {
			values = append(values, k)
		}
		sort.Strings(values)
		return values, nil

	}
	e, ok := r.postings[name]
	if !ok {
		return nil, nil
	}
	if len(e.offsets) == 0 {
		return nil, nil
	}
	values := make([]string, 0, len(e.offsets)*r.postingOffsetsInMemSampling)

	d := encoding.NewDecbufAt(r.b, int(r.toc.PostingsOffsetTable), nil)
	d.Skip(e.offsets[0].tableOff)
	lastVal := e.offsets[len(e.offsets)-1].value

	skip := 0
	for d.Err() == nil {
		if skip == 0 {
			// These are always the same number of bytes,
			// and it's faster to skip than parse.
			skip = d.Len()
			d.Uvarint()      // Keycount.
			d.UvarintBytes() // Label name.
			skip -= d.Len()
		} else {
			d.Skip(skip)
		}
		s := yoloString(d.UvarintBytes()) // Label value.
		values = append(values, s)
		if s == lastVal {
			break
		}
		d.Uvarint64() // Offset.
	}
	if d.Err() != nil {
		return nil, errors.Wrap(d.Err(), "get postings offset entry")
	}
	return values, nil
}

func yoloString(b []byte) string {
	return *((*string)(unsafe.Pointer(&b)))
}

func (r *BinaryReader) LabelNames() ([]string, error) {
	allPostingsKeyName, _ := index.AllPostingsKey()
	labelNames := make([]string, 0, len(r.postings))
	for name := range r.postings {
		if name == allPostingsKeyName {
			// This is not from any metric.
			continue
		}
		labelNames = append(labelNames, name)
	}
	sort.Strings(labelNames)
	return labelNames, nil
}

func (r *BinaryReader) Close() error { return r.c.Close() }

type realByteSlice []byte

func (b realByteSlice) Len() int {
	return len(b)
}

func (b realByteSlice) Range(start, end int) []byte {
	return b[start:end]
}

func (b realByteSlice) Sub(start, end int) index.ByteSlice {
	return b[start:end]
}
