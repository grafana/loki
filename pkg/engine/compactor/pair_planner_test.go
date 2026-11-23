package compactor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
)

// testLogObject is a log object with one section that holds the streams with
// service_name values from minValue to maxValue.
type testLogObject struct {
	path               string
	minValue, maxValue string
}

// seedPairIndex writes an index of objects with the sort schema schema, and
// adds it to the ToC of imWindow.
func seedPairIndex(ctx context.Context, t *testing.T, bucket objstore.Bucket, path, schema string, objects ...testLogObject) {
	t.Helper()
	buildCurrentIndexWithStats(ctx, t, bucket, "acme", path, pairStats(schema, objects...))
}

// pairStats returns the stats rows of objects with the sort schema schema,
// which must name one label. Each object has 100 bytes per value.
func pairStats(schema string, objects ...testLogObject) []stats.Stat {
	label := strings.TrimPrefix(schema, "label:")
	var rows []stats.Stat
	for _, object := range objects {
		for _, value := range []string{object.minValue, object.maxValue} {
			rows = append(rows, stats.Stat{
				ObjectPath: object.path, SectionIndex: 0, SortSchema: schema,
				Labels:       map[string]string{label: value},
				MinTimestamp: imWindow().Add(time.Hour).UnixNano(), MaxTimestamp: imWindow().Add(2 * time.Hour).UnixNano(),
				RowCount: 1, UncompressedSize: 100,
			})
		}
	}
	return rows
}

// pairSourcePaths returns the paths of the source index objects of job.
func pairSourcePaths(job pairJob) []string { return job.replaces().sourcePaths() }

func TestPlanPairWindow(t *testing.T) {
	ctx := context.Background()
	now := imWindow().Add(time.Hour)
	const schema = "label:service_name"

	newCoordinator := func(t *testing.T, bucket objstore.Bucket) *coordinator {
		return newTestCoordinator(t, bucket, &fakeRunner{}, &fakeReplacer{}, fixedClock(now), newFakeLimits("acme"))
	}

	t.Run("merges K pairs of one level and leaves the pairs that do not fill a merge", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairIndex(ctx, t, bucket, "indexes/a", schema, testLogObject{"logs/a", "a", "z"})
		seedPairIndex(ctx, t, bucket, "indexes/b", schema, testLogObject{"logs/b", "a", "z"})
		seedPairIndex(ctx, t, bucket, "indexes/c", schema, testLogObject{"logs/c", "a", "z"})
		c := newCoordinator(t, bucket)

		plan, err := c.newPairPlanner().planWindow(ctx, "acme", imWindow(), leaseSnapshot{})
		require.NoError(t, err)
		require.Equal(t, 3, plan.indexRuns)
		require.Len(t, plan.jobs, 1)
		merge, ok := plan.jobs[0].(*pairMerge)
		require.True(t, ok)
		require.Len(t, merge.scope.sources, 2)
		require.Equal(t, 1, merge.level)
		require.Len(t, merge.logTasks, 1)
		require.Len(t, merge.logTasks[0].Runs, 2)
		require.Equal(t, int64(400), merge.logTasks[0].InputBytes)
	})

	t.Run("keeps the log objects of one log run together as one pair", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairIndex(ctx, t, bucket, "indexes/a", schema, testLogObject{"logs/a-0", "a", "b"}, testLogObject{"logs/a-1", "c", "d"})
		seedPairIndex(ctx, t, bucket, "indexes/b", schema, testLogObject{"logs/b", "a", "z"})
		c := newCoordinator(t, bucket)

		plan, err := c.newPairPlanner().planWindow(ctx, "acme", imWindow(), leaseSnapshot{})
		require.NoError(t, err)
		require.Len(t, plan.jobs, 1)
		merge, ok := plan.jobs[0].(*pairMerge)
		require.True(t, ok)
		require.ElementsMatch(t, []string{"indexes/a", "indexes/b"}, pairSourcePaths(merge))
	})

	t.Run("splits an index run whose log objects form more than one log run", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairIndex(ctx, t, bucket, "indexes/a", schema, testLogObject{"logs/a-0", "a", "c"}, testLogObject{"logs/a-1", "b", "d"})
		c := newCoordinator(t, bucket)

		plan, err := c.newPairPlanner().planWindow(ctx, "acme", imWindow(), leaseSnapshot{})
		require.NoError(t, err)
		require.Len(t, plan.jobs, 1)
		split, ok := plan.jobs[0].(*pairSplit)
		require.True(t, ok)
		require.Equal(t, []string{"indexes/a"}, pairSourcePaths(split))
		require.Len(t, split.logRuns, 2)
	})

	t.Run("skips the index runs that a running job leases", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairIndex(ctx, t, bucket, "indexes/a", schema, testLogObject{"logs/a", "a", "z"})
		seedPairIndex(ctx, t, bucket, "indexes/b", schema, testLogObject{"logs/b", "a", "z"})
		c := newCoordinator(t, bucket)

		plan, err := c.newPairPlanner().planWindow(ctx, "acme", imWindow(), leaseSnapshot{indexes: map[string]struct{}{"indexes/a": {}}})
		require.NoError(t, err)
		require.Equal(t, 2, plan.indexRuns)
		require.Empty(t, plan.jobs)
	})

	t.Run("never merges pairs of different stream layouts", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairIndex(ctx, t, bucket, "indexes/a", schema, testLogObject{"logs/a", "a", "z"})
		seedPairIndex(ctx, t, bucket, "indexes/b", "label:app", testLogObject{"logs/b", "a", "z"})
		c := newCoordinator(t, bucket)

		plan, err := c.newPairPlanner().planWindow(ctx, "acme", imWindow(), leaseSnapshot{})
		require.NoError(t, err)
		require.Empty(t, plan.jobs)
	})

	t.Run("skips every index run that shares a log object with another index run", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairIndex(ctx, t, bucket, "indexes/a", schema, testLogObject{"logs/shared", "a", "z"})
		seedPairIndex(ctx, t, bucket, "indexes/b", schema, testLogObject{"logs/shared", "a", "z"})
		seedPairIndex(ctx, t, bucket, "indexes/c", schema, testLogObject{"logs/c", "a", "z"})
		seedPairIndex(ctx, t, bucket, "indexes/d", schema, testLogObject{"logs/d", "a", "z"})
		c := newCoordinator(t, bucket)

		plan, err := c.newPairPlanner().planWindow(ctx, "acme", imWindow(), leaseSnapshot{})
		require.NoError(t, err)
		require.Len(t, plan.jobs, 1)
		require.ElementsMatch(t, []string{"indexes/c", "indexes/d"}, pairSourcePaths(plan.jobs[0]))
		require.Equal(t, float64(2), testutil.ToFloat64(c.metrics.pairSkippedIndexesTotal.WithLabelValues("acme", pairSkipReasonSharedLogObject)))
	})

	t.Run("skips an index run that shares a log object with a planned job", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairIndex(ctx, t, bucket, "indexes/a", schema, testLogObject{"logs/a", "a", "z"})
		seedPairIndex(ctx, t, bucket, "indexes/b", schema, testLogObject{"logs/b", "a", "z"})
		c := newCoordinator(t, bucket)

		plan, err := c.newPairPlanner().planWindow(ctx, "acme", imWindow(), leaseSnapshot{logObjects: map[string]struct{}{"logs/a": {}}})
		require.NoError(t, err)
		require.Empty(t, plan.jobs)
		require.Equal(t, float64(1), testutil.ToFloat64(c.metrics.pairSkippedIndexesTotal.WithLabelValues("acme", pairSkipReasonSharedLogObject)))
	})

	t.Run("plans no job in a window where the stats of an index run fail to read", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairIndex(ctx, t, bucket, "indexes/a", schema, testLogObject{"logs/a", "a", "z"})
		seedPairIndex(ctx, t, bucket, "indexes/b", schema, testLogObject{"logs/b", "a", "z"})
		seedPairIndex(ctx, t, bucket, "indexes/c", schema, testLogObject{"logs/c", "a", "z"})
		failing := indexReadBucket{Bucket: bucket, onRead: func(_ context.Context, name string) error {
			if name == "indexes/a" {
				return errors.New("read failed")
			}
			return nil
		}}
		c := newCoordinator(t, failing)

		plan, err := c.newPairPlanner().planWindow(ctx, "acme", imWindow(), leaseSnapshot{})
		require.NoError(t, err)
		require.Equal(t, 1, plan.readFailures)
		require.Equal(t, 3, plan.indexRuns)
		require.Empty(t, plan.jobs)
		require.Equal(t, float64(1), testutil.ToFloat64(c.metrics.pairSkippedIndexesTotal.WithLabelValues("acme", pairSkipReasonReadFailed)))
	})

	t.Run("skips and counts a corrupt index run", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		buildMixedLayoutIndex(ctx, t, bucket, "acme", "indexes/corrupt")
		writeToCReferencing(ctx, t, bucket, "acme", imWindow(), "indexes/corrupt")
		c := newCoordinator(t, bucket)

		plan, err := c.newPairPlanner().planWindow(ctx, "acme", imWindow(), leaseSnapshot{})
		require.NoError(t, err)
		require.Empty(t, plan.jobs)
		require.Zero(t, plan.readFailures)
		require.Equal(t, float64(1), testutil.ToFloat64(c.metrics.corruptIndexesTotal.WithLabelValues("acme", phaseLabelPairMerge)))
	})

	t.Run("skips and counts an index run without stats rows", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		buildOverlappingPostingsIndex(ctx, t, bucket, "acme", "indexes/postings-only")
		writeToCReferencing(ctx, t, bucket, "acme", imWindow(), "indexes/postings-only")
		c := newCoordinator(t, bucket)

		plan, err := c.newPairPlanner().planWindow(ctx, "acme", imWindow(), leaseSnapshot{})
		require.NoError(t, err)
		require.Empty(t, plan.jobs)
		require.Equal(t, float64(1), testutil.ToFloat64(c.metrics.pairSkippedIndexesTotal.WithLabelValues("acme", pairSkipReasonNoStats)))
	})

	t.Run("skips a layout group below the minimum compaction size", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairIndex(ctx, t, bucket, "indexes/a", schema, testLogObject{"logs/a", "a", "z"})
		seedPairIndex(ctx, t, bucket, "indexes/b", schema, testLogObject{"logs/b", "a", "z"})
		c := newCoordinator(t, bucket)
		c.cfg.LogMinCompactionSize = flagext.Bytes(401)

		plan, err := c.newPairPlanner().planWindow(ctx, "acme", imWindow(), leaseSnapshot{})
		require.NoError(t, err)
		require.Empty(t, plan.jobs)
	})

	t.Run("skips a merge with a sort prefix above the maximum key size", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairIndex(ctx, t, bucket, "indexes/a", schema, testLogObject{"logs/a", "a", "z"})
		seedPairIndex(ctx, t, bucket, "indexes/b", schema, testLogObject{"logs/b", "a", "z"})
		c := newCoordinator(t, bucket)
		c.cfg.LogMaxKeySize = flagext.Bytes(199)

		plan, err := c.newPairPlanner().planWindow(ctx, "acme", imWindow(), leaseSnapshot{})
		require.NoError(t, err)
		require.Empty(t, plan.jobs)
		require.Equal(t, float64(1), testutil.ToFloat64(c.metrics.logMergeSkippedMergesTotal.WithLabelValues("acme")))
	})

	t.Run("splits a merge above the maximum task size into log tasks by sort prefix", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairIndex(ctx, t, bucket, "indexes/a", schema, testLogObject{"logs/a", "a", "z"})
		seedPairIndex(ctx, t, bucket, "indexes/b", schema, testLogObject{"logs/b", "a", "z"})
		c := newCoordinator(t, bucket)
		c.cfg.LogMaxTaskSize = flagext.Bytes(200)

		plan, err := c.newPairPlanner().planWindow(ctx, "acme", imWindow(), leaseSnapshot{})
		require.NoError(t, err)
		require.Len(t, plan.jobs, 1)
		merge, ok := plan.jobs[0].(*pairMerge)
		require.True(t, ok)
		require.Len(t, merge.logTasks, 2)
		var inputBytes int64
		for _, task := range merge.logTasks {
			inputBytes += task.InputBytes
		}
		require.Equal(t, int64(400), inputBytes)
	})

	t.Run("has no work in a window without a ToC", func(t *testing.T) {
		c := newCoordinator(t, objstore.NewInMemBucket())

		plan, err := c.newPairPlanner().planWindow(ctx, "acme", imWindow(), leaseSnapshot{})
		require.NoError(t, err)
		require.Equal(t, pairWindowPlan{}, plan)
	})
}
