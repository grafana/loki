package planner

import (
	"math"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
)

func TestSplitFingerprintKeyspaceByFactor(t *testing.T) {
	for _, tt := range []struct {
		name   string
		factor int
	}{
		{
			name:   "Factor is 0",
			factor: 0,
		},
		{
			name:   "Factor is 1",
			factor: 1,
		},
		{
			name:   "Factor is 256",
			factor: 256,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitFingerprintKeyspaceByFactor(tt.factor)

			if tt.factor == 0 {
				require.Empty(t, got)
				return
			}

			// Check overall min and max values of the ranges.
			require.Equal(t, model.Fingerprint(math.MaxUint64), got[len(got)-1].Max)
			require.Equal(t, model.Fingerprint(0), got[0].Min)

			// For each range, check that the max value of the previous range is one less than the min value of the current range.
			for i := 1; i < len(got); i++ {
				require.Equal(t, got[i-1].Max+1, got[i].Min)
			}
		})
	}
}

func Test_FindGapsInFingerprintBounds(t *testing.T) {
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
		{
			desc:           "full ownership range with single meta",
			err:            false,
			exp:            nil,
			ownershipRange: v1.NewBounds(0, math.MaxUint64),
			metas: []v1.FingerprintBounds{
				v1.NewBounds(0, math.MaxUint64),
			},
		},
		{
			desc:           "full ownership range with multiple metas",
			err:            false,
			exp:            nil,
			ownershipRange: v1.NewBounds(0, math.MaxUint64),
			// Three metas covering the whole 0 - MaxUint64
			metas: []v1.FingerprintBounds{
				v1.NewBounds(0, math.MaxUint64/3),
				v1.NewBounds(math.MaxUint64/3+1, math.MaxUint64/2),
				v1.NewBounds(math.MaxUint64/2+1, math.MaxUint64),
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			gaps, err := FindGapsInFingerprintBounds(tc.ownershipRange, tc.metas)
			if tc.err {
				require.Error(t, err)
				return
			}
			require.Equal(t, tc.exp, gaps)
		})
	}
}
