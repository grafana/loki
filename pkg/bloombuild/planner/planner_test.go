package planner

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/bloombuild/planner/plannertest"
	"github.com/grafana/loki/v3/pkg/bloombuild/planner/queue"
	"github.com/grafana/loki/v3/pkg/bloombuild/planner/strategies"
	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/storage"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	bloomshipperconfig "github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util/mempool"
)

func createPlanner(
	t *testing.T,
	cfg Config,
	limits Limits,
	logger log.Logger,
) *Planner {
	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From: plannertest.ParseDayTime("2023-09-01"),
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: 24 * time.Hour,
					},
				},
				IndexType:  types.TSDBType,
				ObjectType: types.StorageTypeFileSystem,
				Schema:     "v13",
				RowShards:  16,
			},
		},
	}
	storageCfg := storage.Config{
		BloomShipperConfig: bloomshipperconfig.Config{
			WorkingDirectory:    []string{t.TempDir()},
			DownloadParallelism: 1,
			BlocksCache: bloomshipperconfig.BlocksCacheConfig{
				SoftLimit: flagext.Bytes(10 << 20),
				HardLimit: flagext.Bytes(20 << 20),
				TTL:       time.Hour,
			},
			CacheListOps: false,
		},
		FSConfig: local.FSConfig{
			Directory: t.TempDir(),
		},
	}

	reg := prometheus.NewPedanticRegistry()
	metasCache := cache.NewNoopCache()
	blocksCache := bloomshipper.NewFsBlocksCache(storageCfg.BloomShipperConfig.BlocksCache, reg, logger)
	bloomStore, err := bloomshipper.NewBloomStore(schemaCfg.Configs, storageCfg, storage.ClientMetrics{}, metasCache, blocksCache, &mempool.SimpleHeapAllocator{}, reg, logger)
	require.NoError(t, err)

	planner, err := New(cfg, limits, schemaCfg, storageCfg, storage.ClientMetrics{}, bloomStore, logger, reg, nil)
	require.NoError(t, err)

	return planner
}

func Test_BuilderLoop(t *testing.T) {
	const (
		nTasks    = 100
		nBuilders = 10
	)

	for _, tc := range []struct {
		name                     string
		limits                   Limits
		expectedBuilderLoopError error

		// modifyBuilder should leave the builder in a state where it will not return or return an error
		modifyBuilder            func(builder *fakeBuilder)
		shouldConsumeAfterModify bool

		// resetBuilder should reset the builder to a state where it will return no errors
		resetBuilder func(builder *fakeBuilder)
	}{
		{
			name:                     "success",
			limits:                   &fakeLimits{},
			expectedBuilderLoopError: errPlannerIsNotRunning,
		},
		{
			name:                     "error rpc",
			limits:                   &fakeLimits{},
			expectedBuilderLoopError: errPlannerIsNotRunning,
			modifyBuilder: func(builder *fakeBuilder) {
				builder.SetReturnError(true)
			},
			resetBuilder: func(builder *fakeBuilder) {
				builder.SetReturnError(false)
			},
		},
		{
			name:                     "error msg",
			limits:                   &fakeLimits{},
			expectedBuilderLoopError: errPlannerIsNotRunning,
			modifyBuilder: func(builder *fakeBuilder) {
				builder.SetReturnErrorMsg(true)
			},
			// We don't retry on error messages from the builder
			shouldConsumeAfterModify: true,
		},
		{
			name:                     "exceed max retries",
			limits:                   &fakeLimits{maxRetries: 1},
			expectedBuilderLoopError: errPlannerIsNotRunning,
			modifyBuilder: func(builder *fakeBuilder) {
				builder.SetReturnError(true)
			},
			shouldConsumeAfterModify: true,
		},
		{
			name: "timeout",
			limits: &fakeLimits{
				timeout: 1 * time.Second,
			},
			expectedBuilderLoopError: errPlannerIsNotRunning,
			modifyBuilder: func(builder *fakeBuilder) {
				builder.SetWait(true)
			},
			resetBuilder: func(builder *fakeBuilder) {
				builder.SetWait(false)
			},
		},
		{
			name:   "context cancel",
			limits: &fakeLimits{},
			// Builders cancel the context when they disconnect. We forward this error to the planner.
			expectedBuilderLoopError: context.Canceled,
			modifyBuilder: func(builder *fakeBuilder) {
				builder.CancelContext(true)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := log.NewNopLogger()
			//logger := log.NewLogfmtLogger(os.Stdout)

			cfg := Config{
				PlanningInterval: 1 * time.Hour,
				Queue: queue.Config{
					MaxQueuedTasksPerTenant: 10000,
				},
			}
			planner := createPlanner(t, cfg, tc.limits, logger)

			// Start planner
			err := services.StartAndAwaitRunning(context.Background(), planner)
			require.NoError(t, err)
			t.Cleanup(func() {
				err := services.StopAndAwaitTerminated(context.Background(), planner)
				require.NoError(t, err)
			})

			// Enqueue tasks
			resultsCh := make(chan *protos.TaskResult, nTasks)
			tasks := createTasks(nTasks, resultsCh)
			for _, task := range tasks {
				err := planner.enqueueTask(task)
				require.NoError(t, err)
			}

			// Create builders and call planner.BuilderLoop
			builders := make([]*fakeBuilder, 0, nBuilders)
			for i := 0; i < nBuilders; i++ {
				builder := newMockBuilder(fmt.Sprintf("builder-%d", i))
				builders = append(builders, builder)

				go func(expectedBuilderLoopError error) {
					err := planner.BuilderLoop(builder)
					require.ErrorIs(t, err, expectedBuilderLoopError)
				}(tc.expectedBuilderLoopError)
			}

			// Eventually, all tasks should be sent to builders
			require.Eventually(t, func() bool {
				var receivedTasks int
				for _, builder := range builders {
					receivedTasks += len(builder.ReceivedTasks())
				}
				return receivedTasks == nTasks
			}, 5*time.Second, 10*time.Millisecond)

			// Finally, the queue should be empty
			require.Equal(t, 0, planner.tasksQueue.TotalPending())

			// consume all tasks result to free up the channel for the next round of tasks
			for i := 0; i < nTasks; i++ {
				<-resultsCh
			}

			if tc.modifyBuilder != nil {
				// Configure builders to return errors
				for _, builder := range builders {
					tc.modifyBuilder(builder)
				}

				// Enqueue tasks again
				for _, task := range tasks {
					err := planner.enqueueTask(task)
					require.NoError(t, err)
				}

				if tc.shouldConsumeAfterModify {
					require.Eventuallyf(
						t, func() bool {
							return planner.tasksQueue.TotalPending() == 0
						},
						5*time.Second, 10*time.Millisecond,
						"tasks not consumed, pending: %d", planner.tasksQueue.TotalPending(),
					)
				} else {
					require.Neverf(
						t, func() bool {
							return planner.tasksQueue.TotalPending() == 0
						},
						5*time.Second, 10*time.Millisecond,
						"all tasks were consumed but they should not be",
					)
				}

			}

			if tc.resetBuilder != nil {
				// Configure builders to return no errors
				for _, builder := range builders {
					tc.resetBuilder(builder)
				}

				// Now all tasks should be consumed
				require.Eventuallyf(
					t, func() bool {
						return planner.tasksQueue.TotalPending() == 0
					},
					5*time.Second, 10*time.Millisecond,
					"tasks not consumed, pending: %d", planner.tasksQueue.TotalPending(),
				)
			}
		})
	}
}

func Test_processTenantTaskResults(t *testing.T) {
	for _, tc := range []struct {
		name string

		originalMetas        []bloomshipper.Meta
		taskResults          []*protos.TaskResult
		expectedMetas        []bloomshipper.Meta
		expectedTasksSucceed int
	}{
		{
			name: "errors",
			originalMetas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 10, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(0, 10)}),
				plannertest.GenMeta(10, 20, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(10, 20)}),
			},
			taskResults: []*protos.TaskResult{
				{
					TaskID: "1",
					Error:  errors.New("fake error"),
				},
				{
					TaskID: "2",
					Error:  errors.New("fake error"),
				},
			},
			expectedMetas: []bloomshipper.Meta{
				// The original metas should remain unchanged
				plannertest.GenMeta(0, 10, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(0, 10)}),
				plannertest.GenMeta(10, 20, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(10, 20)}),
			},
			expectedTasksSucceed: 0,
		},
		{
			name: "no new metas",
			originalMetas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 10, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(0, 10)}),
				plannertest.GenMeta(10, 20, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(10, 20)}),
			},
			taskResults: []*protos.TaskResult{
				{
					TaskID: "1",
				},
				{
					TaskID: "2",
				},
			},
			expectedMetas: []bloomshipper.Meta{
				// The original metas should remain unchanged
				plannertest.GenMeta(0, 10, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(0, 10)}),
				plannertest.GenMeta(10, 20, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(10, 20)}),
			},
			expectedTasksSucceed: 2,
		},
		{
			name: "no original metas",
			taskResults: []*protos.TaskResult{
				{
					TaskID: "1",
					CreatedMetas: []bloomshipper.Meta{
						plannertest.GenMeta(0, 10, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(0, 10)}),
					},
				},
				{
					TaskID: "2",
					CreatedMetas: []bloomshipper.Meta{
						plannertest.GenMeta(10, 20, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(10, 20)}),
					},
				},
			},
			expectedMetas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 10, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(0, 10)}),
				plannertest.GenMeta(10, 20, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(10, 20)}),
			},
			expectedTasksSucceed: 2,
		},
		{
			name: "single meta covers all original",
			originalMetas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 5, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(0, 5)}),
				plannertest.GenMeta(6, 10, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(6, 10)}),
			},
			taskResults: []*protos.TaskResult{
				{
					TaskID: "1",
					CreatedMetas: []bloomshipper.Meta{
						plannertest.GenMeta(0, 10, []int{1}, []bloomshipper.BlockRef{plannertest.GenBlockRef(0, 10)}),
					},
				},
			},
			expectedMetas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 10, []int{1}, []bloomshipper.BlockRef{plannertest.GenBlockRef(0, 10)}),
			},
			expectedTasksSucceed: 1,
		},
		{
			name: "multi version ordering",
			originalMetas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 5, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(0, 5)}),
				plannertest.GenMeta(0, 10, []int{1}, []bloomshipper.BlockRef{plannertest.GenBlockRef(0, 10)}), // only part of the range is outdated, must keep
			},
			taskResults: []*protos.TaskResult{
				{
					TaskID: "1",
					CreatedMetas: []bloomshipper.Meta{
						plannertest.GenMeta(8, 10, []int{2}, []bloomshipper.BlockRef{plannertest.GenBlockRef(8, 10)}),
					},
				},
			},
			expectedMetas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 10, []int{1}, []bloomshipper.BlockRef{plannertest.GenBlockRef(0, 10)}),
				plannertest.GenMeta(8, 10, []int{2}, []bloomshipper.BlockRef{plannertest.GenBlockRef(8, 10)}),
			},
			expectedTasksSucceed: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := log.NewNopLogger()
			//logger := log.NewLogfmtLogger(os.Stdout)

			cfg := Config{
				PlanningInterval: 1 * time.Hour,
				Queue: queue.Config{
					MaxQueuedTasksPerTenant: 10000,
				},
			}
			planner := createPlanner(t, cfg, &fakeLimits{}, logger)

			bloomClient, err := planner.bloomStore.Client(plannertest.TestDay.ModelTime())
			require.NoError(t, err)

			// Create original metas and blocks
			err = plannertest.PutMetas(bloomClient, tc.originalMetas)
			require.NoError(t, err)

			ctx, ctxCancel := context.WithCancel(context.Background())
			defer ctxCancel()
			resultsCh := make(chan *protos.TaskResult, len(tc.taskResults))

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()

				completed, err := planner.processTenantTaskResults(
					ctx,
					plannertest.TestTable,
					"fakeTenant",
					tc.originalMetas,
					len(tc.taskResults),
					resultsCh,
				)
				require.NoError(t, err)
				require.Equal(t, tc.expectedTasksSucceed, completed)
			}()

			for _, taskResult := range tc.taskResults {
				if len(taskResult.CreatedMetas) > 0 {
					// Emulate builder putting new metas to obj store
					err = plannertest.PutMetas(bloomClient, taskResult.CreatedMetas)
					require.NoError(t, err)
				}

				resultsCh <- taskResult
			}

			// Wait for all tasks to be processed and outdated metas/blocks deleted
			wg.Wait()

			// Get all metas
			metas, err := planner.bloomStore.FetchMetas(
				context.Background(),
				bloomshipper.MetaSearchParams{
					TenantID: "fakeTenant",
					Interval: bloomshipper.NewInterval(plannertest.TestTable.Bounds()),
					Keyspace: v1.NewBounds(0, math.MaxUint64),
				},
			)
			require.NoError(t, err)
			removeLocFromMetasSources(metas)

			// Compare metas
			require.Equal(t, len(tc.expectedMetas), len(metas))
			require.ElementsMatch(t, tc.expectedMetas, metas)
		})
	}
}

// For some reason, when the tests are run in the CI, we do not encode the `loc` of model.Time for each TSDB.
// As a result, when we fetch them, the loc is empty whereas in the original metas, it is not. Therefore the
// comparison fails. As a workaround to fix the issue, we will manually reset the TS of the sources to the
// fetched metas
func removeLocFromMetasSources(metas []bloomshipper.Meta) []bloomshipper.Meta {
	for i := range metas {
		for j := range metas[i].Sources {
			sec := metas[i].Sources[j].TS.Unix()
			nsec := metas[i].Sources[j].TS.Nanosecond()
			metas[i].Sources[j].TS = time.Unix(sec, int64(nsec))
		}
	}

	return metas
}

func Test_deleteOutdatedMetas(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		originalMetas         []bloomshipper.Meta
		newMetas              []bloomshipper.Meta
		expectedUpToDateMetas []bloomshipper.Meta
	}{
		{
			name: "no metas",
		},
		{
			name: "only up to date metas",
			originalMetas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 10, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(0, 10)}),
			},
			newMetas: []bloomshipper.Meta{
				plannertest.GenMeta(10, 20, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(10, 20)}),
			},
			expectedUpToDateMetas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 10, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(0, 10)}),
				plannertest.GenMeta(10, 20, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(10, 20)}),
			},
		},
		{
			name: "outdated metas",
			originalMetas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 5, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(0, 5)}),
			},
			newMetas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 10, []int{1}, []bloomshipper.BlockRef{plannertest.GenBlockRef(0, 10)}),
			},
			expectedUpToDateMetas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 10, []int{1}, []bloomshipper.BlockRef{plannertest.GenBlockRef(0, 10)}),
			},
		},
		{
			name: "new metas reuse blocks from outdated meta",
			originalMetas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 10, []int{0}, []bloomshipper.BlockRef{ // Outdated
					plannertest.GenBlockRef(0, 5),  // Reuse
					plannertest.GenBlockRef(5, 10), // Delete
				}),
				plannertest.GenMeta(10, 20, []int{0}, []bloomshipper.BlockRef{ // Outdated
					plannertest.GenBlockRef(10, 20), // Reuse
				}),
				plannertest.GenMeta(20, 30, []int{0}, []bloomshipper.BlockRef{ // Up to date
					plannertest.GenBlockRef(20, 30),
				}),
			},
			newMetas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 5, []int{1}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(0, 5), // Reused block
				}),
				plannertest.GenMeta(5, 20, []int{1}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(5, 7),   // New block
					plannertest.GenBlockRef(7, 10),  // New block
					plannertest.GenBlockRef(10, 20), // Reused block
				}),
			},
			expectedUpToDateMetas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 5, []int{1}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(0, 5),
				}),
				plannertest.GenMeta(5, 20, []int{1}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(5, 7),
					plannertest.GenBlockRef(7, 10),
					plannertest.GenBlockRef(10, 20),
				}),
				plannertest.GenMeta(20, 30, []int{0}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(20, 30),
				}),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := log.NewNopLogger()
			// logger := log.NewLogfmtLogger(os.Stdout)

			cfg := Config{
				PlanningInterval: 1 * time.Hour,
				Queue: queue.Config{
					MaxQueuedTasksPerTenant: 10000,
				},
			}
			planner := createPlanner(t, cfg, &fakeLimits{}, logger)

			bloomClient, err := planner.bloomStore.Client(plannertest.TestDay.ModelTime())
			require.NoError(t, err)

			// Create original/new metas and blocks
			err = plannertest.PutMetas(bloomClient, tc.originalMetas)
			require.NoError(t, err)
			err = plannertest.PutMetas(bloomClient, tc.newMetas)
			require.NoError(t, err)

			// Get all metas
			metas, err := planner.bloomStore.FetchMetas(
				context.Background(),
				bloomshipper.MetaSearchParams{
					TenantID: "fakeTenant",
					Interval: bloomshipper.NewInterval(plannertest.TestTable.Bounds()),
					Keyspace: v1.NewBounds(0, math.MaxUint64),
				},
			)
			require.NoError(t, err)
			removeLocFromMetasSources(metas)
			require.ElementsMatch(t, append(tc.originalMetas, tc.newMetas...), metas)

			upToDate, err := planner.deleteOutdatedMetasAndBlocks(context.Background(), plannertest.TestTable, "fakeTenant", tc.newMetas, tc.originalMetas, phasePlanning)
			require.NoError(t, err)
			require.ElementsMatch(t, tc.expectedUpToDateMetas, upToDate)

			// Get all metas
			metas, err = planner.bloomStore.FetchMetas(
				context.Background(),
				bloomshipper.MetaSearchParams{
					TenantID: "fakeTenant",
					Interval: bloomshipper.NewInterval(plannertest.TestTable.Bounds()),
					Keyspace: v1.NewBounds(0, math.MaxUint64),
				},
			)
			require.NoError(t, err)
			removeLocFromMetasSources(metas)
			require.ElementsMatch(t, tc.expectedUpToDateMetas, metas)

			// Fetch all blocks from the metas
			for _, meta := range metas {
				blocks, err := planner.bloomStore.FetchBlocks(context.Background(), meta.Blocks)
				require.NoError(t, err)
				require.Len(t, blocks, len(meta.Blocks))
			}
		})
	}
}

func TestMinMaxTables(t *testing.T) {
	logger := log.NewNopLogger()
	//logger := log.NewLogfmtLogger(os.Stdout)

	cfg := Config{
		PlanningInterval: 1 * time.Hour,
		Queue: queue.Config{
			MaxQueuedTasksPerTenant: 10000,
		},
		// From today till day before tomorrow
		MinTableOffset: 0,
		MaxTableOffset: 2,
	}
	planner := createPlanner(t, cfg, &fakeLimits{}, logger)

	tables := planner.tables(time.Now())
	require.Equal(t, 3, tables.TotalDays())

	dayTables, err := iter.Collect(tables)
	require.NoError(t, err)

	todayTable := config.NewDayTable(config.NewDayTime(model.Now()), "index_")
	yesterdayTable := config.NewDayTable(config.NewDayTime(model.Now().Add(-24*time.Hour)), "index_")
	dayBeforeYesterdayTable := config.NewDayTable(config.NewDayTime(model.Now().Add(-48*time.Hour)), "index_")

	require.Equal(t, dayBeforeYesterdayTable.Addr(), dayTables[0].Addr())
	require.Equal(t, yesterdayTable.Addr(), dayTables[1].Addr())
	require.Equal(t, todayTable.Addr(), dayTables[2].Addr())
}

type fakeBuilder struct {
	mx          sync.Mutex // Protects tasks and currTaskIdx.
	id          string
	tasks       []*protos.Task
	currTaskIdx int
	grpc.ServerStream

	returnError    atomic.Bool
	returnErrorMsg atomic.Bool
	wait           atomic.Bool
	ctx            context.Context
	ctxCancel      context.CancelFunc
}

func newMockBuilder(id string) *fakeBuilder {
	ctx, cancel := context.WithCancel(context.Background())

	return &fakeBuilder{
		id:          id,
		currTaskIdx: -1,
		ctx:         ctx,
		ctxCancel:   cancel,
	}
}

func (f *fakeBuilder) ReceivedTasks() []*protos.Task {
	f.mx.Lock()
	defer f.mx.Unlock()
	return f.tasks
}

func (f *fakeBuilder) SetReturnError(b bool) {
	f.returnError.Store(b)
}

func (f *fakeBuilder) SetReturnErrorMsg(b bool) {
	f.returnErrorMsg.Store(b)
}

func (f *fakeBuilder) SetWait(b bool) {
	f.wait.Store(b)
}

func (f *fakeBuilder) CancelContext(b bool) {
	if b {
		f.ctxCancel()
		return
	}

	// Reset context
	f.ctx, f.ctxCancel = context.WithCancel(context.Background())
}

func (f *fakeBuilder) Context() context.Context {
	return f.ctx
}

func (f *fakeBuilder) Send(req *protos.PlannerToBuilder) error {
	if f.ctx.Err() != nil {
		// Context was canceled
		return f.ctx.Err()
	}

	task, err := protos.FromProtoTask(req.Task)
	if err != nil {
		return err
	}

	f.mx.Lock()
	defer f.mx.Unlock()
	f.tasks = append(f.tasks, task)
	f.currTaskIdx++
	return nil
}

func (f *fakeBuilder) Recv() (*protos.BuilderToPlanner, error) {
	f.mx.Lock()
	tasksLen := len(f.tasks)
	f.mx.Unlock()
	if tasksLen == 0 {
		// First call to Recv answers with builderID
		return &protos.BuilderToPlanner{
			BuilderID: f.id,
		}, nil
	}

	if f.returnError.Load() {
		return nil, fmt.Errorf("fake error from %s", f.id)
	}

	// Wait until `wait` is false
	for f.wait.Load() {
		time.Sleep(time.Second)
	}

	if f.ctx.Err() != nil {
		// Context was canceled
		return nil, f.ctx.Err()
	}

	var errMsg string
	if f.returnErrorMsg.Load() {
		errMsg = fmt.Sprintf("fake error from %s", f.id)
	}

	f.mx.Lock()
	defer f.mx.Unlock()
	return &protos.BuilderToPlanner{
		BuilderID: f.id,
		Result: protos.ProtoTaskResult{
			TaskID:       f.tasks[f.currTaskIdx].ID,
			Error:        errMsg,
			CreatedMetas: nil,
		},
	}, nil
}

func createTasks(n int, resultsCh chan *protos.TaskResult) []*QueueTask {
	tasks := make([]*QueueTask, 0, n)
	// Enqueue tasks
	for i := 0; i < n; i++ {
		task := NewQueueTask(
			context.Background(), time.Now(),
			protos.NewTask(config.NewDayTable(plannertest.TestDay, "fake"), "fakeTenant", v1.NewBounds(model.Fingerprint(i), model.Fingerprint(i+10)), plannertest.TsdbID(1), nil).ToProtoTask(),
			resultsCh,
		)
		tasks = append(tasks, task)
	}
	return tasks
}

type fakeLimits struct {
	Limits
	timeout    time.Duration
	maxRetries int
}

func (f *fakeLimits) BuilderResponseTimeout(_ string) time.Duration {
	return f.timeout
}

func (f *fakeLimits) BloomCreationEnabled(_ string) bool {
	return true
}

func (f *fakeLimits) BloomSplitSeriesKeyspaceBy(_ string) int {
	return 1
}

func (f *fakeLimits) BloomBuildMaxBuilders(_ string) int {
	return 0
}

func (f *fakeLimits) BloomTaskMaxRetries(_ string) int {
	return f.maxRetries
}

func (f *fakeLimits) BloomPlanningStrategy(_ string) string {
	return strategies.SplitBySeriesChunkSizeStrategyName
}

func (f *fakeLimits) BloomTaskTargetSeriesChunksSizeBytes(_ string) uint64 {
	return 1 << 20 // 1MB
}
