package ingester

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/grafana/dskit/ring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"golang.org/x/time/rate"

	"github.com/grafana/loki/v3/pkg/validation"
)

type fixedStrategy struct {
	localLimit int
}

func (strategy *fixedStrategy) convertGlobalToLocalLimit(_ int, _ string) int {
	return strategy.localLimit
}
func TestStreamCountLimiter_AssertNewStreamAllowed(t *testing.T) {
	tests := map[string]struct {
		maxLocalStreamsPerUser  int
		maxGlobalStreamsPerUser int
		calculatedLocalLimit    int
		streams                 int
		expected                error
		useOwnedStreamService   bool
		fixedLimit              int32
		ownedStreamCount        int
	}{
		"both local and global limit are disabled": {
			maxLocalStreamsPerUser:  0,
			maxGlobalStreamsPerUser: 0,
			calculatedLocalLimit:    0,
			streams:                 100,
			expected:                nil,
		},
		"current number of streams is below the limit": {
			maxLocalStreamsPerUser:  0,
			maxGlobalStreamsPerUser: 1000,
			calculatedLocalLimit:    300,
			streams:                 299,
			expected:                nil,
		},
		"current number of streams is above the limit": {
			maxLocalStreamsPerUser:  0,
			maxGlobalStreamsPerUser: 1000,
			calculatedLocalLimit:    300,
			streams:                 300,
			expected:                fmt.Errorf(errMaxStreamsPerUserLimitExceeded, "test", 300, 300, 0, 1000, 300),
		},
		"both local and global limits are disabled": {
			maxLocalStreamsPerUser:  0,
			maxGlobalStreamsPerUser: 0,
			calculatedLocalLimit:    0,
			streams:                 math.MaxInt32 - 1,
			expected:                nil,
		},
		"only local limit is enabled": {
			maxLocalStreamsPerUser:  1000,
			maxGlobalStreamsPerUser: 0,
			calculatedLocalLimit:    1000,
			streams:                 3000,
			expected:                fmt.Errorf(errMaxStreamsPerUserLimitExceeded, "test", 3000, 1000, 1000, 0, 1000),
		},
		"only global limit is enabled": {
			maxLocalStreamsPerUser:  0,
			maxGlobalStreamsPerUser: 1000,
			calculatedLocalLimit:    100,
			streams:                 3000,
			expected:                fmt.Errorf(errMaxStreamsPerUserLimitExceeded, "test", 3000, 100, 0, 1000, 100),
		},
		"both local and global limits are set with local limit < global limit": {
			maxLocalStreamsPerUser:  150,
			maxGlobalStreamsPerUser: 1000,
			calculatedLocalLimit:    150,
			streams:                 3000,
			expected:                fmt.Errorf(errMaxStreamsPerUserLimitExceeded, "test", 3000, 150, 150, 1000, 150),
		},
		"both local and global limits are set with local limit > global limit": {
			maxLocalStreamsPerUser:  500,
			maxGlobalStreamsPerUser: 1000,
			calculatedLocalLimit:    300,
			streams:                 3000,
			expected:                fmt.Errorf(errMaxStreamsPerUserLimitExceeded, "test", 3000, 300, 500, 1000, 300),
		},
		"actual limit must be used if it's greater than fixed limit": {
			maxLocalStreamsPerUser:  500,
			maxGlobalStreamsPerUser: 1000,
			calculatedLocalLimit:    300,
			useOwnedStreamService:   true,
			fixedLimit:              20,
			ownedStreamCount:        3000,
			expected:                fmt.Errorf(errMaxStreamsPerUserLimitExceeded, "test", 3000, 300, 500, 1000, 300),
		},
		"fixed limit must be used if it's greater than actual limit": {
			maxLocalStreamsPerUser:  500,
			maxGlobalStreamsPerUser: 1000,
			calculatedLocalLimit:    500,
			useOwnedStreamService:   true,
			fixedLimit:              2000,
			ownedStreamCount:        2001,
			expected:                fmt.Errorf(errMaxStreamsPerUserLimitExceeded, "test", 2001, 2000, 500, 1000, 500),
		},
		"fixed limit must not be used if both limits are disabled": {
			maxLocalStreamsPerUser:  0,
			maxGlobalStreamsPerUser: 0,
			calculatedLocalLimit:    0,
			useOwnedStreamService:   true,
			fixedLimit:              2000,
			ownedStreamCount:        2001,
			expected:                nil,
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			// Mock limits
			limits, err := validation.NewOverrides(validation.Limits{
				MaxLocalStreamsPerUser:  testData.maxLocalStreamsPerUser,
				MaxGlobalStreamsPerUser: testData.maxGlobalStreamsPerUser,
				UseOwnedStreamCount:     testData.useOwnedStreamService,
			}, nil)
			require.NoError(t, err)

			ownedStreamSvc := &ownedStreamService{
				fixedLimit:       atomic.NewInt32(testData.fixedLimit),
				ownedStreamCount: atomic.NewInt64(int64(testData.ownedStreamCount)),
			}
			strategy := &fixedStrategy{localLimit: testData.calculatedLocalLimit}
			limiter := NewLimiter(limits, NilMetrics, strategy, &TenantBasedStrategy{limits: limits})
			defaultCountSupplier := func() int {
				return testData.streams
			}
			streamCountLimiter := newStreamCountLimiter("test", defaultCountSupplier, limiter, ownedStreamSvc)
			actual := streamCountLimiter.AssertNewStreamAllowed("test")

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
		t.Run(testName, func(t *testing.T) {
			limiter := NewLimiter(nil, NilMetrics, nil, nil)
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

func (m *ringCountMock) ZonesCount() int {
	return 1
}

func (m *ringCountMock) HealthyInstancesInZoneCount() int {
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

type MockRing struct {
	zonesCount                  int
	healthyInstancesCount       int
	healthyInstancesInZoneCount int
}

func (m *MockRing) ZonesCount() int {
	return m.zonesCount
}

func (m *MockRing) HealthyInstancesCount() int {
	return m.healthyInstancesCount
}

func (m *MockRing) HealthyInstancesInZoneCount() int {
	return m.healthyInstancesInZoneCount
}

func TestConvertGlobalToLocalLimit_IngesterRing(t *testing.T) {
	tests := []struct {
		name                        string
		globalLimit                 int
		zonesCount                  int
		healthyInstancesCount       int
		healthyInstancesInZoneCount int
		replicationFactor           int
		expectedLocalLimit          int
	}{
		{"GlobalLimitZero", 0, 1, 1, 1, 3, 0},
		{"SingleZoneMultipleIngesters", 100, 1, 10, 10, 3, 30},
		{"MultipleZones", 200, 3, 30, 10, 3, 20},
		{"MultipleZonesNoHealthyIngesters", 200, 2, 0, 0, 3, 0},
		{"MultipleZonesNoHealthyIngestersInZone", 200, 3, 10, 0, 3, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name+"_ingesterStrategy", func(t *testing.T) {
			mockRing := &MockRing{
				zonesCount:                  tc.zonesCount,
				healthyInstancesCount:       tc.healthyInstancesCount,
				healthyInstancesInZoneCount: tc.healthyInstancesInZoneCount,
			}

			strategy := newIngesterRingLimiterStrategy(mockRing, tc.replicationFactor)

			localLimit := strategy.convertGlobalToLocalLimit(tc.globalLimit, "test")
			if localLimit != tc.expectedLocalLimit {
				t.Errorf("expected %d, got %d", tc.expectedLocalLimit, localLimit)
			}
		})
	}
}

func newMockPartitionRingWithPartitions(activeCount int, inactiveCount int) *ring.PartitionRing {
	partitionRing := ring.PartitionRingDesc{
		Partitions: map[int32]ring.PartitionDesc{},
		Owners:     map[string]ring.OwnerDesc{},
	}

	for i := 0; i < activeCount; i++ {
		id := int32(i)

		partitionRing.Partitions[id] = ring.PartitionDesc{
			Id:     id,
			Tokens: []uint32{uint32(id)},
			State:  ring.PartitionActive,
		}
		partitionRing.Owners[fmt.Sprintf("test%d", id)] = ring.OwnerDesc{
			OwnedPartition: id,
			State:          ring.OwnerActive,
		}
	}
	for i := activeCount; i < activeCount+inactiveCount; i++ {
		id := int32(i)

		partitionRing.Partitions[id] = ring.PartitionDesc{
			Id:     id,
			Tokens: []uint32{uint32(id)},
			State:  ring.PartitionInactive,
		}
	}
	return ring.NewPartitionRing(partitionRing)
}

func TestConvertGlobalToLocalLimit_PartitionRing(t *testing.T) {
	tests := []struct {
		name               string
		globalLimit        int
		activePartitions   int
		inactivePartitions int
		shardsPerUser      int
		expectedLocalLimit int
	}{
		{"GlobalLimitZero", 0, 1, 0, 0, 0},
		{"SinglePartition", 100, 1, 0, 0, 100},
		{"MultiplePartitions", 200, 3, 0, 0, 66},
		{"NoActivePartitions", 200, 0, 3, 0, 0},
		{"PartialActivePartitions", 60, 3, 3, 0, 20},
		{"LimitLessThanActivePartitions", 3, 10, 0, 0, 0},
		{"LimitLessThanActivePartitions", 3, 10, 0, 0, 0},
		{"MultiplePartitionsWithLimitedShardsPerUser", 200, 3, 0, 2, 100},
		{"MultiplePartitionsWithMoreShardsPerUserThanPartitions", 200, 3, 0, 10, 66},
	}

	for _, tc := range tests {
		t.Run(tc.name+"_partitionStrategy", func(t *testing.T) {
			ringReader := &mockPartitionRingReader{
				ring: newMockPartitionRingWithPartitions(tc.activePartitions, tc.inactivePartitions),
			}

			getPartitionsForUser := func(_ string) int {
				return tc.shardsPerUser
			}

			strategy := newPartitionRingLimiterStrategy(ringReader, getPartitionsForUser)

			localLimit := strategy.convertGlobalToLocalLimit(tc.globalLimit, "test")
			if localLimit != tc.expectedLocalLimit {
				t.Errorf("expected %d, got %d", tc.expectedLocalLimit, localLimit)
			}
		})
	}
}
