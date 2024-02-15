package bloomutils

import (
	"math"
	"testing"

	"github.com/grafana/dskit/ring"
	"github.com/stretchr/testify/require"
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

func TestBloomGatewayClient_KeyRangeForInstance(t *testing.T) {
	for name, tc := range map[string]struct {
		id       string
		input    []ring.InstanceDesc
		expected Range[uint64]
	}{
		"first instance includes 0 token": {
			id: "3",
			input: []ring.InstanceDesc{
				{Id: "1", Tokens: []uint32{3}},
				{Id: "2", Tokens: []uint32{5}},
				{Id: "3", Tokens: []uint32{1}},
			},
			expected: uint64Range(0, math.MaxUint64/3-1),
		},
		"middle instance": {
			id: "1",
			input: []ring.InstanceDesc{
				{Id: "1", Tokens: []uint32{3}},
				{Id: "2", Tokens: []uint32{5}},
				{Id: "3", Tokens: []uint32{1}},
			},
			expected: uint64Range(math.MaxUint64/3, math.MaxUint64/3*2-1),
		},
		"last instance includes MaxUint32 token": {
			id: "2",
			input: []ring.InstanceDesc{
				{Id: "1", Tokens: []uint32{3}},
				{Id: "2", Tokens: []uint32{5}},
				{Id: "3", Tokens: []uint32{1}},
			},
			expected: uint64Range(math.MaxUint64/3*2, math.MaxUint64),
		},
	} {
		tc := tc
		t.Run(name, func(t *testing.T) {
			result, err := KeyRangeForInstance(tc.id, tc.input, Uint64Range)
			require.NoError(t, err)
			require.Equal(t, tc.expected, result)
		})
	}
}
