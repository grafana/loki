package indexobj

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
)

// TestMergeBuilder_Empty verifies that an empty merge builder returns ErrBuilderEmpty on flush.
func TestMergeBuilder_Empty(t *testing.T) {
	b, err := NewMergeBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          2048,
		MaxPageRows:             10000,
		TargetObjectSize:        1 << 22, // 4 MiB
		TargetSectionSize:       1 << 21, // 2 MiB
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	_, _, err = b.Flush()
	require.Error(t, err)
	require.Equal(t, ErrBuilderEmpty, err)
}

// TestMergeBuilder_AppendStat verifies that AppendStat works correctly.
func TestMergeBuilder_AppendStat(t *testing.T) {
	b, err := NewMergeBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          2048,
		MaxPageRows:             10000,
		TargetObjectSize:        1 << 22,
		TargetSectionSize:       1 << 21,
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	ts := time.Unix(0, 0).UTC()

	// Append a stat
	stat := stats.Stat{
		ObjectPath:       "/path/to/obj1",
		SectionIndex:     0,
		SortSchema:       "sort_key_1",
		Labels:           map[string]string{"level": "info"},
		MinTimestamp:     ts.UnixNano(),
		MaxTimestamp:     ts.UnixNano(),
		RowCount:         100,
		UncompressedSize: 1024,
	}
	err = b.AppendStat("tenant-1", stat)
	require.NoError(t, err)

	require.Greater(t, b.GetEstimatedSize(), 0)
	require.False(t, b.IsFull())

	// Flush should succeed now
	obj, closer, err := b.Flush()
	require.NoError(t, err)
	require.NotNil(t, obj)
	defer closer.Close()

	// After flush, Reset should have been called
	require.Equal(t, 0, b.GetEstimatedSize())
	require.False(t, b.IsFull())

	// Assert the object contains the expected stats section with the appended row.
	require.Len(t, obj.Sections(), 1)
	sec := obj.Sections()[0]
	require.Equal(t, "tenant-1", sec.Tenant)
	require.True(t, stats.CheckSection(sec))

	ctx := context.Background()
	statSection, err := stats.Open(ctx, sec)
	require.NoError(t, err)

	decodedRows := readAllStatsRows(t, ctx, statSection)
	require.Len(t, decodedRows, 1)

	row := decodedRows[0]
	require.Equal(t, stat.ObjectPath, row.ObjectPath)
	require.Equal(t, stat.SectionIndex, row.SectionIndex)
	require.Equal(t, stat.RowCount, row.RowCount)
	require.Equal(t, stat.UncompressedSize, row.UncompressedSize)
}

// TestMergeBuilder_AppendPostingsLabelEntry verifies that AppendPostingsLabelEntry works.
func TestMergeBuilder_AppendPostingsLabelEntry(t *testing.T) {
	b, err := NewMergeBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          2048,
		MaxPageRows:             10000,
		TargetObjectSize:        1 << 22,
		TargetSectionSize:       1 << 21,
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	ts := int64(1000) // unix nanoseconds

	// Create a valid label entry
	bitmapBytes := []byte{0x01, 0x00}
	entry := postings.LabelEntry{
		ObjectPath:       "/obj1",
		SectionIndex:     0,
		ColumnName:       "env",
		LabelValue:       "prod",
		StreamIDBitmap:   bitmapBytes,
		MinTimestamp:     ts,
		MaxTimestamp:     ts,
		UncompressedSize: 4096,
	}

	err = b.AppendPostingsLabelEntry("tenant-1", entry)
	require.NoError(t, err)

	require.Greater(t, b.GetEstimatedSize(), 0)

	// Flush should succeed
	obj, closer, err := b.Flush()
	require.NoError(t, err)
	require.NotNil(t, obj)
	defer closer.Close()

	// Assert the object contains the expected postings section with the appended row.
	require.Len(t, obj.Sections(), 1)
	sec := obj.Sections()[0]
	require.Equal(t, "tenant-1", sec.Tenant)
	require.True(t, postings.CheckSection(sec))

	ctx := context.Background()
	postingsSection, err := postings.Open(ctx, sec)
	require.NoError(t, err)

	decodedRows := readAllPostingsRows(t, ctx, postingsSection)
	require.Len(t, decodedRows, 1)

	row := decodedRows[0]
	require.Equal(t, postings.KindLabel, row.Kind)
	require.Equal(t, entry.ObjectPath, row.ObjectPath)
	require.Equal(t, entry.SectionIndex, row.SectionIndex)
	require.Equal(t, entry.ColumnName, row.ColumnName)
	require.Equal(t, entry.LabelValue, row.LabelValue)
	require.Equal(t, entry.StreamIDBitmap, row.StreamIDBitmap)
}

// TestMergeBuilder_AppendPostingsBloomEntry verifies that AppendPostingsBloomEntry works.
func TestMergeBuilder_AppendPostingsBloomEntry(t *testing.T) {
	b, err := NewMergeBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          2048,
		MaxPageRows:             10000,
		TargetObjectSize:        1 << 22,
		TargetSectionSize:       1 << 21,
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	timeVal := time.Unix(0, 500).UTC()

	// Create valid bloom bytes using an observation builder
	tempBuilder := postings.NewBuilder(nil, 0, 0)
	tempBuilder.PrepareBloomColumn("/obj2", 1, "service_name", 100)
	err = tempBuilder.ObserveBloomPosting(postings.BloomObservation{
		ObjectPath:       "/obj2",
		SectionIndex:     1,
		ColumnName:       "service_name",
		Value:            "my-service",
		StreamID:         0,
		Timestamp:        timeVal,
		UncompressedSize: 0,
	})
	require.NoError(t, err)

	bloomBytes, err := tempBuilder.BloomBytes("/obj2", 1, "service_name")
	require.NoError(t, err)

	// Now create the entry
	ts := int64(500) // unix nanoseconds
	bitmapBytes := []byte{0x01, 0x00}
	entry := postings.BloomEntry{
		ObjectPath:       "/obj2",
		SectionIndex:     1,
		ColumnName:       "service_name",
		BloomFilter:      bloomBytes,
		StreamIDBitmap:   bitmapBytes,
		MinTimestamp:     ts,
		MaxTimestamp:     ts,
		UncompressedSize: 8192,
	}

	err = b.AppendPostingsBloomEntry("tenant-1", entry)
	require.NoError(t, err)

	require.Greater(t, b.GetEstimatedSize(), 0)

	// Flush should succeed
	obj, closer, err := b.Flush()
	require.NoError(t, err)
	require.NotNil(t, obj)
	defer closer.Close()

	// Assert the object contains the expected postings section with the appended row.
	require.Len(t, obj.Sections(), 1)
	sec := obj.Sections()[0]
	require.Equal(t, "tenant-1", sec.Tenant)
	require.True(t, postings.CheckSection(sec))

	ctx := context.Background()
	postingsSection, err := postings.Open(ctx, sec)
	require.NoError(t, err)

	decodedRows := readAllPostingsRows(t, ctx, postingsSection)
	require.Len(t, decodedRows, 1)

	row := decodedRows[0]
	require.Equal(t, postings.KindBloom, row.Kind)
	require.Equal(t, entry.ObjectPath, row.ObjectPath)
	require.Equal(t, entry.SectionIndex, row.SectionIndex)
	require.Equal(t, entry.ColumnName, row.ColumnName)
	require.Equal(t, entry.BloomFilter, row.BloomFilter)
	require.Equal(t, entry.StreamIDBitmap, row.StreamIDBitmap)
}

// TestMergeBuilder_MultiTenant verifies that the merge builder handles multiple tenants.
func TestMergeBuilder_MultiTenant(t *testing.T) {
	b, err := NewMergeBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          2048,
		MaxPageRows:             10000,
		TargetObjectSize:        1 << 22,
		TargetSectionSize:       1 << 21,
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	ts := time.Unix(0, 0).UTC()

	// Append stats for different tenants
	stat1 := stats.Stat{
		ObjectPath:       "/path/obj1",
		SectionIndex:     0,
		SortSchema:       "sort_key_1",
		Labels:           map[string]string{},
		MinTimestamp:     ts.UnixNano(),
		MaxTimestamp:     ts.UnixNano(),
		RowCount:         100,
		UncompressedSize: 1024,
	}
	err = b.AppendStat("tenant-1", stat1)
	require.NoError(t, err)

	stat2 := stats.Stat{
		ObjectPath:       "/path/obj2",
		SectionIndex:     0,
		SortSchema:       "sort_key_1",
		Labels:           map[string]string{},
		MinTimestamp:     ts.UnixNano(),
		MaxTimestamp:     ts.UnixNano(),
		RowCount:         200,
		UncompressedSize: 2048,
	}
	err = b.AppendStat("tenant-2", stat2)
	require.NoError(t, err)

	// Flush should include both tenants' data
	obj, closer, err := b.Flush()
	require.NoError(t, err)
	require.NotNil(t, obj)
	defer closer.Close()

	// Assert we have sections for both tenants (one stats section per tenant).
	require.Len(t, obj.Sections(), 2)

	// Collect sections by tenant.
	sectionsByTenant := make(map[string]*dataobj.Section)
	for _, sec := range obj.Sections() {
		sectionsByTenant[sec.Tenant] = sec
	}
	require.Len(t, sectionsByTenant, 2)

	// Verify tenant-1 section contains stat1.
	sec1 := sectionsByTenant["tenant-1"]
	require.NotNil(t, sec1)
	require.True(t, stats.CheckSection(sec1))

	ctx := context.Background()
	statSec1, err := stats.Open(ctx, sec1)
	require.NoError(t, err)
	rows1 := readAllStatsRows(t, ctx, statSec1)
	require.Len(t, rows1, 1)
	require.Equal(t, stat1.ObjectPath, rows1[0].ObjectPath)
	require.Equal(t, stat1.RowCount, rows1[0].RowCount)

	// Verify tenant-2 section contains stat2.
	sec2 := sectionsByTenant["tenant-2"]
	require.NotNil(t, sec2)
	require.True(t, stats.CheckSection(sec2))

	statSec2, err := stats.Open(ctx, sec2)
	require.NoError(t, err)
	rows2 := readAllStatsRows(t, ctx, statSec2)
	require.Len(t, rows2, 1)
	require.Equal(t, stat2.ObjectPath, rows2[0].ObjectPath)
	require.Equal(t, stat2.RowCount, rows2[0].RowCount)
}

// postingsRow is a decoded per-row representation of a postings section row,
// covering both label and bloom kinds.
type postingsRow struct {
	Kind           postings.PostingKind
	ObjectPath     string
	SectionIndex   int64
	ColumnName     string
	LabelValue     string // empty for KindBloom rows
	BloomFilter    []byte // nil for KindLabel rows
	StreamIDBitmap []byte
	MinTimestamp   int64 // unix nanos
	MaxTimestamp   int64 // unix nanos
}

// statsRow is a decoded per-row representation of a stats section row.
type statsRow struct {
	ObjectPath       string
	SectionIndex     int64
	RowCount         int64
	UncompressedSize int64
	MinTimestamp     int64
	MaxTimestamp     int64
}

// readAllPostingsRows opens a postings section and returns all decoded rows.
// It drains the reader in batches and decodes each row from the Arrow record batches.
func readAllPostingsRows(t *testing.T, ctx context.Context, sec *postings.Section) []postingsRow {
	t.Helper()

	r := postings.NewReader(postings.ReaderOptions{
		Columns:   sec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	require.NoError(t, r.Open(ctx))
	t.Cleanup(func() { _ = r.Close() })

	var rows []postingsRow
	for {
		batch, err := r.Read(ctx, 128)
		if err != nil && !errors.Is(err, io.EOF) {
			require.NoError(t, err)
		}

		if batch == nil || batch.NumRows() == 0 {
			if errors.Is(err, io.EOF) {
				break
			}
			continue
		}

		// Build a column name to index map for decoding.
		columns := make(map[string]int)
		for i, field := range batch.Schema().Fields() {
			columns[field.Name] = i
		}

		// Decode each row in the batch.
		for rowIdx := 0; rowIdx < int(batch.NumRows()); rowIdx++ {
			row := decodePostingsRow(batch, columns, rowIdx)
			rows = append(rows, row)
		}

		if errors.Is(err, io.EOF) {
			break
		}
	}

	return rows
}

// decodePostingsRow extracts a single row from an Arrow record batch.
func decodePostingsRow(batch arrow.RecordBatch, columns map[string]int, rowIndex int) postingsRow {
	result := postingsRow{}

	// Helper to safely extract a column value by name.
	getColumn := func(name string) arrow.Array {
		if idx, ok := columns[name]; ok {
			return batch.Column(idx)
		}
		return nil
	}

	if col := getColumn("kind.int64"); col != nil && !col.IsNull(rowIndex) {
		result.Kind = postings.PostingKind(col.(*array.Int64).Value(rowIndex))
	}
	if col := getColumn("object_path.utf8"); col != nil && !col.IsNull(rowIndex) {
		result.ObjectPath = col.(*array.String).Value(rowIndex)
	}
	if col := getColumn("section_index.int64"); col != nil && !col.IsNull(rowIndex) {
		result.SectionIndex = col.(*array.Int64).Value(rowIndex)
	}
	if col := getColumn("column_name.utf8"); col != nil && !col.IsNull(rowIndex) {
		result.ColumnName = col.(*array.String).Value(rowIndex)
	}
	if col := getColumn("label_value.utf8"); col != nil && !col.IsNull(rowIndex) {
		result.LabelValue = col.(*array.String).Value(rowIndex)
	}
	if col := getColumn("stream_id_bitmap.binary"); col != nil && !col.IsNull(rowIndex) {
		result.StreamIDBitmap = bytes.Clone(col.(*array.Binary).Value(rowIndex))
	}
	if col := getColumn("bloom_filter.binary"); col != nil && !col.IsNull(rowIndex) {
		result.BloomFilter = bytes.Clone(col.(*array.Binary).Value(rowIndex))
	}
	if col := getColumn("min_timestamp.timestamp"); col != nil && !col.IsNull(rowIndex) {
		result.MinTimestamp = int64(col.(*array.Timestamp).Value(rowIndex))
	}
	if col := getColumn("max_timestamp.timestamp"); col != nil && !col.IsNull(rowIndex) {
		result.MaxTimestamp = int64(col.(*array.Timestamp).Value(rowIndex))
	}

	return result
}

// readAllStatsRows opens a stats section and returns all decoded rows.
// It drains the reader in batches and decodes each row from the Arrow record batches.
func readAllStatsRows(t *testing.T, ctx context.Context, sec *stats.Section) []statsRow {
	t.Helper()

	r := stats.NewReader(stats.ReaderOptions{
		Columns:   sec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	require.NoError(t, r.Open(ctx))
	t.Cleanup(func() { _ = r.Close() })

	var rows []statsRow
	for {
		batch, err := r.Read(ctx, 128)
		if err != nil && !errors.Is(err, io.EOF) {
			require.NoError(t, err)
		}

		if batch == nil || batch.NumRows() == 0 {
			if errors.Is(err, io.EOF) {
				break
			}
			continue
		}

		// Build a column name to index map for decoding.
		columns := make(map[string]int)
		for i, field := range batch.Schema().Fields() {
			columns[field.Name] = i
		}

		// Decode each row in the batch.
		for rowIdx := 0; rowIdx < int(batch.NumRows()); rowIdx++ {
			row := decodeStatsRow(batch, columns, rowIdx)
			rows = append(rows, row)
		}

		if errors.Is(err, io.EOF) {
			break
		}
	}

	return rows
}

// decodeStatsRow extracts a single row from an Arrow record batch.
func decodeStatsRow(batch arrow.RecordBatch, columns map[string]int, rowIndex int) statsRow {
	result := statsRow{}

	// Helper to safely extract a column value by name.
	getColumn := func(name string) arrow.Array {
		if idx, ok := columns[name]; ok {
			return batch.Column(idx)
		}
		return nil
	}

	if col := getColumn("object_path.utf8"); col != nil && !col.IsNull(rowIndex) {
		result.ObjectPath = col.(*array.String).Value(rowIndex)
	}
	if col := getColumn("section_index.int64"); col != nil && !col.IsNull(rowIndex) {
		result.SectionIndex = col.(*array.Int64).Value(rowIndex)
	}
	if col := getColumn("row_count.int64"); col != nil && !col.IsNull(rowIndex) {
		result.RowCount = col.(*array.Int64).Value(rowIndex)
	}
	if col := getColumn("uncompressed_size.int64"); col != nil && !col.IsNull(rowIndex) {
		result.UncompressedSize = col.(*array.Int64).Value(rowIndex)
	}
	if col := getColumn("min_timestamp.timestamp"); col != nil && !col.IsNull(rowIndex) {
		result.MinTimestamp = int64(col.(*array.Timestamp).Value(rowIndex))
	}
	if col := getColumn("max_timestamp.timestamp"); col != nil && !col.IsNull(rowIndex) {
		result.MaxTimestamp = int64(col.(*array.Timestamp).Value(rowIndex))
	}

	return result
}

// TestMergeBuilder_Reset verifies that Reset clears all state.
func TestMergeBuilder_Reset(t *testing.T) {
	b, err := NewMergeBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          2048,
		MaxPageRows:             10000,
		TargetObjectSize:        1 << 22,
		TargetSectionSize:       1 << 21,
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	ts := time.Unix(0, 0).UTC()

	// Add some data
	stat := stats.Stat{
		ObjectPath:       "/path/obj1",
		SectionIndex:     0,
		SortSchema:       "sort_key_1",
		Labels:           map[string]string{},
		MinTimestamp:     ts.UnixNano(),
		MaxTimestamp:     ts.UnixNano(),
		RowCount:         100,
		UncompressedSize: 1024,
	}
	err = b.AppendStat("tenant-1", stat)
	require.NoError(t, err)

	require.Greater(t, b.GetEstimatedSize(), 0)

	// Reset
	b.Reset()

	require.Equal(t, 0, b.GetEstimatedSize())
	require.False(t, b.IsFull())
	require.Equal(t, builderStateEmpty, b.state)

	// After reset, should be able to add data again and flush
	stat2 := stats.Stat{
		ObjectPath:       "/path/obj1",
		SectionIndex:     0,
		SortSchema:       "sort_key_1",
		Labels:           map[string]string{},
		MinTimestamp:     ts.UnixNano(),
		MaxTimestamp:     ts.UnixNano(),
		RowCount:         100,
		UncompressedSize: 1024,
	}
	err = b.AppendStat("tenant-1", stat2)
	require.NoError(t, err)

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	require.NotNil(t, obj)
	closer.Close()
}
