package strategies

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/bloombuild/common"
	"github.com/grafana/loki/v3/pkg/bloombuild/planner/plannertest"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
)

func taskForGap(tsdb tsdb.SingleTenantTSDBIdentifier, forSeries common.ClosableForSeries, bounds v1.FingerprintBounds, blocks []bloomshipper.BlockRef) *Task {
	return NewTask(plannertest.TestTable, "fake", bounds, tsdb, forSeries, []Gap{
		{
			Bounds: bounds,
			Series: plannertest.GenSeriesWithStep(bounds, 10),
			Blocks: blocks,
		},
	})
}

func Test_ChunkSizeStrategy_Plan(t *testing.T) {
	forSeries := plannertest.NewFakeForSeries(plannertest.GenV1SeriesWithStep(v1.NewBounds(0, 100), 10))

	for _, tc := range []struct {
		name          string
		limits        ChunkSizeStrategyLimits
		originalMetas []bloomshipper.Meta
		tsdbs         TSDBSet
		expectedTasks []*Task
	}{
		{
			name:   "no previous blocks and metas",
			limits: fakeChunkSizeLimits{TargetSize: 200 * 1 << 10}, // 2 series (100KB each) per task

			// Each series will have 1 chunk of 100KB each
			tsdbs: TSDBSet{
				plannertest.TsdbID(0): forSeries, // 10 series
			},

			// We expect 5 tasks, each with 2 series each
			expectedTasks: []*Task{
				taskForGap(plannertest.TsdbID(0), forSeries, v1.NewBounds(0, 10), nil),
				taskForGap(plannertest.TsdbID(0), forSeries, v1.NewBounds(20, 30), nil),
				taskForGap(plannertest.TsdbID(0), forSeries, v1.NewBounds(40, 50), nil),
				taskForGap(plannertest.TsdbID(0), forSeries, v1.NewBounds(60, 70), nil),
				taskForGap(plannertest.TsdbID(0), forSeries, v1.NewBounds(80, 90), nil),
				taskForGap(plannertest.TsdbID(0), forSeries, v1.NewBounds(100, 100), nil),
			},
		},
		{
			name:   "previous metas with no gaps",
			limits: fakeChunkSizeLimits{TargetSize: 200 * 1 << 10},

			// Original metas cover the entire range
			// One meta for each 2 series w/ 1 block per series
			originalMetas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 10, []int{0}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(0, 0),
					plannertest.GenBlockRef(10, 10),
				}),
				plannertest.GenMeta(20, 30, []int{0}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(20, 20),
					plannertest.GenBlockRef(30, 30),
				}),
				plannertest.GenMeta(40, 50, []int{0}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(40, 40),
					plannertest.GenBlockRef(50, 50),
				}),
				plannertest.GenMeta(60, 70, []int{0}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(60, 60),
					plannertest.GenBlockRef(70, 70),
				}),
				plannertest.GenMeta(80, 90, []int{0}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(80, 80),
					plannertest.GenBlockRef(90, 90),
				}),
				plannertest.GenMeta(100, 100, []int{0}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(100, 100),
				}),
			},

			tsdbs: TSDBSet{
				plannertest.TsdbID(0): forSeries, // 10 series
			},

			// We expect no tasks
			expectedTasks: []*Task{},
		},
		{
			name:   "Original metas do not cover the entire range",
			limits: fakeChunkSizeLimits{TargetSize: 200 * 1 << 10},

			// Original metas cover only part of the range
			// Original metas cover the entire range
			// One meta for each 2 series w/ 1 block per series
			originalMetas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 10, []int{0}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(0, 0),
					plannertest.GenBlockRef(10, 10),
				}),
				// Missing meta for 20-30
				plannertest.GenMeta(40, 50, []int{0}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(40, 40),
					plannertest.GenBlockRef(50, 50),
				}),
				plannertest.GenMeta(60, 70, []int{0}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(60, 60),
					plannertest.GenBlockRef(70, 70),
				}),
				plannertest.GenMeta(80, 90, []int{0}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(80, 80),
					plannertest.GenBlockRef(90, 90),
				}),
				plannertest.GenMeta(100, 100, []int{0}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(100, 100),
				}),
			},

			tsdbs: TSDBSet{
				plannertest.TsdbID(0): forSeries, // 10 series
			},

			// We expect 1 tasks for the missing series
			expectedTasks: []*Task{
				taskForGap(plannertest.TsdbID(0), forSeries, v1.NewBounds(20, 30), nil),
			},
		},
		{
			name:   "All metas are outdated",
			limits: fakeChunkSizeLimits{TargetSize: 200 * 1 << 10},

			originalMetas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 100, []int{0}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(0, 0),
					plannertest.GenBlockRef(10, 10),
					plannertest.GenBlockRef(20, 20),
					plannertest.GenBlockRef(30, 30),
					plannertest.GenBlockRef(40, 40),
					plannertest.GenBlockRef(50, 50),
					plannertest.GenBlockRef(60, 60),
					plannertest.GenBlockRef(70, 70),
					plannertest.GenBlockRef(80, 80),
					plannertest.GenBlockRef(90, 90),
					plannertest.GenBlockRef(100, 100),
				}),
			},

			tsdbs: TSDBSet{
				plannertest.TsdbID(1): forSeries, // 10 series
			},

			// We expect 5 tasks, each with 2 series each
			expectedTasks: []*Task{
				taskForGap(plannertest.TsdbID(1), forSeries, v1.NewBounds(0, 10), []bloomshipper.BlockRef{
					plannertest.GenBlockRef(0, 0),
					plannertest.GenBlockRef(10, 10),
				}),
				taskForGap(plannertest.TsdbID(1), forSeries, v1.NewBounds(20, 30), []bloomshipper.BlockRef{
					plannertest.GenBlockRef(20, 20),
					plannertest.GenBlockRef(30, 30),
				}),
				taskForGap(plannertest.TsdbID(1), forSeries, v1.NewBounds(40, 50), []bloomshipper.BlockRef{
					plannertest.GenBlockRef(40, 40),
					plannertest.GenBlockRef(50, 50),
				}),
				taskForGap(plannertest.TsdbID(1), forSeries, v1.NewBounds(60, 70), []bloomshipper.BlockRef{
					plannertest.GenBlockRef(60, 60),
					plannertest.GenBlockRef(70, 70),
				}),
				taskForGap(plannertest.TsdbID(1), forSeries, v1.NewBounds(80, 90), []bloomshipper.BlockRef{
					plannertest.GenBlockRef(80, 80),
					plannertest.GenBlockRef(90, 90),
				}),
				taskForGap(plannertest.TsdbID(1), forSeries, v1.NewBounds(100, 100), []bloomshipper.BlockRef{
					plannertest.GenBlockRef(100, 100),
				}),
			},
		},
		{
			name:   "Some metas are outdated",
			limits: fakeChunkSizeLimits{TargetSize: 200 * 1 << 10},

			originalMetas: []bloomshipper.Meta{
				// Outdated meta
				plannertest.GenMeta(0, 49, []int{0}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(0, 0),
					plannertest.GenBlockRef(10, 10),
					plannertest.GenBlockRef(20, 20),
					plannertest.GenBlockRef(30, 30),
					plannertest.GenBlockRef(40, 40),
				}),
				// Updated meta
				plannertest.GenMeta(50, 100, []int{1}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(50, 50),
					plannertest.GenBlockRef(60, 60),
					plannertest.GenBlockRef(70, 70),
					plannertest.GenBlockRef(80, 80),
					plannertest.GenBlockRef(90, 90),
					plannertest.GenBlockRef(100, 100),
				}),
			},

			tsdbs: TSDBSet{
				plannertest.TsdbID(1): forSeries, // 10 series
			},

			// We expect 5 tasks, each with 2 series each
			expectedTasks: []*Task{
				taskForGap(plannertest.TsdbID(1), forSeries, v1.NewBounds(0, 10), []bloomshipper.BlockRef{
					plannertest.GenBlockRef(0, 0),
					plannertest.GenBlockRef(10, 10),
				}),
				taskForGap(plannertest.TsdbID(1), forSeries, v1.NewBounds(20, 30), []bloomshipper.BlockRef{
					plannertest.GenBlockRef(20, 20),
					plannertest.GenBlockRef(30, 30),
				}),
				taskForGap(plannertest.TsdbID(1), forSeries, v1.NewBounds(40, 40), []bloomshipper.BlockRef{
					plannertest.GenBlockRef(40, 40),
				}),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := log.NewNopLogger()
			//logger := log.NewLogfmtLogger(os.Stdout)

			strategy, err := NewChunkSizeStrategy(tc.limits, logger)
			require.NoError(t, err)

			actual, err := strategy.Plan(context.Background(), plannertest.TestTable, "fake", tc.tsdbs, tc.originalMetas)
			require.NoError(t, err)

			require.ElementsMatch(t, tc.expectedTasks, actual)
		})
	}
}

type fakeChunkSizeLimits struct {
	TargetSize uint64
}

func (f fakeChunkSizeLimits) BloomTaskTargetSeriesChunksSizeBytes(_ string) uint64 {
	return f.TargetSize
}
