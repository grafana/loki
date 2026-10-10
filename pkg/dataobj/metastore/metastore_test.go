package metastore

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
)

func BenchmarkWriteMetastores(b *testing.B) {
	bucket := objstore.NewInMemBucket()
	tenantID := "test-tenant"

	toc := NewTableOfContentsWriter(bucket, log.NewNopLogger())

	// Add test data spanning multiple metastore windows
	now := time.Date(2025, 1, 1, 15, 0, 0, 0, time.UTC)

	stats := make([]flushStats, 1000)
	for i := 0; i < 1000; i++ {
		stats[i] = flushStats{
			MinTimestamp: now.Add(-1 * time.Hour).Add(time.Duration(i) * time.Millisecond),
			MaxTimestamp: now,
		}
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(b.Context(), time.Second)
		// Test writing metastores
		stats := stats[i%len(stats)]
		err := toc.WriteEntry(ctx, tenantID, TableOfContentsEntry{
			Path:      "path",
			StartTime: stats.MinTimestamp,
			EndTime:   stats.MaxTimestamp,
		})
		require.NoError(b, err)
		cancel()
	}

	require.Len(b, bucket.Objects(), 1)
}

func TestWriteMetastores(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	tenantID := "test-tenant"

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	toc := NewTableOfContentsWriter(bucket, log.NewNopLogger())

	// Add test data spanning multiple metastore windows
	now := time.Date(2025, 1, 1, 15, 0, 0, 0, time.UTC)

	stats := flushStats{
		MinTimestamp: now.Add(-1 * time.Hour),
		MaxTimestamp: now,
	}

	require.Len(t, bucket.Objects(), 0)

	// Test writing metastores
	err := toc.WriteEntry(ctx, tenantID, TableOfContentsEntry{
		Path:      "test-dataobj-path",
		StartTime: stats.MinTimestamp,
		EndTime:   stats.MaxTimestamp,
	})
	require.NoError(t, err)

	require.Len(t, bucket.Objects(), 1)
	var originalSize int
	for _, obj := range bucket.Objects() {
		originalSize = len(obj)
	}

	flushResult2 := flushStats{
		MinTimestamp: now.Add(-15 * time.Minute),
		MaxTimestamp: now,
	}

	err = toc.WriteEntry(ctx, tenantID, TableOfContentsEntry{
		Path:      "different-dataobj-path",
		StartTime: flushResult2.MinTimestamp,
		EndTime:   flushResult2.MaxTimestamp,
	})
	require.NoError(t, err)

	require.Len(t, bucket.Objects(), 1)
	for _, obj := range bucket.Objects() {
		require.Greater(t, len(obj), originalSize)
	}
}

type flushStats struct {
	MinTimestamp time.Time
	MaxTimestamp time.Time
}
