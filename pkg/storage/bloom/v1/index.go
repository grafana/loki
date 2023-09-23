package v1

import (
	"bytes"
	"hash"
	"io"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/util/encoding"
)

// Block index is a set of series pages along with the headers for each page
// and schema information for the entire block
// It is the entrypoint for reading and writing blocks
type BlockIndex struct {
	schema Schema
	series []SeriesHeaderWithOffset // headers for each series page
}

func NewBlockIndex(encoding chunkenc.Encoding) BlockIndex {
	return BlockIndex{
		schema: Schema{version: 1, encoding: encoding},
	}
}

func (b *BlockIndex) WriteTo(itr Iterator[SeriesPage], w io.Writer) (offset int64, err error) {
	crc32Hash := Crc32HashPool.Get().(hash.Hash32)
	defer Crc32HashPool.Put(crc32Hash)

	// TODO(owen-d): guess sizing better
	encoder := encoding.EncWith(BlockPool.Get(1 << 10))
	enc := &encoder

	// encode schema
	b.schema.Encode(enc)
	written, err := w.Write(enc.Get())
	if err != nil {
		return offset, errors.Wrap(err, "writing schema")
	}
	offset += int64(written)

	compressionPool := chunkenc.GetWriterPool(b.schema.encoding)
	// encode series, accumlate encoded headers with relevant offsets,
	// they will be written at the end of the block
	for itr.Next() {
		page := itr.At()
		header := SeriesHeaderWithOffset{
			SeriesHeader: page.Header,
			Offset:       int(offset),
		}

		// we encode the decompressed length of the page so we can efficiently choose
		// []byte sizes from a pool during decompression to minimize alloc costs
		header.DecompressedLen, err = page.Encode(enc, compressionPool, crc32Hash)
		if err != nil {
			return offset, errors.Wrap(err, "encoding series page")
		}
		written, err := w.Write(enc.Get())
		if err != nil {
			return 0, errors.Wrap(err, "writing series page")
		}
		offset += int64(written)
		header.Len = int(offset) - header.Offset
		b.series = append(b.series, header)
	}

	if itr.Err() != nil {
		return offset, errors.Wrap(err, "iterating series")
	}

	enc.Reset()
	headerOffset := offset
	enc.PutUvarint(len(b.series))
	for _, s := range b.series {
		s.Encode(enc)
	}

	// put offset to beginning of header section
	// cannot be varint encoded because it's offset will be calculated as
	// the 8 bytes prior to the checksum
	enc.PutBE64(uint64(headerOffset))

	// wrap with final checksum
	enc.PutHash(crc32Hash)

	written, err = w.Write(enc.Get())
	if err != nil {
		return offset, errors.Wrap(err, "writing series page")
	}
	offset += int64(written)

	return offset, nil
}

func (b *BlockIndex) Decode(data []byte) error {
	dec := encoding.DecWith(data)
	if err := b.schema.Decode(&dec); err != nil {
		return errors.Wrap(err, "decoding schema")
	}

	// last 12 bytes are (headers offset: 8 byte u64, checksum: 4 byte u32)
	dec.Skip(dec.Len() - 12)
	headerOffset := dec.Be64()

	dec = encoding.DecWith(data[headerOffset:])
	if err := dec.CheckCrc(castagnoliTable); err != nil {
		return errors.Wrap(err, "decoding series header")
	}

	b.series = make([]SeriesHeaderWithOffset, dec.Uvarint())
	for i := 0; i < len(b.series); i++ {
		var s SeriesHeaderWithOffset
		if err := s.Decode(&dec); err != nil {
			return errors.Wrapf(err, "decoding %dth series header", i)
		}
		b.series[i] = s
	}

	return nil
}

type Schema struct {
	version  byte
	encoding chunkenc.Encoding
}

func (s *Schema) Encode(enc *encoding.Encbuf) {
	enc.Reset()
	enc.PutBE32(magicNumber)
	enc.PutByte(s.version)
	enc.PutByte(byte(s.encoding))
}

func (s *Schema) Decode(dec *encoding.Decbuf) error {
	number := dec.Be32()
	if number != magicNumber {
		return errors.Errorf("invalid magic number. expected %x, got  %x", magicNumber, number)
	}
	s.version = dec.Byte()
	if s.version != 1 {
		return errors.Errorf("invalid version. expected %d, got %d", 1, s.version)
	}

	s.encoding = chunkenc.Encoding(dec.Byte())
	if _, err := chunkenc.ParseEncoding(s.encoding.String()); err != nil {
		return errors.Wrap(err, "parsing encoding")
	}

	return dec.Err()
}

type SeriesHeaderWithOffset struct {
	Offset, Len, DecompressedLen int
	SeriesHeader
}

func (h *SeriesHeaderWithOffset) Encode(enc *encoding.Encbuf) {
	enc.PutUvarint(h.Offset)
	enc.PutUvarint(h.Len)
	enc.PutUvarint(h.DecompressedLen)
	h.SeriesHeader.Encode(enc)
}

func (h *SeriesHeaderWithOffset) Decode(dec *encoding.Decbuf) error {
	h.Offset = dec.Uvarint()
	h.Len = dec.Uvarint()
	h.DecompressedLen = dec.Uvarint()
	return h.SeriesHeader.Decode(dec)
}

type SeriesHeader struct {
	NumSeries         int
	FromFp, ThroughFp model.Fingerprint
	FromTs, ThroughTs model.Time
}

// build one aggregated header for the entire block
func aggregateHeaders(xs []SeriesHeader) SeriesHeader {
	if len(xs) == 0 {
		return SeriesHeader{}
	}

	res := SeriesHeader{
		FromFp:    xs[0].FromFp,
		ThroughFp: xs[len(xs)-1].ThroughFp,
	}

	for _, x := range xs {
		if x.FromTs < res.FromTs {
			res.FromTs = x.FromTs
		}
		if x.ThroughTs > res.ThroughTs {
			res.ThroughTs = x.ThroughTs
		}
	}
	return res
}

func (h *SeriesHeader) Encode(enc *encoding.Encbuf) {
	enc.PutUvarint(h.NumSeries)
	enc.PutUvarint64(uint64(h.FromFp))
	enc.PutUvarint64(uint64(h.ThroughFp))
	enc.PutVarint64(int64(h.FromTs))
	enc.PutVarint64(int64(h.ThroughTs))
}

func (h *SeriesHeader) Decode(dec *encoding.Decbuf) error {
	h.NumSeries = dec.Uvarint()
	h.FromFp = model.Fingerprint(dec.Uvarint64())
	h.ThroughFp = model.Fingerprint(dec.Uvarint64())
	h.FromTs = model.Time(dec.Varint64())
	h.ThroughTs = model.Time(dec.Varint64())
	return dec.Err()
}

// series page header
type SeriesPage struct {
	Header SeriesHeader
	Series []Series
}

func (p *SeriesPage) Encode(enc *encoding.Encbuf, pool chunkenc.WriterPool, crc32Hash hash.Hash32) (decompressedLen int, err error) {
	enc.Reset()

	// TODO(owen-d): no need to write to a temp buffer then rewrite to a compressed one;
	// should build a streaming encoder/decoder, although we will need to know the true
	// number of bytes written to calculate offsets (compression changes this)
	// temp buffer for compression
	// TODO(owen-d): pool
	buf := &bytes.Buffer{}

	p.Header.Encode(enc)
	var lastSeries model.Fingerprint
	for _, series := range p.Series {
		lastSeries = series.Encode(enc, lastSeries)
	}
	decompressedLen = enc.Len()

	compressor := pool.GetWriter(buf)
	defer pool.PutWriter(compressor)

	if _, err := compressor.Write(enc.Get()); err != nil {
		return 0, errors.Wrap(err, "compressing series page")
	}

	if err := compressor.Close(); err != nil {
		return 0, errors.Wrap(err, "closing compressor")
	}

	// replace the encoded series page with the compressed one
	enc.B = buf.Bytes()
	enc.PutHash(crc32Hash)
	return decompressedLen, nil
}

func (p *SeriesPage) Decode(dec *encoding.Decbuf, pool chunkenc.ReaderPool, decompressedSize int) error {

	if err := dec.CheckCrc(castagnoliTable); err != nil {
		return errors.Wrap(err, "checksumming series page")
	}

	decompressor, err := pool.GetReader(bytes.NewReader(dec.Get()))
	if err != nil {
		return errors.Wrap(err, "getting decompressor")
	}

	b := BlockPool.Get(decompressedSize)[:decompressedSize]
	defer BlockPool.Put(b)
	if _, err = io.ReadFull(decompressor, b); err != nil {
		return errors.Wrap(err, "decompressing series page")
	}

	// replace decoder's input with the now-decompressed data
	dec.B = b

	if err := p.Header.Decode(dec); err != nil {
		return errors.Wrap(err, "decoding series page header")
	}

	// TODO(owen-d): pool
	p.Series = make([]Series, p.Header.NumSeries)
	var lastFp model.Fingerprint
	for i := 0; i < p.Header.NumSeries; i++ {
		series := &p.Series[i]
		if lastFp, err = series.Decode(dec, lastFp); err != nil {
			return errors.Wrapf(err, "decoding %dth series", i)
		}
	}
	return dec.Err()
}

type Series struct {
	Fingerprint model.Fingerprint
	Offset      BloomOffset
	Chunks      []ChunkRef
}

func (s *Series) Encode(enc *encoding.Encbuf, previousFp model.Fingerprint) model.Fingerprint {
	// delta encode fingerprint
	enc.PutBE64(uint64(s.Fingerprint - previousFp))
	// encode offsets
	s.Offset.Encode(enc)

	// encode chunks using delta encoded timestamps
	var lastEnd model.Time
	enc.PutUvarint(len(s.Chunks))
	for _, chunk := range s.Chunks {
		lastEnd = chunk.Encode(enc, lastEnd)
	}

	return s.Fingerprint
}

func (s *Series) Decode(dec *encoding.Decbuf, previousFp model.Fingerprint) (model.Fingerprint, error) {
	s.Fingerprint = previousFp + model.Fingerprint(dec.Be64())
	s.Offset.Decode(dec)

	// TODO(owen-d): use pool
	s.Chunks = make([]ChunkRef, dec.Uvarint())
	var (
		err     error
		lastEnd model.Time
	)
	for i := range s.Chunks {
		lastEnd, err = s.Chunks[i].Decode(dec, lastEnd)
		if err != nil {
			return 0, errors.Wrapf(err, "decoding %dth chunk", i)
		}
	}
	return s.Fingerprint, dec.Err()
}

type ChunkRef struct {
	Start, End model.Time
	Checksum   uint32
}

func (r *ChunkRef) Encode(enc *encoding.Encbuf, previousEnd model.Time) model.Time {
	// delta encode start time
	enc.PutVarint64(int64(r.Start - previousEnd))
	enc.PutVarint64(int64(r.End - r.Start))
	enc.PutBE32(r.Checksum)
	return r.End
}

func (r *ChunkRef) Decode(dec *encoding.Decbuf, previousEnd model.Time) (model.Time, error) {
	r.Start = previousEnd + model.Time(dec.Varint64())
	r.End = r.Start + model.Time(dec.Varint64())
	r.Checksum = dec.Be32()
	return r.End, dec.Err()
}

type BloomOffset struct {
	PageOffset int // offset to beginnging of bloom page
	ByteOffset int // offset to beginning of bloom within page
}

func (o *BloomOffset) Encode(enc *encoding.Encbuf) {
	enc.PutUvarint(o.PageOffset)
	enc.PutUvarint(o.ByteOffset)
}

func (o *BloomOffset) Decode(dec *encoding.Decbuf) error {
	o.PageOffset = dec.Uvarint()
	o.ByteOffset = dec.Uvarint()
	return dec.Err()
}
