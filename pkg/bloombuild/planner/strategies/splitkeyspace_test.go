package strategies

import (
	"context"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/bloombuild/common"
	"github.com/grafana/loki/v3/pkg/bloombuild/planner/plannertest"
	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func Test_gapsBetweenTSDBsAndMetas(t *testing.T) {
	for _, tc := range []struct {
		desc           string
		err            bool
		exp            []tsdbGaps
		ownershipRange v1.FingerprintBounds
		tsdbs          map[tsdb.SingleTenantTSDBIdentifier]common.ClosableForSeries
		metas          []bloomshipper.Meta
	}{
		{
			desc:           "non-overlapping tsdbs and metas",
			err:            true,
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs: map[tsdb.SingleTenantTSDBIdentifier]common.ClosableForSeries{
				plannertest.TsdbID(0): nil,
			},
			metas: []bloomshipper.Meta{
				plannertest.GenMeta(11, 20, []int{0}, nil),
			},
		},
		{
			desc:           "single tsdb",
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs: map[tsdb.SingleTenantTSDBIdentifier]common.ClosableForSeries{
				plannertest.TsdbID(0): nil,
			},
			metas: []bloomshipper.Meta{
				plannertest.GenMeta(4, 8, []int{0}, nil),
			},
			exp: []tsdbGaps{
				{
					tsdbIdentifier: plannertest.TsdbID(0),
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
			tsdbs: map[tsdb.SingleTenantTSDBIdentifier]common.ClosableForSeries{
				plannertest.TsdbID(0): nil,
				plannertest.TsdbID(1): nil,
			},
			metas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 5, []int{0}, nil),
				plannertest.GenMeta(6, 10, []int{1}, nil),
			},
			exp: []tsdbGaps{
				{
					tsdbIdentifier: plannertest.TsdbID(0),
					gaps: []v1.FingerprintBounds{
						v1.NewBounds(6, 10),
					},
				},
				{
					tsdbIdentifier: plannertest.TsdbID(1),
					gaps: []v1.FingerprintBounds{
						v1.NewBounds(0, 5),
					},
				},
			},
		},
		{
			desc:           "multiple tsdbs with the same blocks",
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs: map[tsdb.SingleTenantTSDBIdentifier]common.ClosableForSeries{
				plannertest.TsdbID(0): nil,
				plannertest.TsdbID(1): nil,
			},
			metas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 5, []int{0, 1}, nil),
				plannertest.GenMeta(6, 8, []int{1}, nil),
			},
			exp: []tsdbGaps{
				{
					tsdbIdentifier: plannertest.TsdbID(0),
					gaps: []v1.FingerprintBounds{
						v1.NewBounds(6, 10),
					},
				},
				{
					tsdbIdentifier: plannertest.TsdbID(1),
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
			require.ElementsMatch(t, tc.exp, gaps)
		})
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
			tsdbs:          []tsdb.SingleTenantTSDBIdentifier{plannertest.TsdbID(0)},
			metas: []bloomshipper.Meta{
				plannertest.GenMeta(5, 20, []int{1}, []bloomshipper.BlockRef{plannertest.GenBlockRef(11, 20)}),
			},
			exp: []blockPlan{
				{
					tsdb: plannertest.TsdbID(0),
					gaps: []protos.Gap{
						{
							Bounds: v1.NewBounds(0, 10),
							Series: plannertest.GenSeries(v1.NewBounds(0, 10)),
						},
					},
				},
			},
		},
		{
			desc:           "single overlapping meta+one overlapping block",
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs:          []tsdb.SingleTenantTSDBIdentifier{plannertest.TsdbID(0)},
			metas: []bloomshipper.Meta{
				plannertest.GenMeta(5, 20, []int{1}, []bloomshipper.BlockRef{plannertest.GenBlockRef(9, 20)}),
			},
			exp: []blockPlan{
				{
					tsdb: plannertest.TsdbID(0),
					gaps: []protos.Gap{
						{
							Bounds: v1.NewBounds(0, 10),
							Series: plannertest.GenSeries(v1.NewBounds(0, 10)),
							Blocks: []bloomshipper.BlockRef{plannertest.GenBlockRef(9, 20)},
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
			tsdbs:          []tsdb.SingleTenantTSDBIdentifier{plannertest.TsdbID(0)},
			metas: []bloomshipper.Meta{
				plannertest.GenMeta(9, 20, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(9, 20)}), // block for same tsdb
				plannertest.GenMeta(9, 20, []int{1}, []bloomshipper.BlockRef{plannertest.GenBlockRef(9, 20)}), // block for different tsdb
			},
			exp: []blockPlan{
				{
					tsdb: plannertest.TsdbID(0),
					gaps: []protos.Gap{
						{
							Bounds: v1.NewBounds(0, 8),
							Series: plannertest.GenSeries(v1.NewBounds(0, 8)),
						},
					},
				},
			},
		},
		{
			desc:           "uses old block for overlapping range",
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs:          []tsdb.SingleTenantTSDBIdentifier{plannertest.TsdbID(0)},
			metas: []bloomshipper.Meta{
				plannertest.GenMeta(9, 20, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(9, 20)}), // block for same tsdb
				plannertest.GenMeta(5, 20, []int{1}, []bloomshipper.BlockRef{plannertest.GenBlockRef(5, 20)}), // block for different tsdb
			},
			exp: []blockPlan{
				{
					tsdb: plannertest.TsdbID(0),
					gaps: []protos.Gap{
						{
							Bounds: v1.NewBounds(0, 8),
							Series: plannertest.GenSeries(v1.NewBounds(0, 8)),
							Blocks: []bloomshipper.BlockRef{plannertest.GenBlockRef(5, 20)},
						},
					},
				},
			},
		},
		{
			desc:           "multi case",
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs:          []tsdb.SingleTenantTSDBIdentifier{plannertest.TsdbID(0), plannertest.TsdbID(1)}, // generate for both tsdbs
			metas: []bloomshipper.Meta{
				plannertest.GenMeta(0, 2, []int{0}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(0, 1),
					plannertest.GenBlockRef(1, 2),
				}), // tsdb_0
				plannertest.GenMeta(6, 8, []int{0}, []bloomshipper.BlockRef{plannertest.GenBlockRef(6, 8)}), // tsdb_0

				plannertest.GenMeta(3, 5, []int{1}, []bloomshipper.BlockRef{plannertest.GenBlockRef(3, 5)}),   // tsdb_1
				plannertest.GenMeta(8, 10, []int{1}, []bloomshipper.BlockRef{plannertest.GenBlockRef(8, 10)}), // tsdb_1
			},
			exp: []blockPlan{
				{
					tsdb: plannertest.TsdbID(0),
					gaps: []protos.Gap{
						// tsdb (id=0) can source chunks from the blocks built from tsdb (id=1)
						{
							Bounds: v1.NewBounds(3, 5),
							Series: plannertest.GenSeries(v1.NewBounds(3, 5)),
							Blocks: []bloomshipper.BlockRef{plannertest.GenBlockRef(3, 5)},
						},
						{
							Bounds: v1.NewBounds(9, 10),
							Series: plannertest.GenSeries(v1.NewBounds(9, 10)),
							Blocks: []bloomshipper.BlockRef{plannertest.GenBlockRef(8, 10)},
						},
					},
				},
				// tsdb (id=1) can source chunks from the blocks built from tsdb (id=0)
				{
					tsdb: plannertest.TsdbID(1),
					gaps: []protos.Gap{
						{
							Bounds: v1.NewBounds(0, 2),
							Series: plannertest.GenSeries(v1.NewBounds(0, 2)),
							Blocks: []bloomshipper.BlockRef{
								plannertest.GenBlockRef(0, 1),
								plannertest.GenBlockRef(1, 2),
							},
						},
						{
							Bounds: v1.NewBounds(6, 7),
							Series: plannertest.GenSeries(v1.NewBounds(6, 7)),
							Blocks: []bloomshipper.BlockRef{plannertest.GenBlockRef(6, 8)},
						},
					},
				},
			},
		},
		{
			desc:           "dedupes block refs",
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs:          []tsdb.SingleTenantTSDBIdentifier{plannertest.TsdbID(0)},
			metas: []bloomshipper.Meta{
				plannertest.GenMeta(9, 20, []int{1}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(1, 4),
					plannertest.GenBlockRef(9, 20),
				}), // blocks for first diff tsdb
				plannertest.GenMeta(5, 20, []int{2}, []bloomshipper.BlockRef{
					plannertest.GenBlockRef(5, 10),
					plannertest.GenBlockRef(9, 20), // same block references in prior meta (will be deduped)
				}), // block for second diff tsdb
			},
			exp: []blockPlan{
				{
					tsdb: plannertest.TsdbID(0),
					gaps: []protos.Gap{
						{
							Bounds: v1.NewBounds(0, 10),
							Series: plannertest.GenSeries(v1.NewBounds(0, 10)),
							Blocks: []bloomshipper.BlockRef{
								plannertest.GenBlockRef(1, 4),
								plannertest.GenBlockRef(5, 10),
								plannertest.GenBlockRef(9, 20),
							},
						},
					},
				},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			// We add series spanning the whole FP ownership range
			tsdbs := make(map[tsdb.SingleTenantTSDBIdentifier]common.ClosableForSeries)
			for _, id := range tc.tsdbs {
				tsdbs[id] = newFakeForSeries(plannertest.GenSeries(tc.ownershipRange))
			}

			// we reuse the gapsBetweenTSDBsAndMetas function to generate the gaps as this function is tested
			// separately and it's used to generate input in our regular code path (easier to write tests this way).
			gaps, err := gapsBetweenTSDBsAndMetas(tc.ownershipRange, tsdbs, tc.metas)
			require.NoError(t, err)

			plans, err := blockPlansForGaps(
				context.Background(),
				"fakeTenant",
				gaps,
				tc.metas,
			)
			if tc.err {
				require.Error(t, err)
				return
			}
			require.ElementsMatch(t, tc.exp, plans)
		})
	}
}

type fakeForSeries struct {
	series []*v1.Series
}

func newFakeForSeries(series []*v1.Series) *fakeForSeries {
	return &fakeForSeries{
		series: series,
	}
}

func (f fakeForSeries) ForSeries(_ context.Context, _ string, ff index.FingerprintFilter, _ model.Time, _ model.Time, fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta) (stop bool), _ ...*labels.Matcher) error {
	overlapping := make([]*v1.Series, 0, len(f.series))
	for _, s := range f.series {
		if ff.Match(s.Fingerprint) {
			overlapping = append(overlapping, s)
		}
	}

	for _, s := range overlapping {
		chunks := make([]index.ChunkMeta, 0, len(s.Chunks))
		for _, c := range s.Chunks {
			chunks = append(chunks, index.ChunkMeta{
				MinTime:  int64(c.From),
				MaxTime:  int64(c.Through),
				Checksum: c.Checksum,
				KB:       100,
			})
		}

		if fn(labels.EmptyLabels(), s.Fingerprint, chunks) {
			break
		}
	}
	return nil
}

func (f fakeForSeries) Close() error {
	return nil
}
