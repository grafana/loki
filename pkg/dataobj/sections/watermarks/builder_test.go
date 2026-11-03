package watermarks_test

import (
	"errors"
	"io"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/watermarks"
)

const tenantID = "tenantID"

func TestBuilder(t *testing.T) {
	buildObject := func(t *testing.T, build func(b *watermarks.Builder)) *dataobj.Object {
		t.Helper()

		wb := watermarks.NewBuilder(watermarks.NewMetrics(nil), tenantID, 1024, 0)
		build(wb)

		ob := dataobj.NewBuilder(nil)
		require.NoError(t, ob.Append(wb))

		obj, closer, err := ob.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { _ = closer.Close() })
		return obj
	}

	buildOtherSectionObject := func(t *testing.T) *dataobj.Object {
		t.Helper()

		pb := indexpointers.NewBuilder(nil, 1024, 0)
		pb.SetTenant(tenantID)
		pb.Append("path", time.Unix(1, 0), time.Unix(2, 0))

		ob := dataobj.NewBuilder(nil)
		require.NoError(t, ob.Append(pb))
		obj, closer, err := ob.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { _ = closer.Close() })
		return obj
	}

	iterAll := func(t *testing.T, obj *dataobj.Object) []watermarks.Watermark {
		t.Helper()

		var got []watermarks.Watermark
		for res := range watermarks.Iter(t.Context(), obj) {
			wm, err := res.Value()
			require.NoError(t, err)
			got = append(got, wm)
		}
		return got
	}

	t.Run("round trips rows of several topics and partitions in sorted order", func(t *testing.T) {
		obj := buildObject(t, func(b *watermarks.Builder) {
			require.NoError(t, b.Set("topic-b", 1, 300))
			require.NoError(t, b.Set("topic-a", 2, 20))
			require.NoError(t, b.Set("topic-b", 0, 0))
			require.NoError(t, b.Set("topic-a", 0, 10))
		})

		require.Equal(t, []watermarks.Watermark{
			{Topic: "topic-a", Partition: 0, Watermark: 10},
			{Topic: "topic-a", Partition: 2, Watermark: 20},
			{Topic: "topic-b", Partition: 0, Watermark: 0},
			{Topic: "topic-b", Partition: 1, Watermark: 300},
		}, iterAll(t, obj))
	})

	t.Run("keeps the last watermark set for the same topic and partition", func(t *testing.T) {
		obj := buildObject(t, func(b *watermarks.Builder) {
			require.NoError(t, b.Set("topic", 3, 100))
			require.NoError(t, b.Set("topic", 3, 50))
		})

		require.Equal(t, []watermarks.Watermark{{Topic: "topic", Partition: 3, Watermark: 50}}, iterAll(t, obj))
	})

	t.Run("round trips the extreme partition and offset values", func(t *testing.T) {
		obj := buildObject(t, func(b *watermarks.Builder) {
			require.NoError(t, b.Set("topic", math.MaxInt32, math.MaxInt64))
		})

		require.Equal(t, []watermarks.Watermark{{Topic: "topic", Partition: math.MaxInt32, Watermark: math.MaxInt64}}, iterAll(t, obj))
	})

	t.Run("reads no rows from an empty section", func(t *testing.T) {
		obj := buildObject(t, func(*watermarks.Builder) {})

		require.Equal(t, 1, obj.Sections().Count(watermarks.CheckSection))
		require.Empty(t, iterAll(t, obj))
	})

	t.Run("the row reader returns EOF on an empty section", func(t *testing.T) {
		obj := buildObject(t, func(*watermarks.Builder) {})

		sec, err := watermarks.Open(t.Context(), obj.Sections()[0])
		require.NoError(t, err)

		r := watermarks.NewRowReader(sec)
		defer r.Close()
		require.NoError(t, r.Open(t.Context()))

		n, err := r.Read(t.Context(), make([]watermarks.Watermark, 4))
		require.Zero(t, n)
		require.ErrorIs(t, err, io.EOF)
	})

	t.Run("the row reader returns the same rows as the iterator", func(t *testing.T) {
		obj := buildObject(t, func(b *watermarks.Builder) {
			require.NoError(t, b.Set("topic-a", 1, 11))
			require.NoError(t, b.Set("topic-a", 0, 10))
			require.NoError(t, b.Set("topic-b", 0, 20))
		})

		sec, err := watermarks.Open(t.Context(), obj.Sections()[0])
		require.NoError(t, err)

		r := watermarks.NewRowReader(sec)
		defer r.Close()

		require.NoError(t, r.Open(t.Context()))
		buf := make([]watermarks.Watermark, 2)

		var got []watermarks.Watermark
		for {
			n, err := r.Read(t.Context(), buf)
			got = append(got, buf[:n]...)
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
		}

		require.Equal(t, iterAll(t, obj), got)
		require.Len(t, got, 3)
	})

	t.Run("the row reader returns an error when read before Open", func(t *testing.T) {
		obj := buildObject(t, func(b *watermarks.Builder) { require.NoError(t, b.Set("topic", 0, 1)) })

		sec, err := watermarks.Open(t.Context(), obj.Sections()[0])
		require.NoError(t, err)

		r := watermarks.NewRowReader(sec)
		defer r.Close()

		_, err = r.Read(t.Context(), make([]watermarks.Watermark, 1))
		require.Error(t, err)
		require.NotErrorIs(t, err, io.EOF)
	})

	t.Run("the row reader returns an error when read after Close", func(t *testing.T) {
		obj := buildObject(t, func(b *watermarks.Builder) { require.NoError(t, b.Set("topic", 0, 1)) })

		sec, err := watermarks.Open(t.Context(), obj.Sections()[0])
		require.NoError(t, err)

		r := watermarks.NewRowReader(sec)
		require.NoError(t, r.Open(t.Context()))
		require.NoError(t, r.Close())

		_, err = r.Read(t.Context(), make([]watermarks.Watermark, 1))
		require.Error(t, err)
		require.NotErrorIs(t, err, io.EOF)
	})

	t.Run("round trips more rows than fit on one page", func(t *testing.T) {
		wb := watermarks.NewBuilder(watermarks.NewMetrics(nil), tenantID, 1024, 2)
		var want []watermarks.Watermark
		for p := range int32(7) {
			require.NoError(t, wb.Set("topic", p, int64(p)*10))
			want = append(want, watermarks.Watermark{Topic: "topic", Partition: p, Watermark: int64(p) * 10})
		}

		ob := dataobj.NewBuilder(nil)
		require.NoError(t, ob.Append(wb))
		obj, closer, err := ob.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Equal(t, want, iterAll(t, obj))
	})

	t.Run("iterates the rows of every watermarks section in object order", func(t *testing.T) {
		ob := dataobj.NewBuilder(nil)
		for i, topic := range []string{"topic-a", "topic-b"} {
			wb := watermarks.NewBuilder(watermarks.NewMetrics(nil), tenantID, 1024, 0)
			require.NoError(t, wb.Set(topic, int32(i), int64(i)))
			require.NoError(t, ob.Append(wb))
		}
		obj, closer, err := ob.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Equal(t, []watermarks.Watermark{
			{Topic: "topic-a", Partition: 0, Watermark: 0},
			{Topic: "topic-b", Partition: 1, Watermark: 1},
		}, iterAll(t, obj))
	})

	t.Run("stops iterating when the caller stops", func(t *testing.T) {
		obj := buildObject(t, func(b *watermarks.Builder) {
			require.NoError(t, b.Set("topic", 0, 1))
			require.NoError(t, b.Set("topic", 1, 2))
		})

		var got []watermarks.Watermark
		for res := range watermarks.Iter(t.Context(), obj) {
			wm, err := res.Value()
			require.NoError(t, err)
			got = append(got, wm)
			break
		}
		require.Len(t, got, 1)
	})

	t.Run("reads no rows from an object without a watermarks section", func(t *testing.T) {
		obj := buildOtherSectionObject(t)

		require.Empty(t, iterAll(t, obj))
	})

	t.Run("Open returns an error for a section of another type", func(t *testing.T) {
		obj := buildOtherSectionObject(t)

		_, err := watermarks.Open(t.Context(), obj.Sections()[0])
		require.Error(t, err)
	})

	t.Run("Set returns an error and stores nothing for an invalid row", func(t *testing.T) {
		wb := watermarks.NewBuilder(watermarks.NewMetrics(nil), tenantID, 1024, 0)

		require.Error(t, wb.Set("", 0, 1))
		require.Error(t, wb.Set("topic", -1, 1))
		require.Error(t, wb.Set("topic", 0, -1))
		require.Zero(t, wb.EstimatedSize())
	})

	t.Run("flushing resets the builder", func(t *testing.T) {
		wb := watermarks.NewBuilder(watermarks.NewMetrics(nil), tenantID, 1024, 0)
		require.NoError(t, wb.Set("topic", 0, 1))
		require.Positive(t, wb.EstimatedSize())

		ob := dataobj.NewBuilder(nil)
		require.NoError(t, ob.Append(wb))
		require.Zero(t, wb.EstimatedSize())
	})

	t.Run("reports the section tenant", func(t *testing.T) {
		obj := buildObject(t, func(b *watermarks.Builder) { require.NoError(t, b.Set("topic", 0, 1)) })

		require.Equal(t, tenantID, obj.Sections()[0].Tenant)
	})
}
