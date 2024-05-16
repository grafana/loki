package wal

import (
	"io"
	"sort"

	"github.com/dolthub/swiss"
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
	// Write Stream offsets and labels ref.
	// TOC
	// len(TOC)

	return total, nil
}

func (s *streamSegment) WriteTo(w io.Writer) (n int64, err error) {
	return 0, nil
}

// Reset clears the writer.
// After calling Reset, the writer can be reused.
// func(b *WalSegmentWriter) Reset() {

// 	b.tenants.Clear()

// 	return nil
// }
