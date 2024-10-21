package strategies

import (
	"context"
	"github.com/grafana/loki/v3/pkg/bloombuild/planner/test_utils"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
)

func taskForGap(bounds v1.FingerprintBounds) *protos.Task {
	return protos.NewTask(test_utils.TestTable, "fake", bounds, test_utils.TsdbID(0), []protos.Gap{
		{
			Bounds: bounds,
			Series: test_utils.GenSeriesWithStep(bounds, 10),
			Blocks: nil,
		},
	})
}

func Test_ChunkSizeStrategy_Plan(t *testing.T) {
	for _, tc := range []struct {
		name          string
		limits        ChunkSizeStrategyLimits
		originalMetas []bloomshipper.Meta
		tsdbs         TSDBSet
		expectedTasks []*protos.Task
	}{
		{
			name:   "no previous blocks and metas",
			limits: fakeChunkSizeLimits{TargetSize: 200*2 ^ 10}, // 2 series (100KB each) per task

			// Each series will have 1 chunk of 100KB each
			tsdbs: TSDBSet{
				test_utils.TsdbID(0): newFakeForSeries(test_utils.GenSeriesWithStep(v1.NewBounds(0, 100), 10)), // 10 series
			},

			// We expect 5 tasks, each with 2 series each
			expectedTasks: []*protos.Task{
				taskForGap(v1.NewBounds(0, 10)),
				taskForGap(v1.NewBounds(20, 30)),
				taskForGap(v1.NewBounds(40, 50)),
				taskForGap(v1.NewBounds(60, 70)),
				taskForGap(v1.NewBounds(80, 90)),
			},
		},
		{
			name:   "previous metas with no gaps",
			limits: fakeChunkSizeLimits{TargetSize: 200*2 ^ 10},

			// Original metas cover the entire range
			// One meta for each 2 series w/ 1 block per series
			originalMetas: []bloomshipper.Meta{
				test_utils.GenMeta(0, 10, []int{0}, []bloomshipper.BlockRef{
					test_utils.GenBlockRef(0, 0),
					test_utils.GenBlockRef(10, 10),
				}),
				test_utils.GenMeta(20, 30, []int{0}, []bloomshipper.BlockRef{
					test_utils.GenBlockRef(20, 20),
					test_utils.GenBlockRef(30, 30),
				}),
				test_utils.GenMeta(40, 50, []int{0}, []bloomshipper.BlockRef{
					test_utils.GenBlockRef(40, 40),
					test_utils.GenBlockRef(50, 50),
				}),
				test_utils.GenMeta(60, 70, []int{0}, []bloomshipper.BlockRef{
					test_utils.GenBlockRef(60, 60),
					test_utils.GenBlockRef(70, 70),
				}),
				test_utils.GenMeta(80, 90, []int{0}, []bloomshipper.BlockRef{
					test_utils.GenBlockRef(80, 80),
					test_utils.GenBlockRef(90, 90),
				}),
			},

			tsdbs: TSDBSet{
				test_utils.TsdbID(0): newFakeForSeries(test_utils.GenSeriesWithStep(v1.NewBounds(0, 100), 10)), // 10 series
			},

			// We expect no tasks
			expectedTasks: []*protos.Task{},
		},
		{
			name:   "Original metas do not cover the entire range",
			limits: fakeChunkSizeLimits{TargetSize: 200*2 ^ 10},

			// Original metas cover only part of the range
			// Original metas cover the entire range
			// One meta for each 2 series w/ 1 block per series
			originalMetas: []bloomshipper.Meta{
				test_utils.GenMeta(0, 10, []int{0}, []bloomshipper.BlockRef{
					test_utils.GenBlockRef(0, 0),
					test_utils.GenBlockRef(10, 10),
				}),
				test_utils.GenMeta(20, 30, []int{0}, []bloomshipper.BlockRef{
					test_utils.GenBlockRef(20, 20),
					// Missing block for 30
				}),
				test_utils.GenMeta(40, 50, []int{0}, []bloomshipper.BlockRef{
					test_utils.GenBlockRef(40, 40),
					test_utils.GenBlockRef(50, 50),
				}),
				test_utils.GenMeta(60, 70, []int{0}, []bloomshipper.BlockRef{
					test_utils.GenBlockRef(60, 60),
					test_utils.GenBlockRef(70, 70),
				}),
				test_utils.GenMeta(80, 90, []int{0}, []bloomshipper.BlockRef{
					test_utils.GenBlockRef(80, 80),
					test_utils.GenBlockRef(90, 90),
				}),
			},

			tsdbs: TSDBSet{
				test_utils.TsdbID(0): newFakeForSeries(test_utils.GenSeriesWithStep(v1.NewBounds(0, 100), 10)), // 10 series
			},

			// We expect 1 tasks for the missing series
			expectedTasks: []*protos.Task{
				taskForGap(v1.NewBounds(30, 30)),
			},
		},
		{
			name:   "Original metas covers the entire range but series are missing",
			limits: fakeChunkSizeLimits{TargetSize: 200*2 ^ 10},

			// Original metas cover only part of the range
			// Original metas cover the entire range
			// One meta for each 2 series w/ 1 block per series
			originalMetas: []bloomshipper.Meta{
				test_utils.GenMeta(0, 10, []int{0}, []bloomshipper.BlockRef{
					test_utils.GenBlockRef(0, 0),
					test_utils.GenBlockRef(10, 10),
				}),
				// Missing meta for 20-30
				test_utils.GenMeta(40, 50, []int{0}, []bloomshipper.BlockRef{
					test_utils.GenBlockRef(40, 40),
					test_utils.GenBlockRef(50, 50),
				}),
				test_utils.GenMeta(60, 70, []int{0}, []bloomshipper.BlockRef{
					test_utils.GenBlockRef(60, 60),
					test_utils.GenBlockRef(70, 70),
				}),
				test_utils.GenMeta(80, 90, []int{0}, []bloomshipper.BlockRef{
					test_utils.GenBlockRef(80, 80),
					test_utils.GenBlockRef(90, 90),
				}),
			},

			tsdbs: TSDBSet{
				test_utils.TsdbID(0): newFakeForSeries(test_utils.GenSeriesWithStep(v1.NewBounds(0, 100), 10)), // 10 series
			},

			// We expect 1 tasks for the missing series
			expectedTasks: []*protos.Task{
				taskForGap(v1.NewBounds(20, 30)),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := log.NewNopLogger()
			//logger := log.NewLogfmtLogger(os.Stdout)

			strategy, err := NewChunkSizeStrategy(tc.limits, logger)
			require.NoError(t, err)

			actual, err := strategy.Plan(context.Background(), test_utils.TestTable, "fake", tc.tsdbs, tc.originalMetas)
			require.NoError(t, err)

			require.ElementsMatch(t, tc.expectedTasks, actual)
		})
	}
}

type fakeChunkSizeLimits struct {
	TargetSize uint64
}

func (f fakeChunkSizeLimits) BloomTaskTargetChunkSizeBytes(tenantID string) uint64 {
	return f.TargetSize
}
