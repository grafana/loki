package consumer

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// countingBuilderFactory wraps a [logsobj.BuilderFactory] and optionally fails
// after a configured number of successful calls. Used to assert that
// TOCAlignedBuilderGroup propagates factory errors.
type countingBuilderFactory struct {
	inner       *logsobj.BuilderFactory
	created     int
	failAfter   int // 0 = never fail
	failWithErr error
}

func (f *countingBuilderFactory) NewBuilder() (*logsobj.Builder, error) {
	if f.failAfter > 0 && f.created >= f.failAfter {
		return nil, f.failWithErr
	}
	f.created++
	return f.inner.NewBuilder()
}

func newCountingFactory(t *testing.T) *countingBuilderFactory {
	t.Helper()
	inner, err := logsobj.NewBuilderFactory(testBuilderCfg, scratch.NewMemory(), logsobj.NewBuilderMetrics())
	require.NoError(t, err)
	return &countingBuilderFactory{inner: inner}
}

// windowEntry returns a stream entry whose timestamp falls inside the
// requested 12-hour TOC window.
func windowEntry(window time.Time, offset time.Duration, line string) push.Entry {
	return push.Entry{Timestamp: window.Add(offset), Line: line}
}

func TestTOCAlignedBuilderGroup_SingleWindow(t *testing.T) {
	factory := newCountingFactory(t)
	g := NewTOCAlignedBuilderGroup(factory, math.MaxInt)

	window := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	require.NoError(t, g.Append("tenant", logproto.Stream{
		Labels: `{app="foo"}`,
		Entries: []push.Entry{
			windowEntry(window, time.Minute, "a"),
			windowEntry(window, 2*time.Minute, "b"),
		},
	}, window))

	require.Equal(t, 1, factory.created)
	require.Len(t, g.GetBuilders(), 1)
	require.False(t, g.IsFull())
}

func TestTOCAlignedBuilderGroup_SplitsAcrossWindows(t *testing.T) {
	factory := newCountingFactory(t)
	g := NewTOCAlignedBuilderGroup(factory, math.MaxInt)

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	w2 := w1.Add(metastore.TOCWindowSize)

	require.NoError(t, g.Append("tenant", logproto.Stream{
		Labels: `{app="foo"}`,
		Entries: []push.Entry{
			windowEntry(w1, time.Minute, "first-window"),
			windowEntry(w2, time.Minute, "second-window"),
		},
	}, w1))

	require.Equal(t, 2, factory.created)
	require.Len(t, g.GetBuilders(), 2)

	for _, b := range g.GetBuilders() {
		ranges := b.TimeRanges()
		require.Len(t, ranges, 1, "each window builder should hold exactly one tenant time range")
		// Verify that each builder's MinTime/MaxTime fall inside a single
		// 12-hour window: truncating the range endpoints to the window size
		// should yield the same window for MinTime and MaxTime.
		require.Equal(t,
			ranges[0].MinTime.Truncate(metastore.TOCWindowSize),
			ranges[0].MaxTime.Truncate(metastore.TOCWindowSize),
			"a window builder must only contain entries from a single TOC window",
		)
	}
}

func TestTOCAlignedBuilderGroup_FactoryFailurePropagates(t *testing.T) {
	factory := newCountingFactory(t)
	factory.failAfter = 1
	factory.failWithErr = errors.New("boom")

	g := NewTOCAlignedBuilderGroup(factory, math.MaxInt)

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	w2 := w1.Add(metastore.TOCWindowSize)

	err := g.Append("tenant", logproto.Stream{
		Labels: `{app="foo"}`,
		Entries: []push.Entry{
			windowEntry(w1, time.Minute, "ok"),
			windowEntry(w2, time.Minute, "fails"),
		},
	}, w1)
	// Exactly one of the two windows will be able to grab a builder; the
	// other one will hit the factory error. Map iteration order decides
	// which one it is but the error must bubble up either way.
	require.Error(t, err)
	require.ErrorContains(t, err, "boom")
}

func TestTOCAlignedBuilderGroup_AppendFailureDoesNotRetainEmptyBuilder(t *testing.T) {
	factory := newCountingFactory(t)
	g := NewTOCAlignedBuilderGroup(factory, math.MaxInt)

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)

	// Invalid LogQL labels force Builder.Append -> parseLabels to fail before
	// the builder records anything. We want to verify that a brand-new
	// per-window builder created for this call does not get left behind in the
	// group's map when Append returns an error.
	err := g.Append("tenant", logproto.Stream{
		Labels: "not a valid label string",
		Entries: []push.Entry{
			windowEntry(w1, time.Minute, "a"),
		},
	}, w1)
	require.Error(t, err)
	require.ErrorContains(t, err, "append for window")

	require.Empty(t, g.GetBuilders(),
		"a builder that never successfully appended must not be retained in the group")
	require.Equal(t, 0, g.GetEstimatedSize())
	require.False(t, g.IsFull())

	// A subsequent valid Append for the same window must succeed, which also
	// sanity-checks that the group is usable after the earlier failure.
	require.NoError(t, g.Append("tenant", logproto.Stream{
		Labels: `{app="foo"}`,
		Entries: []push.Entry{
			windowEntry(w1, time.Minute, "a"),
		},
	}, w1))
	require.Len(t, g.GetBuilders(), 1)
}

func TestTOCAlignedBuilderGroup_IsFullBoundsTotalMemory(t *testing.T) {
	// target is slightly larger than the memory footprint of a single entry
	// but small enough that two windows together exceed it: this lets us
	// assert that IsFull reacts to the *sum* across builders.
	factory := newCountingFactory(t)
	g := NewTOCAlignedBuilderGroup(factory, 1<<10) // 1 KiB

	require.False(t, g.IsFull(), "fresh group should not be full")

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	w2 := w1.Add(metastore.TOCWindowSize)

	// Keep appending until the group reports full. We pump entries into
	// two distinct windows so no single builder would flip IsFull on its
	// own; only the sum does.
	bigLine := make([]byte, 512)
	for i := range bigLine {
		bigLine[i] = 'x'
	}
	var appends int
	for !g.IsFull() && appends < 128 {
		require.NoError(t, g.Append("tenant", logproto.Stream{
			Labels: `{app="foo"}`,
			Entries: []push.Entry{
				windowEntry(w1, time.Duration(appends)*time.Second, string(bigLine)),
				windowEntry(w2, time.Duration(appends)*time.Second, string(bigLine)),
			},
		}, w1))
		appends++
	}
	require.True(t, g.IsFull(), "group must eventually report full when sum exceeds the target")
	require.GreaterOrEqual(t, g.GetEstimatedSize(), 1<<10)
}

func TestTOCAlignedBuilderGroup_ResetClearsState(t *testing.T) {
	factory := newCountingFactory(t)
	g := NewTOCAlignedBuilderGroup(factory, math.MaxInt)

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	require.NoError(t, g.Append("tenant", logproto.Stream{
		Labels: `{app="foo"}`,
		Entries: []push.Entry{
			windowEntry(w1, time.Minute, "a"),
		},
	}, w1))

	require.Len(t, g.GetBuilders(), 1)
	require.Greater(t, g.GetEstimatedSize(), 0)

	g.Reset()
	require.Empty(t, g.GetBuilders())
	require.Equal(t, 0, g.GetEstimatedSize())
	require.False(t, g.IsFull())
}

func TestTOCAlignedBuilderGroup_GetBuildersReturnsSnapshot(t *testing.T) {
	factory := newCountingFactory(t)
	g := NewTOCAlignedBuilderGroup(factory, math.MaxInt)

	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	w2 := w1.Add(metastore.TOCWindowSize)
	require.NoError(t, g.Append("tenant", logproto.Stream{
		Labels: `{app="foo"}`,
		Entries: []push.Entry{
			windowEntry(w1, time.Minute, "a"),
			windowEntry(w2, time.Minute, "b"),
		},
	}, w1))

	snapshot := g.GetBuilders()
	require.Len(t, snapshot, 2)

	// Subsequent Reset must not mutate the already-returned snapshot so
	// flushCommitter can iterate safely after Reset runs in the defer.
	g.Reset()
	require.Len(t, snapshot, 2, "snapshot must be independent of the group's internal state")
}

func TestTOCAlignedBuilderGroup_GetBuildersIsSortedByWindow(t *testing.T) {
	factory := newCountingFactory(t)
	g := NewTOCAlignedBuilderGroup(factory, math.MaxInt)

	// Append entries out of chronological order so the map would be populated
	// in a non-monotonic order. GetBuilders() must still return them in
	// ascending window order.
	w1 := time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	w2 := w1.Add(metastore.TOCWindowSize)
	w3 := w2.Add(metastore.TOCWindowSize)
	require.NoError(t, g.Append("tenant", logproto.Stream{
		Labels: `{app="foo"}`,
		Entries: []push.Entry{
			windowEntry(w3, time.Minute, "c"),
			windowEntry(w1, time.Minute, "a"),
			windowEntry(w2, time.Minute, "b"),
		},
	}, w1))

	builders := g.GetBuilders()
	require.Len(t, builders, 3)

	for i := 0; i < len(builders)-1; i++ {
		a, b := builders[i].TimeRanges(), builders[i+1].TimeRanges()
		require.Len(t, a, 1)
		require.Len(t, b, 1)
		require.True(t,
			a[0].MinTime.Before(b[0].MinTime),
			"builder[%d] window (%s) must come before builder[%d] window (%s)",
			i, a[0].MinTime, i+1, b[0].MinTime,
		)
	}
}
