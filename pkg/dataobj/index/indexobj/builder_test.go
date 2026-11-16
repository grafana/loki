package indexobj

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/watermarks"
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
	testPostings := []postings.LabelObservation{
		{
			ObjectPath:   "test/path",
			SectionIndex: 1,
			ColumnName:   "app",
			LabelValue:   "foo",
			StreamID:     1,
			Time:         postings.At(time.Unix(10, 0).UTC()),
		},
		{
			ObjectPath:   "test/path2",
			SectionIndex: 2,
			ColumnName:   "app",
			LabelValue:   "bar",
			StreamID:     2,
			Time:         postings.At(time.Unix(15, 0).UTC()),
		},
	}

	t.Run("builds one section of each appended kind, all for the builder's tenant", func(t *testing.T) {
		builder, err := NewBuilder(testTenant, testBuilderConfig, nil, NewBuilderMetrics(nil))
		require.NoError(t, err)

		for _, obs := range testPostings {
			builder.ObserveLabelPosting(obs)
		}

		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Equal(t, 0, obj.Sections().Count(streams.CheckSection))
		require.Equal(t, 1, obj.Sections().Count(postings.CheckSection))
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

func TestBuilder_AppendWatermark(t *testing.T) {
	testPointers := []indexpointers.IndexPointer{
		{Path: "test/path-0", StartTs: time.Unix(10, 0).UTC(), EndTs: time.Unix(20, 0).UTC()},
		{Path: "test/path-1", StartTs: time.Unix(30, 0).UTC(), EndTs: time.Unix(40, 0).UTC()},
	}

	newBuilder := func(t *testing.T) *Builder {
		t.Helper()
		builder, err := NewBuilder(testTenant, testBuilderConfig, nil, NewBuilderMetrics(nil))
		require.NoError(t, err)
		return builder
	}

	readWatermarks := func(t *testing.T, obj *dataobj.Object) []watermarks.Watermark {
		t.Helper()
		var got []watermarks.Watermark
		for res := range watermarks.Iter(t.Context(), obj) {
			wm, err := res.Value()
			require.NoError(t, err)
			got = append(got, wm)
		}
		return got
	}

	readPointers := func(t *testing.T, obj *dataobj.Object) []indexpointers.IndexPointer {
		t.Helper()
		var got []indexpointers.IndexPointer
		for res := range indexpointers.Iter(t.Context(), obj) {
			tip, err := res.Value()
			require.NoError(t, err)
			require.Equal(t, testTenant, tip.Tenant)
			tip.StartTs, tip.EndTs = tip.StartTs.UTC(), tip.EndTs.UTC()
			got = append(got, tip.IndexPointer)
		}
		return got
	}

	t.Run("builds a watermarks section for the builder's tenant", func(t *testing.T) {
		builder := newBuilder(t)
		require.NoError(t, builder.AppendWatermark("topic-b", 1, 200))
		require.NoError(t, builder.AppendWatermark("topic-a", 0, 100))

		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Equal(t, 1, obj.Sections().Count(watermarks.CheckSection))
		require.Equal(t, testTenant, obj.Sections()[0].Tenant)
		require.Equal(t, []watermarks.Watermark{
			{Topic: "topic-a", Partition: 0, Watermark: 100},
			{Topic: "topic-b", Partition: 1, Watermark: 200},
		}, readWatermarks(t, obj))
	})

	t.Run("holds index pointers and watermarks in one object", func(t *testing.T) {
		builder := newBuilder(t)
		require.NoError(t, builder.AppendWatermark("topic", 7, 42))
		require.NoError(t, builder.AppendWatermark("topic", 8, 43))
		require.NoError(t, builder.AppendWatermark("other-topic", 8, 44))
		for _, p := range testPointers {
			require.NoError(t, builder.AppendIndexPointer(p))
		}

		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Equal(t, testPointers, readPointers(t, obj))
		require.Equal(t, []watermarks.Watermark{
			{Topic: "other-topic", Partition: 8, Watermark: 44},
			{Topic: "topic", Partition: 7, Watermark: 42},
			{Topic: "topic", Partition: 8, Watermark: 43},
		}, readWatermarks(t, obj))
	})

	t.Run("lists the watermarks section before the index pointers section", func(t *testing.T) {
		builder := newBuilder(t)
		for _, p := range testPointers {
			require.NoError(t, builder.AppendIndexPointer(p))
		}
		require.NoError(t, builder.AppendWatermark("topic", 7, 42))

		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		defer closer.Close()

		var kinds []string
		for _, sec := range obj.Sections() {
			kinds = append(kinds, sec.Type.Kind)
		}
		require.Equal(t, []string{"watermarks", "indexpointers"}, kinds)
	})

	t.Run("returns an error and records nothing for an invalid watermark", func(t *testing.T) {
		builder := newBuilder(t)
		require.Error(t, builder.AppendWatermark("", 0, 1))
		require.Error(t, builder.AppendWatermark("topic", -1, 1))
		require.Error(t, builder.AppendWatermark("topic", 0, -1))

		_, _, err := builder.Flush()
		require.ErrorIs(t, err, ErrBuilderEmpty)
	})

	t.Run("returns an error and keeps the old value when the watermark is lower", func(t *testing.T) {
		builder := newBuilder(t)
		require.NoError(t, builder.AppendWatermark("topic", 1, 5))
		require.Error(t, builder.AppendWatermark("topic", 1, 4))

		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Equal(t, []watermarks.Watermark{{Topic: "topic", Partition: 1, Watermark: 5}}, readWatermarks(t, obj))
	})

	t.Run("builds nothing for a watermark after Reset", func(t *testing.T) {
		builder := newBuilder(t)
		require.NoError(t, builder.AppendWatermark("topic", 0, 1))
		builder.Reset()

		_, _, err := builder.Flush()
		require.ErrorIs(t, err, ErrBuilderEmpty)
	})

	t.Run("a flush does not carry watermarks into the next object", func(t *testing.T) {
		builder := newBuilder(t)
		require.NoError(t, builder.AppendWatermark("topic", 0, 1))
		_, closer, err := builder.Flush()
		require.NoError(t, err)
		require.NoError(t, closer.Close())

		require.NoError(t, builder.AppendIndexPointer(testPointers[0]))
		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Zero(t, obj.Sections().Count(watermarks.CheckSection))
	})
}

func TestBuilder_TimeRange(t *testing.T) {
	t.Run("returns the builder's tenant and zero times when the builder is empty", func(t *testing.T) {
		b, err := NewBuilder(testTenant, testBuilderConfig, nil, NewBuilderMetrics(nil))
		require.NoError(t, err)

		require.Equal(t, dataobj.TimeRange{Tenant: testTenant}, b.TimeRange())
	})

	t.Run("returns the range of the postings when the builder holds postings", func(t *testing.T) {
		b, err := NewBuilder(testTenant, testBuilderConfig, nil, NewBuilderMetrics(nil))
		require.NoError(t, err)

		base := time.Unix(8000, 0).UTC()
		b.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x",
			StreamID: 1, Time: postings.At(base),
		})
		b.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "y",
			StreamID: 2, Time: postings.At(base.Add(time.Hour)),
		})

		require.Equal(t, dataobj.TimeRange{
			Tenant:  testTenant,
			MinTime: base,
			MaxTime: base.Add(time.Hour),
		}, b.TimeRange())
	})

	t.Run("returns zero times after Reset", func(t *testing.T) {
		b, err := NewBuilder(testTenant, testBuilderConfig, nil, NewBuilderMetrics(nil))
		require.NoError(t, err)

		b.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x",
			StreamID: 1, Time: postings.At(time.Unix(8000, 0).UTC()),
		})
		require.False(t, b.TimeRange().MinTime.IsZero())

		b.Reset()
		require.Equal(t, dataobj.TimeRange{Tenant: testTenant}, b.TimeRange())
	})
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
