package worker_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/objtest"
	"github.com/grafana/loki/v3/pkg/engine/internal/worker"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// Test runs an end-to-end test of the worker.
func Test(t *testing.T) {
	builder := objtest.NewBuilder(t)

	logger := log.NewNopLogger()
	if testing.Verbose() {
		logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	}

	net := newTestNetwork()
	sched := newTestScheduler(t, logger, net)
	_ = newTestWorker(t, logger, builder.Location(), net)

	ctx := user.InjectOrgID(t.Context(), objtest.Tenant)

	builder.Append(ctx, logproto.Stream{
		Labels: `{app="loki", env="dev"}`,
		Entries: []logproto.Entry{{
			Timestamp: time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			Line:      "Hello, world!",
		}, {
			Timestamp: time.Date(2025, time.January, 1, 0, 0, 1, 0, time.UTC),
			Line:      "Goodbye, world!",
		}},
	})
	builder.Close()

	params, err := logql.NewLiteralParams(
		`{app="loki"}`,
		time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, time.January, 2, 0, 0, 0, 0, time.UTC),
		0,
		0,
		logproto.BACKWARD,
		1000,
		[]string{"0_of_1"},
		nil,
	)
	require.NoError(t, err, "expected to be able to create literal LogQL params")

	wf := buildWorkflow(ctx, t, logger, builder.Location(), sched, params)
	pipeline, err := wf.Run(ctx)
	require.NoError(t, err)

	expected := arrowtest.Rows{
		{
			"timestamp_ns.builtin.timestamp": time.Date(2025, time.January, 1, 0, 0, 1, 0, time.UTC),
			"utf8.label.app":                 "loki",
			"utf8.label.env":                 "dev",
			"utf8.builtin.message":           "Goodbye, world!",
		},
		{
			"timestamp_ns.builtin.timestamp": time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			"utf8.label.app":                 "loki",
			"utf8.label.env":                 "dev",
			"utf8.builtin.message":           "Hello, world!",
		},
	}

	actual, err := arrowtest.TableRows(memory.DefaultAllocator, readTable(ctx, t, pipeline))
	require.NoError(t, err, "failed to get rows from table")
	require.Equal(t, expected, actual)
}

type testNetwork struct {
	schedulerListener *wire.Local
	workerListener    *wire.Local
	dialer            wire.Dialer
}

func newTestNetwork() *testNetwork {
	schedulerListener := &wire.Local{Address: wire.LocalScheduler}
	workerListener := &wire.Local{Address: wire.LocalWorker}

	return &testNetwork{
		schedulerListener: schedulerListener,
		workerListener:    workerListener,
		dialer:            wire.NewLocalDialer(schedulerListener, workerListener),
	}
}

func newTestScheduler(t *testing.T, logger log.Logger, net *testNetwork) *scheduler.Scheduler {
	t.Helper()

	sched, err := scheduler.New(scheduler.Config{
		Logger:   logger,
		Listener: net.schedulerListener,
	})
	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(t.Context(), sched.Service()))

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		require.NoError(t, services.StopAndAwaitTerminated(ctx, sched.Service()))
	})

	return sched
}

func newTestWorker(t *testing.T, logger log.Logger, loc objtest.Location, net *testNetwork) *worker.Worker {
	t.Helper()

	w, err := worker.New(worker.Config{
		Logger:    logger,
		Bucket:    loc.Bucket,
		BatchSize: 2048,

		Dialer:           net.dialer,
		Listener:         net.workerListener,
		SchedulerAddress: wire.LocalScheduler,

		// Create enough threads to guarantee all tasks can be scheduled without
		// blocking.
		NumThreads: 8,
	})
	require.NoError(t, err, "expected to create worker")
	require.NoError(t, services.StartAndAwaitRunning(t.Context(), w.Service()))

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		require.NoError(t, services.StopAndAwaitTerminated(ctx, w.Service()))
	})

	return w
}

func buildWorkflow(ctx context.Context, t *testing.T, logger log.Logger, loc objtest.Location, sched *scheduler.Scheduler, params logql.Params) *workflow.Workflow {
	logicalPlan, err := logical.BuildPlan(params)
	require.NoError(t, err, "expected to create logical plan")

	ms := metastore.NewObjectMetastore(
		objstore.NewPrefixedBucket(loc.Bucket, loc.IndexPrefix),
		logger,
		prometheus.NewRegistry(),
	)
	catalog := physical.NewMetastoreCatalog(func(start time.Time, end time.Time, selectors []*labels.Matcher, predicates []*labels.Matcher) ([]*metastore.DataobjSectionDescriptor, error) {
		return ms.Sections(ctx, start, end, selectors, predicates)
	})
	planner := physical.NewPlanner(physical.NewContext(params.Start(), params.End()), catalog)
	plan, err := planner.Build(logicalPlan)
	require.NoError(t, err, "expected to create physical plan")

	plan, err = planner.Optimize(plan)
	require.NoError(t, err, "expected to optimize physical plan")

	if testing.Verbose() {
		fmt.Fprintln(os.Stderr, physical.PrintAsTree(plan))
	}

	opts := workflow.Options{
		MaxRunningScanTasks:  32,
		MaxRunningOtherTasks: 0, // unlimited
	}
	wf, err := workflow.New(opts, logger, objtest.Tenant, sched, plan)
	require.NoError(t, err)

	if testing.Verbose() {
		workflow.Fprint(os.Stderr, wf)
	}
	return wf
}

func readTable(ctx context.Context, t *testing.T, p executor.Pipeline) arrow.Table {
	var recs []arrow.RecordBatch

	for {
		rec, err := p.Read(ctx)
		if rec != nil {
			if rec.NumRows() > 0 {
				recs = append(recs, rec)
			}
		}

		if err != nil && errors.Is(err, executor.EOF) {
			break
		}
		require.NoError(t, err)
	}

	return array.NewTableFromRecords(recs[0].Schema(), recs)
}
