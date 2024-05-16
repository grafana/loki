package wal

import (
	"sort"

	"github.com/dolthub/swiss"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type WalSegmentWriter struct {
	tenants *swiss.Map[string, *tenantSegment]
}

type tenantSegment struct {
	streams *swiss.Map[string, *streamSegment]
}

type streamSegment struct {
	entries []*logproto.Entry
	maxt    int64
	// add the labels.Labels
}

// NewWalSegmentWriter creates a new WalSegmentWriter.
func NewWalSegmentWriter() *WalSegmentWriter {
	return &WalSegmentWriter{
		tenants: swiss.NewMap[string, *tenantSegment](64),
	}
}

// Labels are passed a string  `{foo="bar",baz="qux"}`  `{foo="foo",baz="foo"}`. labels.Labels => Symbols foo, baz , qux
func (b *WalSegmentWriter) Append(tenantID, labels string, entries []*logproto.Entry) {
	t, ok := b.tenants.Get(tenantID)
	if !ok {
		t = &tenantSegment{streams: swiss.NewMap[string, *streamSegment](64)}
		b.tenants.Put(tenantID, t)
	}
	s, ok := t.streams.Get(labels)
	if !ok {
		s = &streamSegment{
			// todo: should be pooled.
			// prometheus bucketed pool
			// https://pkg.go.dev/github.com/prometheus/prometheus/util/pool
			entries: make([]*logproto.Entry, 0, 64),
		}
		t.streams.Put(labels, s)
	}

	// check the order.
	if len(s.entries) == 0 {
		s.maxt = entries[len(entries)-1].Timestamp.UnixNano()
		s.entries = append(s.entries, entries...)
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

// func(b *WalSegmentWriter) Reset() {

// 	b.tenants.Clear()

// 	return nil
// }

// func (b *WalSegmentWriter) Close() (io.ReadCloser, error) {
// 	reader,writer := io.Pipe()
// 	writer.Write([]byte("hello"))
// 	return reader, nil
// }

func (b *WalSegmentWriter) Read(p []byte) (n int, err error) {
	return 0, nil
}
