package v1

import (
	"bytes"
	"fmt"
	"io"

	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
	"github.com/grafana/loki/pkg/util/encoding"
)

// NB(chaudum): Some block pages are way bigger than others (400MiB and
// bigger), and loading multiple pages into memory in parallel can cause the
// gateways to OOM.
// Figure out a decent maximum page size that we can process.
// TODO(chaudum): Make max page size configurable
var maxPageSize = 32 << 20 // 32MB
var errPageTooLarge = "bloom page too large to process: N=%d Offset=%d Len=%d DecompressedLen=%d"

type Bloom struct {
	filter.ScalableBloomFilter
}

func (b *Bloom) Encode(enc *encoding.Encbuf) error {
	// divide by 8 b/c bloom capacity is measured in bits, but we want bytes
	buf := bytes.NewBuffer(BlockPool.Get(int(b.Capacity() / 8)))

	// TODO(owen-d): have encoder implement writer directly so we don't need
	// to indirect via a buffer
	_, err := b.WriteTo(buf)
	if err != nil {
		return errors.Wrap(err, "encoding bloom filter")
	}

	data := buf.Bytes()
	enc.PutUvarint(len(data)) // length of bloom filter
	enc.PutBytes(data)
	BlockPool.Put(data[:0]) // release to pool
	return nil
}

func (b *Bloom) DecodeCopy(dec *encoding.Decbuf) error {
	ln := dec.Uvarint()
	data := dec.Bytes(ln)

	_, err := b.ReadFrom(bytes.NewReader(data))
	if err != nil {
		return errors.Wrap(err, "decoding copy of bloom filter")
	}

	return nil
}

func (b *Bloom) Decode(dec *encoding.Decbuf) error {
	ln := dec.Uvarint()
	data := dec.Bytes(ln)

	_, err := b.DecodeFrom(data)
	if err != nil {
		return errors.Wrap(err, "decoding bloom filter")
	}

	return nil
}

func LazyDecodeBloomPage(r io.Reader, pool chunkenc.ReaderPool, page BloomPageHeader) (*BloomPageDecoder, error) {
	data := BlockPool.Get(page.Len)[:page.Len]
	defer BlockPool.Put(data)

	_, err := io.ReadFull(r, data)
	if err != nil {
		return nil, errors.Wrap(err, "reading bloom page")
	}
	dec := encoding.DecWith(data)

	if err := dec.CheckCrc(castagnoliTable); err != nil {
		return nil, errors.Wrap(err, "checksumming bloom page")
	}

	decompressor, err := pool.GetReader(bytes.NewReader(dec.Get()))
	if err != nil {
		return nil, errors.Wrap(err, "getting decompressor")
	}
	defer pool.PutReader(decompressor)

	b := BlockPool.Get(page.DecompressedLen)[:page.DecompressedLen]

	if _, err = io.ReadFull(decompressor, b); err != nil {
		return nil, errors.Wrap(err, "decompressing bloom page")
	}

	decoder := NewBloomPageDecoder(b)

	return decoder, nil
}

// NewBloomPageDecoder returns a decoder for a bloom page.
// If the byte slice passed in the constructor is from the BlockPool pool, then
// the caller needs to ensure that Close() is called to put the slice back to
// its pool.
func NewBloomPageDecoder(data []byte) *BloomPageDecoder {
	// last 8 bytes are the number of blooms in this page
	dec := encoding.DecWith(data[len(data)-8:])
	n := int(dec.Be64())
	// reset data to the bloom portion of the page
	data = data[:len(data)-8]
	dec.B = data

	// reset data to the bloom portion of the page

	decoder := &BloomPageDecoder{
		dec:  &dec,
		data: data,
		n:    n,
	}

	return decoder
}

// Decoder is a seekable, reset-able iterator
// We could optimize this by encoding the mode (read, write) into our structs
// and doing copy-on-write shenannigans, but I'm avoiding this for now.
type BloomPageDecoder struct {
	data []byte
	dec  *encoding.Decbuf

	n   int // number of blooms in page
	cur *Bloom
	err error
}

func (d *BloomPageDecoder) Reset() {
	d.err = nil
	d.cur = nil
	d.dec.B = d.data
}

func (d *BloomPageDecoder) Seek(offset int) {
	d.dec.B = d.data[offset:]
}

func (d *BloomPageDecoder) Next() bool {
	// end of iteration, no error
	if d.dec.Len() == 0 {
		return false
	}

	var b Bloom
	d.err = b.Decode(d.dec)
	// end of iteration, error
	if d.err != nil {
		return false
	}
	d.cur = &b
	return true
}

func (d *BloomPageDecoder) At() *Bloom {
	return d.cur
}

func (d *BloomPageDecoder) Err() error {
	return d.err
}

func (d *BloomPageDecoder) Close() {
	d.err = nil
	d.cur = nil
	d.dec.B = d.data[:0]
	BlockPool.Put(d.data)
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

func (b *BloomBlock) DecodeHeaders(r io.ReadSeeker) (uint32, error) {
	if err := b.schema.DecodeFrom(r); err != nil {
		return 0, errors.Wrap(err, "decoding schema")
	}

	var (
		err error
		dec encoding.Decbuf
	)
	// last 12 bytes are (headers offset: 8 byte u64, checksum: 4 byte u32)
	if _, err := r.Seek(-12, io.SeekEnd); err != nil {
		return 0, errors.Wrap(err, "seeking to bloom headers metadata")
	}
	dec.B, err = io.ReadAll(r)
	if err != nil {
		return 0, errors.Wrap(err, "reading bloom headers metadata")
	}

	headerOffset := dec.Be64()
	checksum := dec.Be32()

	if _, err := r.Seek(int64(headerOffset), io.SeekStart); err != nil {
		return 0, errors.Wrap(err, "seeking to bloom headers")
	}
	dec.B, err = io.ReadAll(r)
	if err != nil {
		return 0, errors.Wrap(err, "reading bloom page headers")
	}

	if err := dec.CheckCrc(castagnoliTable); err != nil {
		return 0, errors.Wrap(err, "checksumming page headers")
	}

	b.pageHeaders = make([]BloomPageHeader, dec.Uvarint())
	for i := 0; i < len(b.pageHeaders); i++ {
		header := &b.pageHeaders[i]
		if err := header.Decode(&dec); err != nil {
			return 0, errors.Wrapf(err, "decoding %dth series header", i)
		}
	}
	return checksum, nil
}

func (b *BloomBlock) BloomPageDecoder(r io.ReadSeeker, pageIdx int) (*BloomPageDecoder, error) {
	if pageIdx < 0 || pageIdx >= len(b.pageHeaders) {
		return nil, fmt.Errorf("invalid page (%d) for bloom page decoding", pageIdx)
	}

	page := b.pageHeaders[pageIdx]

	if page.Len > maxPageSize {
		return nil, fmt.Errorf(errPageTooLarge, page.N, page.Offset, page.Len, page.DecompressedLen)
	}

	if _, err := r.Seek(int64(page.Offset), io.SeekStart); err != nil {
		return nil, errors.Wrap(err, "seeking to bloom page")
	}

	return LazyDecodeBloomPage(r, b.schema.DecompressorPool(), page)
}
