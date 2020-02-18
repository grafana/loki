package distributor

import (
	"testing"

	"github.com/cortexproject/cortex/pkg/util/limiter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/util/validation"
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
	}

	for testName, testData := range tests {
		testData := testData

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
