package bloomutils

import (
	"fmt"
	"math"
	"testing"

	"github.com/grafana/dskit/ring"
	"github.com/stretchr/testify/require"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

func TestBloomGatewayClient_InstanceSortMergeIterator(t *testing.T) {
	//          | 0 1 2 3 4 5 6 7 8 9 |
	// ---------+---------------------+
	// ID 1     |        ***o    ***o |
	// ID 2     |    ***o    ***o     |
	// ID 3     | **o                 |
	input := []ring.InstanceDesc{
		{Id: "1", Tokens: []uint32{5, 9}},
		{Id: "2", Tokens: []uint32{3, 7}},
		{Id: "3", Tokens: []uint32{1}},
	}
	expected := []InstanceWithTokenRange{
		{Instance: input[2], TokenRange: NewTokenRange(0, 1)},
		{Instance: input[1], TokenRange: NewTokenRange(2, 3)},
		{Instance: input[0], TokenRange: NewTokenRange(4, 5)},
		{Instance: input[1], TokenRange: NewTokenRange(6, 7)},
		{Instance: input[0], TokenRange: NewTokenRange(8, 9)},
	}

	var i int
	it := NewInstanceSortMergeIterator(input)
	for it.Next() {
		t.Log(expected[i], it.At())
		require.Equal(t, expected[i], it.At())
		i++
	}
}

func uint64Range(min, max uint64) Range[uint64] {
	return Range[uint64]{min, max}
}

func TestKeyspacesFromTokenRanges(t *testing.T) {
	for i, tc := range []struct {
		tokenRanges ring.TokenRanges
		exp         []v1.FingerprintBounds
	}{
		{
			tokenRanges: ring.TokenRanges{
				0, math.MaxUint32 / 2,
				math.MaxUint32/2 + 1, math.MaxUint32,
			},
			exp: []v1.FingerprintBounds{
				v1.NewBounds(0, math.MaxUint64/2),
				v1.NewBounds(math.MaxUint64/2+1, math.MaxUint64),
			},
		},
		{
			tokenRanges: ring.TokenRanges{
				0, math.MaxUint8,
				math.MaxUint16, math.MaxUint16 << 1,
			},
			exp: []v1.FingerprintBounds{
				v1.NewBounds(0, 0xff00000000|math.MaxUint32),
				v1.NewBounds(math.MaxUint16<<32, math.MaxUint16<<33|math.MaxUint32),
			},
		},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			require.Equal(t, tc.exp, KeyspacesFromTokenRanges(tc.tokenRanges))
		})
	}
}
