package stats

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestRowReader_Next(t *testing.T) {
	const schema = "label:service_name"

	t.Run("returns rows that stay valid after the reader moves to later batches", func(t *testing.T) {
		const rows = 2*rowReaderBatchSize + 100

		want := make([]Stat, rows)
		b := NewBuilder(nil, defaultEncoder, testLayout(schema))
		for i := range want {
			want[i] = Stat{
				ObjectPath:       fmt.Sprintf("objects/%06d/%s", i, strings.Repeat("p", i%7)),
				SectionIndex:     int64(i),
				ShardBucket:      uint32(i / 1000),
				SortSchema:       schema,
				Labels:           map[string]string{"service_name": fmt.Sprintf("svc-%06d-%s", i, strings.Repeat("s", i%5))},
				MinTimestamp:     int64(i),
				MaxTimestamp:     int64(i) + 1,
				RowCount:         int64(i) * 2,
				UncompressedSize: int64(i) * 3,
			}
			b.Append(want[i])
		}
		obj, closer := buildObject(t, b)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		got := readAllRowsFromObject(t, obj)

		require.Len(t, got, rows)
		for i := range want {
			require.Equal(t, want[i], got[i], "row %d", i)
		}
	})

	t.Run("leaves a label out of the map when the row has no value for it", func(t *testing.T) {
		b := NewBuilder(nil, defaultEncoder, testLayout(schema))
		b.Append(Stat{ObjectPath: "/with", SortSchema: schema, Labels: map[string]string{"service_name": "svc"}})
		b.Append(Stat{ObjectPath: "/without", SortSchema: schema})
		obj, closer := buildObject(t, b)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		got := readAllRowsFromObject(t, obj)

		require.Equal(t, []Stat{
			{ObjectPath: "/without", SortSchema: schema, Labels: map[string]string{}},
			{ObjectPath: "/with", SortSchema: schema, Labels: map[string]string{"service_name": "svc"}},
		}, got)
	})

	t.Run("returns false and keeps the error when a later batch fails to read", func(t *testing.T) {
		const rows = rowReaderBatchSize + 1

		b := NewBuilder(nil, defaultEncoder, testLayout(schema))
		for i := range rows {
			b.Append(Stat{
				ObjectPath: fmt.Sprintf("objects/%06d", i),
				SortSchema: schema,
				Labels:     map[string]string{"service_name": fmt.Sprintf("svc-%06d", i)},
			})
		}
		obj, closer := buildObject(t, b)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		ctx, cancel := context.WithCancel(t.Context())
		t.Cleanup(cancel)
		rr := NewRowReader(ctx, onlyStatsSection(t, obj), nil)
		t.Cleanup(func() { require.NoError(t, rr.Close()) })

		require.True(t, rr.Next())
		read := 1
		cancel()
		for rr.Next() {
			read++
		}

		require.ErrorIs(t, rr.Err(), context.Canceled)
		require.Less(t, read, rows)
		require.False(t, rr.Next())
		require.ErrorIs(t, rr.Err(), context.Canceled)
	})
}

func TestRowDecoder_decode(t *testing.T) {
	t.Run("returns rows that own their memory after the arrays of the batch are overwritten", func(t *testing.T) {
		batch := &RecordBatch{
			Rows:             4,
			ObjectPath:       utf8Array(t, "/obj/a", "/obj/bb", "/obj/ccc", "/obj/a"),
			SectionIndex:     int64Array(t, 1, 2, 3, 4),
			SortSchema:       utf8Array(t, "label:a", "label:bb", "label:ccc", "label:dddd"),
			MinTimestamp:     int64Array(t, 10, 20, 30, 40),
			MaxTimestamp:     int64Array(t, 11, 21, 31, 41),
			RowCount:         int64Array(t, 5, 6, 7, 8),
			UncompressedSize: int64Array(t, 50, 60, 70, 80),
			ShardBucket:      int64Array(t, 0, 1, 2, 3),
			Labels: []LabelColumn{
				{Name: "service_name", Values: utf8Array(t, "svc-1", nil, "svc-333", "svc-1")},
				{Name: "cluster", Values: utf8Array(t, "c-a", "c-bb", "c-ccc", "")},
			},
		}

		var d rowDecoder
		got := make([]Stat, batch.Rows)
		for i := range got {
			got[i] = d.decode(batch, i)
		}
		zeroArrays(batch)

		require.Equal(t, []Stat{
			{ObjectPath: "/obj/a", SectionIndex: 1, SortSchema: "label:a", ShardBucket: 0, MinTimestamp: 10, MaxTimestamp: 11, RowCount: 5, UncompressedSize: 50,
				Labels: map[string]string{"service_name": "svc-1", "cluster": "c-a"}},
			{ObjectPath: "/obj/bb", SectionIndex: 2, SortSchema: "label:bb", ShardBucket: 1, MinTimestamp: 20, MaxTimestamp: 21, RowCount: 6, UncompressedSize: 60,
				Labels: map[string]string{"cluster": "c-bb"}},
			{ObjectPath: "/obj/ccc", SectionIndex: 3, SortSchema: "label:ccc", ShardBucket: 2, MinTimestamp: 30, MaxTimestamp: 31, RowCount: 7, UncompressedSize: 70,
				Labels: map[string]string{"service_name": "svc-333", "cluster": "c-ccc"}},
			{ObjectPath: "/obj/a", SectionIndex: 4, SortSchema: "label:dddd", ShardBucket: 3, MinTimestamp: 40, MaxTimestamp: 41, RowCount: 8, UncompressedSize: 80,
				Labels: map[string]string{"service_name": "svc-1", "cluster": ""}},
		}, got)
	})

	t.Run("leaves the field of a column that the batch does not hold at the zero value", func(t *testing.T) {
		batch := &RecordBatch{
			Rows:     1,
			RowCount: int64Array(t, 5),
			Labels:   []LabelColumn{{Name: "app", Values: utf8Array(t, "loki")}},
		}

		var d rowDecoder
		require.Equal(t, Stat{RowCount: 5, Labels: map[string]string{"app": "loki"}}, d.decode(batch, 0))
	})

	t.Run("leaves the field of a null element at the zero value", func(t *testing.T) {
		batch := &RecordBatch{
			Rows:             1,
			ObjectPath:       utf8Array(t, nil),
			SectionIndex:     int64Array(t, nil),
			SortSchema:       utf8Array(t, nil),
			MinTimestamp:     int64Array(t, nil),
			MaxTimestamp:     int64Array(t, nil),
			RowCount:         int64Array(t, nil),
			UncompressedSize: int64Array(t, nil),
			ShardBucket:      int64Array(t, nil),
			Labels:           []LabelColumn{{Name: "app", Values: utf8Array(t, nil)}},
		}

		var d rowDecoder
		require.Equal(t, Stat{Labels: map[string]string{}}, d.decode(batch, 0))
	})

	t.Run("leaves the field of a null element at the zero value when the null slot holds data", func(t *testing.T) {
		validity := memory.NewBitmap(nil, 2)
		validity.AppendValues(false, true)
		numbers := columnar.NewNumber([]int64{7, 9}, validity)
		strs := columnar.NewUTF8([]byte("junkok"), []int32{0, 4, 6}, validity)
		batch := &RecordBatch{
			Rows:             2,
			ObjectPath:       strs,
			SectionIndex:     numbers,
			SortSchema:       strs,
			MinTimestamp:     numbers,
			MaxTimestamp:     numbers,
			RowCount:         numbers,
			UncompressedSize: numbers,
			ShardBucket:      numbers,
			Labels:           []LabelColumn{{Name: "app", Values: strs}},
		}

		var d rowDecoder
		require.Equal(t, Stat{Labels: map[string]string{}}, d.decode(batch, 0))
		require.Equal(t, Stat{
			ObjectPath: "ok", SectionIndex: 9, SortSchema: "ok", ShardBucket: 9,
			MinTimestamp: 9, MaxTimestamp: 9, RowCount: 9, UncompressedSize: 9,
			Labels: map[string]string{"app": "ok"},
		}, d.decode(batch, 1))
	})

	t.Run("returns the labels of the batch when the number of label columns changes between batches", func(t *testing.T) {
		one := &RecordBatch{
			Rows:   1,
			Labels: []LabelColumn{{Name: "env", Values: utf8Array(t, "dev")}},
		}
		two := &RecordBatch{
			Rows: 1,
			Labels: []LabelColumn{
				{Name: "app", Values: utf8Array(t, "loki")},
				{Name: "env", Values: utf8Array(t, "prod")},
			},
		}

		var d rowDecoder
		require.Equal(t, map[string]string{"env": "dev"}, d.decode(one, 0).Labels)
		require.Equal(t, map[string]string{"app": "loki", "env": "prod"}, d.decode(two, 0).Labels)
		require.Equal(t, map[string]string{"env": "dev"}, d.decode(one, 0).Labels)
	})

	t.Run("keeps the last non-null value when two label columns have the same name", func(t *testing.T) {
		batch := &RecordBatch{
			Rows: 2,
			Labels: []LabelColumn{
				{Name: "app", Values: utf8Array(t, "a", "b")},
				{Name: "app", Values: utf8Array(t, nil, "d")},
			},
		}

		var d rowDecoder
		require.Equal(t, map[string]string{"app": "a"}, d.decode(batch, 0).Labels)
		require.Equal(t, map[string]string{"app": "d"}, d.decode(batch, 1).Labels)
	})
}

// onlyStatsSection opens the stats section of obj. It fails the test if obj does
// not hold exactly one.
func onlyStatsSection(t *testing.T, obj *dataobj.Object) *Section {
	t.Helper()

	var sections []*Section
	for _, s := range obj.Sections() {
		if !CheckSection(s) {
			continue
		}
		sec, err := Open(t.Context(), s)
		require.NoError(t, err)
		sections = append(sections, sec)
	}
	require.Len(t, sections, 1)
	return sections[0]
}

func utf8Array(t *testing.T, values ...any) *columnar.UTF8 {
	t.Helper()
	return columnartest.Array(t, types.KindUTF8, nil, values...).(*columnar.UTF8)
}

func int64Array(t *testing.T, values ...any) *columnar.Number[int64] {
	t.Helper()
	return columnartest.Array(t, types.KindInt64, nil, values...).(*columnar.Number[int64])
}

// zeroArrays zeroes the data, the offsets and the values of every array of
// batch. It models the reuse of that memory by the allocator after Reclaim. The
// batch must have every field set.
func zeroArrays(batch *RecordBatch) {
	for _, arr := range []*columnar.UTF8{batch.ObjectPath, batch.SortSchema} {
		clear(arr.Data())
		clear(arr.Offsets())
	}
	for _, arr := range []*columnar.Number[int64]{batch.SectionIndex, batch.MinTimestamp, batch.MaxTimestamp, batch.RowCount, batch.UncompressedSize, batch.ShardBucket} {
		clear(arr.Values())
	}
	for _, label := range batch.Labels {
		clear(label.Values.Data())
		clear(label.Values.Offsets())
	}
}
