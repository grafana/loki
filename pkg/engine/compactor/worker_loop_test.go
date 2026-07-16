package compactor

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
)

func TestPhaseFlip(t *testing.T) {
	require.Equal(t, phaseLogMerge, phaseIndexMerge.flip())
	require.Equal(t, phaseIndexMerge, phaseLogMerge.flip())
}

func imWindow() time.Time {
	return time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
}

func TestRunIndexMergePhase_SingleIndexIsNoWork(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := objstore.NewInMemBucket()
	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{
		"acme": {{path: "indexes/a", start: window.Add(time.Hour), end: window.Add(2 * time.Hour)}},
	})
	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)))

	require.Equal(t, phaseOutcomeNoWork, c.runIndexMergePhase(ctx, "acme", window))
	require.Empty(t, replacer.snapshot(), "no swap for a single-index window")
}

func TestRunIndexMergePhase_MissingToCIsNoWork(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := objstore.NewInMemBucket() // no ToC written
	c := newTestCoordinator(t, bucket, &fakeRunner{}, &fakeReplacer{}, fixedClock(window.Add(time.Hour)))

	require.Equal(t, phaseOutcomeNoWork, c.runIndexMergePhase(ctx, "acme", window))
}

func TestRunIndexMergePhase_MultiIndexSwaps(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := objstore.NewInMemBucket()
	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{
		"acme": {
			{path: "indexes/a", start: window.Add(time.Hour), end: window.Add(2 * time.Hour)},
			{path: "indexes/b", start: window.Add(time.Hour), end: window.Add(2 * time.Hour)},
		},
	})
	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)))

	require.Equal(t, phaseOutcomeSwapped, c.runIndexMergePhase(ctx, "acme", window))
	require.Len(t, replacer.snapshot(), 1)
}

func logMergeBucket(ctx context.Context, t *testing.T, window time.Time, tenant string, paths []string) objstore.Bucket {
	t.Helper()
	bucket := objstore.NewInMemBucket()
	entries := make([]testIndex, 0, len(paths))
	for _, p := range paths {
		buildIndexWithStats(ctx, t, bucket, tenant, p, []stats.Stat{
			{ObjectPath: p + ".log", SectionIndex: 0, SortSchema: "service_name",
				Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 10, MaxTimestamp: 30, RowCount: 1, UncompressedSize: 100},
			{ObjectPath: p + ".log", SectionIndex: 0, SortSchema: "service_name",
				Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 20, MaxTimestamp: 40, RowCount: 1, UncompressedSize: 100},
		})
		entries = append(entries, testIndex{path: p, start: window.Add(time.Hour), end: window.Add(2 * time.Hour)})
	}
	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{tenant: entries})
	return bucket
}

func TestRunLogMergePhase_ZeroEntriesIsNoWork(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := objstore.NewInMemBucket()
	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{}) // no acme entries
	c := newTestCoordinator(t, bucket, &fakeRunner{}, &fakeReplacer{}, fixedClock(window.Add(time.Hour)))

	require.Equal(t, phaseOutcomeNoWork, c.runLogMergePhase(ctx, "acme", window))
}

func TestRunLogMergePhase_PerIndexSwaps(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := logMergeBucket(ctx, t, window, "acme", []string{"indexes/a", "indexes/b"})
	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)))

	require.Equal(t, phaseOutcomeSwapped, c.runLogMergePhase(ctx, "acme", window))
	require.Len(t, replacer.snapshot(), 2, "one swap per index")
	require.Positive(t, testutil.ToFloat64(c.metrics.indexesAddedTotal.WithLabelValues("acme")))
	require.Positive(t, testutil.ToFloat64(c.metrics.tasksTotal.WithLabelValues("acme")))
}

func TestRunLogMergePhase_CancelledMidIterationStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	window := imWindow()
	bucket := logMergeBucket(ctx, t, window, "acme", []string{"indexes/a", "indexes/b"})
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, &fakeRunner{}, replacer, fixedClock(window.Add(time.Hour)))

	cancel() // cancel before running: the phase must not proceed
	require.Equal(t, phaseOutcomeError, c.runLogMergePhase(ctx, "acme", window))
	require.Empty(t, replacer.snapshot(), "cancelled phase performs no swap")
}
