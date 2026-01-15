package queryrange

import (
	"container/heap"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestPriorityQueuePopForward(t *testing.T) {
	pq := &priorityqueue{direction: logproto.FORWARD}
	pq.streams = []*logproto.Stream{
		{
			Labels: "a",
			Entries: []logproto.Entry{
				{Timestamp: time.Unix(0, 1), Line: "a1"},
				{Timestamp: time.Unix(0, 4), Line: "a4"},
			},
		},
		{
			Labels: "b",
			Entries: []logproto.Entry{
				{Timestamp: time.Unix(0, 2), Line: "b2"},
				{Timestamp: time.Unix(0, 3), Line: "b3"},
			},
		},
	}

	heap.Init(pq)

	var (
		gotTimestamps []int64
		gotLabels     []string
	)

	for i := 0; pq.Len() > 0 && i < 4; i++ {
		stream := heap.Pop(pq).(*logproto.Stream)

		require.Len(t, stream.Entries, 1, "heap.Pop should return a stream with exactly one entry")
		entry := stream.Entries[0]

		gotTimestamps = append(gotTimestamps, entry.Timestamp.UnixNano())
		gotLabels = append(gotLabels, stream.Labels)
	}

	require.Equal(t, []int64{1, 2, 3, 4}, gotTimestamps)
	require.Equal(t, []string{"a", "b", "b", "a"}, gotLabels)
	require.Equal(t, 0, pq.Len(), "priority queue should be empty after consuming all entries")
}

func TestPriorityQueuePopBackward(t *testing.T) {
	pq := &priorityqueue{direction: logproto.BACKWARD}
	pq.streams = []*logproto.Stream{
		{
			Labels: "x",
			Entries: []logproto.Entry{
				{Timestamp: time.Unix(0, 10), Line: "x10"},
				{Timestamp: time.Unix(0, 1), Line: "x1"},
			},
		},
		{
			Labels: "y",
			Entries: []logproto.Entry{
				{Timestamp: time.Unix(0, 9), Line: "y9"},
				{Timestamp: time.Unix(0, 5), Line: "y5"},
			},
		},
	}

	heap.Init(pq)

	var (
		gotTimestamps []int64
		gotLabels     []string
	)
	for pq.Len() > 0 {
		stream := heap.Pop(pq).(*logproto.Stream)
		require.Len(t, stream.Entries, 1)

		gotTimestamps = append(gotTimestamps, stream.Entries[0].Timestamp.UnixNano())
		gotLabels = append(gotLabels, stream.Labels)
	}

	require.Equal(t, []int64{10, 9, 5, 1}, gotTimestamps)
	require.Equal(t, []string{"x", "y", "y", "x"}, gotLabels)
}
