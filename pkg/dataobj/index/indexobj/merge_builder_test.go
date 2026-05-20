package indexobj

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
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
	err = b.AppendStat("tenant-1", "/path/to/obj1", 0, "sort_key_1", map[string]string{"level": "info"}, ts, ts, 100, 1024)
	require.NoError(t, err)

	require.Greater(t, b.GetEstimatedSize(), 0)
	require.False(t, b.IsFull())

	// Flush should succeed now
	obj, closer, err := b.Flush()
	require.NoError(t, err)
	require.NotNil(t, obj)
	closer.Close()

	// After flush, Reset should have been called
	require.Equal(t, 0, b.GetEstimatedSize())
	require.False(t, b.IsFull())
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

	ts := time.Unix(0, 1000).UTC()

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
	closer.Close()
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

	ts := time.Unix(0, 500).UTC()

	// Create valid bloom bytes using an observation builder
	tempBuilder := postings.NewBuilder(nil, 0, 0)
	tempBuilder.PrepareBloomColumn("/obj2", 1, "service_name", 100)
	err = tempBuilder.ObserveBloomPosting(postings.BloomObservation{
		ObjectPath:       "/obj2",
		SectionIndex:     1,
		ColumnName:       "service_name",
		Value:            "my-service",
		StreamID:         0,
		Timestamp:        ts,
		UncompressedSize: 0,
	})
	require.NoError(t, err)

	bloomBytes, err := tempBuilder.BloomBytes("/obj2", 1, "service_name")
	require.NoError(t, err)

	// Now create the entry
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
	closer.Close()
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
	err = b.AppendStat("tenant-1", "/path/obj1", 0, "sort_key_1", map[string]string{}, ts, ts, 100, 1024)
	require.NoError(t, err)

	err = b.AppendStat("tenant-2", "/path/obj2", 0, "sort_key_1", map[string]string{}, ts, ts, 200, 2048)
	require.NoError(t, err)

	// Flush should include both tenants' data
	obj, closer, err := b.Flush()
	require.NoError(t, err)
	require.NotNil(t, obj)
	closer.Close()
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
	err = b.AppendStat("tenant-1", "/path/obj1", 0, "sort_key_1", map[string]string{}, ts, ts, 100, 1024)
	require.NoError(t, err)

	require.Greater(t, b.GetEstimatedSize(), 0)

	// Reset
	b.Reset()

	require.Equal(t, 0, b.GetEstimatedSize())
	require.False(t, b.IsFull())
	require.Equal(t, builderStateEmpty, b.state)

	// After reset, should be able to add data again and flush
	err = b.AppendStat("tenant-1", "/path/obj1", 0, "sort_key_1", map[string]string{}, ts, ts, 100, 1024)
	require.NoError(t, err)

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	require.NotNil(t, obj)
	closer.Close()
}
