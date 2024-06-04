package planner

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	"github.com/grafana/loki/v3/pkg/storage"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	bloomshipperconfig "github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/types"
)

func tsdbID(n int) tsdb.SingleTenantTSDBIdentifier {
	return tsdb.SingleTenantTSDBIdentifier{
		TS: time.Unix(int64(n), 0),
	}
}

func genMeta(min, max model.Fingerprint, sources []int, blocks []bloomshipper.BlockRef) bloomshipper.Meta {
	m := bloomshipper.Meta{
		MetaRef: bloomshipper.MetaRef{
			Ref: bloomshipper.Ref{
				Bounds: v1.NewBounds(min, max),
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
	bounds := v1.NewBounds(min, max)
	return bloomshipper.BlockRef{
		Ref: bloomshipper.Ref{
			Bounds: bounds,
		},
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

func Test_BuilderLoop(t *testing.T) {
	const (
		nTasks    = 100
		nBuilders = 10
	)
	logger := log.NewNopLogger()

	limits := &fakeLimits{}
	cfg := Config{
		PlanningInterval:        1 * time.Hour,
		MaxQueuedTasksPerTenant: 10000,
	}
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

	// Create planner
	planner, err := New(cfg, limits, schemaCfg, storageCfg, storage.NewClientMetrics(), nil, logger, prometheus.DefaultRegisterer)
	require.NoError(t, err)

	// Start planner
	err = services.StartAndAwaitRunning(context.Background(), planner)
	require.NoError(t, err)
	t.Cleanup(func() {
		err := services.StopAndAwaitTerminated(context.Background(), planner)
		require.NoError(t, err)
	})

	// Enqueue tasks
	for i := 0; i < nTasks; i++ {
		task := NewTask(
			context.Background(), time.Now(),
			protos.NewTask(config.NewDayTable(config.NewDayTime(0), "fake"), "fakeTenant", v1.NewBounds(0, 10), tsdbID(1), nil),
		)

		err = planner.enqueueTask(task)
		require.NoError(t, err)
	}

	// All tasks should be pending
	require.Equal(t, nTasks, planner.totalPendingTasks())

	// Create builders and call planner.BuilderLoop
	builders := make([]*fakeBuilder, 0, nBuilders)
	for i := 0; i < nBuilders; i++ {
		builder := newMockBuilder(fmt.Sprintf("builder-%d", i))
		builders = append(builders, builder)

		go func() {
			// We ignore the error since when the planner is stopped,
			// the loop will return an error (queue closed)
			_ = planner.BuilderLoop(builder)
		}()
	}

	// Eventually, all tasks should be sent to builders
	require.Eventually(t, func() bool {
		var receivedTasks int
		for _, builder := range builders {
			receivedTasks += len(builder.ReceivedTasks())
		}
		return receivedTasks == nTasks
	}, 15*time.Second, 10*time.Millisecond)

	// Finally, the queue should be empty
	require.Equal(t, 0, planner.totalPendingTasks())
}

type fakeBuilder struct {
	id    string
	tasks []*protos.Task
	grpc.ServerStream
}

func newMockBuilder(id string) *fakeBuilder {
	return &fakeBuilder{id: id}
}

func (f *fakeBuilder) ReceivedTasks() []*protos.Task {
	return f.tasks
}

func (f *fakeBuilder) Context() context.Context {
	return context.Background()
}

func (f *fakeBuilder) Send(req *protos.PlannerToBuilder) error {
	task, err := protos.FromProtoTask(req.Task)
	if err != nil {
		return err
	}

	f.tasks = append(f.tasks, task)
	return nil
}

func (f *fakeBuilder) Recv() (*protos.BuilderToPlanner, error) {
	return &protos.BuilderToPlanner{
		BuilderID: f.id,
	}, nil
}

type fakeLimits struct {
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

func parseDayTime(s string) config.DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return config.DayTime{
		Time: model.TimeFromUnix(t.Unix()),
	}
}
