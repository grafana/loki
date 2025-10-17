package queryrange

import (
	"slices"
	"sort"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/loki/v3/pkg/logproto"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
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

	// Debug logging before merge
	a.debugLogPreMerge()

	sort.Sort(a)
	for _, m := range a.markers {
		result = append(result, m...)
	}

	// Debug logging after merge
	a.debugLogPostMerge(result)

	return result
}

// debugLogPreMerge logs information about markers before merging for FORWARD direction.
// This is used for debugging ordering issues in query responses.
func (a byDir) debugLogPreMerge() {
	if a.direction != logproto.FORWARD {
		return
	}

	level.Debug(util_log.Logger).Log(
		"msg", "byDir.merge start",
		"experiment", "forward-missing-logs",
		"direction", "FORWARD",
		"num_markers", len(a.markers),
		"total_entries", a.EntriesCount(),
		"labels", a.labels,
	)

	// Log details about each marker before sorting
	for idx, m := range a.markers {
		if len(m) == 0 {
			continue
		}
		firstTs := m[0].Timestamp.UnixNano()
		lastTs := m[len(m)-1].Timestamp.UnixNano()

		// Check if entries within this marker are sorted in ascending order
		markerSorted := slices.IsSortedFunc(m, func(a, b logproto.Entry) int {
			if a.Timestamp.UnixNano() < b.Timestamp.UnixNano() {
				return -1
			}
			if a.Timestamp.UnixNano() > b.Timestamp.UnixNano() {
				return 1
			}
			return 0
		})

		level.Debug(util_log.Logger).Log(
			"msg", "byDir.merge marker",
			"experiment", "forward-missing-logs",
			"idx", idx,
			"entries", len(m),
			"first_ts", firstTs,
			"first_ts_human", time.Unix(0, firstTs).Format(time.RFC3339Nano),
			"last_ts", lastTs,
			"last_ts_human", time.Unix(0, lastTs).Format(time.RFC3339Nano),
			"sorted", markerSorted,
		)
	}
}

// debugLogPostMerge logs information about the merged result for FORWARD direction.
// This is used for debugging ordering issues in query responses.
func (a byDir) debugLogPostMerge(result []logproto.Entry) {
	if a.direction != logproto.FORWARD || len(result) == 0 {
		return
	}

	firstTs := result[0].Timestamp.UnixNano()
	lastTs := result[len(result)-1].Timestamp.UnixNano()

	// Check if final result is sorted in ascending order
	resultSorted := slices.IsSortedFunc(result, func(a, b logproto.Entry) int {
		if a.Timestamp.UnixNano() < b.Timestamp.UnixNano() {
			return -1
		}
		if a.Timestamp.UnixNano() > b.Timestamp.UnixNano() {
			return 1
		}
		return 0
	})

	level.Debug(util_log.Logger).Log(
		"msg", "byDir.merge complete",
		"experiment", "forward-missing-logs",
		"sorted", resultSorted,
		"total_entries", len(result),
		"first_ts", firstTs,
		"first_ts_human", time.Unix(0, firstTs).Format(time.RFC3339Nano),
		"last_ts", lastTs,
		"last_ts_human", time.Unix(0, lastTs).Format(time.RFC3339Nano),
	)
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
