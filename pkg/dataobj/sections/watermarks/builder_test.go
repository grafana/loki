package watermarks

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
)

const tenantID = "tenantID"

func buildObject(t *testing.T, build func(b *Builder)) *dataobj.Object {
	t.Helper()

	wb := NewBuilder(NewMetrics(nil), tenantID, 1024, 0)
	build(wb)

	ob := dataobj.NewBuilder(nil)
	require.NoError(t, ob.Append(wb))

	obj, closer, err := ob.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })
	return obj
}

func buildOtherSectionObject(t *testing.T) *dataobj.Object {
	t.Helper()

	pb := indexpointers.NewBuilder(nil, 1024, 0)
	pb.SetTenant(tenantID)
	pb.Append(indexpointers.IndexPointer{Path: "path", StartTs: time.Unix(1, 0), EndTs: time.Unix(2, 0)})

	ob := dataobj.NewBuilder(nil)
	require.NoError(t, ob.Append(pb))
	obj, closer, err := ob.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })
	return obj
}

func iterAll(t *testing.T, obj *dataobj.Object) []Watermark {
	t.Helper()

	var got []Watermark
	for res := range Iter(t.Context(), obj) {
		wm, err := res.Value()
		require.NoError(t, err)
		got = append(got, wm)
	}
	return got
}

func TestBuilder(t *testing.T) {
	t.Run("round trips rows of several topics and partitions in sorted order", func(t *testing.T) {
		obj := buildObject(t, func(b *Builder) {
			require.NoError(t, b.Set("topic-b", 1, 300))
			require.NoError(t, b.Set("topic-a", 2, 20))
			require.NoError(t, b.Set("topic-b", 0, 0))
			require.NoError(t, b.Set("topic-a", 0, 10))
		})

		require.Equal(t, []Watermark{
			{Topic: "topic-a", Partition: 0, Watermark: 10},
			{Topic: "topic-a", Partition: 2, Watermark: 20},
			{Topic: "topic-b", Partition: 0, Watermark: 0},
			{Topic: "topic-b", Partition: 1, Watermark: 300},
		}, iterAll(t, obj))
	})

	t.Run("replaces the watermark when the new one is higher", func(t *testing.T) {
		obj := buildObject(t, func(b *Builder) {
			require.NoError(t, b.Set("topic", 3, 50))
			require.NoError(t, b.Set("topic", 3, 100))
		})

		require.Equal(t, []Watermark{{Topic: "topic", Partition: 3, Watermark: 100}}, iterAll(t, obj))
	})

	t.Run("accepts the same watermark twice", func(t *testing.T) {
		obj := buildObject(t, func(b *Builder) {
			require.NoError(t, b.Set("topic", 3, 50))
			require.NoError(t, b.Set("topic", 3, 50))
		})

		require.Equal(t, []Watermark{{Topic: "topic", Partition: 3, Watermark: 50}}, iterAll(t, obj))
	})

	t.Run("returns an error and keeps the old watermark when the new one is lower", func(t *testing.T) {
		obj := buildObject(t, func(b *Builder) {
			require.NoError(t, b.Set("topic", 3, 100))
			require.Error(t, b.Set("topic", 3, 50))
		})

		require.Equal(t, []Watermark{{Topic: "topic", Partition: 3, Watermark: 100}}, iterAll(t, obj))
	})

	t.Run("returns an error and stores nothing for an invalid row", func(t *testing.T) {
		wb := NewBuilder(NewMetrics(nil), tenantID, 1024, 0)

		require.Error(t, wb.Set("", 0, 1))
		require.Error(t, wb.Set("topic", -1, 1))
		require.Error(t, wb.Set("topic", 0, -1))
		require.Zero(t, wb.EstimatedSize())
	})

	t.Run("round trips the extreme partition and offset values", func(t *testing.T) {
		obj := buildObject(t, func(b *Builder) {
			require.NoError(t, b.Set("topic", math.MaxInt32, math.MaxInt64))
		})

		require.Equal(t, []Watermark{{Topic: "topic", Partition: math.MaxInt32, Watermark: math.MaxInt64}}, iterAll(t, obj))
	})

	t.Run("writes an empty section when the builder holds no rows", func(t *testing.T) {
		obj := buildObject(t, func(*Builder) {})

		require.Equal(t, 1, obj.Sections().Count(CheckSection))
		require.Empty(t, iterAll(t, obj))
	})

	t.Run("round trips more rows than fit on one page", func(t *testing.T) {
		wb := NewBuilder(NewMetrics(nil), tenantID, 1024, 2)
		var want []Watermark
		for p := range int32(7) {
			require.NoError(t, wb.Set("topic", p, int64(p)*10))
			want = append(want, Watermark{Topic: "topic", Partition: p, Watermark: int64(p) * 10})
		}

		ob := dataobj.NewBuilder(nil)
		require.NoError(t, ob.Append(wb))
		obj, closer, err := ob.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Equal(t, want, iterAll(t, obj))
	})

	t.Run("estimates the size from the topic bytes, the numbers and the column overhead", func(t *testing.T) {
		wb := NewBuilder(NewMetrics(nil), tenantID, 1024, 0)
		require.Zero(t, wb.EstimatedSize())

		require.NoError(t, wb.Set("ab", 0, 1))
		require.NoError(t, wb.Set("c", 1, 2))

		require.Equal(t, (2+16)+(1+16)+3*100, wb.EstimatedSize(), "2 and 1 topic bytes, 16 bytes of numbers for each row, 100 bytes for each of 3 columns")
	})

	t.Run("resets the builder after a flush", func(t *testing.T) {
		wb := NewBuilder(NewMetrics(nil), tenantID, 1024, 0)
		require.NoError(t, wb.Set("topic", 0, 1))
		require.Positive(t, wb.EstimatedSize())

		ob := dataobj.NewBuilder(nil)
		require.NoError(t, ob.Append(wb))
		require.Zero(t, wb.EstimatedSize())
	})

	t.Run("writes the section for the builder's tenant", func(t *testing.T) {
		obj := buildObject(t, func(b *Builder) { require.NoError(t, b.Set("topic", 0, 1)) })

		require.Equal(t, tenantID, obj.Sections()[0].Tenant)
	})
}
