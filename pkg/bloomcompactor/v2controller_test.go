package bloomcompactor

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb"
)

func Test_findGaps(t *testing.T) {
	for _, tc := range []struct {
		desc           string
		err            bool
		exp            []v1.FingerprintBounds
		ownershipRange v1.FingerprintBounds
		metas          []v1.FingerprintBounds
	}{
		{
			desc:           "error nonoverlapping metas",
			err:            true,
			exp:            nil,
			ownershipRange: v1.NewBounds(0, 10),
			metas:          []v1.FingerprintBounds{v1.NewBounds(11, 20)},
		},
		{
			desc:           "one meta with entire ownership range",
			err:            false,
			exp:            nil,
			ownershipRange: v1.NewBounds(0, 10),
			metas:          []v1.FingerprintBounds{v1.NewBounds(0, 10)},
		},
		{
			desc:           "two non-overlapping metas with entire ownership range",
			err:            false,
			exp:            nil,
			ownershipRange: v1.NewBounds(0, 10),
			metas: []v1.FingerprintBounds{
				v1.NewBounds(0, 5),
				v1.NewBounds(6, 10),
			},
		},
		{
			desc:           "two overlapping metas with entire ownership range",
			err:            false,
			exp:            nil,
			ownershipRange: v1.NewBounds(0, 10),
			metas: []v1.FingerprintBounds{
				v1.NewBounds(0, 6),
				v1.NewBounds(4, 10),
			},
		},
		{
			desc: "one meta with partial ownership range",
			err:  false,
			exp: []v1.FingerprintBounds{
				v1.NewBounds(6, 10),
			},
			ownershipRange: v1.NewBounds(0, 10),
			metas: []v1.FingerprintBounds{
				v1.NewBounds(0, 5),
			},
		},
		{
			desc: "smaller subsequent meta with partial ownership range",
			err:  false,
			exp: []v1.FingerprintBounds{
				v1.NewBounds(8, 10),
			},
			ownershipRange: v1.NewBounds(0, 10),
			metas: []v1.FingerprintBounds{
				v1.NewBounds(0, 7),
				v1.NewBounds(3, 4),
			},
		},
		{
			desc: "hole in the middle",
			err:  false,
			exp: []v1.FingerprintBounds{
				v1.NewBounds(4, 5),
			},
			ownershipRange: v1.NewBounds(0, 10),
			metas: []v1.FingerprintBounds{
				v1.NewBounds(0, 3),
				v1.NewBounds(6, 10),
			},
		},
		{
			desc: "holes on either end",
			err:  false,
			exp: []v1.FingerprintBounds{
				v1.NewBounds(0, 2),
				v1.NewBounds(8, 10),
			},
			ownershipRange: v1.NewBounds(0, 10),
			metas: []v1.FingerprintBounds{
				v1.NewBounds(3, 5),
				v1.NewBounds(6, 7),
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			gaps, err := findGaps(tc.ownershipRange, tc.metas)
			if tc.err {
				require.Error(t, err)
				return
			}
			require.Equal(t, tc.exp, gaps)
		})
	}
}

func Test_gapsBetweenTSDBsAndMetas(t *testing.T) {
	id := func(n int) tsdb.SingleTenantTSDBIdentifier {
		return tsdb.SingleTenantTSDBIdentifier{
			TS: time.Unix(int64(n), 0),
		}
	}

	meta := func(min, max model.Fingerprint, sources ...int) Meta {
		m := Meta{
			OwnershipRange: v1.NewBounds(min, max),
		}
		for _, source := range sources {
			m.Sources = append(m.Sources, id(source))
		}
		return m
	}

	for _, tc := range []struct {
		desc           string
		err            bool
		exp            []tsdbGaps
		ownershipRange v1.FingerprintBounds
		tsdbs          []tsdb.Identifier
		metas          []Meta
	}{
		{
			desc:           "non-overlapping tsdbs and metas",
			err:            true,
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs:          []tsdb.Identifier{id(0)},
			metas: []Meta{
				meta(11, 20, 0),
			},
		},
		{
			desc:           "single tsdb",
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs:          []tsdb.Identifier{id(0)},
			metas: []Meta{
				meta(4, 8, 0),
			},
			exp: []tsdbGaps{
				{
					tsdb: id(0),
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
			tsdbs:          []tsdb.Identifier{id(0), id(1)},
			metas: []Meta{
				meta(0, 5, 0),
				meta(6, 10, 1),
			},
			exp: []tsdbGaps{
				{
					tsdb: id(0),
					gaps: []v1.FingerprintBounds{
						v1.NewBounds(6, 10),
					},
				},
				{
					tsdb: id(1),
					gaps: []v1.FingerprintBounds{
						v1.NewBounds(0, 5),
					},
				},
			},
		},
		{
			desc:           "multiple tsdbs with the same blocks",
			ownershipRange: v1.NewBounds(0, 10),
			tsdbs:          []tsdb.Identifier{id(0), id(1)},
			metas: []Meta{
				meta(0, 5, 0, 1),
				meta(6, 8, 1),
			},
			exp: []tsdbGaps{
				{
					tsdb: id(0),
					gaps: []v1.FingerprintBounds{
						v1.NewBounds(6, 10),
					},
				},
				{
					tsdb: id(1),
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
