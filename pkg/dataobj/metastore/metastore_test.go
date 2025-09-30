package metastore

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/dskit/user"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
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
		{
			ctx, cancel := context.WithTimeout(b.Context(), time.Second)
			defer cancel()
			// Test writing metastores
			stats := stats[i%len(stats)]
			err := toc.WriteEntry(ctx, "path", []multitenancy.TimeRange{
				{
					Tenant:  tenantID,
					MinTime: stats.MinTimestamp,
					MaxTime: stats.MaxTimestamp,
				},
			})
			require.NoError(b, err)
		}
	}

	require.Len(b, bucket.Objects(), 1)
}

func TestWriteMetastores(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	tenantID := "test-tenant"

	ctx, _ := context.WithTimeout(t.Context(), time.Second) //nolint:govet
	toc := NewTableOfContentsWriter(bucket, log.NewNopLogger())

	// Add test data spanning multiple metastore windows
	now := time.Date(2025, 1, 1, 15, 0, 0, 0, time.UTC)

	stats := flushStats{
		MinTimestamp: now.Add(-1 * time.Hour),
		MaxTimestamp: now,
	}

	require.Len(t, bucket.Objects(), 0)

	// Test writing metastores
	err := toc.WriteEntry(ctx, "test-dataobj-path", []multitenancy.TimeRange{
		{
			Tenant:  tenantID,
			MinTime: stats.MinTimestamp,
			MaxTime: stats.MaxTimestamp,
		},
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

	err = toc.WriteEntry(ctx, "different-dataobj-path", []multitenancy.TimeRange{
		{
			Tenant:  tenantID,
			MinTime: flushResult2.MinTimestamp,
			MaxTime: flushResult2.MaxTimestamp,
		},
	})
	require.NoError(t, err)

	require.Len(t, bucket.Objects(), 1)
	for _, obj := range bucket.Objects() {
		require.Greater(t, len(obj), originalSize)
	}
}

func TestIterTableOfContentsPaths(t *testing.T) {
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
			expected: []string{"tocs/2025-01-01T12_00_00Z.toc"},
		},
		{
			name:     "same start and end",
			start:    now,
			end:      now,
			expected: []string{"tocs/2025-01-01T12_00_00Z.toc"},
		},
		{
			name:  "begin at start of window",
			start: now.Add(-3 * time.Hour),
			end:   now,
			expected: []string{
				"tocs/2025-01-01T12_00_00Z.toc",
			},
		},
		{
			name:  "end at start of next window",
			start: now.Add(-4 * time.Hour),
			end:   now.Add(-3 * time.Hour),
			expected: []string{
				"tocs/2025-01-01T00_00_00Z.toc",
				"tocs/2025-01-01T12_00_00Z.toc",
			},
		},
		{
			name:  "start and end in different windows",
			start: now.Add(-12 * time.Hour),
			end:   now,
			expected: []string{
				"tocs/2025-01-01T00_00_00Z.toc",
				"tocs/2025-01-01T12_00_00Z.toc",
			},
		},
		{
			name:  "span several windows",
			start: now,
			end:   now.Add(48 * time.Hour),
			expected: []string{
				"tocs/2025-01-01T12_00_00Z.toc",
				"tocs/2025-01-02T00_00_00Z.toc",
				"tocs/2025-01-02T12_00_00Z.toc",
				"tocs/2025-01-03T00_00_00Z.toc",
				"tocs/2025-01-03T12_00_00Z.toc",
			},
		},
		{
			name:  "start and end in different years",
			start: time.Date(2024, 12, 31, 3, 0, 0, 0, time.UTC),
			end:   time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC),
			expected: []string{
				"tocs/2024-12-31T00_00_00Z.toc",
				"tocs/2024-12-31T12_00_00Z.toc",
				"tocs/2025-01-01T00_00_00Z.toc",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			iter := iterTableOfContentsPaths(tc.start, tc.end)
			actual := []string{}
			for path := range iter {
				actual = append(actual, path)
			}
			require.Equal(t, tc.expected, actual)
		})
	}
}

func TestDataObjectsPaths(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
	}{
		{name: "read"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := context.WithTimeout(t.Context(), time.Second) //nolint:govet
			ctx = user.InjectOrgID(ctx, tenantID)

			bucket := objstore.NewInMemBucket()
			tenantID := "test-tenant"

			toc := NewTableOfContentsWriter(bucket, log.NewNopLogger())

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
				err := toc.WriteEntry(ctx, tc.path, []multitenancy.TimeRange{
					{
						Tenant:  tenantID,
						MinTime: tc.startTime,
						MaxTime: tc.endTime,
					},
				})
				require.NoError(t, err)
			}

			ms := NewObjectMetastore(bucket, log.NewNopLogger(), nil)

			t.Run("finds objects within current window", func(t *testing.T) {
				paths, err := ms.DataObjects(ctx, now.Add(-1*time.Hour), now)
				require.NoError(t, err)
				require.Len(t, paths, 2)

				require.Contains(t, paths, "path1")
				require.Contains(t, paths, "path2")
			})

			t.Run("finds objects across two 12h windows", func(t *testing.T) {
				paths, err := ms.DataObjects(ctx, now.Add(-14*time.Hour), now)
				require.NoError(t, err)
				require.Len(t, paths, 4)
				require.Contains(t, paths, "path1")
				require.Contains(t, paths, "path2")
				require.Contains(t, paths, "path3")
				require.Contains(t, paths, "path4")
			})

			t.Run("finds objects across three 12h windows", func(t *testing.T) {
				paths, err := ms.DataObjects(ctx, now.Add(-25*time.Hour), now)
				require.NoError(t, err)
				require.Len(t, paths, 5)
				require.Contains(t, paths, "path1")
				require.Contains(t, paths, "path2")
				require.Contains(t, paths, "path3")
				require.Contains(t, paths, "path4")
				require.Contains(t, paths, "path5")
			})

			t.Run("finds all objects across all windows", func(t *testing.T) {
				paths, err := ms.DataObjects(ctx, now.Add(-36*time.Hour), now)
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
				metas, err := ms.DataObjects(ctx, now.Add(1*time.Hour), now.Add(2*time.Hour))
				require.NoError(t, err)
				require.Empty(t, metas)
			})

			t.Run("finds half of objects with partial window overlap", func(t *testing.T) {
				// Query starting from middle of first window to current time
				paths, err := ms.DataObjects(ctx, now.Add(-30*time.Hour), now)
				require.NoError(t, err)
				require.Len(t, paths, 5) // Should exclude path6 which is before -30h
				require.Contains(t, paths, "path1")
				require.Contains(t, paths, "path2")
				require.Contains(t, paths, "path3")
				require.Contains(t, paths, "path4")
				require.Contains(t, paths, "path5")
			})
		})
	}
}

type flushStats struct {
	MinTimestamp time.Time
	MaxTimestamp time.Time
}
