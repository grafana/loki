package limits

import (
	"math"

	"github.com/grafana/dskit/tenant"
	"github.com/grafana/loki/pkg/util/validation"
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

// MaxConsumers is used to compute how many of the available consumers are allowed to handle requests for a given tenant.
func (c *queueLimits) MaxConsumers(tenantID string, allConsumers int) (int, error) {
	if c == nil || c.limits == nil {
		return 0, nil
	}

	// aggregate the max queriers limit in the case of a multi tenant query
	tenantIDs, err := tenant.TenantIDsFromOrgID(tenantID)
	if err != nil {
		return 0, err
	}

	maxConsumers := validation.SmallestPositiveNonZeroIntPerTenant(tenantIDs, c.limits.MaxQueriersPerUser)
	maxCapacity := validation.SmallestPositiveNonZeroFloatPerTenant(tenantIDs, c.limits.MaxQueryCapacity)

	if maxCapacity == 0 {
		return maxConsumers, nil
	}

	res := int(math.Ceil(float64(allConsumers) * maxCapacity))
	if maxConsumers != 0 && maxConsumers < res {
		return maxConsumers, nil
	}

	return res, nil
}
