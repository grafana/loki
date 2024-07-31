package splitkeyspace

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/bloombuild/common"
	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

var testDay = parseDayTime("2023-09-01")
var testTable = config.NewDayTable(testDay, "index_")

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
				tsdbID(0): nil,
			},
			metas: []bloomshipper.Meta{
				genMeta(11, 20, []int{0}, nil),
			},
		},
		{
			desc:           "single tsdb",
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs: map[tsdb.SingleTenantTSDBIdentifier]common.ClosableForSeries{
				tsdbID(0): nil,
			},
			metas: []bloomshipper.Meta{
				genMeta(4, 8, []int{0}, nil),
			},
			exp: []tsdbGaps{
				{
					tsdbIdentifier: tsdbID(0),
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
				tsdbID(0): nil,
				tsdbID(1): nil,
			},
			metas: []bloomshipper.Meta{
				genMeta(0, 5, []int{0}, nil),
				genMeta(6, 10, []int{1}, nil),
			},
			exp: []tsdbGaps{
				{
					tsdbIdentifier: tsdbID(0),
					gaps: []v1.FingerprintBounds{
						v1.NewBounds(6, 10),
					},
				},
				{
					tsdbIdentifier: tsdbID(1),
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
				tsdbID(0): nil,
				tsdbID(1): nil,
			},
			metas: []bloomshipper.Meta{
				genMeta(0, 5, []int{0, 1}, nil),
				genMeta(6, 8, []int{1}, nil),
			},
			exp: []tsdbGaps{
				{
					tsdbIdentifier: tsdbID(0),
					gaps: []v1.FingerprintBounds{
						v1.NewBounds(6, 10),
					},
				},
				{
					tsdbIdentifier: tsdbID(1),
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
					gaps: []protos.Gap{
						{
							Bounds: v1.NewBounds(0, 10),
							Series: genSeries(v1.NewBounds(0, 10)),
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
					gaps: []protos.Gap{
						{
							Bounds: v1.NewBounds(0, 10),
							Series: genSeries(v1.NewBounds(0, 10)),
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
					gaps: []protos.Gap{
						{
							Bounds: v1.NewBounds(0, 8),
							Series: genSeries(v1.NewBounds(0, 8)),
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
					gaps: []protos.Gap{
						{
							Bounds: v1.NewBounds(0, 8),
							Series: genSeries(v1.NewBounds(0, 8)),
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
					gaps: []protos.Gap{
						// tsdb (id=0) can source chunks from the blocks built from tsdb (id=1)
						{
							Bounds: v1.NewBounds(3, 5),
							Series: genSeries(v1.NewBounds(3, 5)),
							Blocks: []bloomshipper.BlockRef{genBlockRef(3, 5)},
						},
						{
							Bounds: v1.NewBounds(9, 10),
							Series: genSeries(v1.NewBounds(9, 10)),
							Blocks: []bloomshipper.BlockRef{genBlockRef(8, 10)},
						},
					},
				},
				// tsdb (id=1) can source chunks from the blocks built from tsdb (id=0)
				{
					tsdb: tsdbID(1),
					gaps: []protos.Gap{
						{
							Bounds: v1.NewBounds(0, 2),
							Series: genSeries(v1.NewBounds(0, 2)),
							Blocks: []bloomshipper.BlockRef{
								genBlockRef(0, 1),
								genBlockRef(1, 2),
							},
						},
						{
							Bounds: v1.NewBounds(6, 7),
							Series: genSeries(v1.NewBounds(6, 7)),
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
					gaps: []protos.Gap{
						{
							Bounds: v1.NewBounds(0, 10),
							Series: genSeries(v1.NewBounds(0, 10)),
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
			// We add series spanning the whole FP ownership range
			tsdbs := make(map[tsdb.SingleTenantTSDBIdentifier]common.ClosableForSeries)
			for _, id := range tc.tsdbs {
				tsdbs[id] = newFakeForSeries(genSeries(tc.ownershipRange))
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
			require.Equal(t, tc.exp, plans)
		})
	}
}

func genSeries(bounds v1.FingerprintBounds) []*v1.Series {
	series := make([]*v1.Series, 0, int(bounds.Max-bounds.Min+1))
	for i := bounds.Min; i <= bounds.Max; i++ {
		series = append(series, &v1.Series{
			Fingerprint: i,
			Chunks: v1.ChunkRefs{
				{
					From:     0,
					Through:  1,
					Checksum: 1,
				},
			},
		})
	}
	return series
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

func tsdbID(n int) tsdb.SingleTenantTSDBIdentifier {
	return tsdb.SingleTenantTSDBIdentifier{
		TS: time.Unix(int64(n), 0),
	}
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
