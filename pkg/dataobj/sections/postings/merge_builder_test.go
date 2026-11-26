package postings

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// TestMergeBuilder_AppendLabelEntry_RoundTrip verifies that appended label
// entries round-trip correctly without aggregation.
func TestMergeBuilder_AppendLabelEntry_RoundTrip(t *testing.T) {
	b := NewMergeBuilder(nil, 0, 0, 1<<20)
	b.SetTenant("test-tenant")

	ts := int64(1000) // unix nanoseconds
	timeVal := time.Unix(0, ts).UTC()

	// Create a bitmap with bits 3, 7, 15 set
	bitmapBytes := make([]byte, 2)
	bitmapBytes[0] = 0b10001000 // bits 3, 7
	bitmapBytes[1] = 0b10000000 // bit 15

	entry := LabelEntry{
		ObjectPath:       "/tenant/abc/obj1",
		SectionIndex:     0,
		ColumnName:       "env",
		LabelValue:       "value1",
		StreamIDBitmap:   bitmapBytes,
		MinTimestamp:     ts,
		MaxTimestamp:     ts,
		UncompressedSize: 4096,
	}

	err := b.AppendLabelEntry(entry)
	require.NoError(t, err)

	sections := flushMergeBuilderAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows, tbl := readAllRows(t, sections[0])
	require.Len(t, rows, 1)

	bitmaps := extractBinaryColumn(t, tbl, "stream_id_bitmap.binary")

	expected := arrowtest.Rows{
		{
			"kind.int64":              int64(KindLabel),
			"object_path.utf8":        "/tenant/abc/obj1",
			"shard_buckets.int64":     int64(0),
			"section_index.int64":     int64(0),
			"column_name.utf8":        "env",
			"label_value.utf8":        "value1",
			"bloom_filter.binary":     nil,
			"uncompressed_size.int64": int64(4096),
			"min_timestamp.timestamp": timeVal,
			"max_timestamp.timestamp": timeVal,
			"min_shard_bucket.int64":  int64(0),
			"max_shard_bucket.int64":  int64(0),
		},
	}

	strictRows := stripOpaqueBinaryColumns(rows, "stream_id_bitmap.binary")
	strictExpected := stripOpaqueBinaryColumns(expected, "stream_id_bitmap.binary")
	require.Equal(t, strictExpected, strictRows)

	// Verify the bitmap bytes match exactly (modulo padding)
	// Since we set bits 3, 7, 15, we need at least 2 bytes
	require.NotEmpty(t, bitmaps[0])
	require.True(t, checkBit(bitmaps[0], 3), "bit 3 should be set")
	require.True(t, checkBit(bitmaps[0], 7), "bit 7 should be set")
	require.True(t, checkBit(bitmaps[0], 15), "bit 15 should be set")
	require.False(t, checkBit(bitmaps[0], 0), "bit 0 should not be set")
}

func TestMergeBuilder_AppendLabelEntry_ShardBuckets(t *testing.T) {
	t.Run("a merged label row keeps the shard bucket range of the source row", func(t *testing.T) {
		source := NewBuilder(nil, 0, 0, 1<<20)
		for streamID, bucket := range map[int64]uint32{1: 3, 2: 9} {
			source.ObserveLabelPosting(LabelObservation{
				ObjectPath: "/o", SectionIndex: 0, ColumnName: "env", LabelValue: "prod",
				StreamID: streamID, Time: At(time.Unix(0, 10).UTC()), ShardBucket: bucket,
			})
		}
		sourceSections := flushAndOpenSections(t, source)
		require.Len(t, sourceSections, 1)

		reader := NewReader(ReaderOptions{Columns: sourceSections[0].Columns()})
		require.NoError(t, reader.Open(t.Context()))
		t.Cleanup(func() { _ = reader.Close() })

		merge := NewMergeBuilder(nil, 0, 0, 1<<20)
		rows := NewRowReader(t.Context(), reader)
		var merged int
		for rows.Next() {
			require.NoError(t, merge.AppendLabelEntry(rows.At().LabelEntry()))
			merged++
		}
		require.NoError(t, rows.Err())
		require.Equal(t, 1, merged)

		mergedSections := flushMergeBuilderAndOpenSections(t, merge)
		require.Len(t, mergedSections, 1)
		got, _ := readAllRows(t, mergedSections[0])
		require.Len(t, got, 1)
		require.Equal(t, int64(3), got[0]["min_shard_bucket.int64"])
		require.Equal(t, int64(9), got[0]["max_shard_bucket.int64"])
	})
}

// TestMergeBuilder_AppendBloomEntry_RoundTrip verifies that appended bloom
// entries round-trip correctly without aggregation.
func TestMergeBuilder_AppendBloomEntry_RoundTrip(t *testing.T) {
	b := NewMergeBuilder(nil, 0, 0, 1<<20)
	b.SetTenant("test-tenant")

	ts := int64(500) // unix nanoseconds
	timeVal := time.Unix(0, ts).UTC()

	bloomBytes := mustBuildBloomBytes(t, "/tenant/abc/obj2", 1, "service_name", "my-service", timeVal)

	// Create bitmap with bits 0, 2, 8 set
	bitmapBytes := make([]byte, 2)
	bitmapBytes[0] = 0b00000101 // bits 0, 2
	bitmapBytes[1] = 0b00000001 // bit 8

	entry := BloomEntry{
		ObjectPath:       "/tenant/abc/obj2",
		SectionIndex:     1,
		ColumnName:       "service_name",
		BloomFilter:      bloomBytes,
		StreamIDBitmap:   bitmapBytes,
		MinTimestamp:     ts,
		MaxTimestamp:     ts,
		UncompressedSize: 8192,
	}

	err := b.AppendBloomEntry(entry)
	require.NoError(t, err)

	sections := flushMergeBuilderAndOpenSections(t, b)
	require.Len(t, sections, 1)

	rows, tbl := readAllRows(t, sections[0])
	require.Len(t, rows, 1)

	bitmaps := extractBinaryColumn(t, tbl, "stream_id_bitmap.binary")
	bloomFilters := extractBinaryColumn(t, tbl, "bloom_filter.binary")

	expected := arrowtest.Rows{
		{
			"kind.int64":              int64(KindBloom),
			"object_path.utf8":        "/tenant/abc/obj2",
			"shard_buckets.int64":     int64(0),
			"section_index.int64":     int64(1),
			"column_name.utf8":        "service_name",
			"label_value.utf8":        nil,
			"bloom_filter.binary":     bloomFilters[0],
			"uncompressed_size.int64": int64(8192),
			"min_timestamp.timestamp": timeVal,
			"max_timestamp.timestamp": timeVal,
			"min_shard_bucket.int64":  nil,
			"max_shard_bucket.int64":  nil,
		},
	}

	strictRows := stripOpaqueBinaryColumns(rows, "stream_id_bitmap.binary", "bloom_filter.binary")
	strictExpected := stripOpaqueBinaryColumns(expected, "stream_id_bitmap.binary", "bloom_filter.binary")
	require.Equal(t, strictExpected, strictRows)

	// Verify bloom filter bytes round-trip exactly
	require.Equal(t, bloomBytes, bloomFilters[0], "bloom filter bytes must round-trip")

	// Verify stream IDs 0, 2, 8 are set
	require.True(t, checkBit(bitmaps[0], 0), "bit 0 should be set")
	require.True(t, checkBit(bitmaps[0], 2), "bit 2 should be set")
	require.True(t, checkBit(bitmaps[0], 8), "bit 8 should be set")
	require.False(t, checkBit(bitmaps[0], 1), "bit 1 should not be set")
}

// testLabelEntry returns a label entry for section 0 of objectPath.
func testLabelEntry(objectPath, columnName, labelValue string, minTimestamp int64) LabelEntry {
	return LabelEntry{
		ObjectPath:     objectPath,
		ColumnName:     columnName,
		LabelValue:     labelValue,
		StreamIDBitmap: []byte{0x01},
		MinTimestamp:   minTimestamp,
		MaxTimestamp:   minTimestamp,
	}
}

// testBloomEntry returns a bloom entry for section 0 of objectPath.
func testBloomEntry(t *testing.T, objectPath, columnName string, minTimestamp int64) BloomEntry {
	t.Helper()
	return BloomEntry{
		ObjectPath:     objectPath,
		ColumnName:     columnName,
		BloomFilter:    mustBuildBloomBytes(t, objectPath, 0, columnName, "value", time.Unix(0, minTimestamp).UTC()),
		StreamIDBitmap: []byte{0x01},
		MinTimestamp:   minTimestamp,
		MaxTimestamp:   minTimestamp,
	}
}

// flushMergeBuilderRows flushes b and returns the rows of all its sections in
// their stored order.
func flushMergeBuilderRows(t *testing.T, b *MergeBuilder) []Row {
	t.Helper()
	var rows []Row
	for _, section := range flushMergeBuilderAndOpenSections(t, b) {
		rows = append(rows, readSectionRows(t, section)...)
	}
	return rows
}

func TestMergeBuilder_Append(t *testing.T) {
	t.Run("returns an error when a label entry sorts before the previous label entry", func(t *testing.T) {
		b := NewMergeBuilder(nil, 0, 0, 1<<20)
		require.NoError(t, b.AppendLabelEntry(testLabelEntry("/obj", "env", "prod", 0)))
		require.ErrorContains(t, b.AppendLabelEntry(testLabelEntry("/obj", "env", "dev", 0)), "sorts before the previous entry")
	})

	t.Run("returns an error when a bloom entry sorts before the previous bloom entry", func(t *testing.T) {
		b := NewMergeBuilder(nil, 0, 0, 1<<20)
		require.NoError(t, b.AppendBloomEntry(testBloomEntry(t, "/obj", "trace_id", 10)))
		require.ErrorContains(t, b.AppendBloomEntry(testBloomEntry(t, "/obj", "trace_id", 5)), "sorts before the previous entry")
	})

	t.Run("returns an error when a label entry repeats the previous label entry", func(t *testing.T) {
		b := NewMergeBuilder(nil, 0, 0, 1<<20)
		require.NoError(t, b.AppendLabelEntry(testLabelEntry("/obj", "env", "prod", 0)))
		require.ErrorContains(t, b.AppendLabelEntry(testLabelEntry("/obj", "env", "prod", 0)), "duplicate posting entry")
	})

	t.Run("returns an error when a bloom entry repeats the previous bloom entry", func(t *testing.T) {
		b := NewMergeBuilder(nil, 0, 0, 1<<20)
		require.NoError(t, b.AppendBloomEntry(testBloomEntry(t, "/obj", "trace_id", 0)))
		require.ErrorContains(t, b.AppendBloomEntry(testBloomEntry(t, "/obj", "trace_id", 0)), "duplicate posting entry")
	})

	t.Run("keeps a rejected label entry out of the flushed rows", func(t *testing.T) {
		b := NewMergeBuilder(nil, 0, 0, 1<<20)
		prod, dev, qa := testLabelEntry("/obj", "env", "prod", 0), testLabelEntry("/obj", "env", "dev", 0), testLabelEntry("/obj", "env", "qa", 0)
		require.NoError(t, b.AppendLabelEntry(prod))
		size := b.EstimatedSize()
		require.Error(t, b.AppendLabelEntry(dev))
		require.Equal(t, size, b.EstimatedSize())
		require.NoError(t, b.AppendLabelEntry(qa))

		require.Equal(t, []Row{prod.Row(), qa.Row()}, flushMergeBuilderRows(t, b))
	})

	t.Run("keeps a rejected bloom entry out of the flushed rows", func(t *testing.T) {
		b := NewMergeBuilder(nil, 0, 0, 1<<20)
		first, earlier, later := testBloomEntry(t, "/obj", "trace_id", 5), testBloomEntry(t, "/obj", "trace_id", 1), testBloomEntry(t, "/obj", "trace_id", 9)
		require.NoError(t, b.AppendBloomEntry(first))
		size := b.EstimatedSize()
		require.Error(t, b.AppendBloomEntry(earlier))
		require.Equal(t, size, b.EstimatedSize())
		require.NoError(t, b.AppendBloomEntry(later))

		require.Equal(t, []Row{first.Row(), later.Row()}, flushMergeBuilderRows(t, b))
	})

	t.Run("accepts interleaved label and bloom entries that are each in order", func(t *testing.T) {
		b := NewMergeBuilder(nil, 0, 0, 1<<20)
		require.NoError(t, b.AppendLabelEntry(testLabelEntry("/obj", "env", "prod", 0)))
		require.NoError(t, b.AppendBloomEntry(testBloomEntry(t, "/obj", "a", 0)))
		require.NoError(t, b.AppendLabelEntry(testLabelEntry("/obj", "env", "qa", 0)))
		require.NoError(t, b.AppendBloomEntry(testBloomEntry(t, "/obj", "b", 0)))
	})

	t.Run("accepts an entry for the same logs section and key with a later time range", func(t *testing.T) {
		b := NewMergeBuilder(nil, 0, 0, 1<<20)
		require.NoError(t, b.AppendLabelEntry(testLabelEntry("/obj", "env", "prod", 0)))
		require.NoError(t, b.AppendLabelEntry(testLabelEntry("/obj", "env", "prod", 2)))
	})
}

func TestMergeBuilder_Reset(t *testing.T) {
	t.Run("accepts any first entry after a reset", func(t *testing.T) {
		b := NewMergeBuilder(nil, 0, 0, 1<<20)
		require.NoError(t, b.AppendLabelEntry(testLabelEntry("/obj", "env", "prod", 0)))
		require.NoError(t, b.AppendBloomEntry(testBloomEntry(t, "/obj", "trace_id", 10)))
		b.Reset()
		require.Zero(t, b.EstimatedSize())
		require.NoError(t, b.AppendLabelEntry(testLabelEntry("/obj", "env", "dev", 0)))
		require.NoError(t, b.AppendBloomEntry(testBloomEntry(t, "/obj", "trace_id", 5)))
	})
}

func TestMergeBuilder_Flush(t *testing.T) {
	t.Run("writes the entries in arrival order across sections", func(t *testing.T) {
		var want []Row
		// A target section size of one byte puts each entry in its own section.
		b := NewMergeBuilder(nil, 0, 0, 1)
		blooms := []BloomEntry{
			testBloomEntry(t, "/obj", "span_id", 0),
			testBloomEntry(t, "/obj-b", "trace_id", 0),
			testBloomEntry(t, "/obj-a", "trace_id", 7),
		}
		for _, entry := range blooms {
			require.NoError(t, b.AppendBloomEntry(entry))
			want = append(want, entry.Row())
		}
		labels := []LabelEntry{
			testLabelEntry("/obj", "app", "api", 3),
			testLabelEntry("/obj", "env", "dev", 1),
			testLabelEntry("/obj-b", "env", "prod", 0),
			testLabelEntry("/obj-a", "env", "prod", 2),
		}
		for _, entry := range labels {
			require.NoError(t, b.AppendLabelEntry(entry))
			want = append(want, entry.Row())
		}

		sections := flushMergeBuilderAndOpenSections(t, b)
		require.Len(t, sections, len(want))

		var got []Row
		for _, section := range sections {
			got = append(got, readSectionRows(t, section)...)
		}
		require.True(t, slices.IsSortedFunc(got, CompareRows))
		require.Equal(t, want, got)
	})

	t.Run("writes only the entries appended after the previous flush", func(t *testing.T) {
		b := NewMergeBuilder(nil, 0, 0, 1<<20)
		require.NoError(t, b.AppendBloomEntry(testBloomEntry(t, "/obj", "trace_id", 10)))
		require.NoError(t, b.AppendLabelEntry(testLabelEntry("/obj", "env", "prod", 0)))
		require.Len(t, flushMergeBuilderRows(t, b), 2)
		require.Zero(t, b.EstimatedSize())

		second := testLabelEntry("/obj", "env", "dev", 0)
		require.NoError(t, b.AppendLabelEntry(second))
		require.Equal(t, []Row{second.Row()}, flushMergeBuilderRows(t, b))
	})
}

// readSectionRows returns the rows of section in their stored order.
func readSectionRows(t *testing.T, section *Section) []Row {
	t.Helper()
	reader := NewReader(ReaderOptions{Columns: section.Columns()})
	require.NoError(t, reader.Open(t.Context()))
	t.Cleanup(func() { _ = reader.Close() })

	var rows []Row
	rowReader := NewRowReader(t.Context(), reader)
	for rowReader.Next() {
		rows = append(rows, rowReader.At())
	}
	require.NoError(t, rowReader.Err())
	return rows
}

// BenchmarkMergeBuilder appends entries in [CompareRows] order and flushes
// them.
func BenchmarkMergeBuilder(b *testing.B) {
	const (
		numBlooms = 10_000
		numLabels = 90_000
	)
	bitmap := []byte{0x01, 0x02}
	bloom := bytes.Repeat([]byte{0xAB}, 64)

	blooms := make([]BloomEntry, numBlooms)
	for i := range blooms {
		blooms[i] = BloomEntry{
			ObjectPath:     fmt.Sprintf("logs/tenant/obj-%06d", i),
			ColumnName:     "trace_id",
			BloomFilter:    bloom,
			StreamIDBitmap: bitmap,
			MinTimestamp:   int64(i),
			MaxTimestamp:   int64(i) + 1,
		}
	}
	labels := make([]LabelEntry, numLabels)
	for i := range labels {
		labels[i] = LabelEntry{
			ObjectPath:     fmt.Sprintf("logs/tenant/obj-%06d", i),
			ColumnName:     "service_name",
			LabelValue:     fmt.Sprintf("svc-%06d", i),
			StreamIDBitmap: bitmap,
			MinTimestamp:   int64(i),
			MaxTimestamp:   int64(i) + 1,
		}
	}

	builder := NewMergeBuilder(nil, 2<<10, 0, 2<<20)
	builder.SetTenant("tenant")

	b.ReportAllocs()
	for b.Loop() {
		for _, entry := range blooms {
			if err := builder.AppendBloomEntry(entry); err != nil {
				b.Fatal(err)
			}
		}
		for _, entry := range labels {
			if err := builder.AppendLabelEntry(entry); err != nil {
				b.Fatal(err)
			}
		}
		if _, err := builder.Flush(discardSectionWriter{}); err != nil {
			b.Fatal(err)
		}
	}
}

// flushMergeBuilderAndOpenSections flushes the merge builder and opens all
// resulting postings sections.
func flushMergeBuilderAndOpenSections(t *testing.T, b *MergeBuilder) []*Section {
	t.Helper()
	// Use a dataobj.Builder to flush the merge builder
	dob := dataobj.NewBuilder(nil)
	err := dob.Append(b)
	require.NoError(t, err)
	obj, closer, err := dob.Flush()
	require.NoError(t, err)
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
