package postings

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// TestMergeBuilder_AppendLabelEntry_RoundTrip verifies that appended label
// entries round-trip correctly without aggregation.
func TestMergeBuilder_AppendLabelEntry_RoundTrip(t *testing.T) {
	b := NewMergeBuilder(nil, 0, 0)
	b.SetTenant("test-tenant")

	ts := time.Unix(0, 1000).UTC()

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

	// Verify the bitmap bytes match exactly (modulo padding)
	// Since we set bits 3, 7, 15, we need at least 2 bytes
	require.NotEmpty(t, bitmaps[0])
	require.True(t, checkBit(bitmaps[0], 3), "bit 3 should be set")
	require.True(t, checkBit(bitmaps[0], 7), "bit 7 should be set")
	require.True(t, checkBit(bitmaps[0], 15), "bit 15 should be set")
	require.False(t, checkBit(bitmaps[0], 0), "bit 0 should not be set")
}

// TestMergeBuilder_AppendBloomEntry_RoundTrip verifies that appended bloom
// entries round-trip correctly without aggregation.
func TestMergeBuilder_AppendBloomEntry_RoundTrip(t *testing.T) {
	b := NewMergeBuilder(nil, 0, 0)
	b.SetTenant("test-tenant")

	ts := time.Unix(0, 500).UTC()

	bloomBytes := mustBuildBloomBytes(t, "/tenant/abc/obj2", 1, "service_name", "my-service", ts)

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
			"section_index.int64":     int64(1),
			"column_name.utf8":        "service_name",
			"label_value.utf8":        nil,
			"bloom_filter.binary":     bloomFilters[0],
			"uncompressed_size.int64": int64(8192),
			"min_timestamp.timestamp": ts,
			"max_timestamp.timestamp": ts,
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

// TestMergeBuilder_AppendLabelEntry_DuplicateKey_Errors verifies that
// appending two label entries with the same key returns an error on the
// second append.
func TestMergeBuilder_AppendLabelEntry_DuplicateKey_Errors(t *testing.T) {
	b := NewMergeBuilder(nil, 0, 0)
	b.SetTenant("test-tenant")

	ts := time.Unix(0, 0).UTC()

	bitmapBytes := []byte{0x01}
	entry := LabelEntry{
		ObjectPath:       "/obj",
		SectionIndex:     0,
		ColumnName:       "env",
		LabelValue:       "prod",
		StreamIDBitmap:   bitmapBytes,
		MinTimestamp:     ts,
		MaxTimestamp:     ts,
		UncompressedSize: 100,
	}

	// First append should succeed
	err := b.AppendLabelEntry(entry)
	require.NoError(t, err)

	// Second append with same key should fail
	err = b.AppendLabelEntry(entry)
	require.Error(t, err, "appending duplicate key should return an error")
	require.Contains(t, err.Error(), "already exists")
}

// TestMergeBuilder_AppendBloomEntry_DuplicateKey_Errors verifies that
// appending two bloom entries with the same key returns an error on the
// second append.
func TestMergeBuilder_AppendBloomEntry_DuplicateKey_Errors(t *testing.T) {
	ts := time.Unix(0, 0).UTC()

	// Generate valid bloom bytes using the helper
	bloomBytes := mustBuildBloomBytes(t, "/obj", 0, "col", "val", ts)

	// Now use a fresh builder for the duplicate append test
	b := NewMergeBuilder(nil, 0, 0)
	b.SetTenant("test-tenant")

	bitmapBytes := []byte{0x01}
	entry := BloomEntry{
		ObjectPath:       "/obj",
		SectionIndex:     0,
		ColumnName:       "col",
		BloomFilter:      bloomBytes,
		StreamIDBitmap:   bitmapBytes,
		MinTimestamp:     ts,
		MaxTimestamp:     ts,
		UncompressedSize: 200,
	}

	// First append should succeed
	err := b.AppendBloomEntry(entry)
	require.NoError(t, err)

	// Second append with same key should fail
	err = b.AppendBloomEntry(entry)
	require.Error(t, err, "appending duplicate key should return an error")
	require.Contains(t, err.Error(), "already exists")
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
