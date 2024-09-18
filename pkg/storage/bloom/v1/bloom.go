package v1

import (
	"bytes"
	"fmt"
	"io"

	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/storage/bloom/v1/filter"
	"github.com/grafana/loki/v3/pkg/util/encoding"
	"github.com/grafana/loki/v3/pkg/util/mempool"
)

// NB(chaudum): Some block pages are way bigger than others (400MiB and
// bigger), and loading multiple pages into memory in parallel can cause the
// gateways to OOM.
// Figure out a decent default maximum page size that we can process.
var DefaultMaxPageSize = 64 << 20 // 64MB

type Bloom struct {
	filter.ScalableBloomFilter
}

func NewBloom() *Bloom {
	return &Bloom{
		// TODO parameterise SBF options. fp_rate
		ScalableBloomFilter: *filter.NewScalableBloomFilter(1024, 0.01, 0.8),
	}
}

func (b *Bloom) Encode(enc *encoding.Encbuf) error {
	// divide by 8 b/c bloom capacity is measured in bits, but we want bytes
	buf := bytes.NewBuffer(make([]byte, 0, int(b.Capacity()/8)))

	// TODO(owen-d): have encoder implement writer directly so we don't need
	// to indirect via a buffer
	_, err := b.WriteTo(buf)
	if err != nil {
		return errors.Wrap(err, "encoding bloom filter")
	}

	data := buf.Bytes()
	enc.PutUvarint(len(data)) // length of bloom filter
	enc.PutBytes(data)
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

func LazyDecodeBloomPage(r io.Reader, alloc mempool.Allocator, pool chunkenc.ReaderPool, page BloomPageHeader) (*BloomPageDecoder, error) {
	data, err := alloc.Get(page.Len)
	if err != nil {
		return nil, errors.Wrap(err, "allocating buffer")
	}
	defer alloc.Put(data)

	_, err = io.ReadFull(r, data)
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

	b, err := alloc.Get(page.DecompressedLen)
	if err != nil {
		return nil, errors.Wrap(err, "allocating buffer")
	}

	if _, err = io.ReadFull(decompressor, b); err != nil {
		return nil, errors.Wrap(err, "decompressing bloom page")
	}

	decoder := NewBloomPageDecoder(b)

	return decoder, nil
}

// shortcut to skip allocations when we know the page is not compressed
func LazyDecodeBloomPageNoCompression(r io.Reader, alloc mempool.Allocator, page BloomPageHeader) (*BloomPageDecoder, error) {
	// data + checksum
	if page.Len != page.DecompressedLen+4 {
		return nil, errors.New("the Len and DecompressedLen of the page do not match")
	}

	data, err := alloc.Get(page.Len)
	if err != nil {
		return nil, errors.Wrap(err, "allocating buffer")
	}

	_, err = io.ReadFull(r, data)
	if err != nil {
		return nil, errors.Wrap(err, "reading bloom page")
	}
	dec := encoding.DecWith(data)

	if err := dec.CheckCrc(castagnoliTable); err != nil {
		return nil, errors.Wrap(err, "checksumming bloom page")
	}

	return NewBloomPageDecoder(dec.Get()), nil
}

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
// TODO(owen-d): use buffer pools. The reason we don't currently
// do this is because the `data` slice currently escapes the decoder
// via the returned bloom, so we can't know when it's safe to return it to the pool.
// This happens via `data ([]byte) -> dec (*encoding.Decbuf) -> bloom (Bloom)` where
// the final Bloom has a reference to the data slice.
// We could optimize this by encoding the mode (read, write) into our structs
// and doing copy-on-write shenannigans, but I'm avoiding this for now.
type BloomPageDecoder struct {
	data []byte
	dec  *encoding.Decbuf

	n   int // number of blooms in page
	cur *Bloom
	err error
}

// Relinquish returns the underlying byte slice to the pool
// for efficiency. It's intended to be used as a
// perf optimization.
// This can only safely be used when the underlying bloom
// bytes don't escape the decoder:
// on reads in the bloom-gw but not in the bloom-builder
func (d *BloomPageDecoder) Relinquish(alloc mempool.Allocator) {
	if d == nil {
		return
	}

	data := d.data
	d.data = nil
	d.Reset() // Reset for cleaning up residual references to data via `dec`

	if cap(data) > 0 {
		_ = alloc.Put(data)
	}

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

// BloomPageDecoder returns a decoder for the given page index.
// It may skip the page if it's too large.
// NB(owen-d): if `skip` is true, err _must_ be nil.
func (b *BloomBlock) BloomPageDecoder(r io.ReadSeeker, alloc mempool.Allocator, pageIdx int, maxPageSize int, metrics *Metrics) (res *BloomPageDecoder, skip bool, err error) {
	if pageIdx < 0 || pageIdx >= len(b.pageHeaders) {
		metrics.pagesSkipped.WithLabelValues(pageTypeBloom, skipReasonOOB).Inc()
		metrics.bytesSkipped.WithLabelValues(pageTypeBloom, skipReasonOOB).Add(float64(b.pageHeaders[pageIdx].DecompressedLen))
		return nil, false, fmt.Errorf("invalid page (%d) for bloom page decoding", pageIdx)
	}

	page := b.pageHeaders[pageIdx]
	// fmt.Printf("pageIdx=%d page=%+v size=%.2fMiB\n", pageIdx, page, float64(page.Len)/float64(1<<20))

	if page.Len > maxPageSize {
		metrics.pagesSkipped.WithLabelValues(pageTypeBloom, skipReasonTooLarge).Inc()
		metrics.bytesSkipped.WithLabelValues(pageTypeBloom, skipReasonTooLarge).Add(float64(page.DecompressedLen))
		return nil, true, nil
	}

	if _, err = r.Seek(int64(page.Offset), io.SeekStart); err != nil {
		metrics.pagesSkipped.WithLabelValues(pageTypeBloom, skipReasonErr).Inc()
		metrics.bytesSkipped.WithLabelValues(pageTypeBloom, skipReasonErr).Add(float64(page.DecompressedLen))
		return nil, false, errors.Wrap(err, "seeking to bloom page")
	}

	if b.schema.encoding == chunkenc.EncNone {
		res, err = LazyDecodeBloomPageNoCompression(r, alloc, page)
	} else {
		res, err = LazyDecodeBloomPage(r, alloc, b.schema.DecompressorPool(), page)
	}

	if err != nil {
		metrics.pagesSkipped.WithLabelValues(pageTypeBloom, skipReasonErr).Inc()
		metrics.bytesSkipped.WithLabelValues(pageTypeBloom, skipReasonErr).Add(float64(page.DecompressedLen))
		return nil, false, errors.Wrap(err, "decoding bloom page")
	}

	metrics.pagesRead.WithLabelValues(pageTypeBloom).Inc()
	metrics.bytesRead.WithLabelValues(pageTypeBloom).Add(float64(page.DecompressedLen))
	return res, false, nil
}
