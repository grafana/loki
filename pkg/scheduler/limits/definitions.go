package limits

import (
	"math"
)

// Limits needed for the Query Scheduler - interface used for decoupling.
type Limits interface {
	// MaxQueriersPerUser returns max queriers to use per tenant, or 0 if shuffle sharding is disabled.
	MaxQueriersPerUser(user string) int

	// MaxQueryCapacity returns how much of the available query capacity can be used by this user.
	MaxQueryCapacity(user string) float64
}

func QueueLimits(limits Limits) *queueLimits {
	return &queueLimits{limits: limits}
}

type queueLimits struct {
	limits Limits
}

// MaxConsumers is used to compute how many of the available queriers are allowed to handle requests for a given tenant.
func (c *queueLimits) MaxConsumers(tenantID string, allConsumers int) int {
	if c == nil || c.limits == nil {
		return 0
	}

	maxQueriers := c.limits.MaxQueriersPerUser(tenantID)
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
