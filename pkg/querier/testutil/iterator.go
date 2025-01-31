package testutil

import (
	"fmt"
	"time"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// mockStreamIterator returns an iterator with 1 stream and quantity entries,
// where entries timestamp and line string are constructed as sequential numbers
// starting at from
func NewFakeStreamIterator(from int, quantity int) iter.EntryIterator {
	return iter.NewStreamIterator(NewFakeStream(from, quantity))
}

// mockStream return a stream with quantity entries, where entries timestamp and
// line string are constructed as sequential numbers starting at from
func NewFakeStream(from int, quantity int) logproto.Stream {
	return NewFakeStreamWithLabels(from, quantity, `{type="test"}`)
}

func NewFakeStreamWithLabels(from int, quantity int, labels string) logproto.Stream {
	entries := make([]logproto.Entry, 0, quantity)

	for i := from; i < from+quantity; i++ {
		entries = append(entries, logproto.Entry{
			Timestamp: time.Unix(int64(i), 0),
			Line:      fmt.Sprintf("line %d", i),
		})
	}

	return logproto.Stream{
		Entries: entries,
		Labels:  labels,
	}
}
