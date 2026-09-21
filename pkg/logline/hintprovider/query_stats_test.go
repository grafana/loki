package hintprovider

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
)

type noopReaderAt struct{}

func (noopReaderAt) ReadAt(p []byte, _ int64) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func TestQueryStats_ContextHelpers(t *testing.T) {
	ctx := NewQueryStatsContext(context.Background())
	stats := QueryStatsFromContext(ctx)
	require.NotNil(t, stats)
	require.Nil(t, QueryStatsFromContext(context.Background()))
}

// TestQueryStats_TrackingReader_ClassifiesBySections writes a minimal index,
// opens it, installs it as the classifier, then verifies that reads at
// representative offsets within each layout section are correctly counted.
func TestQueryStats_TrackingReader_ClassifiesBySections(t *testing.T) {
	for _, version := range logline.AllVersions() {
		t.Run(version, func(t *testing.T) {
			// Write a minimal index with one document and one term.
			path := t.TempDir() + "/test.lidx"
			docs := []format.DocumentMetadata{{ID: 0, MinTimeUnix: 1000, MaxTimeUnix: 2000}}
			w, err := logline.NewWriter(version, path, docs, nil)
			require.NoError(t, err)
			require.NoError(t, w.WriteTermBitmap([8]byte{'a', 'a', 'a', 'a', 'a', 'a'}, format.Bitmap{MatchesAll: true}))
			require.NoError(t, w.Close())

			fr, _, err := logline.OpenFile(path)
			require.NoError(t, err)
			defer fr.Close()

			fi, err := os.Stat(path)
			require.NoError(t, err)
			f, err := os.Open(path)
			require.NoError(t, err)
			defer f.Close()

			stats := NewQueryStats()
			tracked := newTrackingReaderAt(f, stats)
			tracked.SetClassifier(fr)

			var headerOff, metadataOff, termOff, postOff int64 = -1, -1, -1, -1
			for off := int64(0); off < fi.Size(); off++ {
				switch fr.ClassifyRead(off, 16) {
				case format.ReadSectionHeader:
					if headerOff < 0 {
						headerOff = off
					}
				case format.ReadSectionMetadata:
					if metadataOff < 0 {
						metadataOff = off
					}
				case format.ReadSectionTermDict:
					if termOff < 0 {
						termOff = off
					}
				case format.ReadSectionPostings:
					if postOff < 0 {
						postOff = off
					}
				}
			}
			require.GreaterOrEqual(t, headerOff, int64(0))
			require.GreaterOrEqual(t, metadataOff, int64(0))
			require.GreaterOrEqual(t, termOff, int64(0))
			require.GreaterOrEqual(t, postOff, int64(0))

			_, err = tracked.ReadAt(make([]byte, 16), headerOff)
			require.NoError(t, err)
			_, err = tracked.ReadAt(make([]byte, 16), metadataOff)
			require.NoError(t, err)
			_, err = tracked.ReadAt(make([]byte, 16), postOff)
			require.NoError(t, err)
			_, err = tracked.ReadAt(make([]byte, 16), termOff)
			require.NoError(t, err)

			snap := stats.Snapshot()
			require.Equal(t, int64(1), snap.HeaderReads)
			require.Equal(t, int64(1), snap.MetadataReads)
			require.Equal(t, int64(1), snap.BitmapReads)
			require.Equal(t, int64(1), snap.TermDictReads)
			require.Equal(t, int64(64), snap.TotalIOBytes)
		})
	}
}

// TestQueryStats_TrackingReader_UnknownBeforeClassifier verifies that reads
// issued before SetClassifier is called are recorded as unknown (not counted
// in any named section).
func TestQueryStats_TrackingReader_UnknownBeforeClassifier(t *testing.T) {
	stats := NewQueryStats()
	tracked := newTrackingReaderAt(noopReaderAt{}, stats)

	_, err := tracked.ReadAt(make([]byte, 64), 0)
	require.NoError(t, err)
	_, err = tracked.ReadAt(make([]byte, 64), 128)
	require.NoError(t, err)

	snap := stats.Snapshot()
	require.Equal(t, int64(0), snap.HeaderReads)
	require.Equal(t, int64(0), snap.MetadataReads)
	require.Equal(t, int64(0), snap.BitmapReads)
	require.Equal(t, int64(0), snap.TermDictReads)
	// But bytes are still tracked.
	require.Equal(t, int64(128), snap.TotalIOBytes)
}

func TestQueryStats_SnapshotIncludesConcurrencyAndPrefetch(t *testing.T) {
	stats := NewQueryStats()
	stats.SetWallTime(100 * time.Millisecond)

	stats.WorkerStarted()
	stats.WorkerStarted()
	stats.WorkerFinished(time.Now().Add(-80 * time.Millisecond))
	stats.WorkerFinished(time.Now().Add(-40 * time.Millisecond))
	stats.ObservePrefetchCall(false)
	stats.ObservePrefetchCall(true)

	snap := stats.Snapshot()
	require.Equal(t, int32(2), snap.PeakConcurrency)
	require.Greater(t, snap.EffectiveConcurrency, 1.0)
	require.Equal(t, int32(2), snap.PrefetchCalls)
	require.Equal(t, int32(1), snap.PrefetchTimeouts)
	require.Contains(t, stats.String(), "prefetch_calls=2")
}

func TestQueryStats_ObserveQueryMultiple(t *testing.T) {
	stats := NewQueryStats()
	stats.ObserveQueryMultiple(format.QueryMultipleReasonTermMiss, 1)
	stats.ObserveQueryMultiple(format.QueryMultipleReasonEmptyAnd, 2)
	stats.ObserveQueryMultiple(format.QueryMultipleReasonComplete, 3)

	snap := stats.Snapshot()
	require.Equal(t, int64(3), snap.IndexQueriesTotal)
	require.Equal(t, int64(1), snap.IndexQueriesTermMiss)
	require.Equal(t, int64(1), snap.IndexQueriesEmptyAnd)
	require.Equal(t, int64(1), snap.IndexQueriesPositive)
	require.Equal(t, int64(6), snap.TotalTermBatchesProcessed)
	require.Contains(t, stats.String(), "index_queries_term_miss=1")
}

func TestQueryStats_Merge(t *testing.T) {
	left := NewQueryStats()
	left.ObservePrefetchCall(true)
	left.SetWallTime(40 * time.Millisecond)
	left.WorkerStarted()
	left.WorkerFinished(time.Now().Add(-20 * time.Millisecond))

	right := NewQueryStats()
	right.ObservePrefetchCall(false)
	right.ObservePrefetchCall(true)
	right.observeRead(trackedReadHeader, 8, 1*time.Millisecond)
	right.observeRead(trackedReadMetadata, 8, 2*time.Millisecond)
	right.observeRead(trackedReadTermDict, 16, 5*time.Millisecond)
	right.observeRead(trackedReadBitmap, 32, 7*time.Millisecond)
	right.ObserveQueryMultiple(format.QueryMultipleReasonTermMiss, 2)
	right.ObserveQueryMultiple(format.QueryMultipleReasonComplete, 4)
	right.SetWallTime(120 * time.Millisecond)
	right.WorkerStarted()
	right.WorkerStarted()
	right.WorkerFinished(time.Now().Add(-30 * time.Millisecond))
	right.WorkerFinished(time.Now().Add(-10 * time.Millisecond))

	left.Merge(right)
	snap := left.Snapshot()

	require.Equal(t, int64(1), snap.HeaderReads)
	require.Equal(t, int64(1), snap.MetadataReads)
	require.Equal(t, int64(1), snap.TermDictReads)
	require.Equal(t, int64(1), snap.BitmapReads)
	require.Equal(t, int64(4), snap.ObjectStorageRequests)
	require.Equal(t, int64(64), snap.TotalIOBytes)
	require.Equal(t, 15*time.Millisecond, snap.TotalIOWait)
	require.Equal(t, int32(3), snap.PrefetchCalls)
	require.Equal(t, int32(2), snap.PrefetchTimeouts)
	require.Equal(t, int32(2), snap.PeakConcurrency)
	require.Equal(t, int64(2), snap.IndexQueriesTotal)
	require.Equal(t, int64(1), snap.IndexQueriesTermMiss)
	require.Equal(t, int64(1), snap.IndexQueriesPositive)
	require.Equal(t, int64(6), snap.TotalTermBatchesProcessed)
}
