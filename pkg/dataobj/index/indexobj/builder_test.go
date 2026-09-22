package indexobj

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/scratch"
)

var testBuilderConfig = logsobj.BuilderBaseConfig{
	TargetPageSize:    2048,
	TargetObjectSize:  1 << 22, // 4 MiB
	TargetSectionSize: 1 << 21, // 2 MiB

	BufferSize: 2048 * 8,

	SectionStripeMergeLimit: 2,
}

const testTenant = "test-tenant"

func TestBuilder(t *testing.T) {
	testStreams := []streams.Stream{
		{
			ID: 1,
			Labels: labels.New(
				labels.Label{Name: "cluster", Value: "test"},
				labels.Label{Name: "app", Value: "foo"},
			),
			Rows:             2,
			MinTimestamp:     time.Unix(10, 0).UTC(),
			MaxTimestamp:     time.Unix(20, 0).UTC(),
			UncompressedSize: 200,
		},
		{
			ID: 2,
			Labels: labels.New(
				labels.Label{Name: "cluster", Value: "test"},
				labels.Label{Name: "app", Value: "bar"},
			),
			Rows:             3,
			MinTimestamp:     time.Unix(15, 0).UTC(),
			MaxTimestamp:     time.Unix(25, 0).UTC(),
			UncompressedSize: 100,
		},
	}

	testPointers := []pointers.SectionPointer{
		{
			Path:              "test/path",
			Section:           1,
			ColumnName:        "foo",
			ColumnIndex:       1,
			ValuesBloomFilter: []byte{1, 2, 3},
		},
		{
			Path:              "test/path2",
			Section:           2,
			ColumnName:        "bar2",
			ColumnIndex:       2,
			ValuesBloomFilter: []byte{1, 2, 3, 4},
		},
	}

	t.Run("Build", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(nil))
		require.NoError(t, err)

		for _, stream := range testStreams {
			_, err := builder.AppendStream(testTenant, stream)
			require.NoError(t, err)
		}
		for _, pointer := range testPointers {
			err := builder.AppendColumnIndex(testTenant, pointer.Path, pointer.Section, pointer.ColumnName, pointer.ColumnIndex, pointer.ValuesBloomFilter)
			require.NoError(t, err)
		}

		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Equal(t, 1, obj.Sections().Count(streams.CheckSection))
		require.Equal(t, 1, obj.Sections().Count(pointers.CheckSection))
		require.Equal(t, 0, obj.Sections().Count(logs.CheckSection))
		require.Equal(t, 0, obj.Sections().Count(indexpointers.CheckSection))
	})

	t.Run("BuildMultiTenant", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(nil))
		require.NoError(t, err)

		tenants := []string{"test-tenant-1", "test-tenant-2"}

		for i, stream := range testStreams {
			tenant := tenants[i%len(tenants)]
			_, err := builder.AppendStream(tenant, stream)
			require.NoError(t, err)
		}
		for i, pointer := range testPointers {
			tenant := tenants[i%len(tenants)]
			err := builder.AppendColumnIndex(tenant, pointer.Path, pointer.Section, pointer.ColumnName, pointer.ColumnIndex, pointer.ValuesBloomFilter)
			require.NoError(t, err)
		}

		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Equal(t, len(tenants), obj.Sections().Count(streams.CheckSection))
		require.Equal(t, len(tenants), obj.Sections().Count(pointers.CheckSection))
		require.Equal(t, 0, obj.Sections().Count(logs.CheckSection))
		require.Equal(t, 0, obj.Sections().Count(indexpointers.CheckSection))
	})
}

// TestBuilder_Append ensures that appending to the buffer eventually reports
// that the buffer is full.
func TestBuilder_Append(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(nil))
	require.NoError(t, err)

	i := 0
	for {
		require.NoError(t, ctx.Err())

		_, err := builder.AppendStream(testTenant, streams.Stream{
			ID: 1,
			Labels: labels.New(
				labels.Label{Name: "cluster", Value: "test"},
				labels.Label{Name: "app", Value: "foo"},
				labels.Label{Name: "i", Value: fmt.Sprintf("%d", i)},
			),
			Rows:         2,
			MinTimestamp: time.Unix(10, 0).UTC(),
			MaxTimestamp: time.Unix(20, 0).UTC(),
		})
		if builder.IsFull() {
			break
		}
		require.NoError(t, err)
		i++
	}
}

func TestBuilder_AppendIndexPointer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(nil))
	require.NoError(t, err)

	i := 0
	for {
		require.NoError(t, ctx.Err())

		err := builder.AppendIndexPointer(testTenant, indexpointers.IndexPointer{Path: fmt.Sprintf("test/path-%d", i), StartTs: time.Unix(10, 0).Add(time.Duration(i) * time.Second).UTC(), EndTs: time.Unix(20, 0).Add(time.Duration(i) * time.Second).UTC(), FileSize: uint64(1000 + i), UncompressedLogsSize: uint64(100000 + i)})
		if builder.IsFull() {
			break
		}
		require.NoError(t, err)
		i++
	}

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	pointerCount := 0
	for result := range indexpointers.Iter(ctx, obj) {
		require.NoError(t, result.Err())
		pointer := result.MustValue()
		require.Equal(t, testTenant, pointer.Tenant)
		require.Equal(t, uint64(1000+pointerCount), pointer.FileSize)
		require.Equal(t, uint64(100000+pointerCount), pointer.UncompressedLogsSize)
		pointerCount++
	}
	require.Greater(t, pointerCount, 0)
}

func TestBuilder_ObserveLogLine(t *testing.T) {
	builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(nil))
	require.NoError(t, err)

	err = builder.ObserveLogLine(testTenant, "test/path", 1, 1, 1, time.Unix(10, 0).UTC(), 100)
	require.NoError(t, err)

	require.Greater(t, builder.estimatedSize(), 0)
}

func BenchmarkIndexObjBuilder_ObserveLogLine(b *testing.B) {
	builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(nil))
	require.NoError(b, err)

	maxTenants := 1000
	tenants := make([]string, maxTenants)
	for i := range tenants {
		tenants[i] = fmt.Sprintf("test-tenant-%d", i)
	}

	for b.Loop() {
		for _, tenant := range tenants {
			err := builder.ObserveLogLine(tenant, "test/path", 1, 1, 1, time.Unix(10, 0).UTC(), 100)
			require.NoError(b, err)
		}
	}
}

func TestBuilder_TimeRanges_PostingsOnly(t *testing.T) {
	b, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(nil))
	require.NoError(t, err)

	base := time.Unix(8000, 0).UTC()
	tenant := "tenant-a"

	b.ObserveLabelPosting(tenant, postings.LabelObservation{
		ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x",
		StreamID: 1, Timestamp: base,
	})
	b.ObserveLabelPosting(tenant, postings.LabelObservation{
		ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "y",
		StreamID: 2, Timestamp: base.Add(time.Hour),
	})

	ranges := b.TimeRanges()
	require.Len(t, ranges, 1)
	require.Equal(t, tenant, ranges[0].Tenant)
	require.Equal(t, base, ranges[0].MinTime)
	require.Equal(t, base.Add(time.Hour), ranges[0].MaxTime)
}

func TestBuilder_TimeRanges_MultiTenantUnion(t *testing.T) {
	b, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(nil))
	require.NoError(t, err)

	base := time.Unix(9000, 0).UTC()

	// tenant-a: postings only.
	b.ObserveLabelPosting("tenant-a", postings.LabelObservation{
		ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x",
		StreamID: 1, Timestamp: base,
	})
	// tenant-b: postings only, different window.
	b.ObserveLabelPosting("tenant-b", postings.LabelObservation{
		ObjectPath: "/b", SectionIndex: 0, ColumnName: "app", LabelValue: "z",
		StreamID: 1, Timestamp: base.Add(2 * time.Hour),
	})

	ranges := b.TimeRanges()
	require.Len(t, ranges, 2)

	byTenant := map[string]multitenancy.TimeRange{}
	for _, r := range ranges {
		byTenant[r.Tenant] = r
	}
	require.Equal(t, base, byTenant["tenant-a"].MinTime)
	require.Equal(t, base, byTenant["tenant-a"].MaxTime)
	require.Equal(t, base.Add(2*time.Hour), byTenant["tenant-b"].MinTime)
	require.Equal(t, base.Add(2*time.Hour), byTenant["tenant-b"].MaxTime)
}

func TestBuilder_TimeRanges_StreamsAndPostingsUnion(t *testing.T) {
	b, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(nil))
	require.NoError(t, err)

	base := time.Unix(10000, 0).UTC()
	tenant := "tenant-a"

	// Streams cover [base, base+1h]; postings extend the window on both ends.
	_, err = b.AppendStream(tenant, streams.Stream{
		Labels:           labels.FromStrings("app", "x"),
		MinTimestamp:     base,
		MaxTimestamp:     base.Add(time.Hour),
		UncompressedSize: 1,
	})
	require.NoError(t, err)

	b.ObserveLabelPosting(tenant, postings.LabelObservation{
		ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x",
		StreamID: 1, Timestamp: base.Add(-time.Hour),
	})
	b.ObserveLabelPosting(tenant, postings.LabelObservation{
		ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x",
		StreamID: 2, Timestamp: base.Add(2 * time.Hour),
	})

	ranges := b.TimeRanges()
	require.Len(t, ranges, 1)
	require.Equal(t, tenant, ranges[0].Tenant)
	require.Equal(t, base.Add(-time.Hour), ranges[0].MinTime)
	require.Equal(t, base.Add(2*time.Hour), ranges[0].MaxTime)
}

func TestUnionTimeRange(t *testing.T) {
	base := time.Unix(1000, 0).UTC()

	// Case 1: candidate has no data (candMin zero) -> accumulator returned unchanged.
	gotMin, gotMax := unionTimeRange(base, base.Add(time.Hour), time.Time{}, time.Time{})
	require.Equal(t, base, gotMin)
	require.Equal(t, base.Add(time.Hour), gotMax)

	// Case 2: accumulator zero, candidate non-zero -> candidate returned.
	gotMin, gotMax = unionTimeRange(time.Time{}, time.Time{}, base, base.Add(time.Hour))
	require.Equal(t, base, gotMin)
	require.Equal(t, base.Add(time.Hour), gotMax)

	// Case 3: both non-zero, candidate widens on both ends -> widened range.
	gotMin, gotMax = unionTimeRange(base.Add(time.Hour), base.Add(2*time.Hour), base, base.Add(3*time.Hour))
	require.Equal(t, base, gotMin)
	require.Equal(t, base.Add(3*time.Hour), gotMax)

	// Case 4: both non-zero, candidate inside accumulator -> accumulator unchanged.
	gotMin, gotMax = unionTimeRange(base, base.Add(3*time.Hour), base.Add(time.Hour), base.Add(2*time.Hour))
	require.Equal(t, base, gotMin)
	require.Equal(t, base.Add(3*time.Hour), gotMax)

	// Case 5: accumulator zero AND candidate zero -> both zero out.
	gotMin, gotMax = unionTimeRange(time.Time{}, time.Time{}, time.Time{}, time.Time{})
	require.True(t, gotMin.IsZero())
	require.True(t, gotMax.IsZero())
}

func TestBuilder_TimeRanges_AfterReset(t *testing.T) {
	b, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(nil))
	require.NoError(t, err)

	base := time.Unix(8000, 0).UTC()
	tenant := "test-tenant"

	// Observe a label posting so TimeRanges() is non-empty.
	b.ObserveLabelPosting(tenant, postings.LabelObservation{
		ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x",
		StreamID: 1, Timestamp: base,
	})

	ranges := b.TimeRanges()
	require.Len(t, ranges, 1)

	// After Reset, TimeRanges() must be empty.
	b.Reset()
	ranges = b.TimeRanges()
	require.Empty(t, ranges)
}

// failingReadStore is a scratch store whose reads fail: either all of them, or
// only the handle written last, which is the final section's metadata.
type failingReadStore struct {
	inner    scratch.Store
	failLast bool

	handles []scratch.Handle
	removed []scratch.Handle
}

func newFailingReadStore(failLast bool) *failingReadStore {
	return &failingReadStore{inner: scratch.NewMemory(), failLast: failLast}
}

func (s *failingReadStore) Put(p []byte) scratch.Handle {
	h := s.inner.Put(p)
	s.handles = append(s.handles, h)
	return h
}

func (s *failingReadStore) Read(h scratch.Handle) (io.ReadSeekCloser, error) {
	if s.failLast && h != s.handles[len(s.handles)-1] {
		return s.inner.Read(h)
	}
	return nil, errors.New("mock read error")
}

func (s *failingReadStore) Remove(h scratch.Handle) error {
	s.removed = append(s.removed, h)
	return s.inner.Remove(h)
}

func appendStreamPerTenant(t *testing.T, b *Builder, tenants int) {
	t.Helper()
	for i := range tenants {
		_, err := b.AppendStream(fmt.Sprintf("tenant-%04d", i), streams.Stream{
			ID:               int64(i + 1),
			Labels:           labels.New(labels.Label{Name: "app", Value: fmt.Sprintf("v%d", i)}),
			Rows:             1,
			MinTimestamp:     time.Unix(10, 0).UTC(),
			MaxTimestamp:     time.Unix(20, 0).UTC(),
			UncompressedSize: 100,
		})
		require.NoError(t, err)
	}
}

// A closer returned alongside an error is never closed by callers, since they
// stop at the error, so Flush must hand back nothing when it fails.
func TestBuilder_FlushReturnsNoCloserOnError(t *testing.T) {
	t.Run("when the builder is empty", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig, scratch.NewMemory())
		require.NoError(t, err)

		obj, closer, err := builder.Flush()
		require.ErrorIs(t, err, ErrBuilderEmpty)
		require.Nil(t, obj)
		require.Nil(t, closer)
	})

	t.Run("when the object cannot be built", func(t *testing.T) {
		store := newFailingReadStore(false)
		builder, err := NewBuilder(testBuilderConfig, store)
		require.NoError(t, err)
		appendStreamPerTenant(t, builder, 1)

		obj, closer, err := builder.Flush()
		require.ErrorContains(t, err, "flushing object")
		require.Nil(t, obj)
		require.Nil(t, closer)
		require.NotEmpty(t, store.removed, "the buffered sections must be released")
	})

	t.Run("when the built object cannot be observed", func(t *testing.T) {
		// Only the last section's metadata fails to read. With this many
		// sections the metadata outgrows the decoder's prefetch window, so the
		// object opens successfully and the failure lands while observing it,
		// which is where Flush owns the object and has to release it itself.
		store := newFailingReadStore(true)
		builder, err := NewBuilder(testBuilderConfig, store)
		require.NoError(t, err)
		appendStreamPerTenant(t, builder, 64)

		obj, closer, err := builder.Flush()
		require.ErrorContains(t, err, "observing object")
		require.Nil(t, obj)
		require.Nil(t, closer)
		require.NotEmpty(t, store.removed, "the object's scratch handles must be released")
	})
}

// Flush resets the builder whether it succeeds or fails, so a failed flush
// leaves nothing behind for the next one to pick up.
func TestBuilder_FlushResetsBuilder(t *testing.T) {
	tests := []struct {
		name    string
		store   scratch.Store
		tenants int
		wantErr string
	}{
		{
			name:    "when the object is built",
			store:   scratch.NewMemory(),
			tenants: 1,
		},
		{
			name:    "when the object cannot be built",
			store:   newFailingReadStore(false),
			tenants: 1,
			wantErr: "flushing object",
		},
		{
			// See TestBuilder_FlushReturnsNoCloserOnError for why this many
			// tenants are needed to fail while observing.
			name:    "when the built object cannot be observed",
			store:   newFailingReadStore(true),
			tenants: 64,
			wantErr: "observing object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, err := NewBuilder(testBuilderConfig, tt.store)
			require.NoError(t, err)
			appendStreamPerTenant(t, builder, tt.tenants)

			_, closer, err := builder.Flush()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				defer closer.Close()
			}

			require.Zero(t, builder.GetEstimatedSize())
			_, _, err = builder.Flush()
			require.ErrorIs(t, err, ErrBuilderEmpty)
		})
	}
}
