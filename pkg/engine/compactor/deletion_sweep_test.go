package compactor

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

func newTestSweepScheduler(maxConcurrent int, bucket objstore.BucketReader, limits Limits, run runFunc) *sweepScheduler {
	metrics := newCoordinatorMetrics(prometheus.NewRegistry())
	return newSweepScheduler(DeletionSweepConfig{MaxConcurrentSweeps: maxConcurrent}, log.NewNopLogger(), bucket, limits, run, metrics.taskAttemptsTotal)
}

// sweepRecorder records the time and the plan of each sweep. Its respond
// func decides the result of a sweep.
type sweepRecorder struct {
	mu      sync.Mutex
	times   []time.Time
	plans   []*physical.Plan
	opts    []workflow.Options
	respond func(ctx context.Context, n int) (*v2.ResultArtifact, error)
}

func (r *sweepRecorder) run(ctx context.Context, opts workflow.Options, plan *physical.Plan) (*v2.ResultArtifact, error) {
	r.mu.Lock()
	r.times = append(r.times, time.Now())
	r.plans = append(r.plans, plan)
	r.opts = append(r.opts, opts)
	n := len(r.times)
	r.mu.Unlock()
	if r.respond != nil {
		return r.respond(ctx, n)
	}
	return &v2.ResultArtifact{Path: "deletion/t/files.jsonl.gz"}, nil
}

func (r *sweepRecorder) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.times)
}

func TestSweepMinute(t *testing.T) {
	t.Run("returns the same minute in the hour for the same tenant", func(t *testing.T) {
		for _, tenant := range []string{"a", "tenant-1", "29"} {
			minute := sweepMinute(tenant)
			require.Equal(t, minute, sweepMinute(tenant))
			require.GreaterOrEqual(t, minute, 0)
			require.Less(t, minute, 60)
		}
	})
}

func TestNextSweepTime(t *testing.T) {
	hour := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"returns the minute in this hour when it is still ahead", hour.Add(10 * time.Minute), hour.Add(30 * time.Minute)},
		{"returns the minute in the next hour when now is at the minute", hour.Add(30 * time.Minute), hour.Add(90 * time.Minute)},
		{"returns the minute in the next hour when now is after the minute", hour.Add(45 * time.Minute), hour.Add(90 * time.Minute)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, nextSweepTime(tc.now, 30))
		})
	}
}

func TestSweepScheduler_RunTenant(t *testing.T) {
	const tenant = "t"
	minute := time.Duration(sweepMinute(tenant)) * time.Minute

	t.Run("dispatches one DeletionSweep plan for the tenant at its minute of every hour", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			start := time.Now().Truncate(time.Hour)
			ctx, cancel := context.WithCancel(t.Context())
			recorder := &sweepRecorder{}
			recorder.respond = func(context.Context, int) (*v2.ResultArtifact, error) {
				if recorder.calls() == 3 {
					cancel()
				}
				return &v2.ResultArtifact{Path: "deletion/t/files.jsonl.gz"}, nil
			}
			s := newTestSweepScheduler(1, objstore.NewInMemBucket(), newFakeLimits(), recorder.run)

			s.runTenant(ctx, tenant)

			require.Equal(t, []time.Time{start.Add(minute), start.Add(time.Hour + minute), start.Add(2*time.Hour + minute)}, recorder.times)
			for i, plan := range recorder.plans {
				root, err := plan.Root()
				require.NoError(t, err)
				require.Equal(t, tenant, root.(*physical.DeletionSweep).Tenant)
				require.Equal(t, tenant, recorder.opts[i].Tenant)
				require.Equal(t, []string{"compaction", "deletion-sweep"}, recorder.opts[i].Actor)
			}
		})
	})

	t.Run("moves the next sweep to the following slot when a sweep runs past its slot", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			start := time.Now().Truncate(time.Hour)
			ctx, cancel := context.WithCancel(t.Context())
			recorder := &sweepRecorder{}
			recorder.respond = func(context.Context, int) (*v2.ResultArtifact, error) {
				if recorder.calls() == 2 {
					cancel()
				} else {
					time.Sleep(90 * time.Minute)
				}
				return &v2.ResultArtifact{Path: "deletion/t/files.jsonl.gz"}, nil
			}
			s := newTestSweepScheduler(1, objstore.NewInMemBucket(), newFakeLimits(), recorder.run)

			s.runTenant(ctx, tenant)

			require.Equal(t, []time.Time{start.Add(minute), start.Add(2*time.Hour + minute)}, recorder.times)
		})
	})

	t.Run("runs the next sweep one hour after a failed sweep", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			start := time.Now().Truncate(time.Hour)
			ctx, cancel := context.WithCancel(t.Context())
			recorder := &sweepRecorder{}
			recorder.respond = func(context.Context, int) (*v2.ResultArtifact, error) {
				if recorder.calls() == 2 {
					cancel()
				}
				return nil, errors.New("sweep failed")
			}
			s := newTestSweepScheduler(1, objstore.NewInMemBucket(), newFakeLimits(), recorder.run)

			s.runTenant(ctx, tenant)

			require.Equal(t, []time.Time{start.Add(minute), start.Add(time.Hour + minute)}, recorder.times)
		})
	})

	t.Run("returns without a sweep when the context is cancelled before the minute", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			recorder := &sweepRecorder{}
			s := newTestSweepScheduler(1, objstore.NewInMemBucket(), newFakeLimits(), recorder.run)

			s.runTenant(ctx, tenant)

			require.Zero(t, recorder.calls())
		})
	})
}

func TestSweepScheduler_Sweep(t *testing.T) {
	t.Run("waits for a free slot when the maximum number of sweeps runs", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			release := make(chan struct{})
			recorder := &sweepRecorder{respond: func(context.Context, int) (*v2.ResultArtifact, error) {
				<-release
				return &v2.ResultArtifact{Path: "deletion/t/files.jsonl.gz"}, nil
			}}
			s := newTestSweepScheduler(1, objstore.NewInMemBucket(), newFakeLimits(), recorder.run)

			var wg sync.WaitGroup
			wg.Go(func() { s.sweep(t.Context(), "a") })
			wg.Go(func() { s.sweep(t.Context(), "b") })
			synctest.Wait()
			require.Equal(t, 1, recorder.calls())

			close(release)
			wg.Wait()
			require.Equal(t, 2, recorder.calls())
		})
	})

	t.Run("stops a running sweep when the context is cancelled", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			var sweepErr error
			recorder := &sweepRecorder{respond: func(runCtx context.Context, _ int) (*v2.ResultArtifact, error) {
				<-runCtx.Done()
				sweepErr = runCtx.Err()
				return nil, sweepErr
			}}
			s := newTestSweepScheduler(1, objstore.NewInMemBucket(), newFakeLimits(), recorder.run)

			var wg sync.WaitGroup
			wg.Go(func() { s.sweep(ctx, "a") })
			synctest.Wait()
			cancel()
			wg.Wait()

			require.ErrorIs(t, sweepErr, context.Canceled)
			require.Empty(t, s.slots, "the stopped sweep must free its slot")
		})
	})

	t.Run("returns without a sweep when the context ends while it waits for a slot", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			release := make(chan struct{})
			recorder := &sweepRecorder{respond: func(context.Context, int) (*v2.ResultArtifact, error) {
				<-release
				return &v2.ResultArtifact{Path: "deletion/t/files.jsonl.gz"}, nil
			}}
			s := newTestSweepScheduler(1, objstore.NewInMemBucket(), newFakeLimits(), recorder.run)

			var wg sync.WaitGroup
			wg.Go(func() { s.sweep(t.Context(), "a") })
			synctest.Wait()
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			s.sweep(ctx, "b")
			require.Equal(t, 1, recorder.calls())

			close(release)
			wg.Wait()
		})
	})
}

func TestSweepScheduler_Discover(t *testing.T) {
	t.Run("returns the tenants with compacted log or index objects", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		require.NoError(t, bucket.Upload(t.Context(), "objects/tenants/a/aa/compacted", bytes.NewReader([]byte("x"))))
		require.NoError(t, bucket.Upload(t.Context(), "index/v0/indexes/tenants/b/bb/compacted", bytes.NewReader([]byte("x"))))
		require.NoError(t, bucket.Upload(t.Context(), "objects/00/uncompacted", bytes.NewReader([]byte("x"))))
		s := newTestSweepScheduler(1, bucket, newFakeLimits(), (&sweepRecorder{}).run)

		tenants, err := s.discover(t.Context())
		require.NoError(t, err)
		require.Equal(t, map[string]struct{}{"a": {}, "b": {}}, tenants)
	})

	t.Run("returns an error when the listing of compacted tenants fails", func(t *testing.T) {
		s := newTestSweepScheduler(1, errBucket{Bucket: objstore.NewInMemBucket()}, newFakeLimits(), (&sweepRecorder{}).run)

		_, err := s.discover(t.Context())
		require.ErrorContains(t, err, "list tenants with compacted objects")
	})
}

func TestSweepScheduler_Enabled(t *testing.T) {
	t.Run("follows the per-tenant deletion sweep limit", func(t *testing.T) {
		limits := newFakeLimits("a")
		limits.setSweep("b", true)
		s := newTestSweepScheduler(1, objstore.NewInMemBucket(), limits, (&sweepRecorder{}).run)

		require.False(t, s.enabled("a"))
		require.True(t, s.enabled("b"))
	})
}
