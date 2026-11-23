package compactor

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"go.uber.org/atomic"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

const pairTestSchema = "label:service_name"

// newPairTestCoordinator returns a coordinator of the coupled planner for
// tenant acme, with log compaction enabled.
func newPairTestCoordinator(t *testing.T, bucket objstore.Bucket, runner *fakeRunner, replacer *fakeReplacer) *coordinator {
	t.Helper()
	limits := newFakeLimits("acme")
	limits.setLog("acme", true)
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(imWindow().Add(time.Hour)), limits)
	c.cfg.PlanVersion = planVersionCoupled
	return c
}

// seedPairs adds a pair to the ToC of imWindow for each path. The log object
// of each pair holds 200 bytes, in the sort prefixes a and z.
func seedPairs(t *testing.T, bucket objstore.Bucket, paths ...string) {
	t.Helper()
	for _, path := range paths {
		seedPairIndex(t.Context(), t, bucket, path, pairTestSchema, testLogObject{path + ".log", "a", "z"})
	}
}

// seedTaskIndex writes an index at path for the log objects in logBytes, as a
// compaction task writes it. Each log object holds its bytes in one section.
// It returns the stats rows of the index.
func seedTaskIndex(t *testing.T, bucket objstore.Bucket, path string, logBytes map[string]int64) []stats.Stat {
	t.Helper()
	var rows []stats.Stat
	for _, object := range slices.Sorted(func(yield func(string) bool) {
		for object := range logBytes {
			if !yield(object) {
				return
			}
		}
	}) {
		rows = append(rows, stats.Stat{
			ObjectPath: object, SectionIndex: 0, SortSchema: pairTestSchema,
			Labels:       map[string]string{"service_name": "a"},
			MinTimestamp: imWindow().Add(time.Hour).UnixNano(), MaxTimestamp: imWindow().Add(2 * time.Hour).UnixNano(),
			RowCount: 1, UncompressedSize: logBytes[object],
		})
	}
	buildIndexWithStatsAndPostings(t.Context(), t, bucket, "acme", path, rows)
	return rows
}

// pairResponder answers the tasks of the coupled planner by actor.
type pairResponder struct {
	// logMerges are the artifact paths of the LogMerge tasks, in call order.
	logMerges []string
	// onLogMerge, when set, runs before each LogMerge task returns. n is the
	// 0-based number of the call.
	onLogMerge func(n int)

	indexMerge    string
	indexMergeErr error

	logMergeCalls atomic.Int32
}

func (r *pairResponder) respond(_ context.Context, opts workflow.Options, _ *physical.Plan) ([]v2.ResultArtifact, error) {
	switch actor := opts.Actor[len(opts.Actor)-1]; actor {
	case "log-merge":
		n := int(r.logMergeCalls.Inc()) - 1
		if r.onLogMerge != nil {
			r.onLogMerge(n)
		}
		if n >= len(r.logMerges) {
			return nil, fmt.Errorf("log merge call %d has no artifact", n)
		}
		return []v2.ResultArtifact{{Path: r.logMerges[n]}}, nil
	case "index-merge":
		if r.indexMergeErr != nil {
			return nil, r.indexMergeErr
		}
		return []v2.ResultArtifact{{Path: r.indexMerge}}, nil
	default:
		return nil, fmt.Errorf("unexpected actor %s", actor)
	}
}

// waitForQueuedWaits blocks until slots holds n waits, or for at most five
// seconds. It does not fail the test, so a runner may call it outside the test
// goroutine.
func waitForQueuedWaits(slots *taskSlots, n int) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		slots.slotsMu.Lock()
		queued := len(slots.waits)
		slots.slotsMu.Unlock()
		if queued >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func actorsOf(calls []runCall) []string {
	actors := make([]string, len(calls))
	for i, call := range calls {
		actors[i] = call.opts.Actor[len(call.opts.Actor)-1]
	}
	return actors
}

func pairJobsCount(c *coordinator, kind, outcome string) float64 {
	return testutil.ToFloat64(c.metrics.pairJobsTotal.WithLabelValues("acme", kind, outcome))
}

func TestPairCompactorScan(t *testing.T) {
	// splitLogMerge seeds two pairs whose merge runs as two LogMerge tasks
	// of 200 bytes each, and the indexes of those tasks.
	splitLogMerge := func(t *testing.T, bucket objstore.Bucket) []string {
		t.Helper()
		seedPairs(t, bucket, "indexes/a", "indexes/b")
		seedTaskIndex(t, bucket, "indexes/out-0", map[string]int64{"logs/out-0": 200})
		seedTaskIndex(t, bucket, "indexes/out-1", map[string]int64{"logs/out-1": 200})
		return []string{"indexes/out-0", "indexes/out-1"}
	}

	t.Run("a merge with one log task publishes the index of the task as the new pair", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairs(t, bucket, "indexes/a", "indexes/b")
		seedTaskIndex(t, bucket, "indexes/out", map[string]int64{"logs/out": 400})
		responder := &pairResponder{logMerges: []string{"indexes/out"}}
		runner := &fakeRunner{respond: responder.respond}
		replacer := &fakeReplacer{swapped: true}
		c := newPairTestCoordinator(t, bucket, runner, replacer)
		p := c.newPairCompactor("acme")

		require.Equal(t, 1, p.scan(t.Context()))
		p.jobs.Wait()

		require.Equal(t, []string{"log-merge"}, actorsOf(runner.snapshot()))
		swaps := replacer.snapshot()
		require.Len(t, swaps, 1)
		require.ElementsMatch(t, []string{"indexes/a", "indexes/b"}, swaps[0].oldPaths)
		require.Len(t, swaps[0].newRuns, 1)
		require.Len(t, swaps[0].newEntries, 1)
		entry := swaps[0].newEntries[0]
		require.Equal(t, "indexes/out", entry.Path)
		require.Equal(t, imWindow().Add(time.Hour), entry.StartTime)
		require.Equal(t, imWindow().Add(2*time.Hour), entry.EndTime)
		require.Equal(t, indexpointers.NewShardBuckets(streams.DefaultShardLayout()), entry.ShardBuckets)
		require.Equal(t, float64(1), pairJobsCount(c, pairJobKindMerge, pairJobOutcomeCommitted))
		require.Equal(t, float64(2), testutil.ToFloat64(c.metrics.indexesRemovedTotal.WithLabelValues("acme")))
		require.Equal(t, leaseSnapshot{indexes: map[string]struct{}{}, logObjects: map[string]struct{}{}}, p.leases.snapshot())
	})

	t.Run("a merge whose log tasks write several indexes publishes their merged index run", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		logIndexes := splitLogMerge(t, bucket)
		seedTaskIndex(t, bucket, "indexes/merged", map[string]int64{"logs/out-0": 200, "logs/out-1": 200})
		responder := &pairResponder{logMerges: logIndexes, indexMerge: "indexes/merged"}
		runner := &fakeRunner{respond: responder.respond}
		replacer := &fakeReplacer{swapped: true}
		c := newPairTestCoordinator(t, bucket, runner, replacer)
		c.cfg.LogMaxTaskSize = flagext.Bytes(200)
		p := c.newPairCompactor("acme")

		require.Equal(t, 1, p.scan(t.Context()))
		p.jobs.Wait()

		calls := runner.snapshot()
		require.Equal(t, []string{"log-merge", "log-merge", "index-merge"}, actorsOf(calls))
		root, err := calls[2].plan.Root()
		require.NoError(t, err)
		merge, ok := root.(*physical.IndexMerge)
		require.True(t, ok)
		var sources []string
		for _, run := range merge.Runs {
			sources = append(sources, run.Sections[0].ObjectPath)
		}
		require.ElementsMatch(t, logIndexes, sources)
		swaps := replacer.snapshot()
		require.Len(t, swaps, 1)
		require.Equal(t, "indexes/merged", swaps[0].newEntries[0].Path)
		require.Equal(t, float64(3), testutil.ToFloat64(c.metrics.tasksTotal.WithLabelValues("acme")))
	})

	t.Run("a failed index merge after the log merge publishes nothing and frees the sources", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		responder := &pairResponder{logMerges: splitLogMerge(t, bucket), indexMergeErr: errors.New("worker failed")}
		runner := &fakeRunner{respond: responder.respond}
		replacer := &fakeReplacer{swapped: true}
		c := newPairTestCoordinator(t, bucket, runner, replacer)
		c.cfg.LogMaxTaskSize = flagext.Bytes(200)
		p := c.newPairCompactor("acme")

		require.Equal(t, 1, p.scan(t.Context()))
		p.jobs.Wait()

		require.Empty(t, replacer.snapshot())
		require.Equal(t, float64(1), pairJobsCount(c, pairJobKindMerge, pairJobOutcomeFailed))
		require.Empty(t, p.leases.snapshot().indexes)
	})

	t.Run("an index merge that drops a log section of the log merge fails and publishes nothing", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		logIndexes := splitLogMerge(t, bucket)
		seedTaskIndex(t, bucket, "indexes/merged", map[string]int64{"logs/out-0": 200})
		responder := &pairResponder{logMerges: logIndexes, indexMerge: "indexes/merged"}
		runner := &fakeRunner{respond: responder.respond}
		replacer := &fakeReplacer{swapped: true}
		c := newPairTestCoordinator(t, bucket, runner, replacer)
		c.cfg.LogMaxTaskSize = flagext.Bytes(200)
		p := c.newPairCompactor("acme")

		require.Equal(t, 1, p.scan(t.Context()))
		p.jobs.Wait()

		require.Empty(t, replacer.snapshot())
		require.Equal(t, float64(1), pairJobsCount(c, pairJobKindMerge, pairJobOutcomeFailed))
	})

	t.Run("a log merge whose index does not count the input bytes fails and publishes nothing", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairs(t, bucket, "indexes/a", "indexes/b")
		seedTaskIndex(t, bucket, "indexes/out", map[string]int64{"logs/out": 300})
		responder := &pairResponder{logMerges: []string{"indexes/out"}}
		runner := &fakeRunner{respond: responder.respond}
		replacer := &fakeReplacer{swapped: true}
		c := newPairTestCoordinator(t, bucket, runner, replacer)
		p := c.newPairCompactor("acme")

		require.Equal(t, 1, p.scan(t.Context()))
		p.jobs.Wait()

		require.Empty(t, replacer.snapshot())
		require.Equal(t, float64(1), pairJobsCount(c, pairJobKindMerge, pairJobOutcomeFailed))
	})

	t.Run("a log merge that returns no artifact fails and publishes nothing", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairs(t, bucket, "indexes/a", "indexes/b")
		runner := &fakeRunner{respond: func(context.Context, workflow.Options, *physical.Plan) ([]v2.ResultArtifact, error) {
			return nil, nil
		}}
		replacer := &fakeReplacer{swapped: true}
		c := newPairTestCoordinator(t, bucket, runner, replacer)
		p := c.newPairCompactor("acme")

		require.Equal(t, 1, p.scan(t.Context()))
		p.jobs.Wait()

		require.Empty(t, replacer.snapshot())
		require.Equal(t, float64(1), pairJobsCount(c, pairJobKindMerge, pairJobOutcomeFailed))
	})

	splitResponder := func(t *testing.T, bucket objstore.Bucket, filteredIndexes map[string]string) *fakeRunner {
		t.Helper()
		return &fakeRunner{respond: func(_ context.Context, _ workflow.Options, plan *physical.Plan) ([]v2.ResultArtifact, error) {
			root, err := plan.Root()
			if err != nil {
				return nil, err
			}
			filter, ok := root.(*physical.IndexFilter)
			if !ok || len(filter.ObjectPaths) != 1 {
				return nil, fmt.Errorf("want an index filter of one log object, got %v", root)
			}
			return []v2.ResultArtifact{{Path: filteredIndexes[filter.ObjectPaths[0]]}}, nil
		}}
	}
	seedSplitSource := func(t *testing.T, bucket objstore.Bucket) {
		t.Helper()
		seedPairIndex(t.Context(), t, bucket, "indexes/a", pairTestSchema, testLogObject{"logs/a-0", "a", "c"}, testLogObject{"logs/a-1", "b", "d"})
	}
	filteredIndex := func(t *testing.T, bucket objstore.Bucket, path string, objects ...testLogObject) string {
		t.Helper()
		buildIndex(t.Context(), t, bucket, testIndexObject{tenant: "acme", path: path, sectionSize: 1 << 21, shardBuckets: streams.DefaultShardBuckets, stats: pairStats(pairTestSchema, objects...)})
		return path
	}

	t.Run("a split runs one index filter per log run and publishes one index run per filter", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedSplitSource(t, bucket)
		runner := splitResponder(t, bucket, map[string]string{
			"logs/a-0": filteredIndex(t, bucket, "indexes/filtered-0", testLogObject{"logs/a-0", "a", "c"}),
			"logs/a-1": filteredIndex(t, bucket, "indexes/filtered-1", testLogObject{"logs/a-1", "b", "d"}),
		})
		replacer := &fakeReplacer{swapped: true}
		c := newPairTestCoordinator(t, bucket, runner, replacer)
		p := c.newPairCompactor("acme")

		require.Equal(t, 1, p.scan(t.Context()))
		p.jobs.Wait()

		calls := runner.snapshot()
		require.Equal(t, []string{"index-filter", "index-filter"}, actorsOf(calls))
		var keptObjects []string
		for _, call := range calls {
			root, err := call.plan.Root()
			require.NoError(t, err)
			filter, ok := root.(*physical.IndexFilter)
			require.True(t, ok)
			require.Equal(t, []string{"indexes/a"}, filter.SourceIndexPaths)
			keptObjects = append(keptObjects, filter.ObjectPaths...)
		}
		require.ElementsMatch(t, []string{"logs/a-0", "logs/a-1"}, keptObjects)
		swaps := replacer.snapshot()
		require.Len(t, swaps, 1)
		require.Equal(t, []string{"indexes/a"}, swaps[0].oldPaths)
		require.Len(t, swaps[0].newRuns, 2)
		for _, entry := range swaps[0].newEntries {
			require.Equal(t, imWindow().Add(time.Hour), entry.StartTime)
			require.Equal(t, imWindow().Add(2*time.Hour), entry.EndTime)
			require.Equal(t, indexpointers.NewShardBuckets(streams.DefaultShardLayout()), entry.ShardBuckets)
		}
		require.Equal(t, float64(1), pairJobsCount(c, pairJobKindSplit, pairJobOutcomeCommitted))
	})

	t.Run("a split whose filters drop a log object fails and publishes nothing", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedSplitSource(t, bucket)
		filtered := filteredIndex(t, bucket, "indexes/filtered-0", testLogObject{"logs/a-0", "a", "c"})
		runner := splitResponder(t, bucket, map[string]string{"logs/a-0": filtered, "logs/a-1": filtered})
		replacer := &fakeReplacer{swapped: true}
		c := newPairTestCoordinator(t, bucket, runner, replacer)
		p := c.newPairCompactor("acme")

		require.Equal(t, 1, p.scan(t.Context()))
		p.jobs.Wait()

		require.Empty(t, replacer.snapshot())
		require.Equal(t, float64(1), pairJobsCount(c, pairJobKindSplit, pairJobOutcomeFailed))
	})

	t.Run("a failed task publishes nothing and frees the sources for the next scan", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairs(t, bucket, "indexes/a", "indexes/b")
		runner := &fakeRunner{err: errors.New("worker failed")}
		replacer := &fakeReplacer{swapped: true}
		c := newPairTestCoordinator(t, bucket, runner, replacer)
		p := c.newPairCompactor("acme")

		require.Equal(t, 1, p.scan(t.Context()))
		p.jobs.Wait()

		require.Empty(t, replacer.snapshot())
		require.Equal(t, float64(1), pairJobsCount(c, pairJobKindMerge, pairJobOutcomeFailed))
		require.Empty(t, p.leases.snapshot().indexes)
		require.Equal(t, 1, p.scan(t.Context()))
		p.jobs.Wait()
	})

	t.Run("a ToC swap error counts a failed job and frees the sources", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairs(t, bucket, "indexes/a", "indexes/b")
		seedTaskIndex(t, bucket, "indexes/out", map[string]int64{"logs/out": 400})
		responder := &pairResponder{logMerges: []string{"indexes/out"}}
		replacer := &fakeReplacer{err: errors.New("swap failed")}
		c := newPairTestCoordinator(t, bucket, &fakeRunner{respond: responder.respond}, replacer)
		p := c.newPairCompactor("acme")

		require.Equal(t, 1, p.scan(t.Context()))
		p.jobs.Wait()

		require.Len(t, replacer.snapshot(), 1)
		require.Equal(t, float64(1), pairJobsCount(c, pairJobKindMerge, pairJobOutcomeFailed))
		require.Empty(t, p.leases.snapshot().indexes)
	})

	t.Run("a swap that writes nothing counts a noop", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairs(t, bucket, "indexes/a", "indexes/b")
		seedTaskIndex(t, bucket, "indexes/out", map[string]int64{"logs/out": 400})
		responder := &pairResponder{logMerges: []string{"indexes/out"}}
		c := newPairTestCoordinator(t, bucket, &fakeRunner{respond: responder.respond}, &fakeReplacer{swapped: false})
		p := c.newPairCompactor("acme")

		require.Equal(t, 1, p.scan(t.Context()))
		p.jobs.Wait()

		require.Equal(t, float64(1), pairJobsCount(c, pairJobKindMerge, pairJobOutcomeNoop))
		require.Zero(t, testutil.ToFloat64(c.metrics.indexesRemovedTotal.WithLabelValues("acme")))
	})

	t.Run("a scan skips the sources of a planned job", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairs(t, bucket, "indexes/a", "indexes/b")
		seedTaskIndex(t, bucket, "indexes/out", map[string]int64{"logs/out": 400})
		unblock := make(chan struct{})
		var unblockOnce sync.Once
		defer unblockOnce.Do(func() { close(unblock) })
		responder := &pairResponder{logMerges: []string{"indexes/out"}, onLogMerge: func(int) { <-unblock }}
		replacer := &fakeReplacer{swapped: true}
		c := newPairTestCoordinator(t, bucket, &fakeRunner{respond: responder.respond}, replacer)
		p := c.newPairCompactor("acme")

		require.Equal(t, 1, p.scan(t.Context()))
		require.Equal(t, 0, p.scan(t.Context()))
		unblockOnce.Do(func() { close(unblock) })
		p.jobs.Wait()
		require.Len(t, replacer.snapshot(), 1)
	})

	t.Run("a started job runs all its log tasks before a job that did not start", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairs(t, bucket, "indexes/a", "indexes/b", "indexes/c", "indexes/d")
		var (
			p         *pairCompactor
			firstCall sync.Once
		)
		runner := &fakeRunner{respond: func(context.Context, workflow.Options, *physical.Plan) ([]v2.ResultArtifact, error) {
			firstCall.Do(func() { waitForQueuedWaits(p.slots, 2) })
			return []v2.ResultArtifact{{Path: "indexes/missing"}}, nil
		}}
		c := newPairTestCoordinator(t, bucket, runner, &fakeReplacer{swapped: true})
		c.cfg.MaxRunningCompactionTasks = 1
		c.cfg.LogMaxTaskSize = flagext.Bytes(200)
		p = c.newPairCompactor("acme")

		require.Equal(t, 2, p.scan(t.Context()))
		p.jobs.Wait()

		calls := runner.snapshot()
		require.Equal(t, []string{"log-merge", "log-merge", "log-merge", "log-merge"}, actorsOf(calls))
		jobOf := func(call runCall) string {
			return mergeNodeRuns(t, call.plan)[0].Sections[0].ObjectPath
		}
		firstJob := []string{jobOf(calls[0]), jobOf(calls[1])}
		secondJob := []string{jobOf(calls[2]), jobOf(calls[3])}
		for _, object := range secondJob {
			require.NotContains(t, firstJob, object)
		}
	})

	t.Run("at shutdown a started job commits and a job that did not start stops", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairs(t, bucket, "indexes/a", "indexes/b", "indexes/c", "indexes/d")
		seedTaskIndex(t, bucket, "indexes/out", map[string]int64{"logs/out": 400})
		started := make(chan struct{}, 2)
		unblock := make(chan struct{})
		var unblockOnce sync.Once
		defer unblockOnce.Do(func() { close(unblock) })
		responder := &pairResponder{logMerges: []string{"indexes/out", "indexes/out"}, onLogMerge: func(int) {
			started <- struct{}{}
			<-unblock
		}}
		runner := &fakeRunner{respond: responder.respond}
		replacer := &fakeReplacer{swapped: true}
		c := newPairTestCoordinator(t, bucket, runner, replacer)
		c.cfg.MaxRunningCompactionTasks = 1
		p := c.newPairCompactor("acme")
		scanCtx, cancel := context.WithCancel(t.Context())

		require.Equal(t, 2, p.scan(scanCtx))
		<-started
		cancel()
		unblockOnce.Do(func() { close(unblock) })
		p.jobs.Wait()

		require.Len(t, runner.snapshot(), 1)
		require.Len(t, replacer.snapshot(), 1)
	})

	t.Run("at shutdown a merge whose log tasks finished still runs its index merge and commits", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		logIndexes := splitLogMerge(t, bucket)
		seedTaskIndex(t, bucket, "indexes/merged", map[string]int64{"logs/out-0": 200, "logs/out-1": 200})
		scanCtx, cancel := context.WithCancel(t.Context())
		responder := &pairResponder{logMerges: logIndexes, indexMerge: "indexes/merged", onLogMerge: func(n int) {
			if n == 1 {
				cancel()
			}
		}}
		runner := &fakeRunner{respond: responder.respond}
		replacer := &fakeReplacer{swapped: true}
		c := newPairTestCoordinator(t, bucket, runner, replacer)
		c.cfg.MaxRunningCompactionTasks = 1
		c.cfg.LogMaxTaskSize = flagext.Bytes(200)
		p := c.newPairCompactor("acme")

		require.Equal(t, 1, p.scan(scanCtx))
		p.jobs.Wait()

		require.Equal(t, []string{"log-merge", "log-merge", "index-merge"}, actorsOf(runner.snapshot()))
		require.Len(t, replacer.snapshot(), 1)
	})

	t.Run("a scan sets the last plan timestamp only when it read every index run", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairs(t, bucket, "indexes/a")
		failRead := atomic.NewBool(true)
		failing := indexReadBucket{Bucket: bucket, onRead: func(_ context.Context, name string) error {
			if name == "indexes/a" && failRead.Load() {
				return errors.New("read failed")
			}
			return nil
		}}
		c := newPairTestCoordinator(t, failing, &fakeRunner{}, &fakeReplacer{swapped: true})
		p := c.newPairCompactor("acme")
		lastPlan := func() float64 { return testutil.ToFloat64(c.metrics.lastPlanTimestampSeconds.WithLabelValues("acme")) }

		p.scan(t.Context())
		require.Zero(t, lastPlan())

		failRead.Store(false)
		p.scan(t.Context())
		require.Equal(t, float64(imWindow().Add(time.Hour).Unix()), lastPlan())
	})
}

func TestPairCompactorRunJob(t *testing.T) {
	t.Run("a job whose sources left the ToC runs no task and counts stale", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairs(t, bucket, "indexes/a")
		runner := &fakeRunner{}
		replacer := &fakeReplacer{swapped: true}
		c := newPairTestCoordinator(t, bucket, runner, replacer)
		p := c.newPairCompactor("acme")

		p.runJob(t.Context(), &pairMerge{scope: jobScope{window: imWindow(), sources: []indexRun{runOfPath("indexes/a"), runOfPath("indexes/gone")}}}, 0)

		require.Empty(t, runner.snapshot())
		require.Empty(t, replacer.snapshot())
		require.Equal(t, float64(1), pairJobsCount(c, pairJobKindMerge, pairJobOutcomeStale))
	})

	t.Run("a job that ctx stops before its first slot records no outcome", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairs(t, bucket, "indexes/a", "indexes/b")
		runner := &fakeRunner{}
		c := newPairTestCoordinator(t, bucket, runner, &fakeReplacer{swapped: true})
		p := c.newPairCompactor("acme")
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		p.runJob(ctx, &pairMerge{scope: jobScope{window: imWindow(), sources: []indexRun{runOfPath("indexes/a"), runOfPath("indexes/b")}}}, 0)

		require.Empty(t, runner.snapshot())
		for _, outcome := range []string{pairJobOutcomeCommitted, pairJobOutcomeFailed, pairJobOutcomeStale, pairJobOutcomeNoop} {
			require.Zero(t, pairJobsCount(c, pairJobKindMerge, outcome))
		}
	})
}

func TestPairCompactorRun(t *testing.T) {
	// runOneScan runs the compactor of c for one scan and waits for its jobs.
	runOneScan := func(t *testing.T, c *coordinator) {
		t.Helper()
		p := c.newPairCompactor("acme")
		ctx, cancel := context.WithCancel(t.Context())
		p.sleep = func(context.Context, time.Duration) {
			p.jobs.Wait()
			cancel()
		}
		p.run(ctx)
	}

	t.Run("run plans no job while log compaction is disabled", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairs(t, bucket, "indexes/a", "indexes/b")
		runner := &fakeRunner{}
		c := newPairTestCoordinator(t, bucket, runner, &fakeReplacer{swapped: true})
		limits := newFakeLimits("acme")
		c.limits = limits

		runOneScan(t, c)

		require.Empty(t, runner.snapshot())
	})

	t.Run("run commits a merge while log compaction is enabled", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairs(t, bucket, "indexes/a", "indexes/b")
		seedTaskIndex(t, bucket, "indexes/out", map[string]int64{"logs/out": 400})
		responder := &pairResponder{logMerges: []string{"indexes/out"}}
		replacer := &fakeReplacer{swapped: true}
		c := newPairTestCoordinator(t, bucket, &fakeRunner{respond: responder.respond}, replacer)

		runOneScan(t, c)

		require.Len(t, replacer.snapshot(), 1)
	})

	t.Run("disabling log compaction stops the jobs that did not start and enabling it plans them again", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairs(t, bucket, "indexes/a", "indexes/b", "indexes/c", "indexes/d")
		seedTaskIndex(t, bucket, "indexes/out", map[string]int64{"logs/out": 400})
		started := make(chan struct{}, 8)
		unblock := make(chan struct{})
		var unblockOnce sync.Once
		defer unblockOnce.Do(func() { close(unblock) })
		responder := &pairResponder{logMerges: slices.Repeat([]string{"indexes/out"}, 8), onLogMerge: func(int) {
			started <- struct{}{}
			<-unblock
		}}
		runner := &fakeRunner{respond: responder.respond}
		replacer := &fakeReplacer{swapped: true}
		c := newPairTestCoordinator(t, bucket, runner, replacer)
		c.cfg.MaxRunningCompactionTasks = 1
		limits := newFakeLimits("acme")
		limits.setLog("acme", true)
		c.limits = limits
		p := c.newPairCompactor("acme")
		ctx, cancel := context.WithCancel(t.Context())
		var (
			sleeps          int
			callsWhileOff   int
			swapsWhileOff   int
			stoppedJobsDone bool
		)
		p.sleep = func(context.Context, time.Duration) {
			sleeps++
			switch sleeps {
			case 1:
				<-started
				limits.setLog("acme", false)
			case 2:
				unblockOnce.Do(func() { close(unblock) })
				p.jobs.Wait()
				callsWhileOff, swapsWhileOff = len(runner.snapshot()), len(replacer.snapshot())
				stoppedJobsDone = true
				limits.setLog("acme", true)
			default:
				p.jobs.Wait()
				cancel()
			}
		}

		p.run(ctx)

		require.True(t, stoppedJobsDone)
		require.Equal(t, 1, callsWhileOff)
		require.Equal(t, 1, swapsWhileOff)
		require.Greater(t, len(replacer.snapshot()), swapsWhileOff)
	})
}
