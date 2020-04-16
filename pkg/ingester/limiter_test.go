package ingester

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/util/validation"
)

func TestLimiter_maxStreamsPerUser(t *testing.T) {
	tests := map[string]struct {
		maxLocalStreamsPerUser  int
		maxGlobalStreamsPerUser int
		ringReplicationFactor   int
		ringIngesterCount       int
		expected                int
	}{
		"both local and global limits are disabled": {
			maxLocalStreamsPerUser:  0,
			maxGlobalStreamsPerUser: 0,
			ringReplicationFactor:   1,
			ringIngesterCount:       1,
			expected:                math.MaxInt32,
		},
		"only local limit is enabled": {
			maxLocalStreamsPerUser:  1000,
			maxGlobalStreamsPerUser: 0,
			ringReplicationFactor:   1,
			ringIngesterCount:       1,
			expected:                1000,
		},
		"only global limit is enabled with replication-factor=1": {
			maxLocalStreamsPerUser:  0,
			maxGlobalStreamsPerUser: 1000,
			ringReplicationFactor:   1,
			ringIngesterCount:       10,
			expected:                100,
		},
		"only global limit is enabled with replication-factor=3": {
			maxLocalStreamsPerUser:  0,
			maxGlobalStreamsPerUser: 1000,
			ringReplicationFactor:   3,
			ringIngesterCount:       10,
			expected:                300,
		},
		"both local and global limits are set with local limit < global limit": {
			maxLocalStreamsPerUser:  150,
			maxGlobalStreamsPerUser: 1000,
			ringReplicationFactor:   3,
			ringIngesterCount:       10,
			expected:                150,
		},
		"both local and global limits are set with local limit > global limit": {
			maxLocalStreamsPerUser:  500,
			maxGlobalStreamsPerUser: 1000,
			ringReplicationFactor:   3,
			ringIngesterCount:       10,
			expected:                300,
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			// Mock the ring
			ring := &ringCountMock{count: testData.ringIngesterCount}

			// Mock limits
			limits, err := validation.NewOverrides(validation.Limits{
				MaxLocalStreamsPerUser:  testData.maxLocalStreamsPerUser,
				MaxGlobalStreamsPerUser: testData.maxGlobalStreamsPerUser,
			}, nil)
			require.NoError(t, err)

			limiter := NewLimiter(limits, ring, testData.ringReplicationFactor)
			actual := limiter.maxStreamsPerUser("test")

			assert.Equal(t, testData.expected, actual)
		})
	}
}

func TestLimiter_AssertMaxStreamsPerUser(t *testing.T) {
	tests := map[string]struct {
		maxLocalStreamsPerUser  int
		maxGlobalStreamsPerUser int
		ringReplicationFactor   int
		ringIngesterCount       int
		streams                 int
		expected                error
	}{
		"both local and global limit are disabled": {
			maxLocalStreamsPerUser:  0,
			maxGlobalStreamsPerUser: 0,
			ringReplicationFactor:   1,
			ringIngesterCount:       1,
			streams:                 100,
			expected:                nil,
		},
		"current number of streams is below the limit": {
			maxLocalStreamsPerUser:  0,
			maxGlobalStreamsPerUser: 1000,
			ringReplicationFactor:   3,
			ringIngesterCount:       10,
			streams:                 299,
			expected:                nil,
		},
		"current number of streams is above the limit": {
			maxLocalStreamsPerUser:  0,
			maxGlobalStreamsPerUser: 1000,
			ringReplicationFactor:   3,
			ringIngesterCount:       10,
			streams:                 300,
			expected:                fmt.Errorf(errMaxStreamsPerUserLimitExceeded, 0, 1000, 300),
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			// Mock the ring
			ring := &ringCountMock{count: testData.ringIngesterCount}

			// Mock limits
			limits, err := validation.NewOverrides(validation.Limits{
				MaxLocalStreamsPerUser:  testData.maxLocalStreamsPerUser,
				MaxGlobalStreamsPerUser: testData.maxGlobalStreamsPerUser,
			}, nil)
			require.NoError(t, err)

			limiter := NewLimiter(limits, ring, testData.ringReplicationFactor)
			actual := limiter.AssertMaxStreamsPerUser("test", testData.streams)

			assert.Equal(t, testData.expected, actual)
		})
	}
}

func TestLimiter_minNonZero(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		first    int
		second   int
		expected int
	}{
		"both zero": {
			first:    0,
			second:   0,
			expected: 0,
		},
		"first is zero": {
			first:    0,
			second:   1,
			expected: 1,
		},
		"second is zero": {
			first:    1,
			second:   0,
			expected: 1,
		},
		"both non zero, second > first": {
			first:    1,
			second:   2,
			expected: 1,
		},
		"both non zero, first > second": {
			first:    2,
			second:   1,
			expected: 1,
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			limiter := NewLimiter(nil, nil, 0)
			assert.Equal(t, testData.expected, limiter.minNonZero(testData.first, testData.second))
		})
	}
}

type ringCountMock struct {
	count int
}

func (m *ringCountMock) HealthyInstancesCount() int {
	return m.count
}
