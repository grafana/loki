package distributor

import (
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/validation"
	"github.com/prometheus/prometheus/model/labels"
)

// RequestScopedPolicyResolver maintains a cache of policy decisions
// that only exists for the duration of a single push request.
type RequestScopedPolicyResolver struct {
	overrides Limits
	logger    log.Logger
	cache     map[uint64]string
}

// NewRequestScopedPolicyResolver creates a new RequestScopedPolicyResolver for a single request.
// The resolver is not thread-safe, and should not be used concurrently.
// Because we have a fresh new map for each request, we don't need to care about memory explosion/use an LRU cache.
func NewRequestScopedPolicyResolver(overrides Limits, logger log.Logger) *RequestScopedPolicyResolver {
	return &RequestScopedPolicyResolver{
		overrides: overrides,
		logger:    logger,
		cache:     make(map[uint64]string),
	}
}

// ResolvePolicy returns a consistent policy for the given userID and labels,
// caching the result during the lifetime of this resolver.
func (r *RequestScopedPolicyResolver) ResolvePolicy(userID string, lbs labels.Labels) string {
	labelHash := lbs.Hash()

	// Check if we already have a cached decision
	if policy, ok := r.cache[labelHash]; ok {
		return policy
	}

	// If not cached, resolve the policy
	mappings := r.overrides.PoliciesStreamMapping(userID)
	policy := r.fetchPolicy(userID, lbs, mappings, r.logger)

	// Cache the decision
	r.cache[labelHash] = policy

	return policy
}

// AsPolicyResolver returns a push.PolicyResolver function that uses this resolver.
func (r *RequestScopedPolicyResolver) AsPolicyResolver() push.PolicyResolver {
	return r.ResolvePolicy
}

// fetchPolicy picks the first matching policy for a stream based on the label set.
func (r *RequestScopedPolicyResolver) fetchPolicy(userID string, lbs labels.Labels, mapping validation.PolicyStreamMapping, logger log.Logger) string {
	policies := mapping.PolicyFor(lbs)

	var policy string
	if len(policies) > 0 {
		policy = policies[0]
		if len(policies) > 1 {
			level.Warn(logger).Log(
				"msg", "multiple policies matched for the same stream",
				"org_id", userID,
				"stream", lbs.String(),
				"policy", policy,
				"policies", strings.Join(policies, ","),
				"insight", "true",
			)
		}
	}

	return policy
}

// RequestScopedRetentionResolver maintains a cache of retention periods
// that only exists for the duration of a single push request.
type RequestScopedRetentionResolver struct {
	tenantsRetention *retention.TenantsRetention
	cache            map[uint64]time.Duration
}

// NewRequestScopedRetentionResolver creates a new RequestScopedRetentionResolver for a single request.
// The resolver is not thread-safe, and should not be used concurrently.
// Because we have a fresh new map for each request, we don't need to care about memory explosion/use an LRU cache.
func NewRequestScopedRetentionResolver(tenantsRetention *retention.TenantsRetention) *RequestScopedRetentionResolver {
	return &RequestScopedRetentionResolver{
		tenantsRetention: tenantsRetention,
		cache:            make(map[uint64]time.Duration),
	}
}

// RetentionPeriodFor returns a consistent retention period for the given userID and labels,
// caching the result during the lifetime of this resolver.
func (r *RequestScopedRetentionResolver) RetentionPeriodFor(userID string, lbs labels.Labels) time.Duration {
	labelHash := lbs.Hash()

	// Check if we already have a cached decision
	if period, ok := r.cache[labelHash]; ok {
		return period
	}

	// If not cached, resolve the retention period
	period := r.tenantsRetention.RetentionPeriodFor(userID, lbs)

	// Cache the decision
	r.cache[labelHash] = period

	return period
}

// RetentionHoursFor returns the retention period as a string in hours,
// using the cached value if available.
func (r *RequestScopedRetentionResolver) RetentionHoursFor(userID string, lbs labels.Labels) string {
	period := r.RetentionPeriodFor(userID, lbs)

	// Convert to hours format
	if period <= 0 {
		return "0"
	}

	// Convert hours (float64) to string
	return strconv.FormatInt(int64(period.Hours()), 10)
}
