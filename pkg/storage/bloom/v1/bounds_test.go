package v1

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

func Test_ParseFingerprint(t *testing.T) {
	t.Parallel()
	fp, err := model.ParseFingerprint("7d0")
	assert.NoError(t, err)
	assert.Equal(t, model.Fingerprint(2000), fp)
}

func Test_FingerprintBounds_String(t *testing.T) {
	t.Parallel()
	bounds := NewBounds(10, 2000)
	assert.Equal(t, "000000000000000a-00000000000007d0", bounds.String())
}

func Test_ParseBoundsFromAddr(t *testing.T) {
	t.Parallel()
	bounds, err := ParseBoundsFromAddr("a-7d0")
	assert.NoError(t, err)
	assert.Equal(t, NewBounds(10, 2000), bounds)
}

func Test_ParseBoundsFromParts(t *testing.T) {
	t.Parallel()
	bounds, err := ParseBoundsFromParts("a", "7d0")
	assert.NoError(t, err)
	assert.Equal(t, NewBounds(10, 2000), bounds)
}

func Test_FingerprintBounds_Cmp(t *testing.T) {
	t.Parallel()
	bounds := NewBounds(10, 20)
	assert.Equal(t, Before, bounds.Cmp(0))
	assert.Equal(t, Overlap, bounds.Cmp(10))
	assert.Equal(t, Overlap, bounds.Cmp(15))
	assert.Equal(t, Overlap, bounds.Cmp(20))
	assert.Equal(t, After, bounds.Cmp(21))
}

func Test_FingerprintBounds_Overlap(t *testing.T) {
	t.Parallel()
	bounds := NewBounds(10, 20)
	assert.True(t, bounds.Overlaps(FingerprintBounds{Min: 5, Max: 15}))
	assert.True(t, bounds.Overlaps(FingerprintBounds{Min: 15, Max: 25}))
	assert.True(t, bounds.Overlaps(FingerprintBounds{Min: 10, Max: 20}))
	assert.True(t, bounds.Overlaps(FingerprintBounds{Min: 5, Max: 25}))
	assert.False(t, bounds.Overlaps(FingerprintBounds{Min: 1, Max: 9}))
	assert.False(t, bounds.Overlaps(FingerprintBounds{Min: 21, Max: 30}))
}

func Test_FingerprintBounds_Within(t *testing.T) {
	t.Parallel()
	target := NewBounds(10, 20)
	assert.False(t, NewBounds(1, 9).Within(target))
	assert.False(t, NewBounds(21, 30).Within(target))
	assert.True(t, NewBounds(10, 20).Within(target))
	assert.True(t, NewBounds(14, 15).Within(target))
	assert.False(t, NewBounds(5, 15).Within(target))
	assert.False(t, NewBounds(15, 25).Within(target))
	assert.False(t, NewBounds(5, 25).Within(target))
}

func Test_FingerprintBounds_Intersection(t *testing.T) {
	t.Parallel()
	target := NewBounds(10, 20)
	assert.Nil(t, NewBounds(1, 9).Intersection(target))
	assert.Nil(t, NewBounds(21, 30).Intersection(target))
	assert.Equal(t, &FingerprintBounds{Min: 10, Max: 20}, NewBounds(10, 20).Intersection(target))
	assert.Equal(t, &FingerprintBounds{Min: 14, Max: 15}, NewBounds(14, 15).Intersection(target))
	assert.Equal(t, &FingerprintBounds{Min: 10, Max: 15}, NewBounds(5, 15).Intersection(target))
	assert.Equal(t, &FingerprintBounds{Min: 15, Max: 20}, NewBounds(15, 25).Intersection(target))
	assert.Equal(t, &target, NewBounds(5, 25).Intersection(target))
}

func Test_FingerprintBounds_Union(t *testing.T) {
	t.Parallel()
	target := NewBounds(10, 20)

	assert.Equal(t, []FingerprintBounds{
		{Min: 1, Max: 8},
		{Min: 10, Max: 20},
	}, NewBounds(1, 8).Union(target))
	assert.Equal(t, []FingerprintBounds{
		{Min: 10, Max: 20},
		{Min: 22, Max: 30},
	}, NewBounds(22, 30).Union(target))
	assert.Equal(t, []FingerprintBounds{
		{Min: 10, Max: 20},
	}, NewBounds(10, 20).Union(target))
	assert.Equal(t, []FingerprintBounds{
		{Min: 5, Max: 20},
	}, NewBounds(5, 15).Union(target))
	// contiguous range, target before
	assert.Equal(t, []FingerprintBounds{
		{Min: 10, Max: 25},
	}, NewBounds(21, 25).Union(target))
	// contiguous range, target after
	assert.Equal(t, []FingerprintBounds{
		{Min: 5, Max: 20},
	}, NewBounds(5, 9).Union(target))
}

func Test_FingerprintBounds_Unless(t *testing.T) {
	t.Parallel()
	target := NewBounds(10, 20)
	assert.Equal(t, []FingerprintBounds{
		{Min: 1, Max: 9},
	}, NewBounds(1, 9).Unless(target))
	assert.Equal(t, []FingerprintBounds{
		{Min: 21, Max: 30},
	}, NewBounds(21, 30).Unless(target))
	assert.Nil(t, NewBounds(10, 20).Unless(target))
	assert.Equal(t, []FingerprintBounds{
		{Min: 5, Max: 9},
	}, NewBounds(5, 15).Unless(target))
	assert.Equal(t, []FingerprintBounds{
		{Min: 21, Max: 25},
	}, NewBounds(15, 25).Unless(target))
	assert.Equal(t, []FingerprintBounds{
		{Min: 5, Max: 9},
		{Min: 21, Max: 25},
	}, NewBounds(5, 25).Unless(target))
	assert.Nil(t, NewBounds(14, 15).Unless(target))
}

func Test_MultiFingerprintBounds(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		mb     MultiFingerprintBounds
		target FingerprintBounds
		exp    MultiFingerprintBounds
	}{
		{
			desc:   "no elements",
			mb:     MultiFingerprintBounds{},
			target: NewBounds(0, 9),
			exp: MultiFingerprintBounds{
				NewBounds(0, 9),
			},
		},
		{
			desc: "single element before",
			mb: MultiFingerprintBounds{
				NewBounds(5, 9),
			},
			target: NewBounds(15, 19),
			exp: MultiFingerprintBounds{
				NewBounds(5, 9),
				NewBounds(15, 19),
			},
		},
		{
			desc: "single element after",
			mb: MultiFingerprintBounds{
				NewBounds(5, 9),
			},
			target: NewBounds(0, 3),
			exp: MultiFingerprintBounds{
				NewBounds(0, 3),
				NewBounds(5, 9),
			},
		},
		{
			desc: "single element overlapping",
			mb: MultiFingerprintBounds{
				NewBounds(5, 9),
			},
			target: NewBounds(0, 14),
			exp: MultiFingerprintBounds{
				NewBounds(0, 14),
			},
		},
		{
			desc: "multiple elements single overlapping",
			mb: MultiFingerprintBounds{
				NewBounds(5, 9),
				NewBounds(15, 19),
			},
			target: NewBounds(0, 6),
			exp: MultiFingerprintBounds{
				NewBounds(0, 9),
				NewBounds(15, 19),
			},
		},
		{
			desc: "multiple elements single overlapping",
			mb: MultiFingerprintBounds{
				NewBounds(5, 9),
				NewBounds(15, 19),
			},
			target: NewBounds(11, 25),
			exp: MultiFingerprintBounds{
				NewBounds(5, 9),
				NewBounds(11, 25),
			},
		},
		{
			desc: "multiple elements combining overlapping",
			mb: MultiFingerprintBounds{
				NewBounds(5, 9),
				NewBounds(15, 19),
			},
			target: NewBounds(9, 15),
			exp: MultiFingerprintBounds{
				NewBounds(5, 19),
			},
		},
		{
			desc: "combination",
			mb: MultiFingerprintBounds{
				NewBounds(0, 2),
				NewBounds(5, 9),
				NewBounds(15, 19),
				NewBounds(25, 29),
			},
			target: NewBounds(9, 15),
			exp: MultiFingerprintBounds{
				NewBounds(0, 2),
				NewBounds(5, 19),
				NewBounds(25, 29),
			},
		},
		{
			desc: "overlapping ranges",
			mb: MultiFingerprintBounds{
				NewBounds(0, 6),
				NewBounds(5, 15),
			},
			target: NewBounds(8, 10),
			exp: MultiFingerprintBounds{
				NewBounds(0, 15),
			},
		},
		{
			desc: "disjoint ranges and target is between",
			mb: MultiFingerprintBounds{
				NewBounds(0, 9),
				NewBounds(30, 39),
			},
			target: NewBounds(15, 19),
			exp: MultiFingerprintBounds{
				NewBounds(0, 9),
				NewBounds(15, 19),
				NewBounds(30, 39),
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			res := tc.mb.Union(tc.target)
			assert.Equal(t, tc.exp, res)
		})
	}
}
