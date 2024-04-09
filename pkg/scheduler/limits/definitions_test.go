package limits

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQueueLimitsMaxConsumers(t *testing.T) {
	for name, tt := range map[string]struct {
		limits   *QueueLimits
		expected int
	}{
		"nil limits": {
			limits:   NewQueueLimits(nil),
			expected: 0,
		},
		"no limits": {
			limits: NewQueueLimits(mockLimits{
				maxQueriers:      0,
				maxQueryCapacity: 0,
			}),
			expected: 0,
		},
		"enforce max queriers": {
			limits: NewQueueLimits(mockLimits{
				maxQueriers:      5,
				maxQueryCapacity: 0,
			}),
			expected: 5,
		},
		"prefer max queriers over query capacity": {
			limits: NewQueueLimits(mockLimits{
				maxQueriers:      5,
				maxQueryCapacity: 1.0,
			}),
			expected: 5,
		},
		"enforce max query capacity": {
			limits: NewQueueLimits(mockLimits{
				maxQueriers:      0,
				maxQueryCapacity: 0.5,
			}),
			expected: 5,
		},
		"prefer query capacity over max queriers": {
			limits: NewQueueLimits(mockLimits{
				maxQueriers:      5,
				maxQueryCapacity: 0.4,
			}),
			expected: 4,
		},
		"query capacity of 1.0": {
			limits: NewQueueLimits(mockLimits{
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
	maxQueriers      uint
	maxQueryCapacity float64
}

func (l mockLimits) MaxQueriersPerUser(_ string) uint {
	return l.maxQueriers
}

func (l mockLimits) MaxQueryCapacity(_ string) float64 {
	return l.maxQueryCapacity
}
