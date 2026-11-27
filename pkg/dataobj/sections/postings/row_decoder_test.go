package postings

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestRowDecoder_decode(t *testing.T) {
	t.Run("returns rows that own their strings and bytes after the memory of the batch changes", func(t *testing.T) {
		batch := &RecordBatch{
			Rows:             4,
			Kind:             int64Array(t, 0, 1, 1, 1),
			ObjectPath:       utf8Array(t, "/obj/1", "/obj/22", "/obj/1", "/obj/22"),
			ShardBuckets:     int64Array(t, 16, 16, 16, 16),
			SectionIndex:     int64Array(t, 0, 1, 2, 3),
			ColumnName:       utf8Array(t, "trace_id", "env", "service_name", "service_name"),
			LabelValue:       utf8Array(t, nil, "prod", "us-east-1", "us-west-2"),
			BloomFilter:      utf8Array(t, "bloom-0", nil, nil, nil),
			StreamIDBitmap:   utf8Array(t, "bitmap-0", "bitmap-1", "bitmap-2", "bitmap-3"),
			UncompressedSize: int64Array(t, 100, 200, 300, 400),
			MinTimestamp:     int64Array(t, 10, 11, 12, 13),
			MaxTimestamp:     int64Array(t, 20, 21, 22, 23),
			MinShardBucket:   int64Array(t, nil, 3, 4, 5),
			MaxShardBucket:   int64Array(t, nil, 6, 7, 8),
		}

		var decoder rowDecoder
		rows := make([]Row, 0, batch.Rows)
		for i := range batch.Rows {
			rows = append(rows, decoder.decode(batch, i))
		}
		zeroArrays(batch)

		require.Equal(t, []Row{
			{
				Kind: KindBloom, ObjectPath: "/obj/1", ShardBuckets: 16, SectionIndex: 0,
				ColumnName: "trace_id", BloomFilter: []byte("bloom-0"), StreamIDBitmap: []byte("bitmap-0"),
				UncompressedSize: 100, MinTimestamp: 10, MaxTimestamp: 20,
			},
			{
				Kind: KindLabel, ObjectPath: "/obj/22", ShardBuckets: 16, SectionIndex: 1,
				ColumnName: "env", LabelValue: "prod", StreamIDBitmap: []byte("bitmap-1"),
				UncompressedSize: 200, MinTimestamp: 11, MaxTimestamp: 21, MinShardBucket: 3, MaxShardBucket: 6,
			},
			{
				Kind: KindLabel, ObjectPath: "/obj/1", ShardBuckets: 16, SectionIndex: 2,
				ColumnName: "service_name", LabelValue: "us-east-1", StreamIDBitmap: []byte("bitmap-2"),
				UncompressedSize: 300, MinTimestamp: 12, MaxTimestamp: 22, MinShardBucket: 4, MaxShardBucket: 7,
			},
			{
				Kind: KindLabel, ObjectPath: "/obj/22", ShardBuckets: 16, SectionIndex: 3,
				ColumnName: "service_name", LabelValue: "us-west-2", StreamIDBitmap: []byte("bitmap-3"),
				UncompressedSize: 400, MinTimestamp: 13, MaxTimestamp: 23, MinShardBucket: 5, MaxShardBucket: 8,
			},
		}, rows)
	})

	t.Run("returns the zero value for a field that the batch does not hold", func(t *testing.T) {
		for _, tc := range []struct {
			field string
			drop  func(*RecordBatch)
			zero  func(*Row)
		}{
			{"Kind", func(b *RecordBatch) { b.Kind = nil }, func(r *Row) { r.Kind = 0 }},
			{"ObjectPath", func(b *RecordBatch) { b.ObjectPath = nil }, func(r *Row) { r.ObjectPath = "" }},
			{"ShardBuckets", func(b *RecordBatch) { b.ShardBuckets = nil }, func(r *Row) { r.ShardBuckets = 0 }},
			{"SectionIndex", func(b *RecordBatch) { b.SectionIndex = nil }, func(r *Row) { r.SectionIndex = 0 }},
			{"ColumnName", func(b *RecordBatch) { b.ColumnName = nil }, func(r *Row) { r.ColumnName = "" }},
			{"LabelValue", func(b *RecordBatch) { b.LabelValue = nil }, func(r *Row) { r.LabelValue = "" }},
			{"BloomFilter", func(b *RecordBatch) { b.BloomFilter = nil }, func(r *Row) { r.BloomFilter = nil }},
			{"StreamIDBitmap", func(b *RecordBatch) { b.StreamIDBitmap = nil }, func(r *Row) { r.StreamIDBitmap = nil }},
			{"UncompressedSize", func(b *RecordBatch) { b.UncompressedSize = nil }, func(r *Row) { r.UncompressedSize = 0 }},
			{"MinTimestamp", func(b *RecordBatch) { b.MinTimestamp = nil }, func(r *Row) { r.MinTimestamp = 0 }},
			{"MaxTimestamp", func(b *RecordBatch) { b.MaxTimestamp = nil }, func(r *Row) { r.MaxTimestamp = 0 }},
			{"MinShardBucket", func(b *RecordBatch) { b.MinShardBucket = nil }, func(r *Row) { r.MinShardBucket = 0 }},
			{"MaxShardBucket", func(b *RecordBatch) { b.MaxShardBucket = nil }, func(r *Row) { r.MaxShardBucket = 0 }},
		} {
			t.Run("returns the zero "+tc.field+" when the batch does not hold it", func(t *testing.T) {
				batch, want := fullRecordBatch(t)
				tc.drop(batch)
				tc.zero(&want)

				var decoder rowDecoder
				require.Equal(t, want, decoder.decode(batch, 0))
			})
		}
	})

	t.Run("returns the zero value for a null element that holds data", func(t *testing.T) {
		batch := &RecordBatch{
			Rows:             2,
			Kind:             numbersWithNullSlot(1),
			ObjectPath:       stringsWithNullSlot("/obj"),
			ShardBuckets:     numbersWithNullSlot(16),
			SectionIndex:     numbersWithNullSlot(2),
			ColumnName:       stringsWithNullSlot("env"),
			LabelValue:       stringsWithNullSlot("prod"),
			BloomFilter:      stringsWithNullSlot("bloom"),
			StreamIDBitmap:   stringsWithNullSlot("bitmap"),
			UncompressedSize: numbersWithNullSlot(100),
			MinTimestamp:     numbersWithNullSlot(10),
			MaxTimestamp:     numbersWithNullSlot(20),
			MinShardBucket:   numbersWithNullSlot(3),
			MaxShardBucket:   numbersWithNullSlot(6),
		}

		var decoder rowDecoder
		require.Equal(t, Row{}, decoder.decode(batch, 0))
		require.Equal(t, Row{
			Kind: KindLabel, ObjectPath: "/obj", ShardBuckets: 16, SectionIndex: 2,
			ColumnName: "env", LabelValue: "prod", BloomFilter: []byte("bloom"), StreamIDBitmap: []byte("bitmap"),
			UncompressedSize: 100, MinTimestamp: 10, MaxTimestamp: 20, MinShardBucket: 3, MaxShardBucket: 6,
		}, decoder.decode(batch, 1))
	})
}

func int64Array(t *testing.T, values ...any) *columnar.Number[int64] {
	t.Helper()
	return columnartest.Array(t, types.KindInt64, nil, values...).(*columnar.Number[int64])
}

func utf8Array(t *testing.T, values ...any) *columnar.UTF8 {
	t.Helper()
	return columnartest.Array(t, types.KindUTF8, nil, values...).(*columnar.UTF8)
}

// numbersWithNullSlot returns an array of two elements. The first element is
// null and holds junk data. The second element holds value.
func numbersWithNullSlot(value int64) *columnar.Number[int64] {
	validity := memory.NewBitmap(nil, 2)
	validity.AppendValues(false, true)
	return columnar.NewNumber([]int64{7, value}, validity)
}

// stringsWithNullSlot returns an array of two elements. The first element is
// null and holds junk data. The second element holds value.
func stringsWithNullSlot(value string) *columnar.UTF8 {
	validity := memory.NewBitmap(nil, 2)
	validity.AppendValues(false, true)
	return columnar.NewUTF8([]byte("junk"+value), []int32{0, 4, int32(4 + len(value))}, validity)
}

// fullRecordBatch returns a batch of one row that holds every field with a value
// that is not zero, and the row that decodes from it.
func fullRecordBatch(t *testing.T) (*RecordBatch, Row) {
	t.Helper()

	batch := &RecordBatch{
		Rows:             1,
		Kind:             int64Array(t, 1),
		ObjectPath:       utf8Array(t, "/obj"),
		ShardBuckets:     int64Array(t, 16),
		SectionIndex:     int64Array(t, 2),
		ColumnName:       utf8Array(t, "env"),
		LabelValue:       utf8Array(t, "prod"),
		BloomFilter:      utf8Array(t, "bloom"),
		StreamIDBitmap:   utf8Array(t, "bitmap"),
		UncompressedSize: int64Array(t, 100),
		MinTimestamp:     int64Array(t, 10),
		MaxTimestamp:     int64Array(t, 20),
		MinShardBucket:   int64Array(t, 3),
		MaxShardBucket:   int64Array(t, 6),
	}
	row := Row{
		Kind: KindLabel, ObjectPath: "/obj", ShardBuckets: 16, SectionIndex: 2,
		ColumnName: "env", LabelValue: "prod", BloomFilter: []byte("bloom"), StreamIDBitmap: []byte("bitmap"),
		UncompressedSize: 100, MinTimestamp: 10, MaxTimestamp: 20, MinShardBucket: 3, MaxShardBucket: 6,
	}
	return batch, row
}

// zeroArrays zeroes the data, the offsets and the values of every array of
// batch. It models the reuse of that memory by the allocator after Reclaim.
func zeroArrays(batch *RecordBatch) {
	for _, arr := range []*columnar.UTF8{batch.ObjectPath, batch.ColumnName, batch.LabelValue, batch.BloomFilter, batch.StreamIDBitmap} {
		if arr != nil {
			clear(arr.Data())
			clear(arr.Offsets())
		}
	}
	for _, arr := range []*columnar.Number[int64]{
		batch.Kind, batch.ShardBuckets, batch.SectionIndex, batch.UncompressedSize,
		batch.MinTimestamp, batch.MaxTimestamp, batch.MinShardBucket, batch.MaxShardBucket,
	} {
		if arr != nil {
			clear(arr.Values())
		}
	}
}
