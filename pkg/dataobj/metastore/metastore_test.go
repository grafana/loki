package metastore

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/dskit/backoff"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

func BenchmarkWriteMetastores(t *testing.B) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	tenantID := "test-tenant"

	m := NewManager(bucket, tenantID, log.NewNopLogger())

	// Set limits for the test
	m.backoff = backoff.New(context.TODO(), backoff.Config{
		MinBackoff: 10 * time.Millisecond,
		MaxBackoff: 100 * time.Millisecond,
		MaxRetries: 3,
	})

	// Add test data spanning multiple metastore windows
	now := time.Date(2025, 1, 1, 15, 0, 0, 0, time.UTC)

	flushStats := make([]dataobj.FlushStats, 1000)
	for i := 0; i < 1000; i++ {
		flushStats[i] = dataobj.FlushStats{
			MinTimestamp: now.Add(-1 * time.Hour).Add(time.Duration(i) * time.Millisecond),
			MaxTimestamp: now,
		}
	}

	t.ResetTimer()
	t.ReportAllocs()
	for i := 0; i < t.N; i++ {
		// Test writing metastores
		err := m.UpdateMetastore(ctx, "path", flushStats[i%len(flushStats)])
		require.NoError(t, err)
	}

	require.Len(t, bucket.Objects(), 1)
}

func TestWriteMetastores(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	tenantID := "test-tenant"

	m := NewManager(bucket, tenantID, log.NewNopLogger())

	// Set limits for the test
	m.backoff = backoff.New(context.TODO(), backoff.Config{
		MinBackoff: 10 * time.Millisecond,
		MaxBackoff: 100 * time.Millisecond,
		MaxRetries: 3,
	})

	// Add test data spanning multiple metastore windows
	now := time.Date(2025, 1, 1, 15, 0, 0, 0, time.UTC)

	flushStats := dataobj.FlushStats{
		MinTimestamp: now.Add(-1 * time.Hour),
		MaxTimestamp: now,
	}

	require.Len(t, bucket.Objects(), 0)

	// Test writing metastores
	err := m.UpdateMetastore(ctx, "test-dataobj-path", flushStats)
	require.NoError(t, err)

	require.Len(t, bucket.Objects(), 1)
	var originalSize int
	for _, obj := range bucket.Objects() {
		originalSize = len(obj)
	}

	flushResult2 := dataobj.FlushStats{
		MinTimestamp: now.Add(-15 * time.Minute),
		MaxTimestamp: now,
	}

	err = m.UpdateMetastore(ctx, "different-dataobj-path", flushResult2)
	require.NoError(t, err)

	require.Len(t, bucket.Objects(), 1)
	for _, obj := range bucket.Objects() {
		require.Greater(t, len(obj), originalSize)
	}
}
