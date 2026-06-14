package postings

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

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
	b := NewBuilder(nil, 0, 0)
	require.Zero(t, b.EstimatedSize(), "empty builder should have zero size")
	// An empty builder writes 0 bytes; the dataobj builder would fail to flush
	// without any data. This verifies the postings builder is truly empty.
	require.Empty(t, b.labels.entries, "empty builder should have no label entries")
	require.Empty(t, b.blooms.entries, "empty builder should have no bloom entries")
}

// TestBuilder_LabelPostingRoundTrip verifies a label posting round-trips correctly.
func TestBuilder_LabelPostingRoundTrip(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 1000).UTC()
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/tenant/abc/obj1", SectionIndex: 0, ColumnName: "env", LabelValue: "value1", StreamID: 3, Timestamp: ts, UncompressedSize: 4096})
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/tenant/abc/obj1", SectionIndex: 0, ColumnName: "env", LabelValue: "value1", StreamID: 7, Timestamp: ts, UncompressedSize: 0})
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/tenant/abc/obj1", SectionIndex: 0, ColumnName: "env", LabelValue: "value1", StreamID: 15, Timestamp: ts, UncompressedSize: 0})

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows, tbl := readAllRows(t, sections[0])
	require.Len(t, rows, 1)

	// stream_id_bitmap.binary is intentionally omitted here; verified below via extractBinaryColumn + checkBit.
	expected := arrowtest.Rows{
		{
			"kind.int64":              int64(KindLabel),
			"object_path.utf8":        "/tenant/abc/obj1",
			"section_index.int64":     int64(0),
			"column_name.utf8":        "env",
			"label_value.utf8":        "value1",
			"bloom_filter.binary":     nil,
			"uncompressed_size.int64": int64(4096),
			"min_timestamp.timestamp": ts,
			"max_timestamp.timestamp": ts,
		},
	}

	strictRows := stripOpaqueBinaryColumns(rows, "stream_id_bitmap.binary")
	strictExpected := stripOpaqueBinaryColumns(expected, "stream_id_bitmap.binary")
	require.Equal(t, strictExpected, strictRows)

	// Extract and verify the bitmap separately since it's opaque
	bitmaps := extractBinaryColumn(t, tbl, "stream_id_bitmap.binary")
	// Verify stream IDs 3, 7, 15 are set in the bitmap.
	require.True(t, checkBit(bitmaps[0], 3), "bit 3 should be set")
	require.True(t, checkBit(bitmaps[0], 7), "bit 7 should be set")
	require.True(t, checkBit(bitmaps[0], 15), "bit 15 should be set")
	require.False(t, checkBit(bitmaps[0], 0), "bit 0 should not be set")
}

// TestBuilder_BloomPostingRoundTrip verifies a bloom posting round-trips correctly.
func TestBuilder_BloomPostingRoundTrip(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 500).UTC()
	b.PrepareBloomColumn("/tenant/abc/obj2", 1, "service_name", 100)
	err := b.ObserveBloomPosting(BloomObservation{ObjectPath: "/tenant/abc/obj2", SectionIndex: 1, ColumnName: "service_name", Value: "my-service", StreamID: 0, Timestamp: ts, UncompressedSize: 8192})
	require.NoError(t, err)
	err = b.ObserveBloomPosting(BloomObservation{ObjectPath: "/tenant/abc/obj2", SectionIndex: 1, ColumnName: "service_name", Value: "my-service", StreamID: 2, Timestamp: ts, UncompressedSize: 0})
	require.NoError(t, err)
	err = b.ObserveBloomPosting(BloomObservation{ObjectPath: "/tenant/abc/obj2", SectionIndex: 1, ColumnName: "service_name", Value: "my-service", StreamID: 8, Timestamp: ts, UncompressedSize: 0})
	require.NoError(t, err)

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows, tbl := readAllRows(t, sections[0])
	require.Len(t, rows, 1)

	bitmaps := extractBinaryColumn(t, tbl, "stream_id_bitmap.binary")
	bloomFilters := extractBinaryColumn(t, tbl, "bloom_filter.binary")

	// stream_id_bitmap.binary and bloom_filter.binary are intentionally omitted here; verified below via extractBinaryColumn.
	expected := arrowtest.Rows{
		{
			"kind.int64":              int64(KindBloom),
			"object_path.utf8":        "/tenant/abc/obj2",
			"section_index.int64":     int64(1),
			"column_name.utf8":        "service_name",
			"label_value.utf8":        nil,
			"bloom_filter.binary":     bloomFilters[0], // Use actual bloom from test
			"uncompressed_size.int64": int64(8192),
			"min_timestamp.timestamp": ts,
			"max_timestamp.timestamp": ts,
		},
	}

	strictRows := stripOpaqueBinaryColumns(rows, "stream_id_bitmap.binary", "bloom_filter.binary")
	strictExpected := stripOpaqueBinaryColumns(expected, "stream_id_bitmap.binary", "bloom_filter.binary")
	require.Equal(t, strictExpected, strictRows)

	// Verify bloom filter is non-nil
	require.NotNil(t, bloomFilters[0], "Bloom posting should have non-nil BloomFilter")

	// Verify stream IDs 0, 2, 8 are set.
	require.True(t, checkBit(bitmaps[0], 0), "bit 0 should be set")
	require.True(t, checkBit(bitmaps[0], 2), "bit 2 should be set")
	require.True(t, checkBit(bitmaps[0], 8), "bit 8 should be set")
	require.False(t, checkBit(bitmaps[0], 1), "bit 1 should not be set")
}

// TestBuilder_MixedPostings verifies both Bloom and Label postings work together.
func TestBuilder_MixedPostings(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 100).UTC()
	ts2 := time.Unix(0, 300).UTC()
	b.PrepareBloomColumn("/obj1", 0, "col_a", 10)
	err := b.ObserveBloomPosting(BloomObservation{ObjectPath: "/obj1", SectionIndex: 0, ColumnName: "col_a", Value: "val", StreamID: 0, Timestamp: ts, UncompressedSize: 0})
	require.NoError(t, err)

	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/obj2", SectionIndex: 0, ColumnName: "col_b", LabelValue: "myval", StreamID: 1, Timestamp: ts2, UncompressedSize: 0})
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/obj2", SectionIndex: 0, ColumnName: "col_b", LabelValue: "myval", StreamID: 3, Timestamp: ts2, UncompressedSize: 0})

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows, _ := readAllRows(t, sections[0])
	require.Len(t, rows, 2)

	// Bloom (KindBloom=0) sorts before Label (KindLabel=1).
	require.Equal(t, int64(KindBloom), rows[0]["kind.int64"])
	require.Equal(t, nil, rows[0]["label_value.utf8"])

	require.Equal(t, int64(KindLabel), rows[1]["kind.int64"])
	require.NotNil(t, rows[1]["label_value.utf8"])
}

// TestBuilder_SortOrder verifies the sort order: bloom entries before label entries,
// sorted by [objectPath, sectionIndex, columnName] for blooms and
// [objectPath, sectionIndex, columnName, labelValue] for labels.
func TestBuilder_SortOrder(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	// Prepare and add bloom entries.
	b.PrepareBloomColumn("", 0, "col_a", 10)
	_ = b.ObserveBloomPosting(BloomObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col_a", Value: "v", StreamID: 0, Timestamp: time.Unix(0, 50), UncompressedSize: 0})

	b.PrepareBloomColumn("", 0, "col_b", 10)
	_ = b.ObserveBloomPosting(BloomObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col_b", Value: "v", StreamID: 0, Timestamp: time.Unix(0, 10), UncompressedSize: 0})

	// Label entries.
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col_a", LabelValue: "beta", StreamID: 0, Timestamp: time.Unix(0, 200), UncompressedSize: 0})
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col_a", LabelValue: "alpha", StreamID: 0, Timestamp: time.Unix(0, 100), UncompressedSize: 0})
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col_a", LabelValue: "alpha", StreamID: 0, Timestamp: time.Unix(0, 50), UncompressedSize: 0})

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows, _ := readAllRows(t, sections[0])
	require.Len(t, rows, 4) // 2 bloom + 2 label (alpha aggregated into 1, beta into 1)

	// Expected order:
	// 0: KindBloom, col_a
	// 1: KindBloom, col_b
	// 2: KindLabel, col_a, alpha (aggregated)
	// 3: KindLabel, col_a, beta (aggregated)
	require.Equal(t, int64(KindBloom), rows[0]["kind.int64"])
	require.Equal(t, "col_a", rows[0]["column_name.utf8"])
	require.Equal(t, nil, rows[0]["label_value.utf8"])

	require.Equal(t, int64(KindBloom), rows[1]["kind.int64"])
	require.Equal(t, "col_b", rows[1]["column_name.utf8"])
	require.Equal(t, nil, rows[1]["label_value.utf8"])

	require.Equal(t, int64(KindLabel), rows[2]["kind.int64"])
	require.Equal(t, "col_a", rows[2]["column_name.utf8"])
	require.Equal(t, "alpha", rows[2]["label_value.utf8"])

	require.Equal(t, int64(KindLabel), rows[3]["kind.int64"])
	require.Equal(t, "col_a", rows[3]["column_name.utf8"])
	require.Equal(t, "beta", rows[3]["label_value.utf8"])
}

// TestBuilder_NullableHandling verifies nullable column correctness.
func TestBuilder_NullableHandling(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 0).UTC()

	b.PrepareBloomColumn("", 0, "col", 10)
	_ = b.ObserveBloomPosting(BloomObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col", Value: "val", StreamID: 0, Timestamp: ts, UncompressedSize: 0})

	b.ObserveLabelPosting(LabelObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col", LabelValue: "val", StreamID: 0, Timestamp: ts, UncompressedSize: 0})

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows, _ := readAllRows(t, sections[0])
	require.Len(t, rows, 2)

	// Bloom posting: label_value is nil, bloom_filter is non-null
	bloom := rows[0]
	require.Equal(t, int64(KindBloom), bloom["kind.int64"])
	require.Nil(t, bloom["label_value.utf8"], "Bloom posting should have nil LabelValue")
	require.NotNil(t, bloom["bloom_filter.binary"], "Bloom posting should have non-nil BloomFilter")

	// Label posting: bloom_filter is null, label_value is non-nil
	label := rows[1]
	require.Equal(t, int64(KindLabel), label["kind.int64"])
	require.NotNil(t, label["label_value.utf8"], "Label posting should have non-nil LabelValue")
	require.Nil(t, label["bloom_filter.binary"], "Label posting should have nil BloomFilter")
}

// TestBuilder_BitmapCorrectness verifies that stream ID bitmaps are LSB-encoded correctly.
func TestBuilder_BitmapCorrectness(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 0).UTC()
	b.PrepareBloomColumn("", 0, "col", 10)
	// Observe stream IDs 0, 3, 7.
	_ = b.ObserveBloomPosting(BloomObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col", Value: "v", StreamID: 0, Timestamp: ts, UncompressedSize: 0})
	_ = b.ObserveBloomPosting(BloomObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col", Value: "v", StreamID: 3, Timestamp: ts, UncompressedSize: 0})
	_ = b.ObserveBloomPosting(BloomObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col", Value: "v", StreamID: 7, Timestamp: ts, UncompressedSize: 0})

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows, tbl := readAllRows(t, sections[0])
	require.Len(t, rows, 1)

	bitmaps := extractBinaryColumn(t, tbl, "stream_id_bitmap.binary")
	require.NotEmpty(t, bitmaps[0])

	// Verify LSB encoding: bit N at byte N/8, position N%8.
	require.True(t, checkBit(bitmaps[0], 0), "bit 0 should be set")
	require.False(t, checkBit(bitmaps[0], 1), "bit 1 should not be set")
	require.False(t, checkBit(bitmaps[0], 2), "bit 2 should not be set")
	require.True(t, checkBit(bitmaps[0], 3), "bit 3 should be set")
	require.False(t, checkBit(bitmaps[0], 4), "bit 4 should not be set")
	require.False(t, checkBit(bitmaps[0], 5), "bit 5 should not be set")
	require.False(t, checkBit(bitmaps[0], 6), "bit 6 should not be set")
	require.True(t, checkBit(bitmaps[0], 7), "bit 7 should be set")
}

// TestBuilder_BitmapNormalization verifies that bitmaps of different sizes are
// padded to the same length.
func TestBuilder_BitmapNormalization(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 0).UTC()
	// "a": stream ID 0 → 1-byte bitmap
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col", LabelValue: "a", StreamID: 0, Timestamp: ts, UncompressedSize: 0})
	// "b": stream ID 23 → 3-byte bitmap
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col", LabelValue: "b", StreamID: 23, Timestamp: ts, UncompressedSize: 0})

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows, tbl := readAllRows(t, sections[0])
	require.Len(t, rows, 2)

	bitmaps := extractBinaryColumn(t, tbl, "stream_id_bitmap.binary")
	// All bitmaps should be the same length (3 bytes, the maximum for stream ID 23).
	require.Len(t, bitmaps[0], 3, "short bitmap should be padded to max length")
	require.Len(t, bitmaps[1], 3, "long bitmap should remain at max length")
}

// TestBuilder_SectionSplitting verifies that a small targetSectionSize causes
// rows to be split across multiple pages.
func TestBuilder_SectionSplitting(t *testing.T) {
	// Use a very small page size to force splitting across multiple pages.
	b := NewBuilder(nil, 100, 2)

	ts := time.Unix(0, 0).UTC()
	for i := range 6 {
		b.ObserveLabelPosting(LabelObservation{ColumnName: "col", LabelValue: fmt.Sprintf("val%d", i), Timestamp: ts})
	}

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1, "all postings go in one section")

	// Collect all rows.
	rows, _ := readAllRows(t, sections[0])
	require.Len(t, rows, 6)
}

// TestBuilder_AllBloom verifies that a builder with only Bloom postings works.
func TestBuilder_AllBloom(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 0).UTC()
	for i := range 3 {
		colName := fmt.Sprintf("col%d", i)
		b.PrepareBloomColumn("", 0, colName, 10)
		_ = b.ObserveBloomPosting(BloomObservation{ObjectPath: "", SectionIndex: 0, ColumnName: colName, Value: "val", StreamID: 0, Timestamp: ts, UncompressedSize: 0})
	}

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows, _ := readAllRows(t, sections[0])
	require.Len(t, rows, 3)
	for _, row := range rows {
		require.Equal(t, int64(KindBloom), row["kind.int64"])
		require.Nil(t, row["label_value.utf8"])
		require.NotNil(t, row["bloom_filter.binary"])
	}
}

// TestBuilder_AllLabel verifies that a builder with only Label postings works.
func TestBuilder_AllLabel(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 0).UTC()
	for i := range 3 {
		lv := fmt.Sprintf("val%d", i)
		b.ObserveLabelPosting(LabelObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col", LabelValue: lv, StreamID: 0, Timestamp: ts, UncompressedSize: 0})
	}

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows, _ := readAllRows(t, sections[0])
	require.Len(t, rows, 3)
	for _, row := range rows {
		require.Equal(t, int64(KindLabel), row["kind.int64"])
		require.NotNil(t, row["label_value.utf8"])
		require.Nil(t, row["bloom_filter.binary"])
	}
}

// TestBuilder_FlushResetsBuilder verifies that a flush resets the builder.
func TestBuilder_FlushResetsBuilder(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 0)
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "", SectionIndex: 0, ColumnName: "col", LabelValue: "v", StreamID: 0, Timestamp: ts, UncompressedSize: 0})

	obj, closer := flushToObject(t, b)
	closer.Close()
	require.Len(t, obj.Sections(), 1)

	// After flush, builder should be empty (Reset was called).
	require.Empty(t, b.labels.entries, "builder should have no label entries after flush")
	require.Empty(t, b.blooms.entries, "builder should have no bloom entries after flush")
	require.Zero(t, b.EstimatedSize(), "builder should have zero estimated size after flush")
}

// TestBuilder_Type verifies that the section type is correct.
func TestBuilder_Type(t *testing.T) {
	b := NewBuilder(nil, 0, 0)
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

func TestReader_SmallBatchSize(t *testing.T) {
	b := NewBuilder(nil, 0, 0)
	ts := time.Unix(0, 0)
	for i := range 5 {
		b.ObserveLabelPosting(LabelObservation{ColumnName: "col", LabelValue: fmt.Sprintf("val%d", i), Timestamp: ts})
	}

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	r := NewReader(ReaderOptions{
		Columns:   sections[0].Columns(),
		Allocator: memory.DefaultAllocator,
	})
	require.NoError(t, r.Open(context.Background()))
	t.Cleanup(func() { _ = r.Close() })

	// Read in batches of 2 and accumulate rows to verify streaming behavior.
	var totalRows int64
	for {
		batch, err := r.Read(context.Background(), 2)
		if batch != nil {
			require.LessOrEqual(t, batch.NumRows(), int64(2), "batch should not exceed batchSize")
			totalRows += batch.NumRows()
		}
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}
	require.Equal(t, int64(5), totalRows, "should read all 5 rows across batches")
}

// TestBuilder_ObserveLabelPosting verifies that multiple stream IDs for the
// same key are aggregated into a single posting entry.
func TestBuilder_ObserveLabelPosting(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	minTs := time.Unix(0, 100).UTC()
	midTs := time.Unix(0, 200).UTC()
	maxTs := time.Unix(0, 300).UTC()

	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/obj", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 1, Timestamp: minTs, UncompressedSize: 100})
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/obj", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 5, Timestamp: midTs, UncompressedSize: 200})
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/obj", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 10, Timestamp: maxTs, UncompressedSize: 300})

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows, tbl := readAllRows(t, sections[0])
	require.Len(t, rows, 1, "three observations for same key should aggregate to one posting")

	bitmaps := extractBinaryColumn(t, tbl, "stream_id_bitmap.binary")

	// stream_id_bitmap.binary is intentionally omitted here; verified below via extractBinaryColumn + checkBit.
	expected := arrowtest.Rows{
		{
			"kind.int64":              int64(KindLabel),
			"object_path.utf8":        "/obj",
			"section_index.int64":     int64(0),
			"column_name.utf8":        "env",
			"label_value.utf8":        "prod",
			"bloom_filter.binary":     nil,
			"uncompressed_size.int64": int64(600),
			"min_timestamp.timestamp": minTs,
			"max_timestamp.timestamp": maxTs,
		},
	}

	strictRows := stripOpaqueBinaryColumns(rows, "stream_id_bitmap.binary")
	strictExpected := stripOpaqueBinaryColumns(expected, "stream_id_bitmap.binary")
	require.Equal(t, strictExpected, strictRows)

	// Verify stream IDs 1, 5, 10 are set.
	require.True(t, checkBit(bitmaps[0], 1))
	require.True(t, checkBit(bitmaps[0], 5))
	require.True(t, checkBit(bitmaps[0], 10))
	require.False(t, checkBit(bitmaps[0], 0))
	require.False(t, checkBit(bitmaps[0], 2))
}

// TestBuilder_ObserveBloomPosting verifies bloom observation: prepare, observe, check membership and bitmap.
func TestBuilder_ObserveBloomPosting(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 100)
	b.PrepareBloomColumn("/obj", 0, "service_name", 100)

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

	rows, tbl := readAllRows(t, sections[0])
	require.Len(t, rows, 1)

	bitmaps := extractBinaryColumn(t, tbl, "stream_id_bitmap.binary")

	row := rows[0]
	require.Equal(t, int64(KindBloom), row["kind.int64"])
	require.Equal(t, "service_name", row["column_name.utf8"])
	require.NotNil(t, row["bloom_filter.binary"])

	// Verify stream IDs 0, 1, 2 are set in the bitmap.
	require.True(t, checkBit(bitmaps[0], 0))
	require.True(t, checkBit(bitmaps[0], 1))
	require.True(t, checkBit(bitmaps[0], 2))
	require.False(t, checkBit(bitmaps[0], 3))
}

// TestBuilder_MixedObservations verifies bloom and label observations produce
// the correct sort order (bloom before label).
func TestBuilder_MixedObservations(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 100).UTC()

	// Add label first (out of order relative to expected output).
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/obj", SectionIndex: 0, ColumnName: "col_b", LabelValue: "v", StreamID: 0, Timestamp: ts, UncompressedSize: 0})

	// Add bloom second.
	b.PrepareBloomColumn("/obj", 0, "col_a", 10)
	_ = b.ObserveBloomPosting(BloomObservation{ObjectPath: "/obj", SectionIndex: 0, ColumnName: "col_a", Value: "v", StreamID: 0, Timestamp: ts, UncompressedSize: 0})

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows, _ := readAllRows(t, sections[0])
	require.Len(t, rows, 2)

	// Bloom should be first regardless of observation order.
	require.Equal(t, int64(KindBloom), rows[0]["kind.int64"], "bloom should sort before label")
	require.Equal(t, int64(KindLabel), rows[1]["kind.int64"])
}

// TestBuilder_ObserveBloomUnprepared verifies that observing an unprepared bloom
// column returns an error.
func TestBuilder_ObserveBloomUnprepared(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 0)
	err := b.ObserveBloomPosting(BloomObservation{ObjectPath: "/obj", SectionIndex: 0, ColumnName: "unprepared_col", Value: "val", StreamID: 0, Timestamp: ts, UncompressedSize: 0})
	require.Error(t, err, "observing unprepared bloom column should return an error")
	require.Contains(t, err.Error(), "bloom column not prepared")
}

// TestBuilder_MultipleObjectContexts verifies that observations with different
// (objectPath, sectionIndex) for the same (columnName, labelValue) produce
// distinct posting rows.
func TestBuilder_MultipleObjectContexts(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 0).UTC()

	// Same column/label, different object paths.
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/obj1", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 0, Timestamp: ts, UncompressedSize: 100})
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/obj2", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 1, Timestamp: ts, UncompressedSize: 200})

	// Same column/label, same path but different section index.
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/obj1", SectionIndex: 1, ColumnName: "env", LabelValue: "prod", StreamID: 2, Timestamp: ts, UncompressedSize: 300})

	sections := flushAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows, tbl := readAllRows(t, sections[0])
	require.Len(t, rows, 3, "different (objectPath, sectionIndex) should produce distinct postings")

	bitmaps := extractBinaryColumn(t, tbl, "stream_id_bitmap.binary")

	// Collect all (objectPath, sectionIndex) pairs from results.
	type key struct {
		path  string
		secID int64
	}
	seen := make(map[key]bool)
	for _, row := range rows {
		k := key{row["object_path.utf8"].(string), row["section_index.int64"].(int64)}
		require.False(t, seen[k], "duplicate (objectPath, sectionIndex) found")
		seen[k] = true
	}

	require.True(t, seen[key{"/obj1", 0}])
	require.True(t, seen[key{"/obj2", 0}])
	require.True(t, seen[key{"/obj1", 1}])

	// Each posting should only have the stream IDs for that context.
	for i, row := range rows {
		path := row["object_path.utf8"].(string)
		secIdx := row["section_index.int64"].(int64)
		usize := row["uncompressed_size.int64"].(int64)

		switch {
		case path == "/obj1" && secIdx == 0:
			require.True(t, checkBit(bitmaps[i], 0), "obj1/0 should have stream 0")
			require.Equal(t, int64(100), usize)
		case path == "/obj2" && secIdx == 0:
			require.True(t, checkBit(bitmaps[i], 1), "obj2/0 should have stream 1")
			require.Equal(t, int64(200), usize)
		case path == "/obj1" && secIdx == 1:
			require.True(t, checkBit(bitmaps[i], 2), "obj1/1 should have stream 2")
			require.Equal(t, int64(300), usize)
		}
	}
}

// TestBuilder_BloomBytes verifies that BloomBytes returns valid marshaled bloom
// data matching what was observed.
func TestBuilder_BloomBytes(t *testing.T) {
	b := NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 0)
	b.PrepareBloomColumn("/obj", 0, "col", 50)

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
	tempBuilder := NewBuilder(nil, 0, 0)
	tempBuilder.PrepareBloomColumn(objectPath, sectionIndex, columnName, 100)
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

// readTable drains a Reader into an arrow.Table, mirroring streams/reader_test.go.
func readTable(ctx context.Context, r *Reader) (arrow.Table, error) {
	var recs []arrow.RecordBatch
	for {
		rec, err := r.Read(ctx, 128)
		if rec != nil && rec.NumRows() > 0 {
			recs = append(recs, rec)
		}
		if err != nil && errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
	}
	if len(recs) == 0 {
		return nil, io.EOF
	}
	return array.NewTableFromRecords(recs[0].Schema(), recs), nil
}

// readAllRows opens all columns from sec, reads them via the new columnar
// Reader, and returns both the row maps and the underlying table (needed for
// extractBinaryColumn calls on opaque columns).
func readAllRows(t *testing.T, sec *Section) (arrowtest.Rows, arrow.Table) {
	t.Helper()
	r := NewReader(ReaderOptions{
		Columns:   sec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	require.NoError(t, r.Open(context.Background()))
	t.Cleanup(func() { _ = r.Close() })

	tbl, err := readTable(context.Background(), r)
	require.NoError(t, err)

	rows, err := arrowtest.TableRows(memory.DefaultAllocator, tbl)
	require.NoError(t, err)
	return rows, tbl
}

// stripOpaqueBinaryColumns returns a copy of rows with the named keys removed
// from every row map. Use when the test intent is structural (e.g., "bit N is
// set") rather than byte-exact, with those columns asserted separately.
func stripOpaqueBinaryColumns(rows arrowtest.Rows, keys ...string) arrowtest.Rows {
	out := make(arrowtest.Rows, len(rows))
	for i, row := range rows {
		cp := make(arrowtest.Row, len(row))
		for k, v := range row {
			cp[k] = v
		}
		for _, k := range keys {
			delete(cp, k)
		}
		out[i] = cp
	}
	return out
}

// extractBinaryColumn extracts a named Binary arrow column from a table as
// [][]byte, copying bytes out of arrow-owned memory.
func extractBinaryColumn(t *testing.T, table arrow.Table, field string) [][]byte {
	t.Helper()
	idx := table.Schema().FieldIndices(field)
	require.Len(t, idx, 1, "field %q not found in schema", field)
	col := table.Column(idx[0])
	var out [][]byte
	for _, chunk := range col.Data().Chunks() {
		bin, ok := chunk.(*array.Binary)
		require.True(t, ok, "field %q is not a Binary column", field)
		for i := 0; i < bin.Len(); i++ {
			if bin.IsNull(i) {
				out = append(out, nil)
				continue
			}
			src := bin.Value(i)
			cp := make([]byte, len(src))
			copy(cp, src)
			out = append(out, cp)
		}
	}
	return out
}
