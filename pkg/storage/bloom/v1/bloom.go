package v1

import (
	"bytes"
	"fmt"
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

func LazyDecodeBloomPage(dec *encoding.Decbuf, pool chunkenc.ReaderPool, decompressedSize int) (*BloomPageDecoder, error) {
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

	decoder := NewBloomPageDecoder(b)

	return decoder, nil
}

func NewBloomPageDecoder(data []byte) *BloomPageDecoder {
	dec := encoding.DecWith(data)
	decoder := &BloomPageDecoder{
		dec:  &dec,
		data: data,
	}
	decoder.Reset()
	return decoder
}

// Decoder is a seekable, reset-able iterator
type BloomPageDecoder struct {
	data []byte
	dec  *encoding.Decbuf
	n    int // number of blooms in page
}

func (d *BloomPageDecoder) Reset() {
	// last 8 bytes are the number of blooms in this page
	d.dec.B = d.data[len(d.data)-8:]
	d.n = int(d.dec.Be64())

	// reset data to the bloom portion of the page
	d.dec.B = d.data[:len(d.data)-8]
	return
}

func (d *BloomPageDecoder) N() int {
	return d.n
}

func (d *BloomPageDecoder) Seek(offset int) {
	d.dec.B = d.data[offset:]
}

func (d *BloomPageDecoder) Next() (*Bloom, error) {
	var b Bloom
	err := b.Decode(d.dec)
	return &b, err
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

func (b *BloomBlock) DecodeHeaders(r io.ReadSeeker) error {
	if err := b.schema.DecodeFrom(r); err != nil {
		return errors.Wrap(err, "decoding schema")
	}

	var (
		err error
		dec encoding.Decbuf
	)
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

func (b *BloomBlock) BloomPageDecoder(r io.ReadSeeker, offset BloomOffset) (*BloomPageDecoder, error) {
	if offset.Page < 0 || offset.Page >= len(b.pageHeaders) {
		return nil, fmt.Errorf("invalid page offset %d", offset.Page)
	}

	page := b.pageHeaders[offset.Page]

	if _, err := r.Seek(int64(page.Offset), io.SeekStart); err != nil {
		return nil, errors.Wrap(err, "seeking to bloom page")
	}

	data := BlockPool.Get(page.Len)[:page.Len]
	_, err := io.ReadFull(r, data)
	if err != nil {
		return nil, errors.Wrap(err, "reading bloom page")
	}

	dec := encoding.DecWith(data)

	return LazyDecodeBloomPage(&dec, b.schema.DecompressorPool(), page.DecompressedLen)
}
