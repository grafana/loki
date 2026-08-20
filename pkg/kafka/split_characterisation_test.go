package kafka

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// These tests pin the record-splitting behaviour of EncodeWithTopic as it stands, so that
// moving the split behind the pushmodel interface can be shown not to change it. They
// assert properties and exact record counts rather than opaque byte totals, so a failure
// says what changed.

func splitStream(t *testing.T, entries int, lineLen int, maxSize int) ([]int, [][]push.Entry) {
	t.Helper()

	stream := logproto.Stream{Labels: `{app="a", env="prod"}`, Hash: 1234}
	for i := range entries {
		stream.Entries = append(stream.Entries, push.Entry{
			Timestamp:          time.Unix(0, int64(i+1)),
			Line:               fmt.Sprintf("%0*d", lineLen, i),
			StructuredMetadata: push.LabelsAdapter{{Name: "trace_id", Value: fmt.Sprintf("%d", i)}},
		})
	}

	records, err := Encode(0, "tenant", stream, maxSize)
	require.NoError(t, err)

	dec, err := NewDecoder()
	require.NoError(t, err)

	sizes := make([]int, 0, len(records))
	got := make([][]push.Entry, 0, len(records))
	for _, rec := range records {
		require.LessOrEqual(t, len(rec.Value), maxSize, "a record exceeded the size limit")
		sizes = append(sizes, len(rec.Value))

		decoded, err := dec.DecodeWithoutLabels(rec.Value)
		require.NoError(t, err)
		require.Equal(t, stream.Labels, decoded.Labels)
		require.Equal(t, stream.Hash, decoded.Hash)
		got = append(got, decoded.Entries)
	}
	return sizes, got
}

func TestSplitFitsInOneRecord(t *testing.T) {
	sizes, parts := splitStream(t, 10, 20, 1<<20)

	require.Len(t, sizes, 1, "a small stream must stay in a single record")
	require.Len(t, parts[0], 10)
}

func TestSplitAcrossRecordsPreservesEveryEntryInOrder(t *testing.T) {
	const entries = 200
	sizes, parts := splitStream(t, entries, 100, 4096)

	require.Greater(t, len(sizes), 1, "this stream is meant to need several records")

	var flat []push.Entry
	for _, p := range parts {
		require.NotEmpty(t, p, "an empty record must never be emitted")
		flat = append(flat, p...)
	}
	require.Len(t, flat, entries, "every entry must survive the split exactly once")
	for i := range flat {
		require.Equal(t, fmt.Sprintf("%0*d", 100, i), flat[i].Line, "entries must stay in order")
		require.Equal(t, int64(i+1), flat[i].Timestamp.UnixNano())
		require.Equal(t, fmt.Sprintf("%d", i), flat[i].StructuredMetadata[0].Value,
			"structured metadata must ride along with its entry")
	}
}

func TestSplitPacksRecordsUpToTheLimit(t *testing.T) {
	// Every record but the last should be close to full: the encoder only starts a new
	// one when the next entry would not fit. This is what stops the split from
	// degenerating into one record per entry.
	sizes, _ := splitStream(t, 200, 100, 4096)

	for i, size := range sizes[:len(sizes)-1] {
		require.Greater(t, size, 4096-200,
			"record %d is only %d bytes, far below the 4096 limit", i, size)
	}
}

func TestSplitRejectsAnEntryLargerThanTheLimit(t *testing.T) {
	stream := logproto.Stream{Labels: `{app="a"}`, Hash: 1}
	stream.Entries = append(stream.Entries, push.Entry{
		Timestamp: time.Unix(0, 1),
		Line:      strings.Repeat("x", 5000),
	})

	_, err := Encode(0, "tenant", stream, 1000)
	require.ErrorContains(t, err, "exceeds maximum allowed size")
}

func TestSplitRejectsAnOversizedEntryFoundPartWayThrough(t *testing.T) {
	stream := logproto.Stream{Labels: `{app="a"}`, Hash: 1}
	stream.Entries = append(stream.Entries,
		push.Entry{Timestamp: time.Unix(0, 1), Line: strings.Repeat("a", 100)},
		push.Entry{Timestamp: time.Unix(0, 2), Line: strings.Repeat("b", 5000)},
	)

	_, err := Encode(0, "tenant", stream, 1000)
	require.ErrorContains(t, err, "exceeds maximum allowed size")
}
