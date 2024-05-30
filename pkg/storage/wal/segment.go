package wal

import (
	"encoding/binary"
	"hash"
	"hash/crc32"
	"io"
	"sort"

	"github.com/dolthub/swiss"
	"github.com/klauspost/compress/s2"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/prometheus/prometheus/model/labels"
)

type streamID struct {
	labels, tenant string
}

type WalSegmentWriter struct {
	streams *swiss.Map[streamID, *streamSegment]
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
		offset  = make([]int64, 0, len(streams))
	)
	// todo: write magic number and version

	// Collect all streams and sort them by tenantID and labels.
	b.streams.Iter(func(k streamID, v *streamSegment) bool {
		streams = append(streams, v)
		return true
	})
	sort.Slice(streams, func(i, j int) bool {
		if streams[i].tenantID != streams[j].tenantID {
			return streams[i].tenantID < streams[j].tenantID
		}
		return labels.Compare(streams[i].lbls, streams[j].lbls) < 0
	})

	// Write all streams to the writer.
	for _, s := range streams {
		n, err := s.WriteTo(w)
		if err != nil {
			return total, err
		}
		total += n
		offset = append(offset, total)
	}
	// Write Symbols.
	// Write Stream offsets, tenantID, labels ref.
	// TOC
	// len(TOC)

	return total, nil
}

var magicNumber = uint32(0x12EE56A)

// The table gets initialized with sync.Once but may still cause a race
// with any other use of the crc32 package anywhere. Thus we initialize it
// before.
var castagnoliTable *crc32.Table

func init() {
	castagnoliTable = crc32.MakeTable(crc32.Castagnoli)
}

// newCRC32 initializes a CRC32 hash with a preconfigured polynomial, so the
// polynomial may be easily changed in one location at a later time, if necessary.
func newCRC32() hash.Hash32 {
	return crc32.New(castagnoliTable)
}

func (s *streamSegment) WriteTo(w io.Writer) (n int64, err error) {
	// todo how to encode stream segment ?
	// blocks have a footer with min/max timestamp and offsets, and footer size.
	// block has a timestamps delta varint encoded, and list of lengths of entries.
	// todo: support structured metadata
	// s2w := s2.NewWriter(w)
	// written := 0
	// buffer
	// for _, e := range s.entries {
	// 	// s2w.Write(p []byte)
	// }
	return 0, nil
}

func writeChunk(w io.Writer, entries []*logproto.Entry) (int64, error) {
	if len(entries) == 0 {
		return 0, nil
	}
	var written int64
	// write lines entries
	s2w := s2.NewWriter(w)
	for _, e := range entries {
		n, err := s2w.Write([]byte(e.Line))
		if err != nil {
			return written, err
		}
		written += int64(n)
	}
	linesSize := written
	if err := s2w.Close(); err != nil {
		return written, err
	}

	// double delta encode timestamp
	var prevT, prevDelta, t, delta uint64
	buf := make([]byte, binary.MaxVarintLen64)
	for i, e := range entries {
		t = uint64(e.Timestamp.UnixNano())
		switch i {
		case 0:
			n := binary.PutUvarint(buf, t)
			if _, err := w.Write(buf[:n]); err != nil {
				return written, err
			}
			written += int64(n)
		case 1:
			delta = t - prevT
			n := binary.PutUvarint(buf, delta)
			if _, err := w.Write(buf[:n]); err != nil {
				return written, err
			}
			written += int64(n)
		default:
			delta = t - prevT
			dod := delta - prevDelta
			n := binary.PutUvarint(buf, dod)
			if _, err := w.Write(buf[:n]); err != nil {
				return written, err
			}
			written += int64(n)
		}
		prevT = t
		prevDelta = delta
	}
	timestampsSize := written - linesSize
	// write lengths
	for _, e := range entries {
		n := binary.PutUvarint(buf, uint64(len(e.Line)))
		if _, err := w.Write(buf[:n]); err != nil {
			return written, err
		}
		written += int64(n)
	}
	// mint, maxt := entries[0].Timestamp.UnixNano(), entries[len(entries)-1].Timestamp.UnixNano()

	return written, nil
}

// Reset clears the writer.
// After calling Reset, the writer can be reused.
// func(b *WalSegmentWriter) Reset() {

// 	b.tenants.Clear()

// 	return nil
// }
