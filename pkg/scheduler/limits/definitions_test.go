package limits

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQueueLimitsMaxConsumers(t *testing.T) {
	for name, tt := range map[string]struct {
		limits   *queueLimits
		expected int
	}{
		"nil limits": {
			limits:   QueueLimits(nil),
			expected: 0,
		},
		"no limits": {
			limits: QueueLimits(mockLimits{
				maxQueriers:      0,
				maxQueryCapacity: 0,
			}),
			expected: 0,
		},
		"enforce max queriers": {
			limits: QueueLimits(mockLimits{
				maxQueriers:      5,
				maxQueryCapacity: 0,
			}),
			expected: 5,
		},
		"prefer max queriers over query capacity": {
			limits: QueueLimits(mockLimits{
				maxQueriers:      5,
				maxQueryCapacity: 1.0,
			}),
			expected: 5,
		},
		"enforce max query capacity": {
			limits: QueueLimits(mockLimits{
				maxQueriers:      0,
				maxQueryCapacity: 0.5,
			}),
			expected: 5,
		},
		"prefer query capacity over max queriers": {
			limits: QueueLimits(mockLimits{
				maxQueriers:      5,
				maxQueryCapacity: 0.4,
			}),
			expected: 4,
		},
		"query capacity of 1.0": {
			limits: QueueLimits(mockLimits{
				maxQueryCapacity: 1.0,
			}),
			expected: 10,
		},
	} {
		t.Run(name, func(t *testing.T) {
			res := tt.limits.MaxConsumers("", 10)
			assert.Equal(t, tt.expected, res)
		})
	}
}

type mockLimits struct {
	maxQueriers      int
	maxQueryCapacity float64
}

func (l mockLimits) MaxQueriersPerUser(_ string) int {
	return l.maxQueriers
}

func (l mockLimits) MaxQueryCapacity(_ string) float64 {
	return l.maxQueryCapacity
}
