package queryrange

import (
	"sort"

	"github.com/grafana/loki/v3/pkg/logproto"
)

/*
Utils for manipulating ordering
*/

type entries []logproto.Entry

func (m entries) start() int64 {
	if len(m) == 0 {
		return 0
	}
	return m[0].Timestamp.UnixNano()
}

type byDir struct {
	markers   []entries
	direction logproto.Direction
	labels    string
}

func (a byDir) Len() int      { return len(a.markers) }
func (a byDir) Swap(i, j int) { a.markers[i], a.markers[j] = a.markers[j], a.markers[i] }
func (a byDir) Less(i, j int) bool {
	x, y := a.markers[i].start(), a.markers[j].start()

	if a.direction == logproto.BACKWARD {
		return x > y
	}
	return y > x
}
func (a byDir) EntriesCount() (n int) {
	for _, m := range a.markers {
		n += len(m)
	}
	return n
}

func (a byDir) merge() []logproto.Entry {
	result := make([]logproto.Entry, 0, a.EntriesCount())

	sort.Sort(a)
	for _, m := range a.markers {
		result = append(result, m...)
	}
	return result
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
