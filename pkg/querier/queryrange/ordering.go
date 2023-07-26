package queryrange

import (
	"time"

	"github.com/grafana/loki/pkg/logproto"
)

/*
Utils for manipulating ordering
*/

type entries []logproto.Entry

func (m entries) Len() int           { return len(m) }
func (m entries) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }
func (m entries) Less(i, j int) bool { return m[i].Timestamp.Before(m[j].Timestamp) }

type sortedEntry struct {
	labels     string
	entry      logproto.Entry
	streamHash uint64
}

func lessAscending(e1, e2 sortedEntry) bool {
	if e1.entry.Timestamp.Equal(e2.entry.Timestamp) {
		if e1.streamHash == 0 {
			return e1.labels < e2.labels
		}
		return e1.streamHash < e2.streamHash
	}
	return e1.entry.Timestamp.Before(e2.entry.Timestamp)
}

func lessDescending(e1, e2 sortedEntry) bool {
	if e1.entry.Timestamp.Equal(e2.entry.Timestamp) {
		if e1.streamHash == 0 {
			return e1.labels < e2.labels
		}
		return e1.streamHash < e2.streamHash
	}
	return e1.entry.Timestamp.After(e2.entry.Timestamp)
}

func treeLess(direction logproto.Direction) (maxVal sortedEntry, less func(a, b sortedEntry) bool) {
	switch direction {
	case logproto.BACKWARD:
		maxVal = sortedEntry{entry: logproto.Entry{Timestamp: time.Time{}}}
		less = lessDescending
	case logproto.FORWARD:
		maxVal = sortedEntry{entry: logproto.Entry{Timestamp: time.Unix(1<<63-62135596801, 999999999)}}
		less = lessAscending
	default:
		panic("bad direction")
	}
	return
}

// mergeStreamIterator one for a stream, this should be not much
type mergeStreamIterator struct {
	cur    sortedEntry
	stream *logproto.Stream
}

func (n *mergeStreamIterator) Next() bool {
	if len(n.stream.Entries) > 0 {
		n.cur = sortedEntry{
			labels:     n.stream.Labels,
			entry:      n.stream.Entries[0],
			streamHash: n.stream.Hash,
		}
		n.stream.Entries = n.stream.Entries[1:]
		return true
	}
	n.cur = sortedEntry{}
	return false
}

// priorityqueue is used for extracting a limited # of entries from a set of sorted streams
type priorityqueue struct {
	streams   []*logproto.Stream
	direction logproto.Direction
}

func (pq *priorityqueue) Len() int { return len(pq.streams) }

func (pq *priorityqueue) Less(i, j int) bool {
	if pq.direction == logproto.FORWARD {
		return pq.streams[i].Entries[0].Timestamp.UnixNano() < pq.streams[j].Entries[0].Timestamp.UnixNano()
	}
	return pq.streams[i].Entries[0].Timestamp.UnixNano() > pq.streams[j].Entries[0].Timestamp.UnixNano()

}

func (pq *priorityqueue) Swap(i, j int) {
	pq.streams[i], pq.streams[j] = pq.streams[j], pq.streams[i]
}

func (pq *priorityqueue) Push(x interface{}) {
	stream := x.(*logproto.Stream)
	pq.streams = append(pq.streams, stream)
}

// Pop returns a stream with one entry. It pops the first entry of the first stream
// then re-pushes the remainder of that stream if non-empty back into the queue
func (pq *priorityqueue) Pop() interface{} {
	n := pq.Len()
	stream := pq.streams[n-1]
	pq.streams[n-1] = nil // avoid memory leak
	pq.streams = pq.streams[:n-1]

	// put the rest of the stream back into the priorityqueue if more entries exist
	if len(stream.Entries) > 1 {
		remaining := *stream
		remaining.Entries = remaining.Entries[1:]
		pq.Push(&remaining)
	}

	stream.Entries = stream.Entries[:1]
	return stream
}
