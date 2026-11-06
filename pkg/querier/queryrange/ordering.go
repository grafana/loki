package queryrange

import (
	"container/heap"
	"sort"

	"github.com/grafana/loki/v3/pkg/logproto"
)

/*
Utils for manipulating ordering
*/

type entries []logproto.Entry

type byDir struct {
	markers   []entries
	direction logproto.Direction
	labels    string
}

func (a byDir) EntriesCount() (n int) {
	for _, m := range a.markers {
		n += len(m)
	}
	return n
}

func (a byDir) merge() []logproto.Entry {
	// Flatten all entries, tagging each with the index of the response (marker)
	// it came from so we can tell intra-response from inter-response duplicates.
	type tagged struct {
		entry  logproto.Entry
		marker int
	}
	all := make([]tagged, 0, a.EntriesCount())
	for mi, m := range a.markers {
		for _, e := range m {
			all = append(all, tagged{entry: e, marker: mi})
		}
	}

	// Sort by timestamp (respecting direction). A stable sort keeps the original
	// relative order of entries that share a timestamp.
	sort.SliceStable(all, func(i, j int) bool {
		if a.direction == logproto.BACKWARD {
			return all[i].entry.Timestamp.After(all[j].entry.Timestamp)
		}
		return all[i].entry.Timestamp.Before(all[j].entry.Timestamp)
	})

	// These responses are assumed to be non-overlapping, but stream sharding and
	// query sharding can cause the same physical stream to be returned by more
	// than one sub-query. Drop entries that are identical to one already kept
	// from a *different* response at the same timestamp, so overlapping
	// sub-queries don't surface as duplicate log lines. Duplicates originating
	// within a single response are preserved as-is.
	result := make([]logproto.Entry, 0, len(all))
	markers := make([]int, 0, len(all))
	for _, t := range all {
		dup := false
		for j := len(result) - 1; j >= 0 && result[j].Timestamp.Equal(t.entry.Timestamp); j-- {
			if markers[j] != t.marker && result[j].Equal(t.entry) {
				dup = true
				break
			}
		}
		if !dup {
			result = append(result, t.entry)
			markers = append(markers, t.marker)
		}
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
		heap.Push(pq, &remaining)
	}

	stream.Entries = stream.Entries[:1]
	return stream
}
