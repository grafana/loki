package indexobj

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/scratch"
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
	}, nil, "tenant-1", testMergeLayout)
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
	}, nil, "tenant-1", testMergeLayout)
	require.NoError(t, err)

	ts := time.Unix(0, 0).UTC()

	// Append a stat
	stat := stats.Stat{
		ObjectPath:       "/path/to/obj1",
		SectionIndex:     0,
		SortSchema:       "label:app",
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

	decodedRows := readAllStatsRows(ctx, t, statSection)
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
	}, nil, "tenant-1", testMergeLayout)
	require.NoError(t, err)

	ts := int64(1000) // unix nanoseconds

	// Create a valid label entry
	bitmapBytes := []byte{0x01}
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

	decodedRows := readAllPostingsRows(ctx, t, postingsSection)
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
	}, nil, "tenant-1", testMergeLayout)
	require.NoError(t, err)

	timeVal := time.Unix(0, 500).UTC()

	// Create valid bloom bytes using an observation builder
	tempBuilder := postings.NewBuilder(nil, 0, 0, 1<<20)
	tempBuilder.PrepareBloomColumn("/obj2", 1, "service_name", 100, 0)
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
	bitmapBytes := []byte{0x01}
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

	decodedRows := readAllPostingsRows(ctx, t, postingsSection)
	require.Len(t, decodedRows, 1)

	row := decodedRows[0]
	require.Equal(t, postings.KindBloom, row.Kind)
	require.Equal(t, entry.ObjectPath, row.ObjectPath)
	require.Equal(t, entry.SectionIndex, row.SectionIndex)
	require.Equal(t, entry.ColumnName, row.ColumnName)
	require.Equal(t, entry.BloomFilter, row.BloomFilter)
	require.Equal(t, entry.StreamIDBitmap, row.StreamIDBitmap)
}

// TestMergeBuilder_PostingsSpillSectionsAtTargetSize verifies that postings are
// cut into sections mid-stream once accumulation reaches TargetSectionSize, so
// the builder never buffers the whole merged output in memory, and that every
// appended row survives the round-trip across the resulting multiple sections.
func TestMergeBuilder_PostingsSpillSectionsAtTargetSize(t *testing.T) {
	b, err := NewMergeBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          2048,
		MaxPageRows:             10000,
		TargetObjectSize:        1 << 30, // large: isolate section-cutting from IsFull
		TargetSectionSize:       1 << 12, // 4 KiB
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
	}, nil, "tenant-1", testMergeLayout)
	require.NoError(t, err)

	// Each entry is ~300 bytes (256-byte bitmap + keys), so 100 entries far
	// exceed a 4 KiB section and force several mid-stream cuts.
	const numEntries = 100
	bitmap := make([]byte, 256)
	want := make(map[string]struct{}, numEntries)
	for i := range numEntries {
		val := fmt.Sprintf("value-%05d", i)
		want[val] = struct{}{}
		require.NoError(t, b.AppendPostingsLabelEntry("tenant-1", postings.LabelEntry{
			ObjectPath:     "/obj",
			SectionIndex:   0,
			ColumnName:     "env",
			LabelValue:     val,
			StreamIDBitmap: bitmap,
			MinTimestamp:   int64(i),
			MaxTimestamp:   int64(i),
		}))
	}

	// A section must have been cut before Flush — proof that the builder is not
	// holding the whole output in memory.
	require.Greater(t, b.builder.Bytes(), 0, "expected a section to be cut mid-stream")

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	require.NotNil(t, obj)
	defer closer.Close()

	// The output spans multiple postings sections...
	sections := obj.Sections()
	require.Greater(t, len(sections), 1, "expected multiple postings sections")

	// ...and every appended row survives across them, none lost or duplicated at
	// the section boundaries.
	ctx := context.Background()
	got := make(map[string]struct{}, numEntries)
	for _, sec := range sections {
		require.True(t, postings.CheckSection(sec))
		ps, err := postings.Open(ctx, sec)
		require.NoError(t, err)
		for _, row := range readAllPostingsRows(ctx, t, ps) {
			require.Equal(t, postings.KindLabel, row.Kind)
			_, dup := got[row.LabelValue]
			require.False(t, dup, "duplicate row across sections: %s", row.LabelValue)
			got[row.LabelValue] = struct{}{}
		}
	}
	require.Equal(t, want, got)
}

func TestMergeBuilder_OtherTenant(t *testing.T) {
	newBuilder := func(t *testing.T) *MergeBuilder {
		t.Helper()
		b, err := NewMergeBuilder(testBuilderConfig, nil, "tenant-1", testMergeLayout)
		require.NoError(t, err)
		return b
	}

	t.Run("AppendStat returns an error for another tenant", func(t *testing.T) {
		err := newBuilder(t).AppendStat("tenant-2", stats.Stat{ObjectPath: "obj", SortSchema: "label:app"})
		require.ErrorContains(t, err, `got an entry of tenant "tenant-2"`)
	})

	t.Run("AppendPostingsLabelEntry returns an error for another tenant", func(t *testing.T) {
		err := newBuilder(t).AppendPostingsLabelEntry("tenant-2", postings.LabelEntry{ObjectPath: "obj", ColumnName: "app", LabelValue: "a"})
		require.ErrorContains(t, err, `got an entry of tenant "tenant-2"`)
	})

	t.Run("AppendPostingsBloomEntry returns an error for another tenant", func(t *testing.T) {
		err := newBuilder(t).AppendPostingsBloomEntry("tenant-2", postings.BloomEntry{ObjectPath: "obj", ColumnName: "trace_id"})
		require.ErrorContains(t, err, `got an entry of tenant "tenant-2"`)
	})

	t.Run("NewMergeBuilder returns an error for an empty tenant", func(t *testing.T) {
		_, err := NewMergeBuilder(testBuilderConfig, nil, "", testMergeLayout)
		require.Error(t, err)
	})
}

func TestMergeBuilder_RecordsLayout(t *testing.T) {
	t.Run("AppendStat returns an error when the builder has the zero layout", func(t *testing.T) {
		b, err := NewMergeBuilder(testBuilderConfig, nil, "tenant-1", streams.Layout{})
		require.NoError(t, err)
		require.ErrorContains(t, b.AppendStat("tenant-1", stats.Stat{ObjectPath: "obj"}), "without a stream layout")
	})

	t.Run("every stats section records the layout of the builder", func(t *testing.T) {
		b, err := NewMergeBuilder(oneStatPerSectionConfig, nil, "tenant-1", testMergeLayout)
		require.NoError(t, err)
		appendStats(t, b, 3)

		obj, closer, err := b.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		require.Equal(t, 3, obj.Sections().Count(stats.CheckSection))
		for _, sec := range obj.Sections().Filter(stats.CheckSection) {
			opened, err := stats.Open(t.Context(), sec)
			require.NoError(t, err)
			got, ok, err := opened.Layout()
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, testMergeLayout, got)
		}
	})
}

// readAllPostingsRows opens a postings section and returns all decoded rows.
// It drains the reader in batches and decodes each row from the Arrow record batches.
func readAllPostingsRows(ctx context.Context, t *testing.T, sec *postings.Section) []postings.Row {
	t.Helper()

	r := postings.NewReader(postings.ReaderOptions{
		Columns:   sec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	require.NoError(t, r.Open(ctx))
	t.Cleanup(func() { _ = r.Close() })

	var (
		rows    []postings.Row
		columns postings.ColumnIndex
	)
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

		// Build column index on first batch.
		if columns == nil {
			columns = postings.BuildColumnIndex(batch.Schema())
		}

		// Decode each row in the batch.
		for rowIdx := 0; rowIdx < int(batch.NumRows()); rowIdx++ {
			rows = append(rows, postings.DecodeRow(batch, columns, rowIdx))
		}

		if errors.Is(err, io.EOF) {
			break
		}
	}

	return rows
}

// readAllStatsRows opens a stats section and returns all decoded rows.
// It drains the reader in batches and decodes each row from the Arrow record batches.
func readAllStatsRows(ctx context.Context, t *testing.T, sec *stats.Section) []stats.Stat {
	t.Helper()

	r := stats.NewReader(stats.ReaderOptions{
		Columns:   sec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	require.NoError(t, r.Open(ctx))
	t.Cleanup(func() { _ = r.Close() })

	var (
		rows    []stats.Stat
		columns stats.ColumnIndex
	)
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

		// Build column index on first batch.
		if columns == nil {
			columns = stats.BuildColumnIndex(batch.Schema())
		}

		// Decode each row in the batch.
		for rowIdx := 0; rowIdx < int(batch.NumRows()); rowIdx++ {
			rows = append(rows, stats.DecodeRow(batch, columns, rowIdx))
		}

		if errors.Is(err, io.EOF) {
			break
		}
	}

	return rows
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
	}, nil, "tenant-1", testMergeLayout)
	require.NoError(t, err)

	ts := time.Unix(0, 0).UTC()

	// Add some data
	stat := stats.Stat{
		ObjectPath:       "/path/obj1",
		SectionIndex:     0,
		SortSchema:       "label:app",
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
		SortSchema:       "label:app",
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

// oneStatPerSectionConfig cuts a stats section after every stat, so a test
// can build an object with many sections.
var oneStatPerSectionConfig = func() logsobj.BuilderBaseConfig {
	cfg := testBuilderConfig
	cfg.TargetSectionSize = 1
	return cfg
}()

// testMergeLayout is the layout of the merge builders in these tests.
var testMergeLayout = streams.NewLayout(streams.DefaultShardLayout(), []string{"label:app"})

// appendStats appends n stats of tenant-1 to b.
func appendStats(t *testing.T, b *MergeBuilder, n int) {
	t.Helper()
	for i := range n {
		require.NoError(t, b.AppendStat("tenant-1", stats.Stat{
			ObjectPath:       fmt.Sprintf("objects/%04d", i),
			SectionIndex:     int64(i),
			SortSchema:       "label:app",
			Labels:           map[string]string{"app": fmt.Sprintf("v%d", i)},
			MinTimestamp:     time.Unix(10, 0).UnixNano(),
			MaxTimestamp:     time.Unix(20, 0).UnixNano(),
			RowCount:         1,
			UncompressedSize: 100,
		}))
	}
}

// A closer returned alongside an error is never closed by callers, since they
// stop at the error, so Flush must hand back nothing when it fails.
func TestMergeBuilder_FlushReturnsNoCloserOnError(t *testing.T) {
	t.Run("when the object cannot be built", func(t *testing.T) {
		store := newFailingReadStore(false)
		b, err := NewMergeBuilder(testBuilderConfig, store, "tenant-1", testMergeLayout)
		require.NoError(t, err)
		appendStats(t, b, 1)

		obj, closer, err := b.Flush()
		require.ErrorContains(t, err, "flushing object")
		require.Nil(t, obj)
		require.Nil(t, closer)
		require.NotEmpty(t, store.removed, "the buffered sections must be released")
	})

	t.Run("when the built object cannot be observed", func(t *testing.T) {
		// See the equivalent Builder test: only the last section's metadata
		// fails to read, and with this many sections the metadata outgrows the
		// decoder's prefetch window, so the failure lands while observing.
		store := newFailingReadStore(true)
		b, err := NewMergeBuilder(oneStatPerSectionConfig, store, "tenant-1", testMergeLayout)
		require.NoError(t, err)
		appendStats(t, b, 64)

		obj, closer, err := b.Flush()
		require.ErrorContains(t, err, "observing object")
		require.Nil(t, obj)
		require.Nil(t, closer)
		require.NotEmpty(t, store.removed, "the object's scratch handles must be released")
	})
}

// Flush resets the builder whether it succeeds or fails, so a failed flush
// leaves nothing behind for the next one to pick up.
func TestMergeBuilder_FlushResetsBuilder(t *testing.T) {
	tests := []struct {
		name    string
		store   scratch.Store
		stats   int
		wantErr string
	}{
		{
			name:  "when the object is built",
			store: scratch.NewMemory(),
			stats: 1,
		},
		{
			name:    "when the object cannot be built",
			store:   newFailingReadStore(false),
			stats:   1,
			wantErr: "flushing object",
		},
		{
			// See TestBuilder_FlushReturnsNoCloserOnError for why this many
			// sections are needed to fail while observing.
			name:    "when the built object cannot be observed",
			store:   newFailingReadStore(true),
			stats:   64,
			wantErr: "observing object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := NewMergeBuilder(oneStatPerSectionConfig, tt.store, "tenant-1", testMergeLayout)
			require.NoError(t, err)
			appendStats(t, b, tt.stats)

			_, closer, err := b.Flush()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				defer closer.Close()
			}

			require.Zero(t, b.GetEstimatedSize())
			_, _, err = b.Flush()
			require.ErrorIs(t, err, ErrBuilderEmpty)
		})
	}
}
