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
	schema      Schema
	pageHeaders []SeriesPageHeaderWithOffset // headers for each series page
}

func NewBlockIndex(encoding chunkenc.Encoding) BlockIndex {
	return BlockIndex{
		schema: Schema{version: DefaultSchemaVersion, encoding: encoding},
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

	compressionPool := b.schema.CompressorPool()
	// encode series, accumlate encoded headers with relevant offsets,
	// they will be written at the end of the block
	for itr.Next() {
		page := itr.At()
		header := SeriesPageHeaderWithOffset{
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
		b.pageHeaders = append(b.pageHeaders, header)
	}

	if itr.Err() != nil {
		return offset, errors.Wrap(err, "iterating series")
	}

	enc.Reset()
	headerOffset := offset
	enc.PutUvarint(len(b.pageHeaders))
	for _, s := range b.pageHeaders {
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
		return offset, errors.Wrap(err, "writing series page headers")
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
		return errors.Wrap(err, "checksumming page headers")
	}

	b.pageHeaders = make([]SeriesPageHeaderWithOffset, dec.Uvarint())
	for i := 0; i < len(b.pageHeaders); i++ {
		var s SeriesPageHeaderWithOffset
		if err := s.Decode(&dec); err != nil {
			return errors.Wrapf(err, "decoding %dth series header", i)
		}
		b.pageHeaders[i] = s
	}

	return nil
}

type Schema struct {
	version  byte
	encoding chunkenc.Encoding
}

// byte length
func (s Schema) Len() int {
	// magic number + version + encoding
	return 4 + 1 + 1
}

func (s *Schema) DecompressorPool() chunkenc.ReaderPool {
	return chunkenc.GetReaderPool(s.encoding)
}

func (s *Schema) CompressorPool() chunkenc.WriterPool {
	return chunkenc.GetWriterPool(s.encoding)
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

// Header for a series page
type SeriesPageHeaderWithOffset struct {
	Offset, Len, DecompressedLen int
	SeriesHeader
}

func (h *SeriesPageHeaderWithOffset) Encode(enc *encoding.Encbuf) {
	enc.PutUvarint(h.Offset)
	enc.PutUvarint(h.Len)
	enc.PutUvarint(h.DecompressedLen)
	h.SeriesHeader.Encode(enc)
}

func (h *SeriesPageHeaderWithOffset) Decode(dec *encoding.Decbuf) error {
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
	Series []SeriesWithOffset
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
	var (
		lastSeries model.Fingerprint
		lastOffset BloomOffset
	)
	for _, series := range p.Series {
		lastSeries, lastOffset = series.Encode(enc, lastSeries, lastOffset)
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

	decoder, err := p.DecodeLazy(dec, pool, decompressedSize)
	if err != nil {
		return errors.Wrap(err, "building series page decoder")
	}

	// TODO(owen-d): pool
	p.Series = make([]SeriesWithOffset, 0, p.Header.NumSeries)
	var i int
	for decoder.Next() {
		series, err := decoder.At()
		if err != nil {
			return errors.Wrapf(err, "decoding %dth series", i)
		}
		p.Series = append(p.Series, series)
		i++
	}
	return errors.Wrap(dec.Err(), "decoding series page")
}

// decompress page and return an iterator over the bytes
func (p *SeriesPage) DecodeLazy(dec *encoding.Decbuf, pool chunkenc.ReaderPool, decompressedSize int) (*SeriesPageDecoder, error) {
	if err := dec.CheckCrc(castagnoliTable); err != nil {
		return nil, errors.Wrap(err, "checksumming series page")
	}

	decompressor, err := pool.GetReader(bytes.NewReader(dec.Get()))
	if err != nil {
		return nil, errors.Wrap(err, "getting decompressor")
	}

	b := BlockPool.Get(decompressedSize)[:decompressedSize]
	defer BlockPool.Put(b)
	if _, err = io.ReadFull(decompressor, b); err != nil {
		return nil, errors.Wrap(err, "decompressing series page")
	}

	// replace decoder's input with the now-decompressed data
	dec.B = b

	if err := p.Header.Decode(dec); err != nil {
		return nil, errors.Wrap(err, "decoding series page header")
	}

	return NewSeriesPageDecoder(dec, p.Header), nil
}

// can decode a series page one item at a time, useful when we don't
// need to iterate an entire page
type SeriesPageDecoder struct {
	dec    *encoding.Decbuf
	header SeriesHeader

	i              int
	lastFp         model.Fingerprint
	previousOffset BloomOffset
}

func NewSeriesPageDecoder(dec *encoding.Decbuf, header SeriesHeader) *SeriesPageDecoder {
	return &SeriesPageDecoder{
		dec:    dec,
		header: header,

		i: -1,
	}
}

func (d *SeriesPageDecoder) Next() bool {
	d.i++
	return d.i < d.header.NumSeries
}

func (d *SeriesPageDecoder) At() (res SeriesWithOffset, err error) {
	d.lastFp, d.previousOffset, err = res.Decode(d.dec, d.lastFp, d.previousOffset)
	return res, err
}

type Series struct {
	Fingerprint model.Fingerprint
	Chunks      []ChunkRef
}

type SeriesWithOffset struct {
	Offset BloomOffset
	Series
}

func (s *SeriesWithOffset) Encode(
	enc *encoding.Encbuf,
	previousFp model.Fingerprint,
	previousOffset BloomOffset,
) (model.Fingerprint, BloomOffset) {
	// delta encode fingerprint
	enc.PutBE64(uint64(s.Fingerprint - previousFp))
	// delta encode offsets
	s.Offset.Encode(enc, previousOffset)

	// encode chunks using delta encoded timestamps
	var lastEnd model.Time
	enc.PutUvarint(len(s.Chunks))
	for _, chunk := range s.Chunks {
		lastEnd = chunk.Encode(enc, lastEnd)
	}

	return s.Fingerprint, s.Offset
}

func (s *SeriesWithOffset) Decode(dec *encoding.Decbuf, previousFp model.Fingerprint, previousOffset BloomOffset) (model.Fingerprint, BloomOffset, error) {
	s.Fingerprint = previousFp + model.Fingerprint(dec.Be64())
	s.Offset.Decode(dec, previousOffset)

	// TODO(owen-d): use pool
	s.Chunks = make([]ChunkRef, dec.Uvarint())
	var (
		err     error
		lastEnd model.Time
	)
	for i := range s.Chunks {
		lastEnd, err = s.Chunks[i].Decode(dec, lastEnd)
		if err != nil {
			return 0, BloomOffset{}, errors.Wrapf(err, "decoding %dth chunk", i)
		}
	}
	return s.Fingerprint, s.Offset, dec.Err()
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

func (o *BloomOffset) Encode(enc *encoding.Encbuf, previousOffset BloomOffset) {
	enc.PutUvarint(o.PageOffset - previousOffset.PageOffset)
	enc.PutUvarint(o.ByteOffset - previousOffset.ByteOffset)
}

func (o *BloomOffset) Decode(dec *encoding.Decbuf, previousOffset BloomOffset) error {
	o.PageOffset = previousOffset.PageOffset + dec.Uvarint()
	o.ByteOffset = previousOffset.ByteOffset + dec.Uvarint()
	return dec.Err()
}
