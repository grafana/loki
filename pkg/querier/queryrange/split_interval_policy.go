package queryrange

import (
	"time"

	"github.com/grafana/loki/v3/pkg/util/validation"
)

// SplitIntervalPolicy holds the parts of time splitting that depend on where a query reads its
// data from.
type SplitIntervalPolicy interface {
	// RangeQueryInterval returns the interval that cuts the time range of a metric range request.
	// It returns 0 when the tenants turn splitting off.
	RangeQueryInterval(tenantIDs []string, r *LokiRequest) time.Duration

	// InstantQueryInterval returns the interval that cuts the range vector of an instant request.
	// It returns 0 when the tenants turn splitting off.
	InstantQueryInterval(tenantIDs []string) time.Duration
}

var _ SplitIntervalPolicy = chunkSplitIntervalPolicy{}

// chunkSplitIntervalPolicy is the policy of queries that read chunks.
type chunkSplitIntervalPolicy struct {
	limits Limits
}

func newChunkSplitIntervalPolicy(limits Limits) chunkSplitIntervalPolicy {
	return chunkSplitIntervalPolicy{limits: limits}
}

func (p chunkSplitIntervalPolicy) RangeQueryInterval(tenantIDs []string, r *LokiRequest) time.Duration {
	interval := validation.SmallestPositiveNonZeroDurationPerTenant(tenantIDs, p.limits.QuerySplitDuration)
	if interval == 0 {
		return 0
	}

	// A range vector longer than the interval widens the interval to the range vector. Smaller
	// splits would read the same data again and again.
	maxRange, _ := maxRangeVectorAndOffsetDuration(r.Plan.AST)
	return max(interval, maxRange)
}

func (p chunkSplitIntervalPolicy) InstantQueryInterval(tenantIDs []string) time.Duration {
	return validation.SmallestPositiveNonZeroDurationPerTenant(tenantIDs, p.limits.InstantMetricQuerySplitDuration)
}
