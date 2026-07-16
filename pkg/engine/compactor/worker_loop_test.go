package compactor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
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
