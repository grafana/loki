package v1

import (
	"bytes"
	"hash"
	"io"

	"github.com/owen-d/BoomFilters/boom"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/chunkenc"
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
	Blooms []Bloom
}

func (p *BloomPage) Encode(enc *encoding.Encbuf, pool chunkenc.WriterPool, crc32Hash hash.Hash32) (decompressedLen int, err error) {
	enc.Reset()

	// TODO(owen-d): no need to write to a temp buffer then rewrite to a compressed one;
	// should build a streaming encoder/decoder, although we will need to know the true
	// number of bytes written to calculate offsets (compression changes this)
	// temp buffer for compression
	// TODO(owen-d): pool
	buf := &bytes.Buffer{}

	enc.PutUvarint(len(p.Blooms))
	for i, bloom := range p.Blooms {
		if err := bloom.Encode(enc); err != nil {
			return 0, errors.Wrapf(err, "encoding %dth bloom filter", i)
		}
	}
	decompressedLen = enc.Len()

	compressor := pool.GetWriter(buf)
	defer pool.PutWriter(compressor)

	if _, err := compressor.Write(enc.Get()); err != nil {
		return 0, errors.Wrap(err, "compressing bloom page")
	}

	if err := compressor.Close(); err != nil {
		return 0, errors.Wrap(err, "closing compressor")
	}

	// replace the encoded series page with the compressed one
	enc.B = buf.Bytes()
	enc.PutHash(crc32Hash)
	return decompressedLen, nil
}

func (p *BloomPage) Decode(dec *encoding.Decbuf, pool chunkenc.ReaderPool, decompressedSize int) error {
	decoder, err := p.DecodeLazy(dec, pool, decompressedSize)
	if err != nil {
		return errors.Wrap(err, "building bloom page decoder")
	}

	p.Blooms = make([]Bloom, 0, decoder.n)
	var i int
	for decoder.Next() {
		bloom, err := decoder.At()
		if err != nil {
			return errors.Wrapf(err, "decoding %dth bloom filter", i)
		}
		p.Blooms = append(p.Blooms, bloom)
		i++
	}

	return errors.Wrap(dec.Err(), "decoding bloom page")
}

func (p *BloomPage) DecodeLazy(dec *encoding.Decbuf, pool chunkenc.ReaderPool, decompressedSize int) (*BloomPageDecoder, error) {
	if err := dec.CheckCrc(castagnoliTable); err != nil {
		return nil, errors.Wrap(err, "checksumming bloom page")
	}

	decompressor, err := pool.GetReader(bytes.NewReader(dec.Get()))
	if err != nil {
		return nil, errors.Wrap(err, "getting decompressor")
	}

	b := BlockPool.Get(decompressedSize)[:decompressedSize]
	defer BlockPool.Put(b)

	if _, err = io.ReadFull(decompressor, b); err != nil {
		return nil, errors.Wrap(err, "decompressing bloom page")
	}

	// replace decoder's input with the now-decompressed data
	dec.B = b

	return NewBloomPageDecoder(dec), nil
}

func NewBloomPageDecoder(dec *encoding.Decbuf) *BloomPageDecoder {
	return &BloomPageDecoder{
		n:   dec.Uvarint(),
		dec: dec,

		i: -1,
	}
}

type BloomPageDecoder struct {
	n   int
	dec *encoding.Decbuf

	i int
}

func (d *BloomPageDecoder) Next() bool {
	d.i++
	return d.i < d.n
}

func (d *BloomPageDecoder) At() (bloom Bloom, err error) {
	err = bloom.Decode(d.dec)
	return
}

type BloomPageHeader struct {
	N, Offset, Len, DecompressedLen int
}

func (h *BloomPageHeader) Encode(enc *encoding.Encbuf) {
	enc.PutUvarint(h.N)
	enc.PutUvarint(h.Offset)
	enc.PutUvarint(h.Len)
	enc.PutUvarint(h.DecompressedLen)
}

func (h *BloomPageHeader) Decode(dec *encoding.Decbuf) error {
	h.N = dec.Uvarint()
	h.Offset = dec.Uvarint()
	h.Len = dec.Uvarint()
	h.DecompressedLen = dec.Uvarint()
	return dec.Err()
}

type BloomBlock struct {
	schema      Schema
	pageHeaders []BloomPageHeader
}

func NewBloomBlock(encoding chunkenc.Encoding) BloomBlock {
	return BloomBlock{
		schema: Schema{version: DefaultSchemaVersion, encoding: encoding},
	}
}

func (b *BloomBlock) WriteTo(itr Iterator[BloomPage], w io.Writer) (offset int64, err error) {
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

	// encode blooms, accumlate encoded headers with relevant offsets,
	// they will be written at the end of the block
	for itr.Next() {
		page := itr.At()
		header := BloomPageHeader{
			N:      len(page.Blooms),
			Offset: int(offset),
		}

		// we encode the decompressed length of the page so we can efficiently choose
		// []byte sizes from a pool during decompression to minimize alloc costs
		header.DecompressedLen, err = page.Encode(enc, compressionPool, crc32Hash)
		if err != nil {
			return offset, errors.Wrap(err, "encoding bloom page")
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
		return offset, errors.Wrap(err, "iterating blooms")
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
		return offset, errors.Wrap(err, "writing bloom page headers")
	}
	offset += int64(written)

	return offset, nil
}

func (b *BloomBlock) DecodeHeaders(r io.ReadSeeker) error {
	// TODO(owen-d): improve allocations
	var dec encoding.Decbuf

	schemaBytes := make([]byte, b.schema.Len())
	_, err := io.ReadFull(r, schemaBytes)
	if err != nil {
		return errors.Wrap(err, "reading schema")
	}
	dec.B = schemaBytes

	if err := b.schema.Decode(&dec); err != nil {
		return errors.Wrap(err, "decoding schema")
	}

	// last 12 bytes are (headers offset: 8 byte u64, checksum: 4 byte u32)
	r.Seek(-12, io.SeekEnd)
	dec.B, err = io.ReadAll(r)
	if err != nil {
		return errors.Wrap(err, "reading bloom headers metadata")
	}

	headerOffset := dec.Be64()

	r.Seek(int64(headerOffset), io.SeekStart)
	dec.B, err = io.ReadAll(r)
	if err != nil {
		return errors.Wrap(err, "reading bloom page headers")
	}

	if err := dec.CheckCrc(castagnoliTable); err != nil {
		return errors.Wrap(err, "checksumming page headers")
	}

	b.pageHeaders = make([]BloomPageHeader, dec.Uvarint())
	for i := 0; i < len(b.pageHeaders); i++ {
		header := &b.pageHeaders[i]
		if err := header.Decode(&dec); err != nil {
			return errors.Wrapf(err, "decoding %dth series header", i)
		}
	}
	return nil
}
