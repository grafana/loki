package builder

import (
	"context"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"

	"github.com/grafana/loki/pkg/push"
)

// windowEntry returns an entry whose timestamp falls into the given window.
func windowEntry(window time.Time, offset time.Duration, line string) push.Entry {
	return push.Entry{Timestamp: window.Add(offset), Line: line}
}

func TestMultiObjectBuilder_AppendSplitsAcrossWindows(t *testing.T) {
	factory := newTestBuilderFactory()
	m := NewMultiObjectBuilder(factory, math.MaxInt, newTestWatermarks(), testOverrides{})

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
	require.Equal(t, w1, truncatedWindow(t, builders[0].builders[inTimeClass]))
	require.Equal(t, w2, truncatedWindow(t, builders[1].builders[inTimeClass]))
}

func TestMultiObjectBuilder_AppendSplitsAcrossTenantsAndWindows(t *testing.T) {
	factory := newTestBuilderFactory()
	m := NewMultiObjectBuilder(factory, math.MaxInt, newTestWatermarks(), testOverrides{})

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

	type tocID struct {
		tenant string
		window time.Time
	}
	var got []tocID
	for _, b := range builders {
		got = append(got, tocID{b.toc.tenant, b.toc.window})
	}
	require.Equal(t, []tocID{
		{"tenant-a", w1},
		{"tenant-a", w2},
		{"tenant-b", w1},
		{"tenant-b", w2},
	}, got)
}

func TestMultiObjectBuilder_AppendReusesBuilderForSameWindow(t *testing.T) {
	factory := newTestBuilderFactory()
	m := NewMultiObjectBuilder(factory, math.MaxInt, newTestWatermarks(), testOverrides{})

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
	m := NewMultiObjectBuilder(factory, math.MaxInt, newTestWatermarks(), testOverrides{})

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
	require.Equal(t, w1, truncatedWindow(t, builders[0].builders[inTimeClass]))
	require.Equal(t, w2, truncatedWindow(t, builders[1].builders[inTimeClass]))
	require.Equal(t, w3, truncatedWindow(t, builders[2].builders[inTimeClass]))
}

func TestMultiObjectBuilder_IsFullBoundsTotalMemory(t *testing.T) {
	factory := newTestBuilderFactory()
	m := NewMultiObjectBuilder(factory, math.MaxInt, newTestWatermarks(), testOverrides{})

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
	m := NewMultiObjectBuilder(factory, math.MaxInt, newTestWatermarks(), testOverrides{})

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
	m := NewMultiObjectBuilder(factory, math.MaxInt, newTestWatermarks(), testOverrides{})

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
	m := NewMultiObjectBuilder(factory, math.MaxInt, watermarks, testOverrides{})

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

	// The cache keeps the ToC of the dropped builder at the flush offset.
	watermark, err := watermarks.get(t.Context(), tocKey{tenant: "tenant", window: w1})
	require.NoError(t, err)
	require.Equal(t, int64(7), *watermark)
	require.Equal(t, readsBefore, readSamples(t, watermarks, resultOK))
}

// truncatedWindow returns the TOC window that the time range of the builder
// belongs to.
func truncatedWindow(t *testing.T, b logsBuilder) time.Time {
	t.Helper()
	return b.TimeRange().MinTime.UTC().Truncate(metastore.MetastoreWindowSize)
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
		m := NewMultiObjectBuilder(factory, math.MaxInt, newBucketWatermarks(bucket), testOverrides{})

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
		m := NewMultiObjectBuilder(newTestBuilderFactory(), math.MaxInt, w, testOverrides{})
		_, err := m.Append(t.Context(), "tenant", stream, testWindow1, 11)
		require.NoError(t, err)
		// Another ToC pushes the first one out of the cache of one entry.
		_, err = m.Append(t.Context(), "other", stream, testWindow1, 11)
		require.NoError(t, err)

		// Another process raises the watermark of the ToC of the first ToC. A
		// new read would show it, and the builder would hide the conflict.
		writeTestWatermark(t, bucket, "tenant", testWindow1, 50)
		res, err := m.Append(t.Context(), "tenant", stream, testWindow1, 20)

		require.NoError(t, err)
		require.Equal(t, AppendResult{Appended: 1}, res)
		require.Equal(t, uint64(2), readSamples(t, w, resultOK))
		for _, b := range m.getBuilders() {
			if b.toc.tenant == "tenant" {
				require.Equal(t, int64(10), *b.initialWatermark)
			}
		}
	})

	t.Run("starts a new builder of every flushed ToC from the offset of the flush", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writeTestWatermark(t, bucket, "tenant", testWindow1, 10)
		w := newBucketWatermarks(bucket)
		m := NewMultiObjectBuilder(newTestBuilderFactory(), math.MaxInt, w, testOverrides{})
		tocs := []struct {
			tenant string
			window time.Time
		}{{"tenant", testWindow1}, {"tenant", testWindow2}, {"other", testWindow1}}
		for _, sc := range tocs {
			_, err := m.Append(t.Context(), sc.tenant, logproto.Stream{Labels: `{app="foo"}`, Entries: []push.Entry{windowEntry(sc.window, time.Minute, "a")}}, sc.window, 11)
			require.NoError(t, err)
		}
		m.Reset(11)

		for _, sc := range tocs {
			res, err := m.Append(t.Context(), sc.tenant, logproto.Stream{Labels: `{app="foo"}`, Entries: []push.Entry{windowEntry(sc.window, time.Minute, "a")}}, sc.window, 12)
			require.NoError(t, err)
			require.Equal(t, AppendResult{Appended: 1}, res)
		}

		builders := m.getBuilders()
		require.Len(t, builders, len(tocs))
		for _, b := range builders {
			require.Equal(t, int64(11), *b.initialWatermark)
		}
		require.Equal(t, uint64(len(tocs)), readSamples(t, w, resultOK))
	})
}

const testLateThreshold = 30 * time.Minute

// testOverrides is a [TenantOverrides] with no sort schema, the default shard
// layout and no bloom deny list. threshold gives each tenant its late log
// threshold. A nil threshold keeps the late lines of every tenant with its
// in-time lines.
type testOverrides struct {
	logsobj.NoTenantOverrides
	threshold func(tenant string) time.Duration
}

func (o testOverrides) DataObjBloomDenyList(string) []string { return nil }

func (o testOverrides) DataObjLateLogsThreshold(tenant string) time.Duration {
	if o.threshold == nil {
		return 0
	}
	return o.threshold(tenant)
}

// constThreshold returns overrides with the same late log threshold for every
// tenant.
func constThreshold(d time.Duration) TenantOverrides {
	return testOverrides{threshold: func(string) time.Duration { return d }}
}

// lateAndFreshStream returns a stream with a late and an in-time entry for a
// record time of rec. Both entries lie in the window of rec.
func lateAndFreshStream(rec time.Time, tag string) logproto.Stream {
	return logproto.Stream{Labels: `{app="foo"}`, Entries: []push.Entry{
		{Timestamp: rec.Add(-2 * time.Hour), Line: "late-" + tag},
		{Timestamp: rec.Add(-time.Minute), Line: "fresh-" + tag},
	}}
}

// tenantKinds returns the tenant and lateness of each builder of m.
func tenantKinds(m *MultiObjectBuilder) []string {
	var got []string
	for _, sb := range m.getBuilders() {
		if sb.builders[inTimeClass] != nil {
			got = append(got, sb.toc.tenant+"/in_time")
		}
		if sb.builders[lateClass] != nil {
			got = append(got, sb.toc.tenant+"/late")
		}
	}
	return got
}

func TestMultiObjectBuilder_LateLogs(t *testing.T) {
	rec := testWindow1.Add(6 * time.Hour)

	newGroup := func(threshold time.Duration) *MultiObjectBuilder {
		return NewMultiObjectBuilder(newTestBuilderFactory(), math.MaxInt, newTestWatermarks(), constThreshold(threshold))
	}

	for _, tc := range []struct {
		name     string
		lineTime time.Time
		want     string
	}{
		{name: "puts a line exactly at the threshold in the in-time builder", lineTime: rec.Add(-testLateThreshold), want: "tenant/in_time"},
		{name: "puts a line one nanosecond above the threshold in the late builder", lineTime: rec.Add(-testLateThreshold - 1), want: "tenant/late"},
		{name: "puts a line hours older than the record in the late builder", lineTime: rec.Add(-3 * time.Hour), want: "tenant/late"},
		{name: "puts a line newer than the record in the in-time builder", lineTime: rec.Add(time.Hour), want: "tenant/in_time"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newGroup(testLateThreshold)
			stream := logproto.Stream{Labels: `{app="foo"}`, Entries: []push.Entry{{Timestamp: tc.lineTime, Line: "x"}}}

			_, err := m.Append(t.Context(), "tenant", stream, rec, 1)

			require.NoError(t, err)
			require.Equal(t, []string{tc.want}, tenantKinds(m))
		})
	}

	t.Run("builds the late and in-time lines of one record in two builders of one window", func(t *testing.T) {
		m := newGroup(testLateThreshold)

		res, err := m.Append(t.Context(), "tenant", lateAndFreshStream(rec, "1"), rec, 5)

		require.NoError(t, err)
		require.Equal(t, AppendResult{Appended: 2}, res)
		require.Equal(t, []string{"tenant/in_time", "tenant/late"}, tenantKinds(m))
		require.Len(t, m.getBuilders(), 1)
	})

	t.Run("builds every line in one in-time builder when the threshold is zero", func(t *testing.T) {
		m := newGroup(0)

		res, err := m.Append(t.Context(), "tenant", lateAndFreshStream(rec, "1"), rec, 5)

		require.NoError(t, err)
		require.Equal(t, AppendResult{Appended: 1}, res)
		require.Equal(t, []string{"tenant/in_time"}, tenantKinds(m))
	})

	t.Run("separates late lines only for tenants with a threshold", func(t *testing.T) {
		m := NewMultiObjectBuilder(newTestBuilderFactory(), math.MaxInt, newTestWatermarks(), testOverrides{threshold: func(tenant string) time.Duration {
			if tenant == "separated" {
				return testLateThreshold
			}
			return 0
		}})

		for i, tenant := range []string{"separated", "plain"} {
			_, err := m.Append(t.Context(), tenant, lateAndFreshStream(rec, tenant), rec, int64(i))
			require.NoError(t, err)
		}

		require.Equal(t, []string{"plain/in_time", "separated/in_time", "separated/late"}, tenantKinds(m))
	})

	t.Run("splits by window first and by lateness second", func(t *testing.T) {
		m := newGroup(testLateThreshold)
		stream := logproto.Stream{Labels: `{app="foo"}`, Entries: []push.Entry{
			{Timestamp: rec, Line: "w1-fresh"},
			{Timestamp: rec.Add(-2 * time.Hour), Line: "w1-late"},
			{Timestamp: testWindow2.Add(time.Minute), Line: "w2-future"},
		}}

		res, err := m.Append(t.Context(), "tenant", stream, rec, 1)

		require.NoError(t, err)
		require.Equal(t, AppendResult{Appended: 3}, res)
		scopedBuilders := m.getBuilders()
		require.Len(t, scopedBuilders, 2)
		require.NotNil(t, scopedBuilders[0].builders[inTimeClass])
		require.NotNil(t, scopedBuilders[0].builders[lateClass])
		require.NotNil(t, scopedBuilders[1].builders[inTimeClass])
		require.Nil(t, scopedBuilders[1].builders[lateClass])
		require.Equal(t, testWindow2, scopedBuilders[1].toc.window)
	})

	t.Run("keeps the builder that succeeded when creating the other builder fails", func(t *testing.T) {
		factory := newTestBuilderFactory()
		factory.failAt = 1
		m := NewMultiObjectBuilder(factory, math.MaxInt, newTestWatermarks(), constThreshold(testLateThreshold))

		_, err := m.Append(t.Context(), "tenant", lateAndFreshStream(rec, "1"), rec, 5)

		require.ErrorContains(t, err, "boom")
		require.ErrorContains(t, err, "offset 5")
		require.Len(t, tenantKinds(m), 1)
	})

	t.Run("stores no group when creating the first builder fails", func(t *testing.T) {
		factory := newTestBuilderFactory()
		factory.failAt = 0
		m := NewMultiObjectBuilder(factory, math.MaxInt, newTestWatermarks(), constThreshold(testLateThreshold))

		_, err := m.Append(t.Context(), "tenant", lateAndFreshStream(rec, "1"), rec, 5)

		require.ErrorContains(t, err, "boom")
		require.Empty(t, m.getBuilders())
	})

	t.Run("follows a threshold that changes at runtime", func(t *testing.T) {
		threshold := time.Duration(0)
		m := NewMultiObjectBuilder(newTestBuilderFactory(), math.MaxInt, newTestWatermarks(), testOverrides{threshold: func(string) time.Duration { return threshold }})

		_, err := m.Append(t.Context(), "tenant", lateAndFreshStream(rec, "1"), rec, 1)
		require.NoError(t, err)
		require.Equal(t, []string{"tenant/in_time"}, tenantKinds(m))

		threshold = testLateThreshold
		_, err = m.Append(t.Context(), "tenant", lateAndFreshStream(rec, "2"), rec, 2)
		require.NoError(t, err)
		require.Equal(t, []string{"tenant/in_time", "tenant/late"}, tenantKinds(m))

		// A threshold of zero puts the late lines of another window in-time.
		threshold = 0
		rec2 := testWindow2.Add(6 * time.Hour)
		_, err = m.Append(t.Context(), "tenant", lateAndFreshStream(rec2, "3"), rec2, 3)
		require.NoError(t, err)
		tocs := m.getBuilders()
		require.Len(t, tocs, 2)
		require.NotNil(t, tocs[0].builders[lateClass])
		require.NotNil(t, tocs[1].builders[inTimeClass])
		require.Nil(t, tocs[1].builders[lateClass])
	})

	t.Run("drops the window that the watermark covers and keeps the other window of the same record", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writeTestWatermark(t, bucket, "tenant", testWindow1, 10)
		m := NewMultiObjectBuilder(newTestBuilderFactory(), math.MaxInt, newBucketWatermarks(bucket), constThreshold(testLateThreshold))
		rec2 := testWindow2.Add(6 * time.Hour)
		stream := logproto.Stream{Labels: `{app="foo"}`, Entries: []push.Entry{
			{Timestamp: testWindow1.Add(6 * time.Hour), Line: "w1-late"},
			{Timestamp: rec2.Add(-2 * time.Hour), Line: "w2-late"},
			{Timestamp: rec2.Add(-time.Minute), Line: "w2-fresh"},
		}}

		res, err := m.Append(t.Context(), "tenant", stream, rec2, 10)

		require.NoError(t, err)
		require.Equal(t, AppendResult{Dropped: 1, Appended: 2}, res)
		tocs := m.getBuilders()
		require.Len(t, tocs, 1)
		require.Equal(t, testWindow2, tocs[0].toc.window)
	})

	t.Run("treats every line as in-time when the record has no timestamp", func(t *testing.T) {
		m := newGroup(testLateThreshold)

		_, err := m.Append(t.Context(), "tenant", lateAndFreshStream(rec, "1"), time.Time{}, 5)

		require.NoError(t, err)
		require.Equal(t, []string{"tenant/in_time"}, tenantKinds(m))
	})

	t.Run("sums the estimated size of the builders of both classes", func(t *testing.T) {
		m := newGroup(testLateThreshold)
		_, err := m.Append(t.Context(), "tenant", lateAndFreshStream(rec, "1"), rec, 5)
		require.NoError(t, err)

		scopedBuilders := m.getBuilders()
		require.Len(t, scopedBuilders, 1)
		require.Positive(t, scopedBuilders[0].builders[inTimeClass].GetEstimatedSize())
		require.Positive(t, scopedBuilders[0].builders[lateClass].GetEstimatedSize())
		sum := scopedBuilders[0].builders[inTimeClass].GetEstimatedSize() + scopedBuilders[0].builders[lateClass].GetEstimatedSize()
		require.Equal(t, sum, m.GetEstimatedSize())
		m.maxBufferedBytes = sum - 1
		require.True(t, m.IsFull())
		m.maxBufferedBytes = sum
		require.False(t, m.IsFull())
	})

	t.Run("drops a record that feeds both builders as a whole when the watermark covers it", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writeTestWatermark(t, bucket, "tenant", testWindow1, 10)
		factory := newTestBuilderFactory()
		m := NewMultiObjectBuilder(factory, math.MaxInt, newBucketWatermarks(bucket), constThreshold(testLateThreshold))

		res, err := m.Append(t.Context(), "tenant", lateAndFreshStream(rec, "1"), rec, 10)
		require.NoError(t, err)
		require.Equal(t, AppendResult{Dropped: 1}, res)
		require.Empty(t, m.getBuilders())
		require.Equal(t, 0, factory.created)

		res, err = m.Append(t.Context(), "tenant", lateAndFreshStream(rec, "1"), rec, 11)
		require.NoError(t, err)
		require.Equal(t, AppendResult{Appended: 2}, res)
	})

	t.Run("keeps the initial watermark of a window for a late builder created later and raises it on reset", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writeTestWatermark(t, bucket, "tenant", testWindow1, 10)
		watermarks := newBucketWatermarks(bucket)
		m := NewMultiObjectBuilder(newTestBuilderFactory(), math.MaxInt, watermarks, constThreshold(testLateThreshold))
		fresh := logproto.Stream{Labels: `{app="foo"}`, Entries: []push.Entry{{Timestamp: rec, Line: "fresh"}}}

		_, err := m.Append(t.Context(), "tenant", fresh, rec, 11)
		require.NoError(t, err)
		_, err = m.Append(t.Context(), "tenant", lateAndFreshStream(rec, "1"), rec, 12)
		require.NoError(t, err)
		scopedBuilders := m.getBuilders()
		require.Len(t, scopedBuilders, 1)
		require.NotNil(t, scopedBuilders[0].builders[lateClass])
		require.Equal(t, int64(10), *scopedBuilders[0].initialWatermark)

		m.Reset(12)

		got, err := watermarks.get(t.Context(), tocKey{tenant: "tenant", window: testWindow1})
		require.NoError(t, err)
		require.Equal(t, int64(12), *got)
	})
}

// objectBytes returns the bytes of the sorted data object that b builds.
func objectBytes(t *testing.T, b logsBuilder) []byte {
	t.Helper()
	obj, closer, err := b.FlushAndSort(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })
	r, err := obj.Reader(t.Context())
	require.NoError(t, err)
	defer r.Close()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	return data
}

func TestMultiObjectBuilder_LateLogsOffOutput(t *testing.T) {
	rec := testWindow1.Add(6 * time.Hour)
	streams := []logproto.Stream{
		lateAndFreshStream(rec, "1"),
		{Labels: `{app="bar", env="prod"}`, Entries: []push.Entry{{Timestamp: rec, Line: "bar-1"}, {Timestamp: rec.Add(time.Second), Line: "bar-2"}}},
		lateAndFreshStream(rec, "3"),
	}

	// The reference builder gets what the group gave a builder before late-log
	// separation existed: the streams of a window, in order.
	newReference := func(t *testing.T) *logsobj.Builder {
		t.Helper()
		b, err := logsobj.NewBuilder("tenant", testBuilderCfg, scratch.NewMemory(), logsobj.NewBuilderMetrics(), log.NewNopLogger(), logsobj.NoTenantOverrides{})
		require.NoError(t, err)
		for _, s := range streams {
			require.NoError(t, b.Append(s, rec))
		}
		return b
	}

	t.Run("builds the same object as a plain builder when the separation is off", func(t *testing.T) {
		m := NewMultiObjectBuilder(newTestBuilderFactory(), math.MaxInt, newTestWatermarks(), testOverrides{})
		for i, s := range streams {
			_, err := m.Append(t.Context(), "tenant", s, rec, int64(i))
			require.NoError(t, err)
		}
		builders := m.getBuilders()
		require.Len(t, builders, 1)

		require.Equal(t, objectBytes(t, newReference(t)), objectBytes(t, builders[0].builders[inTimeClass]))
	})

	t.Run("builds the same object when the separation is on and no line is late", func(t *testing.T) {
		m := NewMultiObjectBuilder(newTestBuilderFactory(), math.MaxInt, newTestWatermarks(), constThreshold(24*time.Hour))
		for i, s := range streams {
			_, err := m.Append(t.Context(), "tenant", s, rec, int64(i))
			require.NoError(t, err)
		}
		builders := m.getBuilders()
		require.Len(t, builders, 1)

		require.Equal(t, objectBytes(t, newReference(t)), objectBytes(t, builders[0].builders[inTimeClass]))
	})
}

// workloadResult describes the objects that one run of the workload flushed.
type workloadResult struct {
	flushes int
	// inTime and late hold the estimated size in bytes of every flushed object.
	inTime, late []int
}

func (r workloadResult) objects() int {
	return len(r.inTime) + len(r.late)
}

func mean(sizes []int) int {
	if len(sizes) == 0 {
		return 0
	}
	total := 0
	for _, s := range sizes {
		total += s
	}
	return total / len(sizes)
}

func percentile(sizes []int, p float64) int {
	sorted := slices.Sorted(slices.Values(sizes))
	if len(sorted) == 0 {
		return 0
	}
	return sorted[int(float64(len(sorted)-1)*p)]
}

// runLateWorkload feeds records of many tenants to a group that flushes when it
// is full, like the processor does. Each tenant gets a share of the records
// that is proportional to 1/(rank+1). A fraction lateShare of the lines of the
// first lateTenants tenants is 2 to 3 hours older than the record. The seed
// fixes the workload.
func runLateWorkload(t *testing.T, lateThreshold time.Duration, maxBufferedBytes, tenants, lateTenants, records int, lateShare float64) workloadResult {
	t.Helper()

	var (
		rnd    = rand.New(rand.NewPCG(1, 2))
		m      = NewMultiObjectBuilder(newTestBuilderFactory(), maxBufferedBytes, newTestWatermarks(), testOverrides{})
		result workloadResult
	)
	if lateThreshold > 0 {
		m = NewMultiObjectBuilder(newTestBuilderFactory(), maxBufferedBytes, newTestWatermarks(), constThreshold(lateThreshold))
	}

	weights := make([]float64, tenants)
	var totalWeight float64
	for i := range weights {
		weights[i] = 1 / float64(i+1)
		totalWeight += weights[i]
	}
	pickTenant := func() int {
		x := rnd.Float64() * totalWeight
		for i, w := range weights {
			if x -= w; x <= 0 {
				return i
			}
		}
		return tenants - 1
	}

	flush := func() {
		for _, sb := range m.getBuilders() {
			if b := sb.builders[inTimeClass]; b != nil {
				result.inTime = append(result.inTime, b.GetEstimatedSize())
			}
			if b := sb.builders[lateClass]; b != nil {
				result.late = append(result.late, b.GetEstimatedSize())
			}
		}
		result.flushes++
		m.Reset(0)
	}

	recTime := testWindow1.Add(6 * time.Hour)
	for offset := range records {
		rank := pickTenant()
		tenant := fmt.Sprintf("tenant-%03d", rank)
		entries := make([]push.Entry, 50)
		for i := range entries {
			ts := recTime.Add(-time.Duration(i) * time.Millisecond)
			if rank < lateTenants && rnd.Float64() < lateShare {
				ts = recTime.Add(-2*time.Hour - time.Duration(rnd.IntN(3600))*time.Second)
			}
			entries[i] = push.Entry{Timestamp: ts, Line: fmt.Sprintf("level=info msg=\"request handled\" status=200 duration=%dms id=%d", rnd.IntN(5000), rnd.Int64())}
		}
		stream := logproto.Stream{Labels: fmt.Sprintf(`{app="app-%d", job="job"}`, rnd.IntN(8)), Entries: entries}
		if m.IsFull() {
			flush()
		}
		_, err := m.Append(t.Context(), tenant, stream, recTime, int64(offset))
		require.NoError(t, err)
	}
	flush()
	return result
}

func TestMultiObjectBuilder_LateLogsWorkload(t *testing.T) {
	const (
		tenants          = 100
		records          = 6000
		maxBufferedBytes = 4 << 20
		lateShare        = 0.05
	)

	// Every tenant has late lines.
	off := runLateWorkload(t, 0, maxBufferedBytes, tenants, tenants, records, lateShare)
	on := runLateWorkload(t, testLateThreshold, maxBufferedBytes, tenants, tenants, records, lateShare)
	// Only the 5 largest tenants have late lines.
	offFew := runLateWorkload(t, 0, maxBufferedBytes, tenants, 5, records, lateShare)
	onFew := runLateWorkload(t, testLateThreshold, maxBufferedBytes, tenants, 5, records, lateShare)

	report := func(name string, r workloadResult) {
		t.Logf("%-4s flushes=%d objects=%d (%.1f per flush) in-time: n=%d mean=%dB p10=%dB p50=%dB | late: n=%d mean=%dB p10=%dB p50=%dB",
			name, r.flushes, r.objects(), float64(r.objects())/float64(r.flushes),
			len(r.inTime), mean(r.inTime), percentile(r.inTime, 0.1), percentile(r.inTime, 0.5),
			len(r.late), mean(r.late), percentile(r.late, 0.1), percentile(r.late, 0.5))
	}
	report("off", off)
	report("on", on)
	report("offFew", offFew)
	report("onFew", onFew)

	t.Run("flushes no late object when the separation is off", func(t *testing.T) {
		require.Empty(t, off.late)
	})

	t.Run("flushes a late object next to most in-time objects when the separation is on", func(t *testing.T) {
		require.NotEmpty(t, on.late)
		require.Greater(t, len(on.late), len(on.inTime)/2)
	})

	t.Run("flushes about as often as with the separation off", func(t *testing.T) {
		require.InDelta(t, float64(off.flushes), float64(on.flushes), math.Max(1, 0.1*float64(off.flushes)))
	})
}
