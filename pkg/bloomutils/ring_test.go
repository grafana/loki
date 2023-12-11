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

func TestBloomGatewayClient_GetInstanceWithTokenRange(t *testing.T) {
	for name, tc := range map[string]struct {
		id       string
		input    []ring.InstanceDesc
		expected InstancesWithTokenRange
	}{
		"first instance includes 0 token": {
			id: "3",
			input: []ring.InstanceDesc{
				{Id: "1", Tokens: []uint32{3}},
				{Id: "2", Tokens: []uint32{5}},
				{Id: "3", Tokens: []uint32{1}},
			},
			expected: InstancesWithTokenRange{
				{Instance: ring.InstanceDesc{Id: "3", Tokens: []uint32{1}}, MinToken: 0, MaxToken: math.MaxUint32/3 - 1},
			},
		},
		"middle instance": {
			id: "1",
			input: []ring.InstanceDesc{
				{Id: "1", Tokens: []uint32{3}},
				{Id: "2", Tokens: []uint32{5}},
				{Id: "3", Tokens: []uint32{1}},
			},
			expected: InstancesWithTokenRange{
				{Instance: ring.InstanceDesc{Id: "1", Tokens: []uint32{3}}, MinToken: math.MaxUint32 / 3, MaxToken: math.MaxUint32/3*2 - 1},
			},
		},
		"last instance includes MaxUint32 token": {
			id: "2",
			input: []ring.InstanceDesc{
				{Id: "1", Tokens: []uint32{3}},
				{Id: "2", Tokens: []uint32{5}},
				{Id: "3", Tokens: []uint32{1}},
			},
			expected: InstancesWithTokenRange{
				{Instance: ring.InstanceDesc{Id: "2", Tokens: []uint32{5}}, MinToken: math.MaxUint32 / 3 * 2, MaxToken: math.MaxUint32},
			},
		},
	} {
		tc := tc
		t.Run(name, func(t *testing.T) {
			result := GetInstanceWithTokenRange(tc.id, tc.input)
			require.Equal(t, tc.expected, result)
		})
	}
}
