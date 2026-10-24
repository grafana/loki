package pool

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewBufferInvalidFactor(t *testing.T) {
	for _, factor := range []float64{-1, 0, 0.5, 1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		t.Run(fmt.Sprint(factor), func(t *testing.T) {
			require.Panics(t, func() { NewBuffer(1, 10, factor) })
		})
	}
}

func TestNewBufferBucketGrowth(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, tc := range []struct {
		name             string
		minSize, maxSize int
		factor           float64
		want             []int
	}{
		{"geometric", 2, 10, 2, []int{2, 4, 8}},
		{"rounded growth", 1, 10, 1.1, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
		{"integer overflow", maxInt/2 + 1, maxInt, 2, []int{maxInt/2 + 1}},
		{"maximum integer", maxInt, maxInt, 2, []int{maxInt}},
		{"floating point overflow", 2, 10, math.MaxFloat64, []int{2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewBuffer(tc.minSize, tc.maxSize, tc.factor)
			require.Equal(t, tc.want, p.sizes)
			require.Len(t, p.buckets, len(tc.want))
		})
	}
}

func Test_ZeroBuffer(t *testing.T) {
	p := NewBuffer(2, 10, 2)
	require.Equal(t, 0, p.Get(1).Len())
	require.Equal(t, 0, p.Get(1).Len())
	require.Equal(t, 0, p.Get(2).Len())
	require.Equal(t, 0, p.Get(2).Len())
	require.Equal(t, 0, p.Get(20).Len())
	require.Equal(t, 0, p.Get(20).Len())
}
