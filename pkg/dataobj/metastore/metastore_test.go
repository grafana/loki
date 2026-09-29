package metastore

import (
	"context"
	"strings"
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
			expected: []string{"tocs/2025-01-01T12_00_00Z/tenant.toc"},
		},
		{
			name:     "same start and end",
			start:    now,
			end:      now,
			expected: []string{"tocs/2025-01-01T12_00_00Z/tenant.toc"},
		},
		{
			name:  "begin at start of window",
			start: now.Add(-3 * time.Hour),
			end:   now,
			expected: []string{
				"tocs/2025-01-01T12_00_00Z/tenant.toc",
			},
		},
		{
			name:  "end at start of next window",
			start: now.Add(-4 * time.Hour),
			end:   now.Add(-3 * time.Hour),
			expected: []string{
				"tocs/2025-01-01T00_00_00Z/tenant.toc",
				"tocs/2025-01-01T12_00_00Z/tenant.toc",
			},
		},
		{
			name:  "start and end in different windows",
			start: now.Add(-12 * time.Hour),
			end:   now,
			expected: []string{
				"tocs/2025-01-01T00_00_00Z/tenant.toc",
				"tocs/2025-01-01T12_00_00Z/tenant.toc",
			},
		},
		{
			name:  "span several windows",
			start: now,
			end:   now.Add(48 * time.Hour),
			expected: []string{
				"tocs/2025-01-01T12_00_00Z/tenant.toc",
				"tocs/2025-01-02T00_00_00Z/tenant.toc",
				"tocs/2025-01-02T12_00_00Z/tenant.toc",
				"tocs/2025-01-03T00_00_00Z/tenant.toc",
				"tocs/2025-01-03T12_00_00Z/tenant.toc",
			},
		},
		{
			name:  "start and end in different years",
			start: time.Date(2024, 12, 31, 3, 0, 0, 0, time.UTC),
			end:   time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC),
			expected: []string{
				"tocs/2024-12-31T00_00_00Z/tenant.toc",
				"tocs/2024-12-31T12_00_00Z/tenant.toc",
				"tocs/2025-01-01T00_00_00Z/tenant.toc",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			iter := IterTableOfContentsPaths("tenant", tc.start, tc.end)
			actual := []string{}
			for path := range iter {
				actual = append(actual, path)
			}
			require.Equal(t, tc.expected, actual)
		})
	}
}

func TestTableOfContentsPath(t *testing.T) {
	window := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	require.Equal(t, "tocs/2025-01-01T12_00_00Z/", TableOfContentsWindowPrefix(window))
	require.Equal(t, "tocs/2025-01-01T12_00_00Z/tenant.toc", TableOfContentsPath("tenant", window))
}

func TestListTableOfContentsTenants(t *testing.T) {
	var (
		ctx    = t.Context()
		window = time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		other  = window.Add(MetastoreWindowSize)
		bucket = objstore.NewInMemBucket()
	)
	for _, name := range []string{
		TableOfContentsPath("tenant-b", window),
		TableOfContentsPath("tenant-a", window),
		TableOfContentsPath("tenant-c", other),
		TableOfContentsWindowPrefix(window) + "not-a-toc.txt",
		TableOfContentsWindowPrefix(window) + "nested/tenant-d.toc",
		// A shared ToC from before ToCs were split per tenant.
		"tocs/2025-01-01T12_00_00Z.toc",
	} {
		require.NoError(t, bucket.Upload(ctx, name, strings.NewReader("")))
	}

	tenants, err := ListTableOfContentsTenants(ctx, bucket, window)
	require.NoError(t, err)
	require.Equal(t, []string{"tenant-a", "tenant-b"}, tenants)

	tenants, err = ListTableOfContentsTenants(ctx, bucket, window.Add(-MetastoreWindowSize))
	require.NoError(t, err)
	require.Empty(t, tenants)
}

type flushStats struct {
	MinTimestamp time.Time
	MaxTimestamp time.Time
}
