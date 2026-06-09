package consumer

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

// windowEntry returns an entry whose timestamp falls into the given window.
func windowEntry(window time.Time, offset time.Duration, line string) push.Entry {
	return push.Entry{Timestamp: window.Add(offset), Line: line}
}

func TestTOCAlignedMultiBuilder_AppendSplitsAcrossWindows(t *testing.T) {
	factory := newTestBuilderFactory()
	m := NewTOCAlignedMultiBuilder(factory, math.MaxInt)

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	w2 := w1.Add(metastore.MetastoreWindowSize)

	// A single stream whose entries span two windows must be split into one
	// builder per window.
	require.NoError(t, m.Append("tenant", logproto.Stream{
		Labels: `{app="foo"}`,
		Entries: []push.Entry{
			windowEntry(w1, time.Minute, "a"),
			windowEntry(w2, time.Minute, "b"),
			windowEntry(w1, 2*time.Minute, "c"),
		},
	}, w1))

	builders := m.GetBuilders()
	require.Len(t, builders, 2)
	// Two windows means two builders were created.
	require.Equal(t, 2, factory.created)

	// Builders are returned sorted by ascending window start, so the first
	// builder must hold the earlier window.
	require.Equal(t, w1, truncatedWindow(t, builders[0]))
	require.Equal(t, w2, truncatedWindow(t, builders[1]))
}

func TestTOCAlignedMultiBuilder_AppendReusesBuilderForSameWindow(t *testing.T) {
	factory := newTestBuilderFactory()
	m := NewTOCAlignedMultiBuilder(factory, math.MaxInt)

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)

	require.NoError(t, m.Append("tenant", logproto.Stream{
		Labels:  `{app="foo"}`,
		Entries: []push.Entry{windowEntry(w1, time.Minute, "a")},
	}, w1))
	require.NoError(t, m.Append("tenant", logproto.Stream{
		Labels:  `{app="foo"}`,
		Entries: []push.Entry{windowEntry(w1, 2*time.Minute, "b")},
	}, w1))

	// Both appends land in the same window, so only one builder is created and
	// reused.
	require.Len(t, m.GetBuilders(), 1)
	require.Equal(t, 1, factory.created)
}

func TestTOCAlignedMultiBuilder_GetBuildersSorted(t *testing.T) {
	factory := newTestBuilderFactory()
	m := NewTOCAlignedMultiBuilder(factory, math.MaxInt)

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	w2 := w1.Add(metastore.MetastoreWindowSize)
	w3 := w2.Add(metastore.MetastoreWindowSize)

	// Append the windows out of order; GetBuilders must still return them in
	// ascending window order regardless of map iteration randomization.
	require.NoError(t, m.Append("tenant", logproto.Stream{
		Labels: `{app="foo"}`,
		Entries: []push.Entry{
			windowEntry(w3, time.Minute, "c"),
			windowEntry(w1, time.Minute, "a"),
			windowEntry(w2, time.Minute, "b"),
		},
	}, w1))

	builders := m.GetBuilders()
	require.Len(t, builders, 3)
	require.Equal(t, w1, truncatedWindow(t, builders[0]))
	require.Equal(t, w2, truncatedWindow(t, builders[1]))
	require.Equal(t, w3, truncatedWindow(t, builders[2]))
}

func TestTOCAlignedMultiBuilder_IsFullBoundsTotalMemory(t *testing.T) {
	factory := newTestBuilderFactory()
	m := NewTOCAlignedMultiBuilder(factory, math.MaxInt)

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	w2 := w1.Add(metastore.MetastoreWindowSize)

	// Fill a single window and record its size.
	require.NoError(t, m.Append("tenant", logproto.Stream{
		Labels:  `{app="foo"}`,
		Entries: []push.Entry{windowEntry(w1, time.Minute, "some log line")},
	}, w1))
	single := m.GetEstimatedSize()
	require.Positive(t, single)
	require.False(t, m.IsFull())

	// Fill a second window with comparable data; the total now exceeds a
	// single window's footprint.
	require.NoError(t, m.Append("tenant", logproto.Stream{
		Labels:  `{app="foo"}`,
		Entries: []push.Entry{windowEntry(w2, time.Minute, "some log line")},
	}, w1))
	total := m.GetEstimatedSize()
	require.Greater(t, total, single)

	// With a target equal to a single window's size, neither builder is full on
	// its own, but the group reports full because fullness is driven by the
	// *sum* across windows.
	m.maxBufferedBytes = single
	require.True(t, m.IsFull())
}

func TestTOCAlignedMultiBuilder_AppendDoesNotKeepEmptyBuilderOnError(t *testing.T) {
	factory := newTestBuilderFactory()
	m := NewTOCAlignedMultiBuilder(factory, math.MaxInt)

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	// Invalid labels make the underlying builder's Append fail.
	err := m.Append("tenant", logproto.Stream{
		Labels:  `{app="foo"`,
		Entries: []push.Entry{windowEntry(w1, time.Minute, "bad-labels")},
	}, w1)
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to parse labels")
	// A failed append must not leave an empty builder behind.
	require.Empty(t, m.GetBuilders())
}

func TestTOCAlignedMultiBuilder_AppendPropagatesFactoryError(t *testing.T) {
	factory := newTestBuilderFactory()
	factory.failAt = 0 // fail to create the very first builder
	m := NewTOCAlignedMultiBuilder(factory, math.MaxInt)

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	err := m.Append("tenant", logproto.Stream{
		Labels:  `{app="foo"}`,
		Entries: []push.Entry{windowEntry(w1, time.Minute, "a")},
	}, w1)
	require.Error(t, err)
	require.ErrorContains(t, err, "boom")
	require.Empty(t, m.GetBuilders())
}

func TestTOCAlignedMultiBuilder_Reset(t *testing.T) {
	factory := newTestBuilderFactory()
	m := NewTOCAlignedMultiBuilder(factory, math.MaxInt)

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	require.NoError(t, m.Append("tenant", logproto.Stream{
		Labels:  `{app="foo"}`,
		Entries: []push.Entry{windowEntry(w1, time.Minute, "a")},
	}, w1))
	require.NotEmpty(t, m.GetBuilders())
	require.Positive(t, m.GetEstimatedSize())

	m.Reset()
	require.Empty(t, m.GetBuilders())
	require.Equal(t, 0, m.GetEstimatedSize())
	require.False(t, m.IsFull())
}

// truncatedWindow returns the TOC window that the builder's single tenant time
// range belongs to.
func truncatedWindow(t *testing.T, b builder) time.Time {
	t.Helper()
	ranges := b.TimeRanges()
	require.Len(t, ranges, 1)
	return ranges[0].MinTime.UTC().Truncate(metastore.MetastoreWindowSize)
}
