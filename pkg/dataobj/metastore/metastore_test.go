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
