package distributor

import (
	"testing"

	"github.com/grafana/dskit/limiter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/validation"
)

const (
	bytesInMB = 1048576
)

func TestIngestionRateStrategy(t *testing.T) {
	tests := map[string]struct {
		limits        validation.Limits
		ring          ReadLifecycler
		expectedLimit float64
		expectedBurst int
	}{
		"local rate limiter should just return configured limits": {
			limits: validation.Limits{
				IngestionRateStrategy: validation.LocalIngestionRateStrategy,
				IngestionRateMB:       1.0,
				IngestionBurstSizeMB:  2.0,
			},
			ring:          nil,
			expectedLimit: 1.0 * float64(bytesInMB),
			expectedBurst: int(2.0 * float64(bytesInMB)),
		},
		"global rate limiter should share the limit across the number of distributors": {
			limits: validation.Limits{
				IngestionRateStrategy: validation.GlobalIngestionRateStrategy,
				IngestionRateMB:       1.0,
				IngestionBurstSizeMB:  2.0,
			},
			ring: func() ReadLifecycler {
				ring := newReadLifecyclerMock()
				ring.On("HealthyInstancesCount").Return(2)
				return ring
			}(),
			expectedLimit: 0.5 * float64(bytesInMB),
			expectedBurst: int(2.0 * float64(bytesInMB)),
		},
		"global rate limiter should share nothing when there aren't any distributors": {
			limits: validation.Limits{
				IngestionRateStrategy: validation.GlobalIngestionRateStrategy,
				IngestionRateMB:       1.0,
				IngestionBurstSizeMB:  2.0,
			},
			ring: func() ReadLifecycler {
				ring := newReadLifecyclerMock()
				ring.On("HealthyInstancesCount").Return(0)
				return ring
			}(),
			expectedLimit: 1.0 * float64(bytesInMB),
			expectedBurst: int(2.0 * float64(bytesInMB)),
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			var strategy limiter.RateLimiterStrategy

			// Init limits overrides
			overrides, err := validation.NewOverrides(testData.limits, nil)
			require.NoError(t, err)

			// Instance the strategy
			switch testData.limits.IngestionRateStrategy {
			case validation.LocalIngestionRateStrategy:
				strategy = newLocalIngestionRateStrategy(overrides)
			case validation.GlobalIngestionRateStrategy:
				strategy = newGlobalIngestionRateStrategy(overrides, testData.ring)
			default:
				require.Fail(t, "Unknown strategy")
			}

			assert.Equal(t, strategy.Limit("test"), testData.expectedLimit)
			assert.Equal(t, strategy.Burst("test"), testData.expectedBurst)
		})
	}
}

func TestRateLimitKeyEncoding(t *testing.T) {
	for _, tc := range []struct {
		key            string
		expectedTenant string
		expectedPolicy string
	}{
		{"123", "123", ""},
		{"123:finance", "123", "finance"},
		{"123:a:b", "123", "a:b"}, // policy may itself contain the separator; split on first only
		{"", "", ""},
	} {
		tenant, policy := decodeRateLimitKey(tc.key)
		assert.Equal(t, tc.expectedTenant, tenant, "tenant for %q", tc.key)
		assert.Equal(t, tc.expectedPolicy, policy, "policy for %q", tc.key)
	}

	// encode/decode round-trip
	assert.Equal(t, "123", encodeRateLimitKey("123", ""))
	assert.Equal(t, "123:finance", encodeRateLimitKey("123", "finance"))
	tenant, policy := decodeRateLimitKey(encodeRateLimitKey("123", "finance"))
	assert.Equal(t, "123", tenant)
	assert.Equal(t, "finance", policy)
}

func TestIngestionRateStrategy_PolicyOverride(t *testing.T) {
	limits := validation.Limits{
		IngestionRateMB:      1.0,
		IngestionBurstSizeMB: 2.0,
		PolicyOverrideLimits: map[string]validation.PolicyOverridableLimits{
			"finance": {IngestionRateMB: 5.0, IngestionBurstSizeMB: 10.0},
			"ops":     {IngestionRateMB: 5.0}, // rate set, burst unset -> falls back to tenant burst
		},
	}
	overrides, err := validation.NewOverrides(limits, nil)
	require.NoError(t, err)

	t.Run("local", func(t *testing.T) {
		s := newLocalIngestionRateStrategy(overrides)

		// tenant-wide bucket (bare key) is unchanged.
		assert.Equal(t, 1.0*float64(bytesInMB), s.Limit("123"))
		assert.Equal(t, int(2.0*float64(bytesInMB)), s.Burst("123"))

		// per-policy override replaces both rate and burst.
		assert.Equal(t, 5.0*float64(bytesInMB), s.Limit(encodeRateLimitKey("123", "finance")))
		assert.Equal(t, int(10.0*float64(bytesInMB)), s.Burst(encodeRateLimitKey("123", "finance")))

		// policy with rate but no burst -> tenant burst.
		assert.Equal(t, 5.0*float64(bytesInMB), s.Limit(encodeRateLimitKey("123", "ops")))
		assert.Equal(t, int(2.0*float64(bytesInMB)), s.Burst(encodeRateLimitKey("123", "ops")))

		// unknown policy -> tenant rate/burst.
		assert.Equal(t, 1.0*float64(bytesInMB), s.Limit(encodeRateLimitKey("123", "unknown")))
		assert.Equal(t, int(2.0*float64(bytesInMB)), s.Burst(encodeRateLimitKey("123", "unknown")))
	})

	t.Run("global divides policy rate by distributor count", func(t *testing.T) {
		ring := newReadLifecyclerMock()
		ring.On("HealthyInstancesCount").Return(2)
		s := newGlobalIngestionRateStrategy(overrides, ring)

		// per-policy rate is divided by the number of distributors, just like the tenant rate.
		assert.Equal(t, 2.5*float64(bytesInMB), s.Limit(encodeRateLimitKey("123", "finance")))
		// burst is not divided.
		assert.Equal(t, int(10.0*float64(bytesInMB)), s.Burst(encodeRateLimitKey("123", "finance")))
	})
}

type readLifecyclerMock struct {
	mock.Mock
}

func newReadLifecyclerMock() *readLifecyclerMock {
	return &readLifecyclerMock{}
}

func (m *readLifecyclerMock) HealthyInstancesCount() int {
	args := m.Called()
	return args.Int(0)
}
