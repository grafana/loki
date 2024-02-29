package bloomcompactor

import (
	"testing"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func Test_TsdbTokenRange(t *testing.T) {
	type addition struct {
		version int
		bounds  v1.FingerprintBounds
	}
	type exp struct {
		added bool
		err   bool
	}
	mk := func(version int, min, max model.Fingerprint) addition {
		return addition{version, v1.FingerprintBounds{Min: min, Max: max}}
	}
	tok := func(version int, through model.Fingerprint) tsdbToken {
		return tsdbToken{version: version, through: through}
	}

	for _, tc := range []struct {
		desc      string
		additions []addition
		exp       []bool
		result    tsdbTokenRange
	}{
		{
			desc: "ascending versions",
			additions: []addition{
				mk(1, 0, 10),
				mk(2, 11, 20),
				mk(3, 15, 25),
			},
			exp: []bool{true, true, true},
			result: tsdbTokenRange{
				tok(1, 10),
				tok(2, 14),
				tok(3, 25),
			},
		},
		{
			desc: "descending versions",
			additions: []addition{
				mk(3, 15, 25),
				mk(2, 11, 20),
				mk(1, 0, 10),
			},
			exp: []bool{true, true, true},
			result: tsdbTokenRange{
				tok(1, 10),
				tok(2, 14),
				tok(3, 25),
			},
		},
		{
			desc: "simple",
			additions: []addition{
				mk(3, 0, 10),
				mk(2, 11, 20),
				mk(1, 15, 25),
			},
			exp: []bool{true, true, true},
			result: tsdbTokenRange{
				tok(3, 10),
				tok(2, 20),
				tok(1, 25),
			},
		},
		{
			desc: "simple replacement",
			additions: []addition{
				mk(3, 10, 20),
				mk(2, 0, 9),
			},
			exp: []bool{true, true},
			result: tsdbTokenRange{
				tok(2, 9),
				tok(3, 20),
			},
		},
		{
			desc: "complex",
			additions: []addition{
				mk(5, 30, 50),
				mk(4, 20, 45),
				mk(3, 25, 70),
				mk(2, 10, 20),
				mk(1, 1, 5),
			},
			exp: []bool{true, true, true, true, true, true},
			result: tsdbTokenRange{
				tok(-1, 0),
				tok(1, 5),
				tok(-1, 9),
				tok(2, 19),
				tok(4, 29),
				tok(5, 50),
				tok(3, 70),
			},
		},
		{
			desc: "neighboring upper range",
			additions: []addition{
				mk(5, 30, 50),
				mk(4, 51, 60),
			},
			exp: []bool{true, true},
			result: tsdbTokenRange{
				tok(-1, 29),
				tok(5, 50),
				tok(4, 60),
			},
		},
		{
			desc: "non-neighboring upper range",
			additions: []addition{
				mk(5, 30, 50),
				mk(4, 55, 60),
			},
			exp: []bool{true, true},
			result: tsdbTokenRange{
				tok(-1, 29),
				tok(5, 50),
				tok(-1, 54),
				tok(4, 60),
			},
		},
		{
			desc: "earlier version within",
			additions: []addition{
				mk(5, 30, 50),
				mk(4, 40, 45),
			},
			exp: []bool{true, false},
			result: tsdbTokenRange{
				tok(-1, 29),
				tok(5, 50),
			},
		},
		{
			desc: "earlier version right overlapping",
			additions: []addition{
				mk(5, 10, 20),
				mk(4, 15, 25),
			},
			exp: []bool{true, true},
			result: tsdbTokenRange{
				tok(-1, 9),
				tok(5, 20),
				tok(4, 25),
			},
		},
		{
			desc: "older version overlaps two",
			additions: []addition{
				mk(3, 10, 20),
				mk(2, 21, 30),
				mk(1, 15, 25),
			},
			exp: []bool{true, true, false},
			result: tsdbTokenRange{
				tok(-1, 9),
				tok(3, 20),
				tok(2, 30),
			},
		},
		{
			desc: "older version overlaps two w middle",
			additions: []addition{
				mk(3, 10, 20),
				mk(2, 22, 30),
				mk(1, 15, 25),
			},
			exp: []bool{true, true, true},
			result: tsdbTokenRange{
				tok(-1, 9),
				tok(3, 20),
				tok(1, 21),
				tok(2, 30),
			},
		},
		{
			desc: "newer right overflow",
			additions: []addition{
				mk(1, 30, 50),
				mk(2, 40, 60),
			},
			exp: []bool{true, true},
			result: tsdbTokenRange{
				tok(-1, 29),
				tok(1, 39),
				tok(2, 60),
			},
		},
		{
			desc: "newer right overflow superset",
			additions: []addition{
				mk(1, 30, 50),
				mk(2, 30, 60),
			},
			exp: []bool{true, true},
			result: tsdbTokenRange{
				tok(-1, 29),
				tok(2, 60),
			},
		},
		{
			desc: "newer right overflow partial",
			additions: []addition{
				mk(1, 30, 50),
				mk(2, 40, 60),
			},
			exp: []bool{true, true},
			result: tsdbTokenRange{
				tok(-1, 29),
				tok(1, 39),
				tok(2, 60),
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			var (
				tr    tsdbTokenRange
				added bool
			)
			for i, a := range tc.additions {
				tr, added = tr.Add(a.version, a.bounds)
				exp := tc.exp[i]
				require.Equal(t, exp, added, "on iteration %d", i)
			}
			require.Equal(t, tc.result, tr)
		})
	}
}
