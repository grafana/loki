package v1

import (
	"bytes"
	"hash"
	"io"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/util/encoding"
	"github.com/pkg/errors"
)

// SerieResolver iterates two sequences, one from blocks which have already been indexed
// into blooms and another from TSDBs. The series+chunks which exist in the TSDBs but not the
// blocks need to then be indexed into the blocks.
// type SeriesResolver struct {
// 	fromBlocks Iterator[Series]
// 	fromTSDBs  Iterator[Series]
// }

type BlockOptions struct {
	schema Schema

	// target size in bytes (decompressed)
	// of each page type
	SeriesPageSize, BloomPageSize int
}

type BlockBuilder struct {
	opts BlockOptions

	// index  IndexBuilder
	blooms *BloomBlockBuilder
}

func NewBlockBuilder(opts BlockOptions, index, blooms io.WriteCloser) *BlockBuilder {
	return &BlockBuilder{
		opts: opts,
		// index:  NewIndexBuilder(opts, index),
		blooms: NewBloomBlockBuilder(opts, blooms),
	}
}

type SeriesWithBloom struct {
	Series *Series
	Bloom  *Bloom
}

func (b *BlockBuilder) WriteTo(itr Iterator[SeriesWithBloom]) error {
	for itr.Next() {
		series := itr.At()

		_, err := b.blooms.Append(series)
		if err != nil {
			return errors.Wrapf(err, "writing bloom for series %v", series.Series.Fingerprint)
		}

		// if err := b.index.Write(series, offset); err != nil {
		// 	return errors.Wrapf(err, "writing index for series %v", series.Series.Fingerprint)
		// }

	}
	return itr.Err()
}

type BloomBlockBuilder struct {
	opts   BlockOptions
	writer io.WriteCloser

	offset        int // track the offset of the file
	writtenSchema bool
	pages         []BloomPageHeader
	page          BloomPageWriter
	scratch       *encoding.Encbuf
}

func NewBloomBlockBuilder(opts BlockOptions, writer io.WriteCloser) *BloomBlockBuilder {
	return &BloomBlockBuilder{
		opts:    opts,
		writer:  writer,
		page:    NewBloomPageWriter(opts.BloomPageSize),
		scratch: &encoding.Encbuf{},
	}
}

func (b *BloomBlockBuilder) WriteSchema() error {
	if b.writtenSchema {
		return nil
	}

	b.scratch.Reset()
	b.opts.schema.Encode(b.scratch)
	if _, err := b.writer.Write(b.scratch.Get()); err != nil {
		return errors.Wrap(err, "writing schema")
	}
	b.writtenSchema = true
	b.offset += b.scratch.Len()
	return nil
}

func (b *BloomBlockBuilder) Append(series SeriesWithBloom) (BloomOffset, error) {
	if !b.writtenSchema {
		if err := b.WriteSchema(); err != nil {
			return BloomOffset{}, errors.Wrap(err, "writing schema")
		}
	}

	b.scratch.Reset()
	if err := series.Bloom.Encode(b.scratch); err != nil {
		return BloomOffset{}, errors.Wrapf(err, "encoding bloom for %v", series.Series.Fingerprint)
	}

	if !b.page.SpaceFor(b.scratch.Len()) {
		if err := b.flushPage(); err != nil {
			return BloomOffset{}, errors.Wrap(err, "flushing bloom page")
		}
	}

	return BloomOffset{
		Page:       len(b.pages),
		ByteOffset: b.page.Add(b.scratch.Get()),
	}, nil
}

func (b *BloomBlockBuilder) Close() error {
	if b.page.Count() > 0 {
		if err := b.flushPage(); err != nil {
			return errors.Wrap(err, "flushing final bloom page")
		}
	}

	b.scratch.Reset()
	b.scratch.PutUvarint(len(b.pages))
	for _, h := range b.pages {
		h.Encode(b.scratch)
	}
	// put offset to beginning of header section
	// cannot be varint encoded because it's offset will be calculated as
	// the 8 bytes prior to the checksum
	b.scratch.PutBE64(uint64(b.offset))

	crc32Hash := Crc32HashPool.Get().(hash.Hash32)
	defer Crc32HashPool.Put(crc32Hash)
	// wrap with final checksum
	b.scratch.PutHash(crc32Hash)
	_, err := b.writer.Write(b.scratch.Get())
	if err != nil {
		return errors.Wrap(err, "writing bloom page headers")
	}
	return nil
}

func (b *BloomBlockBuilder) flushPage() error {
	crc32Hash := Crc32HashPool.Get().(hash.Hash32)
	defer Crc32HashPool.Put(crc32Hash)

	decompressedLen, compressedLen, err := b.page.writePage(
		b.writer,
		b.opts.schema.CompressorPool(),
		crc32Hash,
	)
	if err != nil {
		return errors.Wrap(err, "writing bloom page")
	}
	header := BloomPageHeader{
		N:               b.page.Count(),
		Offset:          int(b.offset),
		Len:             compressedLen,
		DecompressedLen: decompressedLen,
	}
	b.pages = append(b.pages, header)
	b.offset += compressedLen
	b.page.Reset()
	return nil
}

type BloomPageWriter struct {
	enc        *encoding.Encbuf
	targetSize int
	n          int // number of encoded blooms
}

func NewBloomPageWriter(targetSize int) BloomPageWriter {
	return BloomPageWriter{
		enc:        &encoding.Encbuf{},
		targetSize: targetSize,
	}
}

func (w *BloomPageWriter) Count() int {
	return w.n
}

func (w *BloomPageWriter) Reset() {
	w.enc.Reset()
	w.n = 0
}

func (w *BloomPageWriter) SpaceFor(numBytes int) bool {
	// if a single bloom exceeds the target size, still accept it
	// otherwise only accept it if adding it would not exceed the target size
	return w.n == 0 || w.enc.Len()+numBytes <= w.targetSize
}

func (w *BloomPageWriter) Add(bloom []byte) (offset int) {
	offset = w.enc.Len()
	w.enc.PutBytes(bloom)
	w.n++
	return offset
}

func (w *BloomPageWriter) writePage(writer io.Writer, pool chunkenc.WriterPool, crc32Hash hash.Hash32) (int, int, error) {
	// write the number of blooms in this page, must not be varint
	// so we can calculate it's position+len during decoding
	w.enc.PutBE64(uint64(w.n))
	decompressedLen := w.enc.Len()

	buf := &bytes.Buffer{}
	compressor := pool.GetWriter(buf)
	defer pool.PutWriter(compressor)

	if _, err := compressor.Write(w.enc.Get()); err != nil {
		return 0, 0, errors.Wrap(err, "compressing bloom page")
	}

	if err := compressor.Close(); err != nil {
		return 0, 0, errors.Wrap(err, "closing compressor")
	}

	// replace the encoded series page with the compressed one
	w.enc.B = buf.Bytes()
	w.enc.PutHash(crc32Hash)

	// write the page
	if _, err := writer.Write(w.enc.Get()); err != nil {
		return 0, 0, errors.Wrap(err, "writing bloom page")
	}
	return decompressedLen, w.enc.Len(), nil
}
