package v1

import (
	"bytes"
	"hash"

	"github.com/owen-d/BoomFilters/boom"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/util/encoding"
)

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
