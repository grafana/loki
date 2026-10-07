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

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
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

	t.Run("builds one section of each appended kind, all for the builder's tenant", func(t *testing.T) {
		builder, err := NewBuilder(testTenant, testBuilderConfig, nil, NewBuilderMetrics(nil))
		require.NoError(t, err)

		for _, stream := range testStreams {
			_, err := builder.AppendStream(stream)
			require.NoError(t, err)
		}
		for _, pointer := range testPointers {
			err := builder.AppendColumnIndex(pointer.Path, pointer.Section, pointer.ColumnName, pointer.ColumnIndex, pointer.ValuesBloomFilter)
			require.NoError(t, err)
		}

		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Equal(t, 1, obj.Sections().Count(streams.CheckSection))
		require.Equal(t, 1, obj.Sections().Count(pointers.CheckSection))
		require.Equal(t, 0, obj.Sections().Count(logs.CheckSection))
		require.Equal(t, 0, obj.Sections().Count(indexpointers.CheckSection))
		require.Equal(t, []string{testTenant}, obj.Tenants())
	})
}

func TestNewBuilder(t *testing.T) {
	t.Run("returns a builder bound to the tenant", func(t *testing.T) {
		builder, err := NewBuilder(testTenant, testBuilderConfig, nil, NewBuilderMetrics(nil))
		require.NoError(t, err)
		require.Equal(t, testTenant, builder.Tenant())
	})

	t.Run("returns an error when the tenant is empty", func(t *testing.T) {
		_, err := NewBuilder("", testBuilderConfig, nil, NewBuilderMetrics(nil))
		require.ErrorContains(t, err, "tenant must not be empty")
	})
}

func TestBuilder_AppendIndexPointer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	builder, err := NewBuilder(testTenant, testBuilderConfig, nil, NewBuilderMetrics(nil))
	require.NoError(t, err)

	for i := range 300 {
		require.NoError(t, ctx.Err())

		err := builder.AppendIndexPointer(indexpointers.IndexPointer{Path: fmt.Sprintf("test/path-%d", i), StartTs: time.Unix(10, 0).Add(time.Duration(i) * time.Second).UTC(), EndTs: time.Unix(20, 0).Add(time.Duration(i) * time.Second).UTC()})
		require.NoError(t, err)
	}

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	pointerCount := 0
	for result := range indexpointers.Iter(ctx, obj) {
		require.NoError(t, result.Err())
		pointer := result.MustValue()
		require.Equal(t, testTenant, pointer.Tenant)
		pointerCount++
	}
	require.Greater(t, pointerCount, 0)
}

func TestBuilder_ObserveLogLine(t *testing.T) {
	builder, err := NewBuilder(testTenant, testBuilderConfig, nil, NewBuilderMetrics(nil))
	require.NoError(t, err)

	err = builder.ObserveLogLine("test/path", 1, 1, 1, time.Unix(10, 0).UTC(), 100)
	require.NoError(t, err)

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	defer closer.Close()
	require.Equal(t, 1, obj.Sections().Count(pointers.CheckSection))
}

func BenchmarkIndexObjBuilder_ObserveLogLine(b *testing.B) {
	builder, err := NewBuilder(testTenant, testBuilderConfig, nil, NewBuilderMetrics(nil))
	require.NoError(b, err)

	const streamCount = 1000

	for b.Loop() {
		for i := range int64(streamCount) {
			err := builder.ObserveLogLine("test/path", 1, i, i, time.Unix(10, 0).UTC(), 100)
			require.NoError(b, err)
		}
	}
}

func TestBuilder_TimeRange(t *testing.T) {
	t.Run("returns the builder's tenant and zero times when the builder is empty", func(t *testing.T) {
		b, err := NewBuilder(testTenant, testBuilderConfig, nil, NewBuilderMetrics(nil))
		require.NoError(t, err)

		require.Equal(t, dataobj.TimeRange{Tenant: testTenant}, b.TimeRange())
	})

	t.Run("returns the postings range when the builder holds only postings", func(t *testing.T) {
		b, err := NewBuilder(testTenant, testBuilderConfig, nil, NewBuilderMetrics(nil))
		require.NoError(t, err)

		base := time.Unix(8000, 0).UTC()
		b.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x",
			StreamID: 1, Timestamp: base,
		})
		b.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "y",
			StreamID: 2, Timestamp: base.Add(time.Hour),
		})

		require.Equal(t, dataobj.TimeRange{
			Tenant:  testTenant,
			MinTime: base,
			MaxTime: base.Add(time.Hour),
		}, b.TimeRange())
	})

	t.Run("returns the union of the streams and postings ranges", func(t *testing.T) {
		b, err := NewBuilder(testTenant, testBuilderConfig, nil, NewBuilderMetrics(nil))
		require.NoError(t, err)

		base := time.Unix(10000, 0).UTC()

		// Streams cover [base, base+1h]; postings extend the window on both ends.
		_, err = b.AppendStream(streams.Stream{
			Labels:           labels.FromStrings("app", "x"),
			MinTimestamp:     base,
			MaxTimestamp:     base.Add(time.Hour),
			UncompressedSize: 1,
		})
		require.NoError(t, err)

		b.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x",
			StreamID: 1, Timestamp: base.Add(-time.Hour),
		})
		b.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x",
			StreamID: 2, Timestamp: base.Add(2 * time.Hour),
		})

		require.Equal(t, dataobj.TimeRange{
			Tenant:  testTenant,
			MinTime: base.Add(-time.Hour),
			MaxTime: base.Add(2 * time.Hour),
		}, b.TimeRange())
	})

	t.Run("returns zero times after Reset", func(t *testing.T) {
		b, err := NewBuilder(testTenant, testBuilderConfig, nil, NewBuilderMetrics(nil))
		require.NoError(t, err)

		b.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x",
			StreamID: 1, Timestamp: time.Unix(8000, 0).UTC(),
		})
		require.False(t, b.TimeRange().MinTime.IsZero())

		b.Reset()
		require.Equal(t, dataobj.TimeRange{Tenant: testTenant}, b.TimeRange())
	})
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

// newBuilderWithSections returns a builder that holds n index pointers
// sections. Its target section size is 1 byte, so the builder writes a section
// for every index pointer appended.
func newBuilderWithSections(t *testing.T, store scratch.Store, n int) *Builder {
	t.Helper()

	cfg := testBuilderConfig
	cfg.TargetSectionSize = 1
	builder, err := NewBuilder(testTenant, cfg, store, NewBuilderMetrics(nil))
	require.NoError(t, err)

	for i := range n {
		err := builder.AppendIndexPointer(indexpointers.IndexPointer{
			Path:    fmt.Sprintf("test/path-%04d", i),
			StartTs: time.Unix(10, 0).UTC(),
			EndTs:   time.Unix(20, 0).UTC(),
		})
		require.NoError(t, err)
	}
	return builder
}

// A closer returned alongside an error is never closed by callers, since they
// stop at the error, so Flush must hand back nothing when it fails.
func TestBuilder_FlushReturnsNoCloserOnError(t *testing.T) {
	t.Run("when the builder is empty", func(t *testing.T) {
		builder, err := NewBuilder(testTenant, testBuilderConfig, scratch.NewMemory(), NewBuilderMetrics(nil))
		require.NoError(t, err)

		obj, closer, err := builder.Flush()
		require.ErrorIs(t, err, ErrBuilderEmpty)
		require.Nil(t, obj)
		require.Nil(t, closer)
	})

	t.Run("when the object cannot be built", func(t *testing.T) {
		store := newFailingReadStore(false)
		builder := newBuilderWithSections(t, store, 1)

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
		builder := newBuilderWithSections(t, store, 64)

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
		name  string
		store scratch.Store
		// sections is the number of sections to append. With 64 sections the
		// metadata outgrows the decoder's prefetch window, so the object opens
		// and a failing read of the last section lands while observing it.
		sections int
		wantErr  string
	}{
		{
			name:     "when the object is built",
			store:    scratch.NewMemory(),
			sections: 1,
		},
		{
			name:     "when the object cannot be built",
			store:    newFailingReadStore(false),
			sections: 1,
			wantErr:  "flushing object",
		},
		{
			name:     "when the built object cannot be observed",
			store:    newFailingReadStore(true),
			sections: 64,
			wantErr:  "observing object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := newBuilderWithSections(t, tt.store, tt.sections)

			_, closer, err := builder.Flush()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				defer closer.Close()
			}

			_, _, err = builder.Flush()
			require.ErrorIs(t, err, ErrBuilderEmpty)
		})
	}
}
