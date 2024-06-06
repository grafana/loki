package v1

import (
	"bytes"
	"fmt"
	"io"
	"sort"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/encoding"
)

type Schema struct {
	version                byte
	encoding               chunkenc.Encoding
	nGramLength, nGramSkip uint64
}

func (s Schema) String() string {
	return fmt.Sprintf("v%d,encoding=%s,ngram=%d,skip=%d", s.version, s.encoding, s.nGramLength, s.nGramSkip)
}

func (s Schema) Compatible(other Schema) bool {
	return s == other
}

func (s Schema) NGramLen() int {
	return int(s.nGramLength)
}

func (s Schema) NGramSkip() int {
	return int(s.nGramSkip)
}

// byte length
func (s Schema) Len() int {
	// magic number + version + encoding + ngram length + ngram skip
	return 4 + 1 + 1 + 8 + 8
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
	enc.PutBE64(s.nGramLength)
	enc.PutBE64(s.nGramSkip)

}

func (s *Schema) DecodeFrom(r io.ReadSeeker) error {
	// TODO(owen-d): improve allocations
	schemaBytes := make([]byte, s.Len())
	_, err := io.ReadFull(r, schemaBytes)
	if err != nil {
		return errors.Wrap(err, "reading schema")
	}

	dec := encoding.DecWith(schemaBytes)
	return s.Decode(&dec)
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

	s.nGramLength = dec.Be64()
	s.nGramSkip = dec.Be64()

	return dec.Err()
}

// Block index is a set of series pages along with
// the headers for each page
type BlockIndex struct {
	opts BlockOptions

	pageHeaders []SeriesPageHeaderWithOffset // headers for each series page
}

func (b *BlockIndex) DecodeHeaders(r io.ReadSeeker) (uint32, error) {
	if err := b.opts.DecodeFrom(r); err != nil {
		return 0, errors.Wrap(err, "decoding block options")
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
		return 0, errors.Wrap(err, "seeking to index headers")
	}
	dec.B, err = io.ReadAll(r)
	if err != nil {
		return 0, errors.Wrap(err, "reading index page headers")
	}

	if err := dec.CheckCrc(castagnoliTable); err != nil {
		return 0, errors.Wrap(err, "checksumming page headers")
	}

	b.pageHeaders = make(
		[]SeriesPageHeaderWithOffset,
		dec.Uvarint(),
	)

	for i := 0; i < len(b.pageHeaders); i++ {
		var s SeriesPageHeaderWithOffset
		if err := s.Decode(&dec); err != nil {
			return 0, errors.Wrapf(err, "decoding %dth series header", i)
		}
		b.pageHeaders[i] = s
	}

	return checksum, nil
}

// decompress page and return an iterator over the bytes
func (b *BlockIndex) NewSeriesPageDecoder(r io.ReadSeeker, header SeriesPageHeaderWithOffset, metrics *Metrics) (res *SeriesPageDecoder, err error) {
	defer func() {
		if err != nil {
			metrics.pagesSkipped.WithLabelValues(pageTypeSeries, skipReasonErr).Inc()
			metrics.bytesSkipped.WithLabelValues(pageTypeSeries, skipReasonErr).Add(float64(header.DecompressedLen))
		} else {
			metrics.pagesRead.WithLabelValues(pageTypeSeries).Inc()
			metrics.bytesRead.WithLabelValues(pageTypeSeries).Add(float64(header.DecompressedLen))
		}
	}()

	if _, err := r.Seek(int64(header.Offset), io.SeekStart); err != nil {
		return nil, errors.Wrap(err, "seeking to series page")
	}

	data, _ := SeriesPagePool.Get(header.Len)
	defer SeriesPagePool.Put(data)
	_, err = io.ReadFull(r, data)
	if err != nil {
		return nil, errors.Wrap(err, "reading series page")
	}

	dec := encoding.DecWith(data)

	if err := dec.CheckCrc(castagnoliTable); err != nil {
		return nil, errors.Wrap(err, "checksumming series page")
	}

	decompressor, err := b.opts.Schema.DecompressorPool().GetReader(bytes.NewReader(dec.Get()))
	if err != nil {
		return nil, errors.Wrap(err, "getting decompressor")
	}

	decompressed := make([]byte, header.DecompressedLen)
	if _, err = io.ReadFull(decompressor, decompressed); err != nil {
		return nil, errors.Wrap(err, "decompressing series page")
	}

	res = &SeriesPageDecoder{
		data:   decompressed,
		header: header.SeriesHeader,
	}

	res.Reset()
	return res, nil
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
	Bounds            FingerprintBounds
	FromTs, ThroughTs model.Time
}

// build one aggregated header for the entire block
func aggregateHeaders(xs []SeriesHeader) SeriesHeader {
	if len(xs) == 0 {
		return SeriesHeader{}
	}

	fromFp, _ := xs[0].Bounds.Bounds()
	_, throughFP := xs[len(xs)-1].Bounds.Bounds()
	res := SeriesHeader{
		Bounds: NewBounds(fromFp, throughFP),
	}

	for i, x := range xs {
		if i == 0 || x.FromTs < res.FromTs {
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
	enc.PutUvarint64(uint64(h.Bounds.Min))
	enc.PutUvarint64(uint64(h.Bounds.Max))
	enc.PutVarint64(int64(h.FromTs))
	enc.PutVarint64(int64(h.ThroughTs))
}

func (h *SeriesHeader) Decode(dec *encoding.Decbuf) error {
	h.NumSeries = dec.Uvarint()
	h.Bounds.Min = model.Fingerprint(dec.Uvarint64())
	h.Bounds.Max = model.Fingerprint(dec.Uvarint64())
	h.FromTs = model.Time(dec.Varint64())
	h.ThroughTs = model.Time(dec.Varint64())
	return dec.Err()
}

// can decode a series page one item at a time, useful when we don't
// need to iterate an entire page
type SeriesPageDecoder struct {
	data   []byte
	dec    encoding.Decbuf
	header SeriesHeader

	// state
	i              int // current index
	cur            *SeriesWithOffset
	err            error
	previousFp     model.Fingerprint // previous series' fingerprint for delta-decoding
	previousOffset BloomOffset       // previous series' bloom offset for delta-decoding
}

func (d *SeriesPageDecoder) Reset() {
	d.i = -1
	d.cur = nil
	d.err = nil
	d.previousFp = 0
	d.previousOffset = BloomOffset{}
	d.dec.B = d.data
}

func (d *SeriesPageDecoder) Next() bool {
	if d.err != nil {
		return false
	}

	d.i++
	if d.i >= d.header.NumSeries {
		return false
	}

	var res SeriesWithOffset
	d.previousFp, d.previousOffset, d.err = res.Decode(&d.dec, d.previousFp, d.previousOffset)
	if d.err != nil {
		return false
	}

	d.cur = &res
	return true
}

func (d *SeriesPageDecoder) Seek(fp model.Fingerprint) {
	if fp > d.header.Bounds.Max {
		// shortcut: we know the fingerprint is too large so nothing in this page
		// will match the seek call, which returns the first found fingerprint >= fp.
		// so masquerade the index as if we've already iterated through
		d.i = d.header.NumSeries
	}

	// if we've seen an error or we've potentially skipped the desired fp, reset the page state
	if d.Err() != nil || (d.cur != nil && d.cur.Fingerprint >= fp) {
		d.Reset()
	}

	for {
		// previous byte offset in decoder, used for resetting
		// position after finding the desired fp
		offset := len(d.data) - d.dec.Len()
		// previous bloom offset in decoder, used for
		// resetting position after finding the desired fp
		// since offsets are delta-encoded
		previousBloomOffset := d.previousOffset
		previousFp := d.previousFp

		// iteration finished
		if ok := d.Next(); !ok {
			return
		}

		// we've seeked to the correct location. reverse one step and return
		cur := d.At()
		if cur.Fingerprint >= fp {
			d.i--
			d.previousOffset = previousBloomOffset
			d.previousFp = previousFp
			d.dec.B = d.data[offset:]
			return
		}
	}
}

func (d *SeriesPageDecoder) At() (res *SeriesWithOffset) {
	return d.cur
}

func (d *SeriesPageDecoder) Err() error {
	if d.err != nil {
		return d.err
	}
	return d.dec.Err()
}

type Series struct {
	Fingerprint model.Fingerprint
	Chunks      ChunkRefs
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
	sort.Sort(s.Chunks) // ensure order
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
	if err := s.Offset.Decode(dec, previousOffset); err != nil {
		return 0, BloomOffset{}, errors.Wrap(err, "decoding bloom offset")
	}

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

type ChunkRef logproto.ShortRef

func (r *ChunkRef) Less(other ChunkRef) bool {
	if r.From != other.From {
		return r.From < other.From
	}

	if r.Through != other.Through {
		return r.Through < other.Through
	}

	return r.Checksum < other.Checksum
}

func (r *ChunkRef) Cmp(other ChunkRef) int {
	if r.From != other.From {
		return int(r.From) - int(other.From)
	}

	if r.Through != other.Through {
		return int(r.Through) - int(other.Through)
	}

	return int(r.Checksum) - int(other.Checksum)
}

func (r *ChunkRef) Encode(enc *encoding.Encbuf, previousEnd model.Time) model.Time {
	// delta encode start time
	enc.PutVarint64(int64(r.From - previousEnd))
	enc.PutVarint64(int64(r.Through - r.From))
	enc.PutBE32(r.Checksum)
	return r.Through
}

func (r *ChunkRef) Decode(dec *encoding.Decbuf, previousEnd model.Time) (model.Time, error) {
	r.From = previousEnd + model.Time(dec.Varint64())
	r.Through = r.From + model.Time(dec.Varint64())
	r.Checksum = dec.Be32()
	return r.Through, dec.Err()
}

type BloomOffset struct {
	Page       int // page number in bloom block
	ByteOffset int // offset to beginning of bloom within page
}

func (o *BloomOffset) Encode(enc *encoding.Encbuf, previousOffset BloomOffset) {
	enc.PutUvarint(o.Page - previousOffset.Page)
	enc.PutUvarint(o.ByteOffset - previousOffset.ByteOffset)
}

func (o *BloomOffset) Decode(dec *encoding.Decbuf, previousOffset BloomOffset) error {
	o.Page = previousOffset.Page + dec.Uvarint()
	o.ByteOffset = previousOffset.ByteOffset + dec.Uvarint()
	return dec.Err()
}

type ChunkRefs []ChunkRef

func (refs ChunkRefs) Len() int {
	return len(refs)
}

func (refs ChunkRefs) Less(i, j int) bool {
	return refs[i].Less(refs[j])
}

func (refs ChunkRefs) Swap(i, j int) {
	refs[i], refs[j] = refs[j], refs[i]
}

// Unless returns the chunk refs in this set that are not in the other set.
// Both must be sorted.
func (refs ChunkRefs) Unless(others []ChunkRef) ChunkRefs {
	res, _ := refs.Compare(others, false)
	return res
}

// Compare returns two sets of chunk refs, both must be sorted:
// 1) the chunk refs which are in the original set but not in the other set
// 2) the chunk refs which are in both sets
// the `populateInclusive` argument allows avoiding populating the inclusive set
// if it is not needed
// TODO(owen-d): can be improved to use binary search when one list
// is signficantly larger than the other
func (refs ChunkRefs) Compare(others ChunkRefs, populateInclusive bool) (exclusive ChunkRefs, inclusive ChunkRefs) {
	var i, j int
	for i < len(refs) && j < len(others) {
		switch {

		case refs[i] == others[j]:
			if populateInclusive {
				inclusive = append(inclusive, refs[i])
			}
			i++
			j++
		case refs[i].Less(others[j]):
			exclusive = append(exclusive, refs[i])
			i++
		default:
			j++
		}
	}

	// append any remaining refs
	if i < len(refs) {
		exclusive = append(exclusive, refs[i:]...)
	}

	return
}
