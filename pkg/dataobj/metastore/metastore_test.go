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

func TestIter(t *testing.T) {
	tenantID := "TEST"
	now := time.Date(2025, 1, 1, 15, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name     string
		start    time.Time
		end      time.Time
		expected []string
	}{
		{
			name:     "within single window",
			start:    now,
			end:      now.Add(1 * time.Hour),
			expected: []string{"tenant-TEST/metastore/2025-01-01T12:00:00Z.store"},
		},
		{
			name:     "same start and end",
			start:    now,
			end:      now,
			expected: []string{"tenant-TEST/metastore/2025-01-01T12:00:00Z.store"},
		},
		{
			name:  "begin at start of window",
			start: now.Add(-3 * time.Hour),
			end:   now,
			expected: []string{
				"tenant-TEST/metastore/2025-01-01T12:00:00Z.store",
			},
		},
		{
			name:  "end at start of next window",
			start: now.Add(-4 * time.Hour),
			end:   now.Add(-3 * time.Hour),
			expected: []string{
				"tenant-TEST/metastore/2025-01-01T00:00:00Z.store",
				"tenant-TEST/metastore/2025-01-01T12:00:00Z.store",
			},
		},
		{
			name:  "start and end in different windows",
			start: now.Add(-12 * time.Hour),
			end:   now,
			expected: []string{
				"tenant-TEST/metastore/2025-01-01T00:00:00Z.store",
				"tenant-TEST/metastore/2025-01-01T12:00:00Z.store",
			},
		},
		{
			name:  "span several windows",
			start: now,
			end:   now.Add(48 * time.Hour),
			expected: []string{
				"tenant-TEST/metastore/2025-01-01T12:00:00Z.store",
				"tenant-TEST/metastore/2025-01-02T00:00:00Z.store",
				"tenant-TEST/metastore/2025-01-02T12:00:00Z.store",
				"tenant-TEST/metastore/2025-01-03T00:00:00Z.store",
				"tenant-TEST/metastore/2025-01-03T12:00:00Z.store",
			},
		},
		{
			name:  "start and end in different years",
			start: time.Date(2024, 12, 31, 3, 0, 0, 0, time.UTC),
			end:   time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC),
			expected: []string{
				"tenant-TEST/metastore/2024-12-31T00:00:00Z.store",
				"tenant-TEST/metastore/2024-12-31T12:00:00Z.store",
				"tenant-TEST/metastore/2025-01-01T00:00:00Z.store",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			iter := Iter(tenantID, tc.start, tc.end)
			actual := []string{}
			for store := range iter {
				actual = append(actual, store)
			}
			require.Equal(t, tc.expected, actual)
		})
	}
}

func TestDataObjectsPaths(t *testing.T) {
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

	// Create test data spanning multiple metastore windows
	now := time.Date(2025, 1, 1, 15, 0, 0, 0, time.UTC)

	// Add files in different time windows spanning multiple 12h periods
	testCases := []struct {
		path      string
		startTime time.Time
		endTime   time.Time
	}{
		{
			path:      "path1",
			startTime: now.Add(-1 * time.Hour),
			endTime:   now,
		},
		{
			path:      "path2",
			startTime: now.Add(-30 * time.Minute),
			endTime:   now,
		},
		{
			path:      "path3",
			startTime: now.Add(-13 * time.Hour), // Previous 12h window
			endTime:   now.Add(-12 * time.Hour),
		},
		{
			path:      "path4",
			startTime: now.Add(-14 * time.Hour), // Previous 12h window
			endTime:   now.Add(-13 * time.Hour),
		},
		{
			path:      "path5",
			startTime: now.Add(-25 * time.Hour), // Two windows back
			endTime:   now.Add(-24 * time.Hour),
		},
		{
			path:      "path6",
			startTime: now.Add(-36 * time.Hour), // Three windows back
			endTime:   now.Add(-35 * time.Hour),
		},
	}

	for _, tc := range testCases {
		err := m.UpdateMetastore(ctx, tc.path, dataobj.FlushStats{
			MinTimestamp: tc.startTime,
			MaxTimestamp: tc.endTime,
		})
		require.NoError(t, err)
	}

	t.Run("finds objects within current window", func(t *testing.T) {
		paths, err := ListDataObjects(ctx, bucket, tenantID, now.Add(-1*time.Hour), now)
		require.NoError(t, err)
		require.Len(t, paths, 2)
		require.Contains(t, paths, "path1")
		require.Contains(t, paths, "path2")
	})

	t.Run("finds objects across two 12h windows", func(t *testing.T) {
		paths, err := ListDataObjects(ctx, bucket, tenantID, now.Add(-14*time.Hour), now)
		require.NoError(t, err)
		require.Len(t, paths, 4)
		require.Contains(t, paths, "path1")
		require.Contains(t, paths, "path2")
		require.Contains(t, paths, "path3")
		require.Contains(t, paths, "path4")
	})

	t.Run("finds objects across three 12h windows", func(t *testing.T) {
		paths, err := ListDataObjects(ctx, bucket, tenantID, now.Add(-25*time.Hour), now)
		require.NoError(t, err)
		require.Len(t, paths, 5)
		require.Contains(t, paths, "path1")
		require.Contains(t, paths, "path2")
		require.Contains(t, paths, "path3")
		require.Contains(t, paths, "path4")
		require.Contains(t, paths, "path5")
	})

	t.Run("finds all objects across all windows", func(t *testing.T) {
		paths, err := ListDataObjects(ctx, bucket, tenantID, now.Add(-36*time.Hour), now)
		require.NoError(t, err)
		require.Len(t, paths, 6)
		require.Contains(t, paths, "path1")
		require.Contains(t, paths, "path2")
		require.Contains(t, paths, "path3")
		require.Contains(t, paths, "path4")
		require.Contains(t, paths, "path5")
		require.Contains(t, paths, "path6")
	})

	t.Run("returns empty list when no objects in range", func(t *testing.T) {
		paths, err := ListDataObjects(ctx, bucket, tenantID, now.Add(1*time.Hour), now.Add(2*time.Hour))
		require.NoError(t, err)
		require.Empty(t, paths)
	})

	t.Run("finds half of objects with partial window overlap", func(t *testing.T) {
		// Query starting from middle of first window to current time
		paths, err := ListDataObjects(ctx, bucket, tenantID, now.Add(-30*time.Hour), now)
		require.NoError(t, err)
		require.Len(t, paths, 5) // Should exclude path6 which is before -30h
		require.Contains(t, paths, "path1")
		require.Contains(t, paths, "path2")
		require.Contains(t, paths, "path3")
		require.Contains(t, paths, "path4")
		require.Contains(t, paths, "path5")
	})
}
