package v1

import (
	"bytes"
	"hash"
	"io"

	"github.com/owen-d/BoomFilters/boom"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/util/encoding"
)

type Block struct {
	schema Schema
	header SeriesHeader             // header for the entire block
	series []SeriesHeaderWithOffset // headers for each series page
	// blooms []BloomPage              // pages of blooms referenced by series pages
}

func (b *Block) WriteTo(itr Iterator[SeriesPage], w io.Writer) (int64, error) {
	crc32Hash := Crc32HashPool.Get().(hash.Hash32)
	defer Crc32HashPool.Put(crc32Hash)

	var offset int64

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

	// encode series, accumlate encoded headers with relevant offsets,
	// they will be written at the end of the block
	for itr.Next() {
		page := itr.At()
		b.series = append(b.series, SeriesHeaderWithOffset{
			SeriesHeader: page.Header,
			Offset:       int(offset),
		})

		page.Encode(enc, crc32Hash)
		written, err := w.Write(enc.Get())
		if err != nil {
			return offset, errors.Wrap(err, "writing series page")
		}
		offset += int64(written)

	}
	if itr.Err() != nil {
		return offset, errors.Wrap(err, "iterating series")
	}

	enc.Reset()
	headerOffset := offset

	// TODO: create a header for the entire block
	// put aggregated series header

	// put number of pages
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
}

// schema
// magic num, version, encoding, are the first few bytes
// the offset of the final header detailing the series+time range
// this block covers is encoded at the end of the block
// Finally, a checksum of the entire block is the last 4 bytes
type Schema struct {
	version byte
	enc     chunkenc.Encoding // encoding algorithm
}

func (s *Schema) Encode(enc *encoding.Encbuf) {
	enc.Reset()
	enc.PutByte(s.version)
	enc.PutByte(byte(s.enc))
}

func (s *Schema) Decode(dec *encoding.Decbuf) error {
	s.version = dec.Byte()
	s.enc = chunkenc.Encoding(dec.Byte())
	return dec.Err()
}

type SeriesHeaderWithOffset struct {
	Offset int
	SeriesHeader
}

func (h *SeriesHeaderWithOffset) Encode(enc *encoding.Encbuf) {
	enc.PutUvarint(h.Offset)
	h.SeriesHeader.Encode(enc)
}

func (h *SeriesHeaderWithOffset) Decode(dec *encoding.Decbuf) error {
	h.Offset = dec.Uvarint()
	return h.SeriesHeader.Decode(dec)
}

type SeriesHeader struct {
	NumSeries         int
	FromFp, ThroughFp model.Fingerprint
	FromTs, ThroughTs int64
}

func (h *SeriesHeader) Encode(enc *encoding.Encbuf) {
	enc.PutUvarint(h.NumSeries)
	enc.PutUvarint64(uint64(h.FromFp))
	enc.PutUvarint64(uint64(h.ThroughFp))
	enc.PutVarint64(h.FromTs)
	enc.PutVarint64(h.ThroughTs)
}

func (h *SeriesHeader) Decode(dec *encoding.Decbuf) error {
	h.NumSeries = dec.Uvarint()
	h.FromFp = model.Fingerprint(dec.Uvarint64())
	h.ThroughFp = model.Fingerprint(dec.Uvarint64())
	h.FromTs = dec.Varint64()
	h.ThroughTs = dec.Varint64()
	return dec.Err()
}

// series page header
type SeriesPage struct {
	Header SeriesHeader
	Series []Series
}

func (p *SeriesPage) Encode(enc *encoding.Encbuf, crc32Hash hash.Hash32) {
	enc.Reset()

	p.Header.Encode(enc)
	var lastSeries model.Fingerprint
	for _, series := range p.Series {
		lastSeries = series.Encode(enc, lastSeries)
	}

	enc.PutHash(crc32Hash)
}

func (p *SeriesPage) Decode(dec *encoding.Decbuf) error {

	if err := dec.CheckCrc(castagnoliTable); err != nil {
		return errors.Wrap(err, "decoding series page")
	}
	if err := p.Header.Decode(dec); err != nil {
		return errors.Wrap(err, "decoding series page header")
	}

	// TODO(owen-d): pool
	p.Series = make([]Series, p.Header.NumSeries)
	var (
		lastFp model.Fingerprint
		err    error
	)
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

type Bloom struct {
	sbf boom.ScalableBloomFilter
}

func (b *Bloom) Encode(enc *encoding.Encbuf) error {
	// divide by 8 b/c bloom capacity is measured in bits, but we want bytes
	buf := bytes.NewBuffer(BlockPool.Get(int(b.sbf.Capacity() / 8)))

	_, err := b.sbf.WriteTo(buf)
	if err != nil {
		return errors.Wrap(err, "encoding bloom filter")
	}

	data := buf.Bytes()
	enc.PutUvarint(len(data)) // length of bloom filter
	enc.PutBytes(data)
	BlockPool.Put(data[:0]) // release to pool
	return nil
}

func (b *Bloom) Decode(dec *encoding.Decbuf) error {
	ln := dec.Uvarint()
	data := dec.Bytes(ln)

	_, err := b.sbf.ReadFrom(bytes.NewReader(data))
	if err != nil {
		return errors.Wrap(err, "decoding bloom filter")
	}

	return nil
}

type BloomPage struct {
	N      int
	Blooms []Bloom
}

func (p *BloomPage) Encode(enc *encoding.Encbuf, crc32Hash hash.Hash32) error {
	enc.Reset()
	enc.PutUvarint(p.N)

	for i, bloom := range p.Blooms {
		if err := bloom.Encode(enc); err != nil {
			return errors.Wrapf(err, "encoding %dth bloom filter", i)
		}
	}

	enc.PutHash(crc32Hash)
	return nil
}

func (p *BloomPage) Decode(dec *encoding.Decbuf) error {
	if err := dec.CheckCrc(castagnoliTable); err != nil {
		return errors.Wrap(err, "decoding bloom page")
	}

	p.N = dec.Uvarint()
	// TODO(owen-d): pool
	p.Blooms = make([]Bloom, p.N)
	for i := 0; i < p.N; i++ {
		if err := p.Blooms[i].Decode(dec); err != nil {
			return errors.Wrapf(err, "decoding %dth bloom filter", i)
		}
	}
	return nil
}
