package bloomutils

import (
	"math"
	"testing"

	"github.com/grafana/dskit/ring"
	"github.com/stretchr/testify/require"
)

func TestBloomGatewayClient_SortInstancesByToken(t *testing.T) {
	input := []ring.InstanceDesc{
		{Id: "1", Tokens: []uint32{5, 9}},
		{Id: "2", Tokens: []uint32{3, 7}},
		{Id: "3", Tokens: []uint32{1}},
	}
	expected := []InstanceWithTokenRange{
		{Instance: input[2], MinToken: 0, MaxToken: 1},
		{Instance: input[1], MinToken: 2, MaxToken: 3},
		{Instance: input[0], MinToken: 4, MaxToken: 5},
		{Instance: input[1], MinToken: 6, MaxToken: 7},
		{Instance: input[0], MinToken: 8, MaxToken: 9},
	}

	var i int
	it := NewInstanceSortMergeIterator(input)
	for it.Next() {
		t.Log(expected[i], it.At())
		require.Equal(t, expected[i], it.At())
		i++
	}
}

func TestBloomGatewayClient_GetInstancesWithTokenRanges(t *testing.T) {
	t.Run("instance does not own first token in the ring", func(t *testing.T) {
		input := []ring.InstanceDesc{
			{Id: "1", Tokens: []uint32{5, 9}},
			{Id: "2", Tokens: []uint32{3, 7}},
			{Id: "3", Tokens: []uint32{1}},
		}
		expected := InstancesWithTokenRange{
			{Instance: input[1], MinToken: 2, MaxToken: 3},
			{Instance: input[1], MinToken: 6, MaxToken: 7},
		}

		result := GetInstancesWithTokenRanges("2", input)
		require.Equal(t, expected, result)
	})

	t.Run("instance owns first token in the ring", func(t *testing.T) {
		input := []ring.InstanceDesc{
			{Id: "1", Tokens: []uint32{5, 9}},
			{Id: "2", Tokens: []uint32{3, 7}},
			{Id: "3", Tokens: []uint32{1}},
		}
		expected := InstancesWithTokenRange{
			{Instance: input[2], MinToken: 0, MaxToken: 1},
			{Instance: input[2], MinToken: 10, MaxToken: math.MaxUint32},
		}

		result := GetInstancesWithTokenRanges("3", input)
		require.Equal(t, expected, result)
	})
}
