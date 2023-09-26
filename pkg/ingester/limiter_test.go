package ingester

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"github.com/grafana/loki/pkg/validation"
)

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
			expected:                fmt.Errorf(errMaxStreamsPerUserLimitExceeded, "test", 300, 300, 0, 1000, 300),
		},
		"both local and global limits are disabled": {
			maxLocalStreamsPerUser:  0,
			maxGlobalStreamsPerUser: 0,
			ringReplicationFactor:   1,
			ringIngesterCount:       1,
			streams:                 math.MaxInt32 - 1,
			expected:                nil,
		},
		"only local limit is enabled": {
			maxLocalStreamsPerUser:  1000,
			maxGlobalStreamsPerUser: 0,
			ringReplicationFactor:   1,
			ringIngesterCount:       1,
			streams:                 3000,
			expected:                fmt.Errorf(errMaxStreamsPerUserLimitExceeded, "test", 3000, 1000, 1000, 0, 0),
		},
		"only global limit is enabled with replication-factor=1": {
			maxLocalStreamsPerUser:  0,
			maxGlobalStreamsPerUser: 1000,
			ringReplicationFactor:   1,
			ringIngesterCount:       10,
			streams:                 3000,
			expected:                fmt.Errorf(errMaxStreamsPerUserLimitExceeded, "test", 3000, 100, 0, 1000, 100),
		},
		"only global limit is enabled with replication-factor=3": {
			maxLocalStreamsPerUser:  0,
			maxGlobalStreamsPerUser: 1000,
			ringReplicationFactor:   3,
			ringIngesterCount:       10,
			streams:                 3000,
			expected:                fmt.Errorf(errMaxStreamsPerUserLimitExceeded, "test", 3000, 300, 0, 1000, 300),
		},
		"both local and global limits are set with local limit < global limit": {
			maxLocalStreamsPerUser:  150,
			maxGlobalStreamsPerUser: 1000,
			ringReplicationFactor:   3,
			ringIngesterCount:       10,
			streams:                 3000,
			expected:                fmt.Errorf(errMaxStreamsPerUserLimitExceeded, "test", 3000, 150, 150, 1000, 300),
		},
		"both local and global limits are set with local limit > global limit": {
			maxLocalStreamsPerUser:  500,
			maxGlobalStreamsPerUser: 1000,
			ringReplicationFactor:   3,
			ringIngesterCount:       10,
			streams:                 3000,
			expected:                fmt.Errorf(errMaxStreamsPerUserLimitExceeded, "test", 3000, 300, 500, 1000, 300),
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

			limiter := NewLimiter(limits, NilMetrics, ring, testData.ringReplicationFactor)
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
			limiter := NewLimiter(nil, NilMetrics, nil, 0)
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

// Assert some of the weirder (bug?) behavior of golang.org/x/time/rate
func TestGoLimiter(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		lim   *rate.Limiter
		at    time.Time
		burst int
		limit rate.Limit
		allow int
		exp   bool
	}{
		{
			// I (owen-d) think this _should_ work and am supplying this test
			// case by way of explanation for how the StreamRateLimiter
			// works around the rate.Inf edge case.
			desc:  "changing inf limits unnecessarily cordons",
			lim:   rate.NewLimiter(rate.Inf, 0),
			at:    time.Now(),
			burst: 2,
			limit: 1,
			allow: 1,
			exp:   false,
		},
		{
			desc:  "non inf limit works",
			lim:   rate.NewLimiter(1, 2),
			at:    time.Now(),
			burst: 2,
			limit: 1,
			allow: 1,
			exp:   true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			tc.lim.SetBurstAt(tc.at, tc.burst)
			tc.lim.SetLimitAt(tc.at, tc.limit)
			require.Equal(t, tc.exp, tc.lim.AllowN(tc.at, tc.allow))
		})
	}
}
