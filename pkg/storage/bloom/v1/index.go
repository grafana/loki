package v1

import (
	"bytes"
	"io"
	"sort"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/encoding"
)

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
		schema: b.opts.Schema,
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
	schema Schema
	data   []byte
	dec    encoding.Decbuf
	header SeriesHeader

	// state
	i              int // current index
	cur            *SeriesWithMeta
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

	var res SeriesWithMeta
	d.previousFp, d.previousOffset, d.err = res.Decode(&d.dec, d.schema.version, d.previousFp, d.previousOffset)
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

func (d *SeriesPageDecoder) At() (res *SeriesWithMeta) {
	return d.cur
}

func (d *SeriesPageDecoder) Err() error {
	if d.err != nil {
		return d.err
	}
	return d.dec.Err()
}

// series encoding/decoding --------------------------------------------------

type Series struct {
	Fingerprint model.Fingerprint
	Chunks      ChunkRefs
}

type Meta struct {
	Fields  Fields
	Offsets []BloomOffset
}

// SeriesWithMeta is a series with a a variable number of bloom offsets.
// Used in v2+ to store blooms for larger series in parts
type SeriesWithMeta struct {
	Series
	Meta
}

func (s *SeriesWithMeta) Encode(
	enc *encoding.Encbuf,
	version Version,
	previousFp model.Fingerprint,
	previousOffset BloomOffset,
) BloomOffset {
	// delta encode fingerprint
	enc.PutUvarint64(uint64(s.Fingerprint - previousFp))
	// encode number of bloom offsets in this series
	enc.PutUvarint(len(s.Offsets))

	lastOffset := previousOffset
	for _, offset := range s.Offsets {
		// delta encode offsets.
		offset.Encode(enc, version, lastOffset)
		lastOffset = offset
	}

	// encode chunks using delta encoded timestamps
	var lastEnd model.Time
	enc.PutUvarint(len(s.Chunks))
	sort.Sort(s.Chunks) // ensure order
	for _, chunk := range s.Chunks {
		lastEnd = chunk.Encode(enc, version, lastEnd)
	}

	enc.PutUvarint(len(s.Fields))
	sort.Sort(s.Fields) // ensure order
	for _, field := range s.Fields {
		field.Encode(enc, version)
	}

	return lastOffset
}

func (s *SeriesWithMeta) Decode(
	dec *encoding.Decbuf,
	version Version,
	previousFp model.Fingerprint,
	previousOffset BloomOffset,
) (model.Fingerprint, BloomOffset, error) {
	if version < V3 {
		return 0, BloomOffset{}, errUnsupportedSchemaVersion
	}

	s.Fingerprint = previousFp + model.Fingerprint(dec.Uvarint64())
	numOffsets := dec.Uvarint()

	var (
		err        error
		lastEnd    model.Time
		lastOffset = previousOffset
	)

	s.Offsets = make([]BloomOffset, numOffsets)
	for i := range s.Offsets {
		// SeriesWithOffsets is a v2+ feature with multiple bloom offsets per series
		// so we signal that to the decoder
		err = s.Offsets[i].Decode(dec, version, lastOffset)
		lastOffset = s.Offsets[i]
		if err != nil {
			return 0, BloomOffset{}, errors.Wrapf(err, "decoding %dth bloom offset", i)
		}
	}

	s.Chunks = make([]ChunkRef, dec.Uvarint())
	for i := range s.Chunks {
		lastEnd, err = s.Chunks[i].Decode(dec, version, lastEnd)
		if err != nil {
			return 0, BloomOffset{}, errors.Wrapf(err, "decoding %dth chunk", i)
		}
	}

	s.Fields = make([]Field, dec.Uvarint())
	for i := range s.Fields {
		err = s.Fields[i].Decode(dec, version)
		if err != nil {
			return 0, BloomOffset{}, errors.Wrapf(err, "decoding %dth field", i)
		}
	}

	return s.Fingerprint, lastOffset, dec.Err()
}

// field encoding/decoding ---------------------------------------------------

type Field []byte // key of an indexed structured metadata field

func (f *Field) Encode(enc *encoding.Encbuf, _ Version) {
	enc.PutUvarintBytes(*f)
}

func (f *Field) Decode(dec *encoding.Decbuf, _ Version) error {
	*f = Field(dec.UvarintBytes())
	return dec.Err()
}

func (f *Field) String() string {
	return string(*f)
}

func (f *Field) Less(other Field) bool {
	// avoid string allocations
	return string(*f) < string(other)
}

type Fields []Field

func (f Fields) Len() int {
	return len(f)
}

func (f Fields) Less(i, j int) bool {
	return f[i].Less(f[j])
}

func (f Fields) Swap(i, j int) {
	f[i], f[j] = f[j], f[i]
}

// chunk encoding/decoding ---------------------------------------------------

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

func (r *ChunkRef) Encode(enc *encoding.Encbuf, _ Version, previousEnd model.Time) model.Time {
	// delta encode start time
	enc.PutVarint64(int64(r.From - previousEnd))
	enc.PutVarint64(int64(r.Through - r.From))
	enc.PutBE32(r.Checksum)
	return r.Through
}

func (r *ChunkRef) Decode(dec *encoding.Decbuf, _ Version, previousEnd model.Time) (model.Time, error) {
	r.From = previousEnd + model.Time(dec.Varint64())
	r.Through = r.From + model.Time(dec.Varint64())
	r.Checksum = dec.Be32()
	return r.Through, dec.Err()
}

type BloomOffset struct {
	Page       int // page number in bloom block
	ByteOffset int // offset to beginning of bloom within page
}

func (o *BloomOffset) Encode(enc *encoding.Encbuf, _ Version, previousOffset BloomOffset) {
	// page offsets diffs are always ascending
	enc.PutUvarint(o.Page - previousOffset.Page)
	enc.PutVarint64(int64(o.ByteOffset - previousOffset.ByteOffset))
}

func (o *BloomOffset) Decode(dec *encoding.Decbuf, _ Version, previousOffset BloomOffset) error {
	o.Page = previousOffset.Page + dec.Uvarint()
	o.ByteOffset = previousOffset.ByteOffset + int(dec.Varint64())
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

func (refs ChunkRefs) Intersect(others ChunkRefs) ChunkRefs {
	_, res := refs.Compare(others, true)
	return res
}

func (refs ChunkRefs) Union(others ChunkRefs) ChunkRefs {
	var res ChunkRefs
	var i, j int
	for i < len(refs) && j < len(others) {
		switch {
		case refs[i] == others[j]:
			res = append(res, refs[i])
			i++
			j++
		case refs[i].Less(others[j]):
			res = append(res, refs[i])
			i++
		default:
			res = append(res, others[j])
			j++
		}
	}

	// append any remaining refs
	if i < len(refs) {
		res = append(res, refs[i:]...)
	}

	if j < len(others) {
		res = append(res, others[j:]...)
	}

	return res
}
