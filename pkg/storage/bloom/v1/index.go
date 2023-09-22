package v1

import (
	"hash"
	"io"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/util/encoding"
)

// Block index is a set of series pages along with the headers for each page
// and schema information for the entire block
// It is the entrypoint for reading and writing blocks
type BlockIndex struct {
	schema Schema
	series []SeriesHeaderWithOffset // headers for each series page
}

func NewBlockIndex() BlockIndex {
	return BlockIndex{
		schema: Schema{version: 1},
	}
}

func (b *BlockIndex) WriteTo(itr Iterator[SeriesPage], w io.Writer) (int64, error) {
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

	return offset, nil
}

func (b *BlockIndex) Decode(data []byte) error {
	dec := encoding.DecWith(data)
	if err := b.schema.Decode(&dec); err != nil {
		return errors.Wrap(err, "decoding schema")
	}

	// last 12 bytes are (series_lengths: 8 byte u64, checksum: 4 byte u32)
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

// schema
// magic num, version, encoding, are the first few bytes
// the offset of the final header detailing the series+time range
// this block covers is encoded at the end of the block
// Finally, a checksum of the entire block is the last 4 bytes
type Schema struct {
	version byte
}

func (s *Schema) Encode(enc *encoding.Encbuf) {
	enc.Reset()
	enc.PutBE32(magicNumber)
	enc.PutByte(s.version)
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
