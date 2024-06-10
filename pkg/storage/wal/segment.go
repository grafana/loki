package wal

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/dolthub/swiss"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/wal/chunks"
	"github.com/grafana/loki/v3/pkg/storage/wal/index"
	"github.com/grafana/loki/v3/pkg/util/encoding"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
)

// LOKW is the magic number for the Loki WAL format.
var (
	magicNumber = uint32(0x4C4F4B57)
	magicBuf    [4]byte
)

func init() {
	binary.BigEndian.PutUint32(magicBuf[:], magicNumber)
}

type streamID struct {
	labels, tenant string
}

type WalSegmentWriter struct {
	streams *swiss.Map[streamID, *streamSegment]
	buf1    encoding.Encbuf
}

type streamSegment struct {
	lbls     labels.Labels
	entries  []*logproto.Entry
	tenantID string
	maxt     int64
}

// NewWalSegmentWriter creates a new WalSegmentWriter.
func NewWalSegmentWriter() *WalSegmentWriter {
	return &WalSegmentWriter{
		streams: swiss.NewMap[streamID, *streamSegment](64),
		buf1:    encoding.EncWith(make([]byte, 0, 4)),
	}
}

// Labels are passed a string  `{foo="bar",baz="qux"}`  `{foo="foo",baz="foo"}`. labels.Labels => Symbols foo, baz , qux
func (b *WalSegmentWriter) Append(tenantID, labelsString string, lbls labels.Labels, entries []*logproto.Entry) {
	if len(entries) == 0 {
		return
	}
	id := streamID{labels: labelsString, tenant: tenantID}
	s, ok := b.streams.Get(id)
	if !ok {
		if lbls.Get(tsdb.TenantLabel) == "" {
			lbls = labels.NewBuilder(lbls).Set(tsdb.TenantLabel, tenantID).Labels()
		}
		s = &streamSegment{
			// todo: should be pooled.
			// prometheus bucketed pool
			// https://pkg.go.dev/github.com/prometheus/prometheus/util/pool
			entries:  make([]*logproto.Entry, 0, 64),
			lbls:     lbls,
			tenantID: tenantID,
		}
		s.maxt = entries[len(entries)-1].Timestamp.UnixNano()
		s.entries = append(s.entries, entries...)
		b.streams.Put(id, s)
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

// todo document format.
func (b *WalSegmentWriter) WriteTo(w io.Writer) (int64, error) {
	var (
		total   int64
		streams = make([]*streamSegment, 0, b.streams.Count())
	)

	// Collect all streams and sort them by tenantID and labels.
	b.streams.Iter(func(k streamID, v *streamSegment) bool {
		streams = append(streams, v)
		return false
	})
	sort.Slice(streams, func(i, j int) bool {
		if streams[i].tenantID != streams[j].tenantID {
			return streams[i].tenantID < streams[j].tenantID
		}
		return labels.Compare(streams[i].lbls, streams[j].lbls) < 0
	})

	idxw, err := index.NewWriter(context.TODO())
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
		if err := idxw.AddSymbol(symbol); err != nil {
			return total, err
		}
	}
	// Writes magic header
	n, err := w.Write(magicBuf[:])
	if err != nil {
		return total, err
	}
	total += int64(n)

	ref0 := uint64(total)
	// Write all streams to the writer.
	for i, s := range streams {
		if len(s.entries) == 0 {
			continue
		}
		n, err := s.WriteTo(w)
		if err != nil {
			return total, err
		}
		total += n
		idxw.AddSeries(storage.SeriesRef(i), s.lbls, chunks.Meta{
			MinTime: s.entries[0].Timestamp.UnixNano(),
			MaxTime: s.entries[len(s.entries)-1].Timestamp.UnixNano(),
			Ref:     chunks.ChunkRef(ref0),
		})
		ref0 = uint64(n)
	}

	if err := idxw.Close(); err != nil {
		return total, err
	}

	buf, closer, err := idxw.Buffer()
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
func (b *WalSegmentWriter) Reset() {
	b.streams.Clear()
	b.buf1.Reset()
}

type WalSegmentReader struct {
	idr *index.Reader
	b   []byte
}

func NewReader(b []byte) (*WalSegmentReader, error) {
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
	return &WalSegmentReader{
		idr: idr,
		b:   b[:len(b)-n-int(indexLen)],
	}, nil
}
