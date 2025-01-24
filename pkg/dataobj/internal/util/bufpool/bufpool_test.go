package bufpool

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_findBucket(t *testing.T) {
	tt := []struct {
		size   uint64
		expect uint64
	}{
		{size: 0, expect: 1024},
		{size: 512, expect: 1024},
		{size: 1024, expect: 1024},
		{size: 1025, expect: 2048},
		{size: (1 << 36), expect: (1 << 36)},
		{size: (1 << 37), expect: math.MaxUint64},
	}

	for _, tc := range tt {
		t.Run(fmt.Sprintf("size=%d", tc.size), func(t *testing.T) {
			got := findBucket(tc.size).size
			require.Equal(t, tc.expect, got)
		})
	}
}

func Test(t *testing.T) {
	buf := Get(1_500_000)
	require.NotNil(t, buf)
	require.Less(t, buf.Cap(), 2<<20, "buffer should not have grown to next bucket size")
}
