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
			actual := streamCountLimiter.AssertNewStreamAllowed("test", noPolicy)

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
	r, err := ring.NewPartitionRing(partitionRing)
	if err != nil {
		panic(err)
	}
	return r
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

func TestLimiter_PolicyLimitsAndPrecedence(t *testing.T) {
	// Create a comprehensive mock limits implementation
	mockLimits := &mockLimits{
		maxLocalStreams:  200,
		maxGlobalStreams: 2000,
		policyLimits: map[string]struct {
			local  int
			global int
		}{
			"finance": {local: 100, global: 1000},
			"ops":     {local: 50, global: 500},
		},
	}

	// Create a simple ring strategy for testing
	ringStrategy := &mockRingStrategy{
		healthyInstances:  2,
		replicationFactor: 1,
	}

	limiter := &Limiter{
		limits:       mockLimits,
		ringStrategy: ringStrategy,
	}

	// Test 1: Limit calculation accuracy for different policies
	t.Run("limit_calculation", func(t *testing.T) {
		// Test with no policy - should use default limits
		calculated, local, global, adjusted := limiter.GetStreamCountLimit("tenant1", noPolicy)
		require.Equal(t, 200, local, "No policy should use default local limit")
		require.Equal(t, 2000, global, "No policy should use default global limit")
		require.Equal(t, 1000, adjusted, "Adjusted global limit should be 2000/2*1 = 1000")
		require.Equal(t, 200, calculated, "Calculated limit should be min(200, 1000) = 200")

		// Test with finance policy - should use policy-specific limits
		calculated, local, global, adjusted = limiter.GetStreamCountLimit("tenant1", "finance")
		require.Equal(t, 100, local, "Finance policy should use policy local limit")
		require.Equal(t, 1000, global, "Finance policy should use policy global limit")
		require.Equal(t, 500, adjusted, "Adjusted global limit should be 1000/2*1 = 500")
		require.Equal(t, 100, calculated, "Calculated limit should be min(100, 500) = 100")

		// Test with ops policy - should use policy-specific limits
		calculated, local, global, adjusted = limiter.GetStreamCountLimit("tenant1", "ops")
		require.Equal(t, 50, local, "Ops policy should use policy local limit")
		require.Equal(t, 500, global, "Ops policy should use policy global limit")
		require.Equal(t, 250, adjusted, "Adjusted global limit should be 500/2*1 = 250")
		require.Equal(t, 50, calculated, "Calculated limit should be min(50, 250) = 50")

		// Test with non-existent policy - should fall back to default limits
		calculated, local, global, adjusted = limiter.GetStreamCountLimit("tenant1", "nonexistent")
		require.Equal(t, 200, local, "Non-existent policy should use default local limit")
		require.Equal(t, 2000, global, "Non-existent policy should use default global limit")
		require.Equal(t, 1000, adjusted, "Adjusted global limit should be 2000/2*1 = 1000")
		require.Equal(t, 200, calculated, "Calculated limit should be min(200, 1000) = 200")
	})

	// Test 2: Policy precedence over fixed limits
	t.Run("policy_precedence", func(t *testing.T) {
		// Create a stream count limiter with a fixed limit supplier that returns 150
		fixedLimitSupplier := func() int { return 150 }
		streamCountLimiter := &streamCountLimiter{
			limiter: limiter,
		}

		// Test: No policy specified - fixed limit should be applied
		// No-policy limit: min(200, 2000/2) = min(200, 1000) = 200
		// Fixed limit: 150
		// Expected: 200 (the higher of the two)
		calculated, _, _, _ := streamCountLimiter.getCurrentLimit("tenant1", noPolicy, fixedLimitSupplier)
		require.Equal(t, 200, calculated, "Without policy, should use the higher of calculated and fixed limits")

		// Test: Policy specified - policy limit should take precedence over fixed limit
		// Policy limit: min(100, 1000/2) = min(100, 500) = 100
		// Fixed limit: 150 (should be ignored when policy is specified)
		// Expected: 100 (policy limit takes precedence)
		calculated, _, _, _ = streamCountLimiter.getCurrentLimit("tenant1", "finance", fixedLimitSupplier)
		require.Equal(t, 100, calculated, "With policy, policy limit should take precedence over fixed limit")

		// Test: Verify that policy limits are actually being enforced end-to-end
		calculated, local, global, adjusted := streamCountLimiter.getCurrentLimit("tenant1", "finance", fixedLimitSupplier)
		require.Equal(t, 100, local, "Policy local limit should be 100")
		require.Equal(t, 1000, global, "Policy global limit should be 1000")
		require.Equal(t, 500, adjusted, "Policy adjusted global limit should be 500")
		require.Equal(t, 100, calculated, "Policy calculated limit should be 100")
	})
}

// Mock implementations for testing
type mockLimits struct {
	Limits
	maxLocalStreams  int
	maxGlobalStreams int
	policyLimits     map[string]struct {
		local  int
		global int
	}
}

func (m *mockLimits) MaxLocalStreamsPerUser(_ string) int {
	return m.maxLocalStreams
}

func (m *mockLimits) MaxGlobalStreamsPerUser(_ string) int {
	return m.maxGlobalStreams
}

func (m *mockLimits) PolicyMaxLocalStreamsPerUser(_, policy string) int {
	if policyLimit, exists := m.policyLimits[policy]; exists {
		return policyLimit.local
	}
	return 0
}

func (m *mockLimits) PolicyMaxGlobalStreamsPerUser(_, policy string) (int, bool) {
	if policyLimit, exists := m.policyLimits[policy]; exists {
		return policyLimit.global, true
	}
	return 0, false
}

type mockRingStrategy struct {
	healthyInstances  int
	replicationFactor int
}

func (m *mockRingStrategy) convertGlobalToLocalLimit(globalLimit int, _ string) int {
	if globalLimit == 0 || m.replicationFactor == 0 {
		return 0
	}
	return int(float64(globalLimit) / float64(m.healthyInstances) * float64(m.replicationFactor))
}
