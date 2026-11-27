package postings

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func timeUnix(sec, nsec int64) time.Time { return time.Unix(sec, nsec).UTC() }

// checkBit returns true if bit n is set in the LSB-encoded bitmap data.
func checkBit(data []byte, n int) bool {
	byteIdx := n / 8
	bitPos := uint(n % 8)
	if byteIdx >= len(data) {
		return false
	}
	return (data[byteIdx]>>bitPos)&1 == 1
}

// TestBuilder_Empty verifies that an empty builder produces no sections.
func TestBuilder_Empty(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)
	require.Zero(t, b.EstimatedSize(), "empty builder should have zero size")
	// An empty builder writes 0 bytes; the dataobj builder would fail to flush
	// without any data. This verifies the postings builder is truly empty.
	require.Empty(t, b.labels.entries, "empty builder should have no label entries")
	require.Empty(t, b.blooms.entries, "empty builder should have no bloom entries")
}

// TestBuilder_LabelPostingRoundTrip verifies a label posting round-trips correctly.
func TestBuilder_LabelPostingRoundTrip(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)

	ts := time.Unix(0, 1000).UTC()
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/tenant/abc/obj1", ShardBuckets: 16, SectionIndex: 0, ColumnName: "env", LabelValue: "value1", StreamID: 3, Time: At(ts), UncompressedSize: 4096, ShardBucket: 2})
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/tenant/abc/obj1", ShardBuckets: 16, SectionIndex: 0, ColumnName: "env", LabelValue: "value1", StreamID: 7, Time: At(ts), UncompressedSize: 0, ShardBucket: 7})
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/tenant/abc/obj1", ShardBuckets: 16, SectionIndex: 0, ColumnName: "env", LabelValue: "value1", StreamID: 15, Time: At(ts), UncompressedSize: 0, ShardBucket: 4})

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows := readAllRows(t, sections[0])
	require.Len(t, rows, 1)

	// Verify stream IDs 3, 7, 15 are set in the bitmap. The bit checks cover the
	// bitmap, so the comparison below leaves it out.
	bitmap := rows[0].StreamIDBitmap
	require.True(t, checkBit(bitmap, 3), "bit 3 should be set")
	require.True(t, checkBit(bitmap, 7), "bit 7 should be set")
	require.True(t, checkBit(bitmap, 15), "bit 15 should be set")
	require.False(t, checkBit(bitmap, 0), "bit 0 should not be set")
	rows[0].StreamIDBitmap = nil

	require.Equal(t, Row{
		Kind:             KindLabel,
		ObjectPath:       "/tenant/abc/obj1",
		ShardBuckets:     16,
		SectionIndex:     0,
		ColumnName:       "env",
		LabelValue:       "value1",
		UncompressedSize: 4096,
		MinTimestamp:     ts.UnixNano(),
		MaxTimestamp:     ts.UnixNano(),
		MinShardBucket:   2,
		MaxShardBucket:   7,
	}, rows[0])
}

// TestBuilder_BloomPostingRoundTrip verifies a bloom posting round-trips correctly.
func TestBuilder_BloomPostingRoundTrip(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)

	ts := time.Unix(0, 500).UTC()
	b.PrepareBloomColumn("/tenant/abc/obj2", 1, "service_name", 100, 0)
	err := b.ObserveBloomPosting(BloomObservation{ObjectPath: "/tenant/abc/obj2", ShardBuckets: 16, SectionIndex: 1, ColumnName: "service_name", Value: "my-service", StreamID: 0, Timestamp: ts, UncompressedSize: 8192})
	require.NoError(t, err)
	err = b.ObserveBloomPosting(BloomObservation{ObjectPath: "/tenant/abc/obj2", ShardBuckets: 16, SectionIndex: 1, ColumnName: "service_name", Value: "my-service", StreamID: 2, Timestamp: ts, UncompressedSize: 0})
	require.NoError(t, err)
	err = b.ObserveBloomPosting(BloomObservation{ObjectPath: "/tenant/abc/obj2", ShardBuckets: 16, SectionIndex: 1, ColumnName: "service_name", Value: "my-service", StreamID: 8, Timestamp: ts, UncompressedSize: 0})
	require.NoError(t, err)

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows := readAllRows(t, sections[0])
	require.Len(t, rows, 1)

	// A bloom row has no label value and no shard bucket range. A row decodes a
	// null as the zero value, so check the nulls in the batch.
	batch := readBatch(t, sections[0])
	require.True(t, batch.LabelValue.IsNull(0), "bloom posting should have a null label value")
	require.True(t, batch.MinShardBucket.IsNull(0), "bloom posting should have a null min shard bucket")
	require.True(t, batch.MaxShardBucket.IsNull(0), "bloom posting should have a null max shard bucket")

	// The checks below cover the bloom filter and the bitmap, so the comparison
	// after them leaves both out.
	require.NotEmpty(t, rows[0].BloomFilter, "Bloom posting should have a BloomFilter")
	rows[0].BloomFilter = nil

	// Verify stream IDs 0, 2, 8 are set.
	bitmap := rows[0].StreamIDBitmap
	require.True(t, checkBit(bitmap, 0), "bit 0 should be set")
	require.True(t, checkBit(bitmap, 2), "bit 2 should be set")
	require.True(t, checkBit(bitmap, 8), "bit 8 should be set")
	require.False(t, checkBit(bitmap, 1), "bit 1 should not be set")
	rows[0].StreamIDBitmap = nil

	require.Equal(t, Row{
		Kind:             KindBloom,
		ObjectPath:       "/tenant/abc/obj2",
		ShardBuckets:     16,
		SectionIndex:     1,
		ColumnName:       "service_name",
		UncompressedSize: 8192,
		MinTimestamp:     ts.UnixNano(),
		MaxTimestamp:     ts.UnixNano(),
	}, rows[0])
}

// TestBuilder_MixedPostings verifies both Bloom and Label postings work together.
func TestBuilder_MixedPostings(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)

	ts := time.Unix(0, 100).UTC()
	ts2 := time.Unix(0, 300).UTC()
	b.PrepareBloomColumn("/obj1", 0, "col_a", 10, 0)
	err := b.ObserveBloomPosting(BloomObservation{ObjectPath: "/obj1", SectionIndex: 0, ColumnName: "col_a", Value: "val", StreamID: 0, Timestamp: ts, UncompressedSize: 0})
	require.NoError(t, err)

	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/obj2", SectionIndex: 0, ColumnName: "col_b", LabelValue: "myval", StreamID: 1, Time: At(ts2), UncompressedSize: 0})
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/obj2", SectionIndex: 0, ColumnName: "col_b", LabelValue: "myval", StreamID: 3, Time: At(ts2), UncompressedSize: 0})

	sections := flushAndOpenSections(t, b)
	require.Greater(t, len(sections), 0)

	allRows := readAllRows(t, sections...)
	require.Len(t, allRows, 2)

	// Bloom (KindBloom=0) sorts before Label (KindLabel=1).
	require.Equal(t, KindBloom, allRows[0].Kind)
	require.Empty(t, allRows[0].LabelValue)

	require.Equal(t, KindLabel, allRows[1].Kind)
	require.NotEmpty(t, allRows[1].LabelValue)
}

// TestBuilder_SortOrder verifies the sort order: bloom entries before label
// entries, blooms by [ColumnName, MinTimestamp, ...] and labels by
// [ColumnName, LabelValue, MinTimestamp, ...].
func TestBuilder_SortOrder(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)

	// Prepare and add bloom entries.
	b.PrepareBloomColumn("", 0, "col_a", 10, 0)
	_ = b.ObserveBloomPosting(BloomObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col_a", Value: "v", StreamID: 0, Timestamp: time.Unix(0, 50), UncompressedSize: 0})

	b.PrepareBloomColumn("", 0, "col_b", 10, 0)
	_ = b.ObserveBloomPosting(BloomObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col_b", Value: "v", StreamID: 0, Timestamp: time.Unix(0, 10), UncompressedSize: 0})

	// Label entries.
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col_a", LabelValue: "beta", StreamID: 0, Time: At(time.Unix(0, 200)), UncompressedSize: 0})
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col_a", LabelValue: "alpha", StreamID: 0, Time: At(time.Unix(0, 100)), UncompressedSize: 0})
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col_a", LabelValue: "alpha", StreamID: 0, Time: At(time.Unix(0, 50)), UncompressedSize: 0})

	sections := flushAndOpenSections(t, b)
	require.Greater(t, len(sections), 0)

	allRows := readAllRows(t, sections...)
	require.Len(t, allRows, 4) // 2 bloom + 2 label (alpha aggregated into 1, beta into 1)

	// Expected order:
	// 0: KindBloom, col_a
	// 1: KindBloom, col_b
	// 2: KindLabel, col_a, alpha (aggregated)
	// 3: KindLabel, col_a, beta (aggregated)
	require.Equal(t, KindBloom, allRows[0].Kind)
	require.Equal(t, "col_a", allRows[0].ColumnName)
	require.Empty(t, allRows[0].LabelValue)

	require.Equal(t, KindBloom, allRows[1].Kind)
	require.Equal(t, "col_b", allRows[1].ColumnName)
	require.Empty(t, allRows[1].LabelValue)

	require.Equal(t, KindLabel, allRows[2].Kind)
	require.Equal(t, "col_a", allRows[2].ColumnName)
	require.Equal(t, "alpha", allRows[2].LabelValue)

	require.Equal(t, KindLabel, allRows[3].Kind)
	require.Equal(t, "col_a", allRows[3].ColumnName)
	require.Equal(t, "beta", allRows[3].LabelValue)
}

// TestBuilder_SortOrder_TimeBeforeObject verifies that within a
// (ColumnName, LabelValue) run, MinTimestamp outranks ObjectPath: the row with
// the earlier MinTimestamp sorts first even when its ObjectPath is larger.
func TestBuilder_SortOrder_TimeBeforeObject(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)

	// Same column_name=env, label_value=prod. The earlier-timestamp row has the
	// LARGER ObjectPath, so only MinTimestamp-before-ObjectPath yields this order.
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 1, Time: At(timeUnix(0, 300)), UncompressedSize: 0})
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/z", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 2, Time: At(timeUnix(0, 100)), UncompressedSize: 0})

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)
	rows := readAllRows(t, sections[0])
	require.Len(t, rows, 2)

	// Earlier MinTimestamp (100, on "/z") sorts before later MinTimestamp (300,
	// on "/a"), even though "/z" > "/a".
	require.Equal(t, "/z", rows[0].ObjectPath)
	require.Equal(t, "/a", rows[1].ObjectPath)
}

// TestBuilder_NullableHandling verifies nullable column correctness.
func TestBuilder_NullableHandling(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)

	ts := time.Unix(0, 0).UTC()

	b.PrepareBloomColumn("", 0, "col", 10, 0)
	_ = b.ObserveBloomPosting(BloomObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col", Value: "val", StreamID: 0, Timestamp: ts, UncompressedSize: 0})

	b.ObserveLabelPosting(LabelObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col", LabelValue: "val", StreamID: 0, Time: At(ts), UncompressedSize: 0})

	// Blooms are encoded before labels, so sections[0] holds the bloom row and
	// sections[1] the label row.
	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 2)

	// A row decodes a null as the zero value, so check the nulls in the batches.
	bloom := readBatch(t, sections[0])
	require.Equal(t, 1, bloom.Rows)
	require.Equal(t, int64(KindBloom), bloom.Kind.Get(0))
	require.True(t, bloom.LabelValue.IsNull(0), "Bloom posting should have a null LabelValue")
	require.False(t, bloom.BloomFilter.IsNull(0), "Bloom posting should have a non-null BloomFilter")

	label := readBatch(t, sections[1])
	require.Equal(t, 1, label.Rows)
	require.Equal(t, int64(KindLabel), label.Kind.Get(0))
	require.False(t, label.LabelValue.IsNull(0), "Label posting should have a non-null LabelValue")
	require.True(t, label.BloomFilter.IsNull(0), "Label posting should have a null BloomFilter")
}

// TestBuilder_BitmapCorrectness verifies that stream ID bitmaps are LSB-encoded correctly.
func TestBuilder_BitmapCorrectness(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)

	ts := time.Unix(0, 0).UTC()
	b.PrepareBloomColumn("", 0, "col", 10, 0)
	// Observe stream IDs 0, 3, 7.
	_ = b.ObserveBloomPosting(BloomObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col", Value: "v", StreamID: 0, Timestamp: ts, UncompressedSize: 0})
	_ = b.ObserveBloomPosting(BloomObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col", Value: "v", StreamID: 3, Timestamp: ts, UncompressedSize: 0})
	_ = b.ObserveBloomPosting(BloomObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col", Value: "v", StreamID: 7, Timestamp: ts, UncompressedSize: 0})

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows := readAllRows(t, sections[0])
	require.Len(t, rows, 1)

	bitmap := rows[0].StreamIDBitmap
	require.NotEmpty(t, bitmap)

	// Verify LSB encoding: bit N at byte N/8, position N%8.
	require.True(t, checkBit(bitmap, 0), "bit 0 should be set")
	require.False(t, checkBit(bitmap, 1), "bit 1 should not be set")
	require.False(t, checkBit(bitmap, 2), "bit 2 should not be set")
	require.True(t, checkBit(bitmap, 3), "bit 3 should be set")
	require.False(t, checkBit(bitmap, 4), "bit 4 should not be set")
	require.False(t, checkBit(bitmap, 5), "bit 5 should not be set")
	require.False(t, checkBit(bitmap, 6), "bit 6 should not be set")
	require.True(t, checkBit(bitmap, 7), "bit 7 should be set")
}

// TestBuilder_BitmapSizes verifies that each bitmap is stored at its natural
// length (trailing zero bytes trimmed), not zero-padded to the section's
// longest bitmap. Readers derive the bit count per row and zero-extend during
// unions, so variable-length storage is correct and avoids padding every row to
// the maximum length.
func TestBuilder_BitmapSizes(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)

	ts := time.Unix(0, 0).UTC()
	// "a": stream ID 0 → 1-byte bitmap
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col", LabelValue: "a", StreamID: 0, Time: At(ts), UncompressedSize: 0})
	// "b": stream ID 23 → 3-byte bitmap
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col", LabelValue: "b", StreamID: 23, Time: At(ts), UncompressedSize: 0})

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows := readAllRows(t, sections[0])
	require.Len(t, rows, 2)

	// Rows are sorted by (kind, column_name, label_value): "a" (stream 0) then
	// "b" (stream 23). Each keeps its natural byte length.
	require.Len(t, rows[0].StreamIDBitmap, 1, "bitmap for stream ID 0 should be 1 byte")
	require.Len(t, rows[1].StreamIDBitmap, 3, "bitmap for stream ID 23 should be 3 bytes")
	require.True(t, checkBit(rows[0].StreamIDBitmap, 0), "bit 0 should be set for stream 0")
	require.True(t, checkBit(rows[1].StreamIDBitmap, 23), "bit 23 should be set for stream 23")
}

// TestBuilder_SectionSplitting verifies that a small targetSectionSize causes
// rows to be split across multiple pages.
func TestBuilder_SectionSplitting(t *testing.T) {
	// Use a very small page size to force splitting across multiple pages.
	b := NewBuilder(nil, 100, 2, 1<<20)

	ts := time.Unix(0, 0).UTC()
	for i := range 6 {
		b.ObserveLabelPosting(LabelObservation{ColumnName: "col", LabelValue: fmt.Sprintf("val%d", i), Time: At(ts)})
	}

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1, "all postings go in one section")

	// Collect all rows.
	rows := readAllRows(t, sections[0])
	require.Len(t, rows, 6)
}

// TestBuilder_AllBloom verifies that a builder with only Bloom postings works.
func TestBuilder_AllBloom(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)

	ts := time.Unix(0, 0).UTC()
	for i := range 3 {
		colName := fmt.Sprintf("col%d", i)
		b.PrepareBloomColumn("", 0, colName, 10, 0)
		_ = b.ObserveBloomPosting(BloomObservation{ObjectPath: "", SectionIndex: 0, ColumnName: colName, Value: "val", StreamID: 0, Timestamp: ts, UncompressedSize: 0})
	}

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	// A row decodes a null as the zero value, so check the nulls in the batch.
	batch := readBatch(t, sections[0])
	require.Equal(t, 3, batch.Rows)
	for i := range batch.Rows {
		require.Equal(t, int64(KindBloom), batch.Kind.Get(i))
		require.True(t, batch.LabelValue.IsNull(i))
		require.False(t, batch.BloomFilter.IsNull(i))
	}
}

// TestBuilder_AllLabel verifies that a builder with only Label postings works.
func TestBuilder_AllLabel(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)

	ts := time.Unix(0, 0).UTC()
	for i := range 3 {
		lv := fmt.Sprintf("val%d", i)
		b.ObserveLabelPosting(LabelObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col", LabelValue: lv, StreamID: 0, Time: At(ts), UncompressedSize: 0})
	}

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	// A row decodes a null as the zero value, so check the nulls in the batch.
	batch := readBatch(t, sections[0])
	require.Equal(t, 3, batch.Rows)
	for i := range batch.Rows {
		require.Equal(t, int64(KindLabel), batch.Kind.Get(i))
		require.False(t, batch.LabelValue.IsNull(i))
		require.True(t, batch.BloomFilter.IsNull(i))
	}
}

// TestBuilder_FlushResetsBuilder verifies that a flush resets the builder.
func TestBuilder_FlushResetsBuilder(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)

	ts := time.Unix(0, 0).UTC()
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col", LabelValue: "v", StreamID: 0, Time: At(ts), UncompressedSize: 0})

	obj, closer := flushToObject(t, b)
	closer.Close()
	require.Len(t, obj.Sections(), 1)

	// After flush, builder should be empty (Reset was called).
	require.Empty(t, b.labels.entries, "builder should have no label entries after flush")
	require.Empty(t, b.blooms.entries, "builder should have no bloom entries after flush")
	require.Zero(t, b.EstimatedSize(), "builder should have zero estimated size after flush")
	minTime, maxTime := b.TimeRange()
	require.True(t, minTime.IsZero(), "TimeRange min must be zero after flush")
	require.True(t, maxTime.IsZero(), "TimeRange max must be zero after flush")
}

// TestBuilder_KindColumnRangeStats verifies the kind column carries range
// statistics so a kind predicate can prune pages whose kind is out of range.
func TestBuilder_KindColumnRangeStats(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)

	ts := time.Unix(0, 1000).UTC()
	b.PrepareBloomColumn("/a", 0, "svc", 16, 0)
	require.NoError(t, b.ObserveBloomPosting(BloomObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "svc", Value: "v", StreamID: 1, Timestamp: ts}))
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "env", LabelValue: "x", StreamID: 2, Time: At(ts)})

	// Blooms are encoded before labels, so each section is single-kind and the
	// kind column's range stats pin that kind (min == max).
	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 2)

	require.Equal(t, int64(KindBloom), kindStat(t, sections[0]), "first section must be the bloom section")
	require.Equal(t, int64(KindLabel), kindStat(t, sections[1]), "second section must be the label section")
}

// decodeInt64Stat decodes a serialized INT64 stat value (page/column MinValue or
// MaxValue) to its int64.
func decodeInt64Stat(t *testing.T, b []byte) int64 {
	t.Helper()
	var v dataset.Value
	require.NoError(t, v.UnmarshalBinary(b))
	return v.Int64()
}

// kindStat returns the kind column's range-stat value for a single-kind
// section, asserting the column carries stats and that min == max.
func kindStat(t *testing.T, sec *Section) int64 {
	t.Helper()

	var kindCol *Column
	for _, c := range sec.Columns() {
		if c.Type == ColumnTypeKind {
			kindCol = c
			break
		}
	}
	require.NotNil(t, kindCol, "kind column must be present")

	stats := kindCol.inner.Metadata().Statistics
	require.NotNil(t, stats, "kind column must carry statistics")
	require.NotNil(t, stats.MinValue, "kind column must carry a min value")
	require.NotNil(t, stats.MaxValue, "kind column must carry a max value")

	minKind := decodeInt64Stat(t, stats.MinValue)
	maxKind := decodeInt64Stat(t, stats.MaxValue)
	require.Equal(t, minKind, maxKind, "kind min and max must be equal within a section")
	return minKind
}

// TestBuilder_KindColumnPruning verifies that a kind == X equality predicate
// prunes the section whose kind is not X: reading the bloom section with a
// label predicate (and vice versa) returns no rows, while the matching
// predicate returns all rows.
func TestBuilder_KindColumnPruning(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)

	ts := time.Unix(0, 1000).UTC()
	b.PrepareBloomColumn("/a", 0, "svc", 16, 0)
	require.NoError(t, b.ObserveBloomPosting(BloomObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "svc", Value: "v", StreamID: 1, Timestamp: ts}))
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "env", LabelValue: "x", StreamID: 2, Time: At(ts)})

	// Blooms are encoded before labels: sections[0] is the bloom section,
	// sections[1] is the label section.
	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 2)

	bloomSection, labelSection := sections[0], sections[1]

	// Matching kind: the row is returned and no page is pruned.
	rows, pruned := readWithKindPredicate(t, bloomSection, int64(KindBloom))
	require.Equal(t, 1, rows, "bloom section with kind=bloom must return the row")
	require.Equal(t, int64(0), pruned, "matching kind must not prune the page")

	rows, pruned = readWithKindPredicate(t, labelSection, int64(KindLabel))
	require.Equal(t, 1, rows, "label section with kind=label must return the row")
	require.Equal(t, int64(0), pruned, "matching kind must not prune the page")

	// Non-matching kind: the kind column's range stats let the reader skip the
	// page entirely, so a page is pruned and no row is read.
	rows, pruned = readWithKindPredicate(t, bloomSection, int64(KindLabel))
	require.Equal(t, 0, rows, "bloom section with kind=label must return no rows")
	require.Equal(t, int64(1), pruned, "non-matching kind must prune the page via range stats")

	rows, pruned = readWithKindPredicate(t, labelSection, int64(KindBloom))
	require.Equal(t, 0, rows, "label section with kind=bloom must return no rows")
	require.Equal(t, int64(1), pruned, "non-matching kind must prune the page via range stats")
}

// readWithKindPredicate reads sec through a dataset RowReader with a
// kind == wantKind equality predicate and returns the number of rows returned
// and the number of pages pruned by page-level range statistics.
func readWithKindPredicate(t *testing.T, sec *Section, wantKind int64) (rows int, pagesPruned int64) {
	t.Helper()

	dset, err := columnar.MakeDataset(sec.inner, columnarColumns(sec))
	require.NoError(t, err)

	cols := dset.Columns()
	var kindCol dataset.Column
	for i, c := range sec.Columns() {
		if c.Type == ColumnTypeKind {
			kindCol = cols[i]
			break
		}
	}
	require.NotNil(t, kindCol, "kind column not found in dataset")

	reader := dataset.NewRowReader(dataset.RowReaderOptions{
		Dataset:          dset,
		ProjectedColumns: cols,
		Predicates: []dataset.Predicate{
			dataset.EqualPredicate{Column: kindCol, Value: dataset.Int64Value(wantKind)},
		},
	})

	ctx, _ := xcap.NewCapture(context.Background(), nil)
	ctx, region := xcap.StartRegion(ctx, "postings.kindPruning")
	defer region.End()

	require.NoError(t, reader.Open(ctx))

	buf := make([]dataset.Row, 16)
	for {
		n, err := reader.Read(ctx, buf)
		rows += n
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}

	for _, obs := range region.Observations() {
		if obs.Statistic.Name() == dataobj.StatDatasetPagesPruned.Name() {
			pagesPruned = obs.Value.(int64)
		}
	}
	return rows, pagesPruned
}

// columnarColumns returns the inner columnar columns for a postings section,
// in the same order as sec.Columns().
func columnarColumns(sec *Section) []*columnar.Column {
	cols := sec.Columns()
	inner := make([]*columnar.Column, len(cols))
	for i, c := range cols {
		inner[i] = c.inner
	}
	return inner
}

// TestBuilder_Type verifies that the section type is correct.
func TestBuilder_Type(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)
	require.Equal(t, sectionType, b.Type())
}

// TestCheckSection verifies CheckSection correctness.
func TestCheckSection(t *testing.T) {
	t.Run("returns true for postings section type", func(t *testing.T) {
		sec := &dataobj.Section{Type: sectionType}
		require.True(t, CheckSection(sec))
	})

	t.Run("returns false for non-postings section type", func(t *testing.T) {
		sec := &dataobj.Section{Type: dataobj.SectionType{
			Namespace: "github.com/grafana/loki",
			Kind:      "stats",
			Version:   1,
		}}
		require.False(t, CheckSection(sec))
	})
}

func TestReader_Open(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)
	ts := time.Unix(0, 0)
	for i := range 5 {
		b.ObserveLabelPosting(LabelObservation{ColumnName: "col", LabelValue: fmt.Sprintf("val%d", i), Time: At(ts), ShardBuckets: 8, ShardBucket: uint32(i)})
	}

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)
	section := sections[0]

	t.Run("returns only the matching rows and the projected columns when a predicate reads a column outside Columns", func(t *testing.T) {
		r := NewReader(ReaderOptions{
			Columns: []*Column{sectionColumn(section, ColumnTypeLabelValue)},
			Predicates: []Predicate{
				EqualPredicate{Column: sectionColumn(section, ColumnTypeMaxShardBucket), Value: scalar.NewInt64Scalar(3)},
			},
		})
		require.NoError(t, r.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		alloc := memory.NewAllocator(nil)
		t.Cleanup(alloc.Free)

		batch, err := r.ReadRecordBatch(t.Context(), alloc, 10)
		require.NoError(t, err)
		require.Equal(t, 1, batch.Rows)
		require.Equal(t, "val3", string(batch.LabelValue.Get(0)))
		require.Nil(t, batch.MaxShardBucket, "the batch holds only the projected columns")
	})

	t.Run("leaves the columns of the section unchanged when Columns is a prefix of them and a predicate reads another column", func(t *testing.T) {
		before := slices.Clone(section.Columns())

		r := NewReader(ReaderOptions{
			Columns: section.Columns()[:1],
			Predicates: []Predicate{
				EqualPredicate{Column: sectionColumn(section, ColumnTypeMaxShardBucket), Value: scalar.NewInt64Scalar(3)},
			},
		})
		require.NoError(t, r.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		require.True(t, slices.Equal(before, section.Columns()), "Open changed the columns of the section")
	})
}

// TestBuilder_ObserveLabelPosting verifies that multiple stream IDs for the
// same key are aggregated into a single posting entry.
func TestBuilder_ObserveLabelPosting(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)

	minTs := time.Unix(0, 100).UTC()
	midTs := time.Unix(0, 200).UTC()
	maxTs := time.Unix(0, 300).UTC()

	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/obj", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 1, Time: At(minTs), UncompressedSize: 100, ShardBucket: 1})
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/obj", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 5, Time: At(midTs), UncompressedSize: 200, ShardBucket: 5})
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/obj", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 10, Time: At(maxTs), UncompressedSize: 300, ShardBucket: 3})

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows := readAllRows(t, sections[0])
	require.Len(t, rows, 1, "three observations for same key should aggregate to one posting")

	// Verify stream IDs 1, 5, 10 are set. The bit checks cover the bitmap, so the
	// comparison below leaves it out.
	bitmap := rows[0].StreamIDBitmap
	require.True(t, checkBit(bitmap, 1))
	require.True(t, checkBit(bitmap, 5))
	require.True(t, checkBit(bitmap, 10))
	require.False(t, checkBit(bitmap, 0))
	require.False(t, checkBit(bitmap, 2))
	rows[0].StreamIDBitmap = nil

	require.Equal(t, Row{
		Kind:             KindLabel,
		ObjectPath:       "/obj",
		ColumnName:       "env",
		LabelValue:       "prod",
		UncompressedSize: 600,
		MinTimestamp:     minTs.UnixNano(),
		MaxTimestamp:     maxTs.UnixNano(),
		MinShardBucket:   1,
		MaxShardBucket:   5,
	}, rows[0])
}

// TestBuilder_ObserveBloomPosting verifies bloom observation: prepare, observe, check membership and bitmap.
func TestBuilder_ObserveBloomPosting(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)

	ts := time.Unix(0, 100)
	b.PrepareBloomColumn("/obj", 0, "service_name", 100, 0)

	values := []string{"alpha", "beta", "gamma"}
	for i, v := range values {
		err := b.ObserveBloomPosting(BloomObservation{ObjectPath: "/obj", SectionIndex: 0, ColumnName: "service_name", Value: v, StreamID: int64(i), Timestamp: ts, UncompressedSize: 10})
		require.NoError(t, err)
	}

	// Check bloom bytes contain values.
	bloomBytes, err := b.BloomBytes("/obj", 0, "service_name")
	require.NoError(t, err)
	require.NotEmpty(t, bloomBytes)

	// Verify bloom membership via the aggregator's bloom filter.
	entry := b.blooms.entries[bloomPostingKey{"/obj", 0, "service_name"}]
	require.NotNil(t, entry)
	for _, v := range values {
		require.True(t, entry.BloomFilter().Test([]byte(v)), "bloom filter should contain %q", v)
	}
	require.False(t, entry.BloomFilter().Test([]byte("delta")), "bloom filter should not contain 'delta'")

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows := readAllRows(t, sections[0])
	require.Len(t, rows, 1)

	row := rows[0]
	require.Equal(t, KindBloom, row.Kind)
	require.Equal(t, "service_name", row.ColumnName)
	require.NotEmpty(t, row.BloomFilter)

	// Verify stream IDs 0, 1, 2 are set in the bitmap.
	require.True(t, checkBit(row.StreamIDBitmap, 0))
	require.True(t, checkBit(row.StreamIDBitmap, 1))
	require.True(t, checkBit(row.StreamIDBitmap, 2))
	require.False(t, checkBit(row.StreamIDBitmap, 3))
}

// TestBuilder_MixedObservations verifies bloom and label observations produce
// the correct sort order (bloom before label).
func TestBuilder_MixedObservations(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)

	ts := time.Unix(0, 100).UTC()

	// Add label first (out of order relative to expected output).
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/obj", SectionIndex: 0, ColumnName: "col_b", LabelValue: "v", StreamID: 0, Time: At(ts), UncompressedSize: 0})

	// Add bloom second.
	b.PrepareBloomColumn("/obj", 0, "col_a", 10, 0)
	_ = b.ObserveBloomPosting(BloomObservation{ObjectPath: "/obj", SectionIndex: 0, ColumnName: "col_a", Value: "v", StreamID: 0, Timestamp: ts, UncompressedSize: 0})

	sections := flushAndOpenSections(t, b)
	require.Greater(t, len(sections), 0)

	allRows := readAllRows(t, sections...)
	require.Len(t, allRows, 2)

	// Bloom should be first regardless of observation order.
	require.Equal(t, KindBloom, allRows[0].Kind, "bloom should sort before label")
	require.Equal(t, KindLabel, allRows[1].Kind)
}

// TestBuilder_ObserveBloomUnprepared verifies that observing an unprepared bloom
// column returns an error.
func TestBuilder_ObserveBloomUnprepared(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)

	ts := time.Unix(0, 0)
	err := b.ObserveBloomPosting(BloomObservation{ObjectPath: "/obj", SectionIndex: 0, ColumnName: "unprepared_col", Value: "val", StreamID: 0, Timestamp: ts, UncompressedSize: 0})
	require.Error(t, err, "observing unprepared bloom column should return an error")
	require.Contains(t, err.Error(), "bloom column not prepared")
}

// TestBuilder_MultipleObjectContexts verifies that observations with different
// (objectPath, sectionIndex) for the same (columnName, labelValue) produce
// distinct posting rows.
func TestBuilder_MultipleObjectContexts(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)

	ts := time.Unix(0, 0).UTC()

	// Same column/label, different object paths.
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/obj1", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 0, Time: At(ts), UncompressedSize: 100})
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/obj2", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 1, Time: At(ts), UncompressedSize: 200})

	// Same column/label, same path but different section index.
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/obj1", SectionIndex: 1, ColumnName: "env", LabelValue: "prod", StreamID: 2, Time: At(ts), UncompressedSize: 300})

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows := readAllRows(t, sections[0])
	require.Len(t, rows, 3, "different (objectPath, sectionIndex) should produce distinct postings")

	// Collect all (objectPath, sectionIndex) pairs from results.
	type key struct {
		path  string
		secID int64
	}
	seen := make(map[key]bool)
	for _, row := range rows {
		k := key{row.ObjectPath, row.SectionIndex}
		require.False(t, seen[k], "duplicate (objectPath, sectionIndex) found")
		seen[k] = true
	}

	require.True(t, seen[key{"/obj1", 0}])
	require.True(t, seen[key{"/obj2", 0}])
	require.True(t, seen[key{"/obj1", 1}])

	// Each posting should only have the stream IDs for that context.
	for _, row := range rows {
		switch {
		case row.ObjectPath == "/obj1" && row.SectionIndex == 0:
			require.True(t, checkBit(row.StreamIDBitmap, 0), "obj1/0 should have stream 0")
			require.Equal(t, int64(100), row.UncompressedSize)
		case row.ObjectPath == "/obj2" && row.SectionIndex == 0:
			require.True(t, checkBit(row.StreamIDBitmap, 1), "obj2/0 should have stream 1")
			require.Equal(t, int64(200), row.UncompressedSize)
		case row.ObjectPath == "/obj1" && row.SectionIndex == 1:
			require.True(t, checkBit(row.StreamIDBitmap, 2), "obj1/1 should have stream 2")
			require.Equal(t, int64(300), row.UncompressedSize)
		}
	}
}

// TestBuilder_BloomBytes verifies that BloomBytes returns valid marshaled bloom
// data matching what was observed.
func TestBuilder_BloomBytes(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1<<20)

	ts := time.Unix(0, 0)
	b.PrepareBloomColumn("/obj", 0, "col", 50, 0)

	values := []string{"foo", "bar", "baz"}
	for _, v := range values {
		err := b.ObserveBloomPosting(BloomObservation{ObjectPath: "/obj", SectionIndex: 0, ColumnName: "col", Value: v, StreamID: 0, Timestamp: ts, UncompressedSize: 0})
		require.NoError(t, err)
	}

	bloomBytes, err := b.BloomBytes("/obj", 0, "col")
	require.NoError(t, err)
	require.NotEmpty(t, bloomBytes)

	// Verify the bloom bytes are valid by unmarshaling them.
	// The values we added should be present after round-trip.
	entry := b.blooms.entries[bloomPostingKey{"/obj", 0, "col"}]
	require.NotNil(t, entry)

	for _, v := range values {
		require.True(t, entry.BloomFilter().Test([]byte(v)), "expected %q to be in bloom filter", v)
	}
	require.False(t, entry.BloomFilter().Test([]byte("unknown")), "unexpected value in bloom filter")

	// Error for non-existent column.
	_, err = b.BloomBytes("/obj", 0, "nonexistent")
	require.Error(t, err)
}

// mustBuildBloomBytes constructs a bloom filter section, observes one value,
// and returns the marshaled filter bytes. Used by tests that need realistic
// bloom-filter bytes to feed into AppendBloomEntry.
func mustBuildBloomBytes(t *testing.T, objectPath string, sectionIndex int64, columnName, value string, ts time.Time) []byte {
	t.Helper()
	tempBuilder := NewBuilder(nil, 0, 0, 1<<20)
	tempBuilder.PrepareBloomColumn(objectPath, sectionIndex, columnName, 100, 0)
	err := tempBuilder.ObserveBloomPosting(BloomObservation{
		ObjectPath:       objectPath,
		SectionIndex:     sectionIndex,
		ColumnName:       columnName,
		Value:            value,
		StreamID:         0,
		Timestamp:        ts,
		UncompressedSize: 0,
	})
	require.NoError(t, err)
	bytes, err := tempBuilder.BloomBytes(objectPath, sectionIndex, columnName)
	require.NoError(t, err)
	return bytes
}

// flushToObject flushes the builder into a dataobj.Object using dataobj.Builder.
func flushToObject(t *testing.T, b *Builder) (*dataobj.Object, io.Closer) {
	t.Helper()
	objBuilder := dataobj.NewBuilder(nil)
	err := objBuilder.Append(b)
	require.NoError(t, err)
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	return obj, closer
}

// flushAndOpenSections flushes the builder and opens all resulting postings sections.
func flushAndOpenSections(t *testing.T, b *Builder) []*Section {
	t.Helper()
	obj, closer := flushToObject(t, b)
	t.Cleanup(func() { _ = closer.Close() })

	var sections []*Section
	for _, s := range obj.Sections() {
		if !CheckSection(s) {
			continue
		}
		sec, err := Open(context.Background(), s)
		require.NoError(t, err)
		sections = append(sections, sec)
	}
	return sections
}

// TestBuilder_SplitsIntoMultipleSections verifies Flush emits multiple sections
// when accumulated entries exceed targetSectionSize.
func TestBuilder_SplitsIntoMultipleSections(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 64)
	ts := time.Unix(0, 0).UTC()
	for _, lv := range []string{"a", "b", "c", "d", "e"} {
		b.ObserveLabelPosting(LabelObservation{ObjectPath: "/o", SectionIndex: 0, ColumnName: "env", LabelValue: lv, StreamID: 0, Time: At(ts), UncompressedSize: 0})
	}

	sections := flushAndOpenSections(t, b)
	require.Greater(t, len(sections), 1, "expected multiple postings sections")

	// Collect label values per section to verify deterministic splitting.
	sectionValues := [][]string{}
	for _, sec := range sections {
		var values []string
		for _, row := range readAllRows(t, sec) {
			values = append(values, row.LabelValue)
		}
		sectionValues = append(sectionValues, values)
	}

	expectedSections := [][]string{
		{"a", "b"},
		{"c", "d"},
		{"e"},
	}
	require.Equal(t, expectedSections, sectionValues, "section splits should be deterministic")
}

// TestBuilder_SplitsBloomAndLabelIntoSeparateSections verifies that with a small
// targetSectionSize, bloom and label postings are split into multiple sections
// and no section mixes bloom rows with label rows.
func TestBuilder_SplitsBloomAndLabelIntoSeparateSections(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 64)
	ts := time.Unix(0, 0).UTC()

	for i := range 4 {
		col := fmt.Sprintf("bcol%d", i)
		b.PrepareBloomColumn("/o", 0, col, 10, 0)
		require.NoError(t, b.ObserveBloomPosting(BloomObservation{ObjectPath: "/o", SectionIndex: 0, ColumnName: col, Value: "v", StreamID: 0, Timestamp: ts, UncompressedSize: 0}))
	}
	for _, lv := range []string{"a", "b", "c", "d"} {
		b.ObserveLabelPosting(LabelObservation{ObjectPath: "/o", SectionIndex: 0, ColumnName: "env", LabelValue: lv, StreamID: 0, Time: At(ts), UncompressedSize: 0})
	}

	sections := flushAndOpenSections(t, b)
	require.Greater(t, len(sections), 1, "expected multiple sections")

	// Verify each section contains only one kind and collect which values are in which section.
	type sectionInfo struct {
		kind   PostingKind
		values []string
	}
	sectionInfos := []sectionInfo{}

	for si, sec := range sections {
		rows := readAllRows(t, sec)
		require.NotEmpty(t, rows)

		kinds := map[PostingKind]struct{}{}
		var values []string
		for _, row := range rows {
			kinds[row.Kind] = struct{}{}

			// Collect bloom column names or label values
			if row.Kind == KindBloom {
				values = append(values, row.ColumnName)
			} else {
				values = append(values, row.LabelValue)
			}
		}
		require.Len(t, kinds, 1, "section %d mixes bloom and label rows", si)

		// Get the kind
		var kind PostingKind
		for k := range kinds {
			kind = k
		}
		sectionInfos = append(sectionInfos, sectionInfo{kind: kind, values: values})
	}

	expectedSections := []sectionInfo{
		{kind: KindBloom, values: []string{"bcol0"}},
		{kind: KindBloom, values: []string{"bcol1"}},
		{kind: KindBloom, values: []string{"bcol2"}},
		{kind: KindBloom, values: []string{"bcol3"}},
		{kind: KindLabel, values: []string{"a", "b"}},
		{kind: KindLabel, values: []string{"c", "d"}},
	}
	require.Equal(t, expectedSections, sectionInfos, "section splits should be deterministic")
}

// TestBuilder_LargeEntryExceedsTarget verifies that a single entry larger than
// targetSectionSize still becomes its own section (can't split below one entry).
func TestBuilder_LargeEntryExceedsTarget(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 50) // Small target
	ts := time.Unix(0, 0).UTC()

	// Create a label entry with a long ObjectPath that exceeds the target size.
	longPath := "/" + string(make([]byte, 200)) // Much larger than target of 50
	for i := range len(longPath) {
		longPath = longPath[:i] + "x" + longPath[i+1:]
	}

	b.ObserveLabelPosting(LabelObservation{
		ObjectPath:   longPath,
		SectionIndex: 0,
		ColumnName:   "env",
		LabelValue:   "prod",
		StreamID:     0,
		Time:         At(ts),
	})

	sections := flushAndOpenSections(t, b)
	// Even though the entry is larger than target, it still creates exactly 1 section.
	require.Len(t, sections, 1, "single large entry should become its own section")

	rows := readAllRows(t, sections[0])
	require.Len(t, rows, 1, "section should contain the single entry")
	require.Equal(t, longPath, rows[0].ObjectPath)
}

// TestBuilder_SplitPreservesAllEntries verifies that when entries are split
// across multiple sections, all entries are preserved with correct values.
func TestBuilder_SplitPreservesAllEntries(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 64)
	ts := time.Unix(0, 0).UTC()

	// Add multiple entries with varying ObjectPath lengths to create specific splits.
	expectedValues := []string{"a", "b", "c", "d", "e", "f"}
	for _, val := range expectedValues {
		b.ObserveLabelPosting(LabelObservation{
			ObjectPath:   "/o",
			SectionIndex: 0,
			ColumnName:   "env",
			LabelValue:   val,
			StreamID:     0,
			Time:         At(ts),
		})
	}

	sections := flushAndOpenSections(t, b)
	require.Greater(t, len(sections), 1, "should split into multiple sections")

	// Collect all label values across all sections.
	var allValues []string
	for _, row := range readAllRows(t, sections...) {
		allValues = append(allValues, row.LabelValue)
	}

	// All values should be preserved in sorted order.
	require.Equal(t, expectedValues, allValues, "all entries must be preserved across sections")
}

func TestBuilder_TimeRange(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1024)

	gotMin, gotMax := b.TimeRange()
	require.True(t, gotMin.IsZero())
	require.True(t, gotMax.IsZero())
	require.NotEqual(t, time.Unix(0, 0).UTC(), gotMin, "empty min must not be the Unix epoch")

	base := time.Unix(5000, 0).UTC()

	// Label-only.
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x", StreamID: 1, Time: At(base)})
	gotMin, gotMax = b.TimeRange()
	require.Equal(t, base, gotMin)
	require.Equal(t, base, gotMax)

	// Bloom observation extends the range on both ends.
	b.PrepareBloomColumn("/a", 0, "svc", 16, 0)
	require.NoError(t, b.ObserveBloomPosting(BloomObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "svc", Value: "v", StreamID: 2, Timestamp: base.Add(-time.Hour)}))
	require.NoError(t, b.ObserveBloomPosting(BloomObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "svc", Value: "w", StreamID: 3, Timestamp: base.Add(time.Hour)}))

	gotMin, gotMax = b.TimeRange()
	require.Equal(t, base.Add(-time.Hour), gotMin)
	require.Equal(t, base.Add(time.Hour), gotMax)

	// Reset clears the range back to empty (verifies Builder.Reset propagates
	// to both aggregators).
	b.Reset()
	gotMin, gotMax = b.TimeRange()
	require.True(t, gotMin.IsZero(), "after Reset min must be zero")
	require.True(t, gotMax.IsZero(), "after Reset max must be zero")
}

func TestBuilder_TimeRange_BloomOnly(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1024)
	base := time.Unix(6000, 0).UTC()

	b.PrepareBloomColumn("/a", 0, "svc", 16, 0)
	require.NoError(t, b.ObserveBloomPosting(BloomObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "svc", Value: "v", StreamID: 1, Timestamp: base}))

	gotMin, gotMax := b.TimeRange()
	require.Equal(t, base, gotMin)
	require.Equal(t, base, gotMax)
}

func TestBuilder_TimeRange_PreparedBloomNoObservation(t *testing.T) {
	b := NewBuilder(nil, 0, 0, 1024)
	base := time.Unix(7000, 0).UTC()

	// Label observation sets the range.
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x", StreamID: 1, Time: At(base)})
	// Prepared-but-unobserved bloom column must not widen the range.
	b.PrepareBloomColumn("/a", 0, "svc", 16, 0)

	gotMin, gotMax := b.TimeRange()
	require.Equal(t, base, gotMin)
	require.Equal(t, base, gotMax)
}

// readAllRows reads every column of the sections with a [RowReader], and
// returns the rows in the order of the sections.
func readAllRows(t *testing.T, sections ...*Section) []Row {
	t.Helper()

	var rows []Row
	for _, sec := range sections {
		r := NewReader(ReaderOptions{Columns: sec.Columns()})
		require.NoError(t, r.Open(t.Context()))

		rr := NewRowReader(t.Context(), r)
		for rr.Next() {
			rows = append(rows, rr.At())
		}
		require.NoError(t, rr.Err())
		require.NoError(t, rr.Close())
	}
	return rows
}

// readBatch returns all rows of sec as one batch with every column. It fails the
// test if sec holds more than 1024 rows. The batch stays valid until the test
// ends. A [Row] decodes a null as the zero value, so a test that checks a null
// needs the batch.
func readBatch(t *testing.T, sec *Section) *RecordBatch {
	t.Helper()

	r := NewReader(ReaderOptions{Columns: sec.Columns()})
	require.NoError(t, r.Open(t.Context()))
	t.Cleanup(func() { require.NoError(t, r.Close()) })

	alloc := memory.NewAllocator(nil)
	t.Cleanup(alloc.Free)

	batch, err := r.ReadRecordBatch(t.Context(), alloc, 1024)
	require.NoError(t, err)

	_, err = r.ReadRecordBatch(t.Context(), alloc, 1)
	require.Equal(t, io.EOF, err, "sec holds more than 1024 rows")
	return batch
}
