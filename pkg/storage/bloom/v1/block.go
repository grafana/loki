package v1

import (
	"bytes"

	"github.com/grafana/loki/pkg/util/encoding"
	"github.com/owen-d/BoomFilters/boom"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/util/pool"
)

// 1KB -> 8MB
var BlockPool = pool.New(1<<10, 1<<24, 4, func(size int) interface{} {
	return make([]byte, size)
})

type Block struct {
	toc    ToC
	series []SeriesPage
	blooms []BloomPage
}

// table of contents
type ToC struct {
	version byte
	header  SeriesHeader
}

type SeriesHeader struct {
	NumSeries         int
	FromFp, ThroughFp model.Fingerprint
	FromTs, ThroughTs int64
}

func (h *SeriesHeader) Encode(enc encoding.Encbuf) error {
	enc.PutUvarint(h.NumSeries)
	enc.PutUvarint64(uint64(h.FromFp))
	enc.PutUvarint64(uint64(h.ThroughFp))
	enc.PutVarint64(h.FromTs)
	enc.PutVarint64(h.ThroughTs)
	return nil
}

func (h *SeriesHeader) Decode(dec encoding.Decbuf) error {
	h.NumSeries = dec.Uvarint()
	h.FromFp = model.Fingerprint(dec.Uvarint64())
	h.ThroughFp = model.Fingerprint(dec.Uvarint64())
	h.FromTs = dec.Varint64()
	h.ThroughTs = dec.Varint64()
	return nil
}

// series page header
type SeriesPage struct {
	Header SeriesHeader
	Series []Series
}

func (p *SeriesPage) Encode(enc encoding.Encbuf) {
	p.Header.Encode(enc)
	var lastSeries model.Fingerprint
	for _, series := range p.Series {
		lastSeries = series.Encode(enc, lastSeries)
	}
}

func (p *SeriesPage) Decode(dec encoding.Decbuf) error {
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
	Chunks      []ChunkRef
	Offset      BloomOffset
}

func (s *Series) Encode(enc encoding.Encbuf, previousFp model.Fingerprint) model.Fingerprint {
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

func (s *Series) Decode(dec encoding.Decbuf, previousFp model.Fingerprint) (model.Fingerprint, error) {
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

func (r *ChunkRef) Encode(enc encoding.Encbuf, previousEnd model.Time) model.Time {
	// delta encode start time
	enc.PutVarint64(int64(r.Start - previousEnd))
	enc.PutVarint64(int64(r.End - r.Start))
	enc.PutBE32(r.Checksum)
	return r.End
}

func (r *ChunkRef) Decode(dec encoding.Decbuf, previousEnd model.Time) (model.Time, error) {
	r.Start = previousEnd + model.Time(dec.Varint64())
	r.End = r.Start + model.Time(dec.Varint64())
	r.Checksum = dec.Be32()
	return r.End, dec.Err()
}

type BloomOffset struct {
	PageOffset int // offset to beginnging of bloom page
	ByteOffset int // offset to beginning of bloom within page
}

func (o *BloomOffset) Encode(enc encoding.Encbuf) {
	enc.PutUvarint(o.PageOffset)
	enc.PutUvarint(o.ByteOffset)
}

func (o *BloomOffset) Decode(dec encoding.Decbuf) error {
	o.PageOffset = dec.Uvarint()
	o.ByteOffset = dec.Uvarint()
	return dec.Err()
}

type BloomPage struct {
	N      int
	Blooms []Bloom
}

type Bloom struct {
	sbf           *boom.ScalableBloomFilter
	lastKnownSize int // bytes
}

func (p *BloomPage) Encode(enc encoding.Encbuf) error {
	enc.PutUvarint(p.N)

	for i, bloom := range p.Blooms {
		buf := bytes.NewBuffer(BlockPool.Get(bloom.lastKnownSize).([]byte))

		_, err := bloom.sbf.WriteTo(buf)
		if err != nil {
			return errors.Wrapf(err, "encoding %dth bloom filter", i)
		}

		b := buf.Bytes()
		enc.PutUvarint(len(b)) // length of bloom filter
		enc.PutBytes(b)
		BlockPool.Put(b[:0]) // release to pool
	}
	return nil
}

func (p *BloomPage) Decode(dec encoding.Decbuf) error {
	p.N = dec.Uvarint()
	p.Blooms = make([]Bloom, 0, p.N)
	for i := 0; i < p.N; i++ {
		bloom := Bloom{
			sbf: &boom.ScalableBloomFilter{},
		}

		ln := dec.Uvarint()
		_, err := bloom.sbf.ReadFrom(bytes.NewReader(dec.Bytes(ln)))
		if err != nil {
			return errors.Wrapf(err, "decoding %dth bloom filter", i)
		}

	}
	return nil
}
