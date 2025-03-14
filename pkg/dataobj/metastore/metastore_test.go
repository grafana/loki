package metastore

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/dskit/backoff"
	"github.com/thanos-io/objstore"

	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

func BenchmarkWriteMetastores(t *testing.B) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	tenantID := "test-tenant"

	m := NewUpdater(bucket, tenantID, log.NewNopLogger())

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
		err := m.Update(ctx, "path", flushStats[i%len(flushStats)])
		require.NoError(t, err)
	}

	require.Len(t, bucket.Objects(), 1)
}

func TestWriteMetastores(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	tenantID := "test-tenant"

	m := NewUpdater(bucket, tenantID, log.NewNopLogger())

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
	err := m.Update(ctx, "test-dataobj-path", flushStats)
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

	err = m.Update(ctx, "different-dataobj-path", flushResult2)
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
			iter := iterStorePaths(tenantID, tc.start, tc.end)
			actual := []string{}
			for store := range iter {
				actual = append(actual, store)
			}
			require.Equal(t, tc.expected, actual)
		})
	}
}

func TestDataObjectsPaths(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	tenantID := "test-tenant"
	ctx := user.InjectOrgID(context.Background(), tenantID)

	m := NewUpdater(bucket, tenantID, log.NewNopLogger())

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
		err := m.Update(ctx, tc.path, dataobj.FlushStats{
			MinTimestamp: tc.startTime,
			MaxTimestamp: tc.endTime,
		})
		require.NoError(t, err)
	}

	ms := NewObjectMetastore(bucket)

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
}

func TestObjectOverlapsRange(t *testing.T) {
	testPath := "test/path"

	tests := []struct {
		name       string
		objStart   time.Time
		objEnd     time.Time
		queryStart time.Time
		queryEnd   time.Time
		wantMatch  bool
		desc       string
	}{
		{
			name:       "object fully within query range",
			objStart:   time.Unix(11, 0),
			objEnd:     time.Unix(12, 0),
			queryStart: time.Unix(10, 0),
			queryEnd:   time.Unix(13, 0),
			wantMatch:  true,
			desc:       "query: [10,13], obj: [11,12]",
		},
		{
			name:       "object and query equal",
			objStart:   time.Unix(11, 0),
			objEnd:     time.Unix(12, 0),
			queryStart: time.Unix(11, 0),
			queryEnd:   time.Unix(122, 0),
			wantMatch:  true,
			desc:       "query: [11,12], obj: [11,12]",
		},
		{
			name:       "object fully contains query range",
			objStart:   time.Unix(10, 0),
			objEnd:     time.Unix(13, 0),
			queryStart: time.Unix(11, 0),
			queryEnd:   time.Unix(12, 0),
			wantMatch:  true,
			desc:       "query: [11,12], obj: [10,13]",
		},
		{
			name:       "object overlaps start of query range",
			objStart:   time.Unix(9, 0),
			objEnd:     time.Unix(11, 0),
			queryStart: time.Unix(10, 0),
			queryEnd:   time.Unix(12, 0),
			wantMatch:  true,
			desc:       "query: [10,12], obj: [9,11]",
		},
		{
			name:       "object overlaps end of query range",
			objStart:   time.Unix(11, 0),
			objEnd:     time.Unix(13, 0),
			queryStart: time.Unix(10, 0),
			queryEnd:   time.Unix(12, 0),
			wantMatch:  true,
			desc:       "query: [10,12], obj: [11,13]",
		},
		{
			name:       "object ends before query range",
			objStart:   time.Unix(8, 0),
			objEnd:     time.Unix(9, 0),
			queryStart: time.Unix(10, 0),
			queryEnd:   time.Unix(11, 0),
			wantMatch:  false,
			desc:       "query: [10,11], obj: [8,9]",
		},
		{
			name:       "object starts after query range",
			objStart:   time.Unix(12, 0),
			objEnd:     time.Unix(13, 0),
			queryStart: time.Unix(10, 0),
			queryEnd:   time.Unix(11, 0),
			wantMatch:  false,
			desc:       "query: [10,11], obj: [12,13]",
		},
		{
			name:       "object touches start of query range",
			objStart:   time.Unix(9, 0),
			objEnd:     time.Unix(10, 0),
			queryStart: time.Unix(10, 0),
			queryEnd:   time.Unix(11, 0),
			wantMatch:  true,
			desc:       "query: [10,11], obj: [9,10]",
		},
		{
			name:       "object touches end of query range",
			objStart:   time.Unix(11, 0),
			objEnd:     time.Unix(12, 0),
			queryStart: time.Unix(10, 0),
			queryEnd:   time.Unix(11, 0),
			wantMatch:  true,
			desc:       "query: [10,11], obj: [11,12]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create labels with timestamps in nanoseconds
			lbs := labels.Labels{
				{Name: "__start__", Value: strconv.FormatInt(tt.objStart.UnixNano(), 10)},
				{Name: "__end__", Value: strconv.FormatInt(tt.objEnd.UnixNano(), 10)},
				{Name: "__path__", Value: testPath},
			}

			gotMatch, gotPath := objectOverlapsRange(lbs, tt.queryStart, tt.queryEnd)
			require.Equal(t, tt.wantMatch, gotMatch, "overlap match failed for %s", tt.desc)
			if tt.wantMatch {
				require.Equal(t, testPath, gotPath, "path should match when ranges overlap")
			} else {
				require.Empty(t, gotPath, "path should be empty when ranges don't overlap")
			}
		})
	}
}
