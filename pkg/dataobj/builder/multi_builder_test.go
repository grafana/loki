package builder

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

// windowEntry returns an entry whose timestamp falls into the given window.
func windowEntry(window time.Time, offset time.Duration, line string) push.Entry {
	return push.Entry{Timestamp: window.Add(offset), Line: line}
}

func TestMultiObjectBuilder_AppendSplitsAcrossWindows(t *testing.T) {
	factory := newTestBuilderFactory()
	m := NewMultiObjectBuilder(factory, math.MaxInt, newTestWatermarks())

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	w2 := w1.Add(metastore.MetastoreWindowSize)

	// A single stream whose entries span two windows must be split into one
	// builder per window.
	require.NoError(t, appendToTestBuilder(m, "tenant", logproto.Stream{
		Labels: `{app="foo"}`,
		Entries: []push.Entry{
			windowEntry(w1, time.Minute, "a"),
			windowEntry(w2, time.Minute, "b"),
			windowEntry(w1, 2*time.Minute, "c"),
		},
	}, w1))

	builders := m.getBuilders()
	require.Len(t, builders, 2)
	// Two windows means two builders were created.
	require.Equal(t, 2, factory.created)

	// Builders are returned sorted by ascending window start, so the first
	// builder must hold the earlier window.
	require.Equal(t, w1, truncatedWindow(t, builders[0]))
	require.Equal(t, w2, truncatedWindow(t, builders[1]))
}

func TestMultiObjectBuilder_AppendSplitsAcrossTenantsAndWindows(t *testing.T) {
	factory := newTestBuilderFactory()
	m := NewMultiObjectBuilder(factory, math.MaxInt, newTestWatermarks())

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	w2 := w1.Add(metastore.MetastoreWindowSize)

	// Append tenants and windows out of order to show that getBuilders sorts
	// them rather than relying on insertion order.
	for _, tenant := range []string{"tenant-b", "tenant-a"} {
		require.NoError(t, appendToTestBuilder(m, tenant, logproto.Stream{
			Labels: `{app="foo"}`,
			Entries: []push.Entry{
				windowEntry(w2, time.Minute, "b"),
				windowEntry(w1, time.Minute, "a"),
			},
		}, w1))
	}

	builders := m.getBuilders()
	require.Len(t, builders, 4)
	require.Equal(t, 4, factory.created)

	type scope struct {
		tenant string
		window time.Time
	}
	var got []scope
	for _, b := range builders {
		ranges := b.TimeRanges()
		require.Len(t, ranges, 1, "each builder must hold exactly one tenant")
		got = append(got, scope{ranges[0].Tenant, truncatedWindow(t, b)})
	}
	require.Equal(t, []scope{
		{"tenant-a", w1},
		{"tenant-a", w2},
		{"tenant-b", w1},
		{"tenant-b", w2},
	}, got)
}

func TestMultiObjectBuilder_AppendReusesBuilderForSameWindow(t *testing.T) {
	factory := newTestBuilderFactory()
	m := NewMultiObjectBuilder(factory, math.MaxInt, newTestWatermarks())

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)

	require.NoError(t, appendToTestBuilder(m, "tenant", logproto.Stream{
		Labels:  `{app="foo"}`,
		Entries: []push.Entry{windowEntry(w1, time.Minute, "a")},
	}, w1))
	require.NoError(t, appendToTestBuilder(m, "tenant", logproto.Stream{
		Labels:  `{app="foo"}`,
		Entries: []push.Entry{windowEntry(w1, 2*time.Minute, "b")},
	}, w1))

	// Both appends land in the same window, so only one builder is created and
	// reused.
	require.Len(t, m.getBuilders(), 1)
	require.Equal(t, 1, factory.created)
}

func TestMultiObjectBuilder_GetBuildersSorted(t *testing.T) {
	factory := newTestBuilderFactory()
	m := NewMultiObjectBuilder(factory, math.MaxInt, newTestWatermarks())

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	w2 := w1.Add(metastore.MetastoreWindowSize)
	w3 := w2.Add(metastore.MetastoreWindowSize)

	// Append the windows out of order; getBuilders must still return them in
	// ascending window order regardless of map iteration randomization.
	require.NoError(t, appendToTestBuilder(m, "tenant", logproto.Stream{
		Labels: `{app="foo"}`,
		Entries: []push.Entry{
			windowEntry(w3, time.Minute, "c"),
			windowEntry(w1, time.Minute, "a"),
			windowEntry(w2, time.Minute, "b"),
		},
	}, w1))

	builders := m.getBuilders()
	require.Len(t, builders, 3)
	require.Equal(t, w1, truncatedWindow(t, builders[0]))
	require.Equal(t, w2, truncatedWindow(t, builders[1]))
	require.Equal(t, w3, truncatedWindow(t, builders[2]))
}

func TestMultiObjectBuilder_IsFullBoundsTotalMemory(t *testing.T) {
	factory := newTestBuilderFactory()
	m := NewMultiObjectBuilder(factory, math.MaxInt, newTestWatermarks())

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	w2 := w1.Add(metastore.MetastoreWindowSize)

	// Fill a single window and record its size.
	require.NoError(t, appendToTestBuilder(m, "tenant", logproto.Stream{
		Labels:  `{app="foo"}`,
		Entries: []push.Entry{windowEntry(w1, time.Minute, "some log line")},
	}, w1))
	single := m.GetEstimatedSize()
	require.Positive(t, single)
	require.False(t, m.IsFull())

	// Fill a second window with comparable data; the total now exceeds a
	// single window's footprint.
	require.NoError(t, appendToTestBuilder(m, "tenant", logproto.Stream{
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

func TestMultiObjectBuilder_AppendDoesNotKeepEmptyBuilderOnError(t *testing.T) {
	factory := newTestBuilderFactory()
	m := NewMultiObjectBuilder(factory, math.MaxInt, newTestWatermarks())

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	// Invalid labels make the underlying builder's Append fail.
	err := appendToTestBuilder(m, "tenant", logproto.Stream{
		Labels:  `{app="foo"`,
		Entries: []push.Entry{windowEntry(w1, time.Minute, "bad-labels")},
	}, w1)
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to parse labels")
	// A failed append must not leave an empty builder behind.
	require.Empty(t, m.getBuilders())
}

func TestMultiObjectBuilder_AppendPropagatesFactoryError(t *testing.T) {
	factory := newTestBuilderFactory()
	factory.failAt = 0 // fail to create the very first builder
	m := NewMultiObjectBuilder(factory, math.MaxInt, newTestWatermarks())

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	err := appendToTestBuilder(m, "tenant", logproto.Stream{
		Labels:  `{app="foo"}`,
		Entries: []push.Entry{windowEntry(w1, time.Minute, "a")},
	}, w1)
	require.Error(t, err)
	require.ErrorContains(t, err, "boom")
	require.Empty(t, m.getBuilders())
}

func TestMultiObjectBuilder_Reset(t *testing.T) {
	factory := newTestBuilderFactory()
	watermarks := newTestWatermarks()
	m := NewMultiObjectBuilder(factory, math.MaxInt, watermarks)

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	require.NoError(t, appendToTestBuilder(m, "tenant", logproto.Stream{
		Labels:  `{app="foo"}`,
		Entries: []push.Entry{windowEntry(w1, time.Minute, "a")},
	}, w1))
	require.NotEmpty(t, m.getBuilders())
	require.Positive(t, m.GetEstimatedSize())

	readsBefore := readSamples(t, watermarks, resultOK)
	m.Reset(7)
	require.Empty(t, m.getBuilders())
	require.Equal(t, 0, m.GetEstimatedSize())
	require.False(t, m.IsFull())

	// The cache keeps the scope of the dropped builder at the flush offset.
	watermark, err := watermarks.get(t.Context(), builderScope{tenant: "tenant", window: w1})
	require.NoError(t, err)
	require.Equal(t, int64(7), *watermark)
	require.Equal(t, readsBefore, readSamples(t, watermarks, resultOK))
}

// truncatedWindow returns the TOC window that the builder's single tenant time
// range belongs to.
func truncatedWindow(t *testing.T, b builder) time.Time {
	t.Helper()
	ranges := b.TimeRanges()
	require.Len(t, ranges, 1)
	return ranges[0].MinTime.UTC().Truncate(metastore.MetastoreWindowSize)
}

// appendToTestBuilder appends the stream as a record at offset 0.
func appendToTestBuilder(m *MultiObjectBuilder, tenant string, stream logproto.Stream, recTime time.Time) error {
	_, err := m.Append(context.Background(), tenant, stream, recTime, 0)
	return err
}

func TestMultiObjectBuilder_AppendWatermarks(t *testing.T) {
	stream := logproto.Stream{Labels: `{app="foo"}`, Entries: []push.Entry{windowEntry(testWindow1, time.Minute, "a")}}

	t.Run("drops a record at or below the watermark of its window and keeps the others", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writeTestWatermark(t, bucket, "tenant", testWindow1, 10)
		factory := newTestBuilderFactory()
		m := NewMultiObjectBuilder(factory, math.MaxInt, newBucketWatermarks(bucket))

		res, err := m.Append(t.Context(), "tenant", stream, testWindow1, 10)
		require.NoError(t, err)
		require.Equal(t, AppendResult{Dropped: 1}, res)
		require.Empty(t, m.getBuilders())
		require.Equal(t, 0, factory.created)

		res, err = m.Append(t.Context(), "tenant", stream, testWindow1, 11)
		require.NoError(t, err)
		require.Equal(t, AppendResult{Appended: 1}, res)
		require.Len(t, m.getBuilders(), 1)
	})

	t.Run("checks later records against the initial watermark of the builder, also after the cache forgot it", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writeTestWatermark(t, bucket, "tenant", testWindow1, 10)
		w := newBucketWatermarks(bucket)
		w.entries = newWatermarkEntries(1)
		m := NewMultiObjectBuilder(newTestBuilderFactory(), math.MaxInt, w)
		_, err := m.Append(t.Context(), "tenant", stream, testWindow1, 11)
		require.NoError(t, err)
		// Another scope pushes the first one out of the cache of one entry.
		_, err = m.Append(t.Context(), "other", stream, testWindow1, 11)
		require.NoError(t, err)

		// Another process raises the watermark of the ToC of the first scope. A
		// new read would show it, and the builder would hide the conflict.
		writeTestWatermark(t, bucket, "tenant", testWindow1, 50)
		res, err := m.Append(t.Context(), "tenant", stream, testWindow1, 20)

		require.NoError(t, err)
		require.Equal(t, AppendResult{Appended: 1}, res)
		require.Equal(t, uint64(2), readSamples(t, w, resultOK))
		for _, b := range m.getBuilders() {
			if b.TimeRanges()[0].Tenant == "tenant" {
				require.Equal(t, int64(10), *b.(WatermarkedBuilder).InitialWatermark())
			}
		}
	})

	t.Run("starts a new builder of every flushed scope from the offset of the flush", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writeTestWatermark(t, bucket, "tenant", testWindow1, 10)
		w := newBucketWatermarks(bucket)
		m := NewMultiObjectBuilder(newTestBuilderFactory(), math.MaxInt, w)
		scopes := []struct {
			tenant string
			window time.Time
		}{{"tenant", testWindow1}, {"tenant", testWindow2}, {"other", testWindow1}}
		for _, sc := range scopes {
			_, err := m.Append(t.Context(), sc.tenant, logproto.Stream{Labels: `{app="foo"}`, Entries: []push.Entry{windowEntry(sc.window, time.Minute, "a")}}, sc.window, 11)
			require.NoError(t, err)
		}
		m.Reset(11)

		for _, sc := range scopes {
			res, err := m.Append(t.Context(), sc.tenant, logproto.Stream{Labels: `{app="foo"}`, Entries: []push.Entry{windowEntry(sc.window, time.Minute, "a")}}, sc.window, 12)
			require.NoError(t, err)
			require.Equal(t, AppendResult{Appended: 1}, res)
		}

		builders := m.getBuilders()
		require.Len(t, builders, len(scopes))
		for _, b := range builders {
			require.Equal(t, int64(11), *b.(WatermarkedBuilder).InitialWatermark())
		}
		require.Equal(t, uint64(len(scopes)), readSamples(t, w, resultOK))
	})
}
