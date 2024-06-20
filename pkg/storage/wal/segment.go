package wal

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/wal/chunks"
	"github.com/grafana/loki/v3/pkg/storage/wal/index"
	"github.com/grafana/loki/v3/pkg/util/encoding"
)

// LOKW is the magic number for the Loki WAL format.
var (
	magicNumber       = uint32(0x4C4F4B57)
	magicBuf          [4]byte
	streamSegmentPool = sync.Pool{
		New: func() interface{} {
			return &streamSegment{
				entries: make([]*logproto.Entry, 0, 4096),
			}
		},
	}
)

func init() {
	binary.BigEndian.PutUint32(magicBuf[:], magicNumber)
}

type streamID struct {
	labels, tenant string
}

type SegmentWriter struct {
	streams   map[streamID]*streamSegment
	buf1      encoding.Encbuf
	inputSize int64
	idxWriter *index.Writer
}

type streamSegment struct {
	lbls     labels.Labels
	entries  []*logproto.Entry
	tenantID string
	maxt     int64
}

// NewWalSegmentWriter creates a new WalSegmentWriter.
func NewWalSegmentWriter() (*SegmentWriter, error) {
	idxWriter, err := index.NewWriter(context.TODO())
	if err != nil {
		return nil, err
	}
	return &SegmentWriter{
		streams:   make(map[streamID]*streamSegment, 64),
		buf1:      encoding.EncWith(make([]byte, 0, 4)),
		idxWriter: idxWriter,
	}, nil
}

// Labels are passed a string  `{foo="bar",baz="qux"}`  `{foo="foo",baz="foo"}`. labels.Labels => Symbols foo, baz , qux
func (b *SegmentWriter) Append(tenantID, labelsString string, lbls labels.Labels, entries []*logproto.Entry) {
	if len(entries) == 0 {
		return
	}
	for _, e := range entries {
		b.inputSize += int64(len(e.Line))
	}
	id := streamID{labels: labelsString, tenant: tenantID}
	s, ok := b.streams[id]
	if !ok {
		if lbls.Get(tsdb.TenantLabel) == "" {
			lbls = labels.NewBuilder(lbls).Set(tsdb.TenantLabel, tenantID).Labels()
		}
		s = streamSegmentPool.Get().(*streamSegment)
		s.lbls = lbls
		s.tenantID = tenantID
		s.entries = s.entries[:0]
		s.maxt = entries[len(entries)-1].Timestamp.UnixNano()
		s.entries = append(s.entries, entries...)
		b.streams[id] = s
		return
	}

	for i, e := range entries {
		if e.Timestamp.UnixNano() >= s.maxt {
			s.entries = append(s.entries, entries[i])
			s.maxt = e.Timestamp.UnixNano()
			continue
		}
		// search for the right place to insert.
		idx := sort.Search(len(s.entries), func(i int) bool {
			return s.entries[i].Timestamp.UnixNano() > e.Timestamp.UnixNano()
		})
		// insert at the right place.
		s.entries = append(s.entries, nil)
		copy(s.entries[idx+1:], s.entries[idx:])
		s.entries[idx] = e
	}
}

func (b *SegmentWriter) WriteTo(w io.Writer) (int64, error) {
	var (
		total   int64
		streams = make([]*streamSegment, 0, len(b.streams))
	)

	// Collect all streams and sort them by tenantID and labels.
	for _, s := range b.streams {
		if len(s.entries) == 0 {
			continue
		}
		streams = append(streams, s)
	}

	sort.Slice(streams, func(i, j int) bool {
		if streams[i].tenantID != streams[j].tenantID {
			return streams[i].tenantID < streams[j].tenantID
		}
		return labels.Compare(streams[i].lbls, streams[j].lbls) < 0
	})

	err := b.idxWriter.Reset()
	if err != nil {
		return total, err
	}

	// Build symbols
	symbolsMap := make(map[string]struct{})
	for _, s := range streams {
		for _, l := range s.lbls {
			symbolsMap[l.Name] = struct{}{}
			symbolsMap[l.Value] = struct{}{}
		}
	}

	// Sort symbols
	symbols := make([]string, 0, len(symbolsMap))
	for s := range symbolsMap {
		symbols = append(symbols, s)
	}
	sort.Strings(symbols)

	// Add symbols
	for _, symbol := range symbols {
		if err := b.idxWriter.AddSymbol(symbol); err != nil {
			return total, err
		}
	}
	// Writes magic header
	n, err := w.Write(magicBuf[:])
	if err != nil {
		return total, err
	}
	total += int64(n)

	// Write all streams to the writer.
	for i, s := range streams {
		if len(s.entries) == 0 {
			continue
		}
		n, err := s.WriteTo(w)
		if err != nil {
			return total, err
		}
		err = b.idxWriter.AddSeries(storage.SeriesRef(i), s.lbls, chunks.Meta{
			MinTime: s.entries[0].Timestamp.UnixNano(),
			MaxTime: s.entries[len(s.entries)-1].Timestamp.UnixNano(),
			Ref:     chunks.NewChunkRef(uint64(total), uint64(n)),
		})
		if err != nil {
			return total, err
		}
		total += n

	}

	if err := b.idxWriter.Close(); err != nil {
		return total, err
	}

	buf, closer, err := b.idxWriter.Buffer()
	if err != nil {
		return total, err
	}
	defer closer.Close()

	n, err = w.Write(buf)
	if err != nil {
		return total, err
	}
	if n != len(buf) {
		return total, errors.New("invalid written index len")
	}
	total += int64(n)

	// write index len 4b
	b.buf1.PutBE32int(n)
	n, err = w.Write(b.buf1.Get())
	if err != nil {
		return total, err
	}
	total += int64(n)

	// write the version
	n, err = w.Write([]byte{1})
	if err != nil {
		return total, err
	}
	total += int64(n)

	// Writes magic footer
	n, err = w.Write(magicBuf[:])
	if err != nil {
		return total, err
	}
	total += int64(n)

	return total, nil
}

func (s *streamSegment) WriteTo(w io.Writer) (n int64, err error) {
	return chunks.WriteChunk(w, s.entries, chunks.EncodingSnappy)
}

// Reset clears the writer.
// After calling Reset, the writer can be reused.
func (b *SegmentWriter) Reset() {
	for _, s := range b.streams {
		s := s
		streamSegmentPool.Put(s)
	}
	b.streams = make(map[streamID]*streamSegment, 64)
	b.buf1.Reset()
	b.inputSize = 0
}

// InputSize returns the total size of the input data written to the writer.
// It doesn't account for timestamps and labels.
func (b *SegmentWriter) InputSize() int64 {
	return b.inputSize
}

type SegmentReader struct {
	idr *index.Reader
	b   []byte
}

func NewReader(b []byte) (*SegmentReader, error) {
	if len(b) < 13 {
		return nil, errors.New("segment too small")
	}
	if !bytes.Equal(magicBuf[:], b[:4]) {
		return nil, errors.New("invalid segment header")
	}
	if !bytes.Equal(magicBuf[:], b[len(b)-4:]) {
		return nil, errors.New("invalid segment footer")
	}
	n := 5
	version := b[len(b)-n]
	if version != 1 {
		return nil, fmt.Errorf("invalid segment version: %d", version)
	}
	indexLen := binary.BigEndian.Uint32(b[len(b)-n-4 : len(b)-n])
	n += 4
	idr, err := index.NewReader(index.RealByteSlice(b[len(b)-n-int(indexLen) : len(b)-n]))
	if err != nil {
		return nil, err
	}
	return &SegmentReader{
		idr: idr,
		b:   b[:len(b)-n-int(indexLen)],
	}, nil
}

// todo: Evaluate/benchmark wal segment using apache arrow as format ?

type SeriesIter struct {
	ir  *index.Reader
	ps  tsdbindex.Postings
	err error

	curSeriesRef  storage.SeriesRef
	curLabels     labels.Labels
	labelsBuilder *labels.ScratchBuilder
	chunksMeta    []chunks.Meta
	blocks        []byte
}

func NewSeriesIter(ir *index.Reader, ps tsdbindex.Postings, blocks []byte) *SeriesIter {
	return &SeriesIter{
		ir:            ir,
		ps:            ps,
		blocks:        blocks,
		labelsBuilder: &labels.ScratchBuilder{},
		chunksMeta:    make([]chunks.Meta, 0, 1),
	}
}

func (iter *SeriesIter) Next() bool {
	if !iter.ps.Next() {
		return false
	}
	if iter.ps.At() != iter.curSeriesRef {
		iter.curSeriesRef = iter.ps.At()
		err := iter.ir.Series(iter.curSeriesRef, iter.labelsBuilder, &iter.chunksMeta)
		if err != nil {
			iter.err = err
			return false
		}
		iter.curLabels = iter.labelsBuilder.Labels()
	}
	return true
}

func (iter *SeriesIter) At() labels.Labels {
	return iter.curLabels
}

func (iter *SeriesIter) Err() error {
	return iter.err
}

func (iter *SeriesIter) ChunkReader(_ *chunks.ChunkReader) (*chunks.ChunkReader, error) {
	if len(iter.chunksMeta) == 0 {
		return nil, fmt.Errorf("no chunks found for series %d", iter.curSeriesRef)
	}
	if len(iter.chunksMeta) > 1 {
		return nil, fmt.Errorf("multiple chunks found for series %d", iter.curSeriesRef)
	}
	offset, size := iter.chunksMeta[0].Ref.Unpack()
	if offset < 0 || offset >= len(iter.blocks) || size < 0 || offset+size > len(iter.blocks) {
		return nil, fmt.Errorf("invalid offset or size for series %d: offset %d, size %d, blocks len %d", iter.curSeriesRef, offset, size, len(iter.blocks))
	}

	return chunks.NewChunkReader(iter.blocks[offset : offset+size])
}

func (r *SegmentReader) Series(ctx context.Context) (*SeriesIter, error) {
	ps, err := r.idr.Postings(ctx, index.AllPostingsKey.Name, index.AllPostingsKey.Value)
	if err != nil {
		return nil, err
	}
	if ps.Err() != nil {
		return nil, ps.Err()
	}

	return NewSeriesIter(r.idr, ps, r.b), nil
}

type Sizes struct {
	Index  int64
	Series []int64
}

func (r *SegmentReader) Sizes() (Sizes, error) {
	var sizes Sizes
	sizes.Index = r.idr.Size()
	it, err := r.Series(context.Background())
	if err != nil {
		return sizes, err
	}
	sizes.Series = []int64{}
	for it.Next() {
		_, size := it.chunksMeta[0].Ref.Unpack()
		sizes.Series = append(sizes.Series, int64(size))
	}
	return sizes, err
}
