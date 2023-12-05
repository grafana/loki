package limits

import (
	"math"
)

// Limits needed for the Query Scheduler - interface used for decoupling.
type Limits interface {
	// MaxQueriersPerUser returns max queriers to use per tenant, or 0 if shuffle sharding is disabled.
	MaxQueriersPerUser(user string) uint

	// MaxQueryCapacity returns how much of the available query capacity can be used by this user.
	MaxQueryCapacity(user string) float64
}

func NewQueueLimits(limits Limits) *QueueLimits {
	return &QueueLimits{limits: limits}
}

type QueueLimits struct {
	limits Limits
}

// MaxConsumers is used to compute how many of the available queriers are allowed to handle requests for a given tenant.
// Returns the min value or one of (frontend.max-queriers-per-tenant, ceil(querier_replicas * frontend.max-query-capacity))
// depending of whether both or only one of the two limits are configured.
// 0 is returned when neither limits are applied.
func (c *QueueLimits) MaxConsumers(tenantID string, allConsumers int) int {
	if c == nil || c.limits == nil {
		return 0
	}

	maxQueriers := int(c.limits.MaxQueriersPerUser(tenantID))
	maxCapacity := c.limits.MaxQueryCapacity(tenantID)

	if maxCapacity == 0 {
		return maxQueriers
	}

	res := int(math.Ceil(float64(allConsumers) * maxCapacity))
	if maxQueriers != 0 && maxQueriers < res {
		return maxQueriers
	}

	return res
}
