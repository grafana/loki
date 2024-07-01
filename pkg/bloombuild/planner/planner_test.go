package planner

import (
	"context"
	"fmt"
	"io"
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

	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	"github.com/grafana/loki/v3/pkg/storage"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	bloomshipperconfig "github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util/mempool"
)

var testDay = parseDayTime("2023-09-01")
var testTable = config.NewDayTable(testDay, "index_")

func tsdbID(n int) tsdb.SingleTenantTSDBIdentifier {
	return tsdb.SingleTenantTSDBIdentifier{
		TS: time.Unix(int64(n), 0),
	}
}

func genMeta(min, max model.Fingerprint, sources []int, blocks []bloomshipper.BlockRef) bloomshipper.Meta {
	m := bloomshipper.Meta{
		MetaRef: bloomshipper.MetaRef{
			Ref: bloomshipper.Ref{
				TenantID:  "fakeTenant",
				TableName: testTable.Addr(),
				Bounds:    v1.NewBounds(min, max),
			},
		},
		Blocks: blocks,
	}
	for _, source := range sources {
		m.Sources = append(m.Sources, tsdbID(source))
	}
	return m
}

func Test_gapsBetweenTSDBsAndMetas(t *testing.T) {

	for _, tc := range []struct {
		desc           string
		err            bool
		exp            []tsdbGaps
		ownershipRange v1.FingerprintBounds
		tsdbs          []tsdb.SingleTenantTSDBIdentifier
		metas          []bloomshipper.Meta
	}{
		{
			desc:           "non-overlapping tsdbs and metas",
			err:            true,
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs:          []tsdb.SingleTenantTSDBIdentifier{tsdbID(0)},
			metas: []bloomshipper.Meta{
				genMeta(11, 20, []int{0}, nil),
			},
		},
		{
			desc:           "single tsdb",
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs:          []tsdb.SingleTenantTSDBIdentifier{tsdbID(0)},
			metas: []bloomshipper.Meta{
				genMeta(4, 8, []int{0}, nil),
			},
			exp: []tsdbGaps{
				{
					tsdb: tsdbID(0),
					gaps: []v1.FingerprintBounds{
						v1.NewBounds(0, 3),
						v1.NewBounds(9, 10),
					},
				},
			},
		},
		{
			desc:           "multiple tsdbs with separate blocks",
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs:          []tsdb.SingleTenantTSDBIdentifier{tsdbID(0), tsdbID(1)},
			metas: []bloomshipper.Meta{
				genMeta(0, 5, []int{0}, nil),
				genMeta(6, 10, []int{1}, nil),
			},
			exp: []tsdbGaps{
				{
					tsdb: tsdbID(0),
					gaps: []v1.FingerprintBounds{
						v1.NewBounds(6, 10),
					},
				},
				{
					tsdb: tsdbID(1),
					gaps: []v1.FingerprintBounds{
						v1.NewBounds(0, 5),
					},
				},
			},
		},
		{
			desc:           "multiple tsdbs with the same blocks",
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs:          []tsdb.SingleTenantTSDBIdentifier{tsdbID(0), tsdbID(1)},
			metas: []bloomshipper.Meta{
				genMeta(0, 5, []int{0, 1}, nil),
				genMeta(6, 8, []int{1}, nil),
			},
			exp: []tsdbGaps{
				{
					tsdb: tsdbID(0),
					gaps: []v1.FingerprintBounds{
						v1.NewBounds(6, 10),
					},
				},
				{
					tsdb: tsdbID(1),
					gaps: []v1.FingerprintBounds{
						v1.NewBounds(9, 10),
					},
				},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			gaps, err := gapsBetweenTSDBsAndMetas(tc.ownershipRange, tc.tsdbs, tc.metas)
			if tc.err {
				require.Error(t, err)
				return
			}
			require.Equal(t, tc.exp, gaps)
		})
	}
}

func genBlockRef(min, max model.Fingerprint) bloomshipper.BlockRef {
	startTS, endTS := testDay.Bounds()
	return bloomshipper.BlockRef{
		Ref: bloomshipper.Ref{
			TenantID:       "fakeTenant",
			TableName:      testTable.Addr(),
			Bounds:         v1.NewBounds(min, max),
			StartTimestamp: startTS,
			EndTimestamp:   endTS,
			Checksum:       0,
		},
	}
}

func genBlock(ref bloomshipper.BlockRef) bloomshipper.Block {
	return bloomshipper.Block{
		BlockRef: ref,
		Data:     &DummyReadSeekCloser{},
	}
}

func Test_blockPlansForGaps(t *testing.T) {
	for _, tc := range []struct {
		desc           string
		ownershipRange v1.FingerprintBounds
		tsdbs          []tsdb.SingleTenantTSDBIdentifier
		metas          []bloomshipper.Meta
		err            bool
		exp            []blockPlan
	}{
		{
			desc:           "single overlapping meta+no overlapping block",
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs:          []tsdb.SingleTenantTSDBIdentifier{tsdbID(0)},
			metas: []bloomshipper.Meta{
				genMeta(5, 20, []int{1}, []bloomshipper.BlockRef{genBlockRef(11, 20)}),
			},
			exp: []blockPlan{
				{
					tsdb: tsdbID(0),
					gaps: []protos.GapWithBlocks{
						{
							Bounds: v1.NewBounds(0, 10),
						},
					},
				},
			},
		},
		{
			desc:           "single overlapping meta+one overlapping block",
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs:          []tsdb.SingleTenantTSDBIdentifier{tsdbID(0)},
			metas: []bloomshipper.Meta{
				genMeta(5, 20, []int{1}, []bloomshipper.BlockRef{genBlockRef(9, 20)}),
			},
			exp: []blockPlan{
				{
					tsdb: tsdbID(0),
					gaps: []protos.GapWithBlocks{
						{
							Bounds: v1.NewBounds(0, 10),
							Blocks: []bloomshipper.BlockRef{genBlockRef(9, 20)},
						},
					},
				},
			},
		},
		{
			// the range which needs to be generated doesn't overlap with existing blocks
			// from other tsdb versions since theres an up to date tsdb version block,
			// but we can trim the range needing generation
			desc:           "trims up to date area",
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs:          []tsdb.SingleTenantTSDBIdentifier{tsdbID(0)},
			metas: []bloomshipper.Meta{
				genMeta(9, 20, []int{0}, []bloomshipper.BlockRef{genBlockRef(9, 20)}), // block for same tsdb
				genMeta(9, 20, []int{1}, []bloomshipper.BlockRef{genBlockRef(9, 20)}), // block for different tsdb
			},
			exp: []blockPlan{
				{
					tsdb: tsdbID(0),
					gaps: []protos.GapWithBlocks{
						{
							Bounds: v1.NewBounds(0, 8),
						},
					},
				},
			},
		},
		{
			desc:           "uses old block for overlapping range",
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs:          []tsdb.SingleTenantTSDBIdentifier{tsdbID(0)},
			metas: []bloomshipper.Meta{
				genMeta(9, 20, []int{0}, []bloomshipper.BlockRef{genBlockRef(9, 20)}), // block for same tsdb
				genMeta(5, 20, []int{1}, []bloomshipper.BlockRef{genBlockRef(5, 20)}), // block for different tsdb
			},
			exp: []blockPlan{
				{
					tsdb: tsdbID(0),
					gaps: []protos.GapWithBlocks{
						{
							Bounds: v1.NewBounds(0, 8),
							Blocks: []bloomshipper.BlockRef{genBlockRef(5, 20)},
						},
					},
				},
			},
		},
		{
			desc:           "multi case",
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs:          []tsdb.SingleTenantTSDBIdentifier{tsdbID(0), tsdbID(1)}, // generate for both tsdbs
			metas: []bloomshipper.Meta{
				genMeta(0, 2, []int{0}, []bloomshipper.BlockRef{
					genBlockRef(0, 1),
					genBlockRef(1, 2),
				}), // tsdb_0
				genMeta(6, 8, []int{0}, []bloomshipper.BlockRef{genBlockRef(6, 8)}), // tsdb_0

				genMeta(3, 5, []int{1}, []bloomshipper.BlockRef{genBlockRef(3, 5)}),   // tsdb_1
				genMeta(8, 10, []int{1}, []bloomshipper.BlockRef{genBlockRef(8, 10)}), // tsdb_1
			},
			exp: []blockPlan{
				{
					tsdb: tsdbID(0),
					gaps: []protos.GapWithBlocks{
						// tsdb (id=0) can source chunks from the blocks built from tsdb (id=1)
						{
							Bounds: v1.NewBounds(3, 5),
							Blocks: []bloomshipper.BlockRef{genBlockRef(3, 5)},
						},
						{
							Bounds: v1.NewBounds(9, 10),
							Blocks: []bloomshipper.BlockRef{genBlockRef(8, 10)},
						},
					},
				},
				// tsdb (id=1) can source chunks from the blocks built from tsdb (id=0)
				{
					tsdb: tsdbID(1),
					gaps: []protos.GapWithBlocks{
						{
							Bounds: v1.NewBounds(0, 2),
							Blocks: []bloomshipper.BlockRef{
								genBlockRef(0, 1),
								genBlockRef(1, 2),
							},
						},
						{
							Bounds: v1.NewBounds(6, 7),
							Blocks: []bloomshipper.BlockRef{genBlockRef(6, 8)},
						},
					},
				},
			},
		},
		{
			desc:           "dedupes block refs",
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs:          []tsdb.SingleTenantTSDBIdentifier{tsdbID(0)},
			metas: []bloomshipper.Meta{
				genMeta(9, 20, []int{1}, []bloomshipper.BlockRef{
					genBlockRef(1, 4),
					genBlockRef(9, 20),
				}), // blocks for first diff tsdb
				genMeta(5, 20, []int{2}, []bloomshipper.BlockRef{
					genBlockRef(5, 10),
					genBlockRef(9, 20), // same block references in prior meta (will be deduped)
				}), // block for second diff tsdb
			},
			exp: []blockPlan{
				{
					tsdb: tsdbID(0),
					gaps: []protos.GapWithBlocks{
						{
							Bounds: v1.NewBounds(0, 10),
							Blocks: []bloomshipper.BlockRef{
								genBlockRef(1, 4),
								genBlockRef(5, 10),
								genBlockRef(9, 20),
							},
						},
					},
				},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			// we reuse the gapsBetweenTSDBsAndMetas function to generate the gaps as this function is tested
			// separately and it's used to generate input in our regular code path (easier to write tests this way).
			gaps, err := gapsBetweenTSDBsAndMetas(tc.ownershipRange, tc.tsdbs, tc.metas)
			require.NoError(t, err)

			plans, err := blockPlansForGaps(gaps, tc.metas)
			if tc.err {
				require.Error(t, err)
				return
			}
			require.Equal(t, tc.exp, plans)

		})
	}
}

func createTasks(n int, resultsCh chan *protos.TaskResult) []*QueueTask {
	tasks := make([]*QueueTask, 0, n)
	// Enqueue tasks
	for i := 0; i < n; i++ {
		task := NewQueueTask(
			context.Background(), time.Now(),
			protos.NewTask(config.NewDayTable(testDay, "fake"), "fakeTenant", v1.NewBounds(0, 10), tsdbID(1), nil),
			resultsCh,
		)
		tasks = append(tasks, task)
	}
	return tasks
}

func createPlanner(
	t *testing.T,
	cfg Config,
	limits Limits,
	logger log.Logger,
) *Planner {
	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From: parseDayTime("2023-09-01"),
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

	planner, err := New(cfg, limits, schemaCfg, storageCfg, storage.ClientMetrics{}, bloomStore, logger, reg)
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
				PlanningInterval:        1 * time.Hour,
				MaxQueuedTasksPerTenant: 10000,
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
			require.Equal(t, 0, planner.totalPendingTasks())

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
							return planner.totalPendingTasks() == 0
						},
						5*time.Second, 10*time.Millisecond,
						"tasks not consumed, pending: %d", planner.totalPendingTasks(),
					)
				} else {
					require.Neverf(
						t, func() bool {
							return planner.totalPendingTasks() == 0
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
						return planner.totalPendingTasks() == 0
					},
					5*time.Second, 10*time.Millisecond,
					"tasks not consumed, pending: %d", planner.totalPendingTasks(),
				)
			}
		})
	}
}

func putMetas(bloomClient bloomshipper.Client, metas []bloomshipper.Meta) error {
	for _, meta := range metas {
		err := bloomClient.PutMeta(context.Background(), meta)
		if err != nil {
			return err
		}

		for _, block := range meta.Blocks {
			err := bloomClient.PutBlock(context.Background(), genBlock(block))
			if err != nil {
				return err
			}
		}
	}
	return nil
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
				genMeta(0, 10, []int{0}, []bloomshipper.BlockRef{genBlockRef(0, 10)}),
				genMeta(10, 20, []int{0}, []bloomshipper.BlockRef{genBlockRef(10, 20)}),
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
				genMeta(0, 10, []int{0}, []bloomshipper.BlockRef{genBlockRef(0, 10)}),
				genMeta(10, 20, []int{0}, []bloomshipper.BlockRef{genBlockRef(10, 20)}),
			},
			expectedTasksSucceed: 0,
		},
		{
			name: "no new metas",
			originalMetas: []bloomshipper.Meta{
				genMeta(0, 10, []int{0}, []bloomshipper.BlockRef{genBlockRef(0, 10)}),
				genMeta(10, 20, []int{0}, []bloomshipper.BlockRef{genBlockRef(10, 20)}),
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
				genMeta(0, 10, []int{0}, []bloomshipper.BlockRef{genBlockRef(0, 10)}),
				genMeta(10, 20, []int{0}, []bloomshipper.BlockRef{genBlockRef(10, 20)}),
			},
			expectedTasksSucceed: 2,
		},
		{
			name: "no original metas",
			taskResults: []*protos.TaskResult{
				{
					TaskID: "1",
					CreatedMetas: []bloomshipper.Meta{
						genMeta(0, 10, []int{0}, []bloomshipper.BlockRef{genBlockRef(0, 10)}),
					},
				},
				{
					TaskID: "2",
					CreatedMetas: []bloomshipper.Meta{
						genMeta(10, 20, []int{0}, []bloomshipper.BlockRef{genBlockRef(10, 20)}),
					},
				},
			},
			expectedMetas: []bloomshipper.Meta{
				genMeta(0, 10, []int{0}, []bloomshipper.BlockRef{genBlockRef(0, 10)}),
				genMeta(10, 20, []int{0}, []bloomshipper.BlockRef{genBlockRef(10, 20)}),
			},
			expectedTasksSucceed: 2,
		},
		{
			name: "single meta covers all original",
			originalMetas: []bloomshipper.Meta{
				genMeta(0, 5, []int{0}, []bloomshipper.BlockRef{genBlockRef(0, 5)}),
				genMeta(6, 10, []int{0}, []bloomshipper.BlockRef{genBlockRef(6, 10)}),
			},
			taskResults: []*protos.TaskResult{
				{
					TaskID: "1",
					CreatedMetas: []bloomshipper.Meta{
						genMeta(0, 10, []int{1}, []bloomshipper.BlockRef{genBlockRef(0, 10)}),
					},
				},
			},
			expectedMetas: []bloomshipper.Meta{
				genMeta(0, 10, []int{1}, []bloomshipper.BlockRef{genBlockRef(0, 10)}),
			},
			expectedTasksSucceed: 1,
		},
		{
			name: "multi version ordering",
			originalMetas: []bloomshipper.Meta{
				genMeta(0, 5, []int{0}, []bloomshipper.BlockRef{genBlockRef(0, 5)}),
				genMeta(0, 10, []int{1}, []bloomshipper.BlockRef{genBlockRef(0, 10)}), // only part of the range is outdated, must keep
			},
			taskResults: []*protos.TaskResult{
				{
					TaskID: "1",
					CreatedMetas: []bloomshipper.Meta{
						genMeta(8, 10, []int{2}, []bloomshipper.BlockRef{genBlockRef(8, 10)}),
					},
				},
			},
			expectedMetas: []bloomshipper.Meta{
				genMeta(0, 10, []int{1}, []bloomshipper.BlockRef{genBlockRef(0, 10)}),
				genMeta(8, 10, []int{2}, []bloomshipper.BlockRef{genBlockRef(8, 10)}),
			},
			expectedTasksSucceed: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := log.NewNopLogger()
			//logger := log.NewLogfmtLogger(os.Stdout)

			cfg := Config{
				PlanningInterval:        1 * time.Hour,
				MaxQueuedTasksPerTenant: 10000,
			}
			planner := createPlanner(t, cfg, &fakeLimits{}, logger)

			bloomClient, err := planner.bloomStore.Client(testDay.ModelTime())
			require.NoError(t, err)

			// Create original metas and blocks
			err = putMetas(bloomClient, tc.originalMetas)
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
					testTable,
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
					err = putMetas(bloomClient, taskResult.CreatedMetas)
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
					Interval: bloomshipper.NewInterval(testTable.Bounds()),
					Keyspace: v1.NewBounds(0, math.MaxUint64),
				},
			)
			require.NoError(t, err)

			// TODO(salvacorts): Fix this
			// For some reason, when the tests are run in the CI, we do not encode the `loc` of model.Time for each TSDB.
			// As a result, when we fetch them, the loc is empty whereas in the original metas, it is not. Therefore the
			// comparison fails. As a workaround to fix the issue, we will manually reset the TS of the sources to the
			// fetched metas
			for i := range metas {
				for j := range metas[i].Sources {
					sec := metas[i].Sources[j].TS.Unix()
					nsec := metas[i].Sources[j].TS.Nanosecond()
					metas[i].Sources[j].TS = time.Unix(sec, int64(nsec))
				}
			}

			// Compare metas
			require.Equal(t, len(tc.expectedMetas), len(metas))
			require.ElementsMatch(t, tc.expectedMetas, metas)
		})
	}
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
	if len(f.tasks) == 0 {
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

type fakeLimits struct {
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

func parseDayTime(s string) config.DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return config.DayTime{
		Time: model.TimeFromUnix(t.Unix()),
	}
}

type DummyReadSeekCloser struct{}

func (d *DummyReadSeekCloser) Read(_ []byte) (n int, err error) {
	return 0, io.EOF
}

func (d *DummyReadSeekCloser) Seek(_ int64, _ int) (int64, error) {
	return 0, nil
}

func (d *DummyReadSeekCloser) Close() error {
	return nil
}
