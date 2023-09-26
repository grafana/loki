package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShuffleShardExpectedInstancesPerZone(t *testing.T) {
	tests := []struct {
		shardSize int
		numZones  int
		expected  int
	}{
		{
			shardSize: 1,
			numZones:  1,
			expected:  1,
		},
		{
			shardSize: 1,
			numZones:  3,
			expected:  1,
		},
		{
			shardSize: 3,
			numZones:  3,
			expected:  1,
		},
		{
			shardSize: 4,
			numZones:  3,
			expected:  2,
		},
		{
			shardSize: 6,
			numZones:  3,
			expected:  2,
		},
	}

	for _, test := range tests {
		assert.Equal(t, test.expected, ShuffleShardExpectedInstancesPerZone(test.shardSize, test.numZones))
	}
}

func TestShuffleShardExpectedInstances(t *testing.T) {
	tests := []struct {
		shardSize int
		numZones  int
		expected  int
	}{
		{
			shardSize: 1,
			numZones:  1,
			expected:  1,
		},
		{
			shardSize: 1,
			numZones:  3,
			expected:  3,
		},
		{
			shardSize: 3,
			numZones:  3,
			expected:  3,
		},
		{
			shardSize: 4,
			numZones:  3,
			expected:  6,
		},
		{
			shardSize: 6,
			numZones:  3,
			expected:  6,
		},
	}

	for _, test := range tests {
		assert.Equal(t, test.expected, ShuffleShardExpectedInstances(test.shardSize, test.numZones))
	}
}
