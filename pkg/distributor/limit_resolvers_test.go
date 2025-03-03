package distributor

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/validation"
)

// mockLimits is a simple implementation of the Limits interface for testing
type mockLimits struct {
	policyMapping   validation.PolicyStreamMapping
	retentions      []validation.StreamRetention
	globalRetention time.Duration

	Limits
}

// PoliciesStreamMapping implements the Limits interface method needed for policy resolution
func (m *mockLimits) PoliciesStreamMapping(userID string) validation.PolicyStreamMapping {
	return m.policyMapping
}

func (m *mockLimits) StreamRetention(userID string) []validation.StreamRetention {
	return m.retentions
}

func (m *mockLimits) RetentionPeriod(userID string) time.Duration {
	return m.globalRetention
}

func TestRequestScopedPolicyResolver_StabilityAcrossConfigChanges(t *testing.T) {
	// Set up mock overrides with initial policy mapping
	policyMapping := validation.PolicyStreamMapping{
		"policy-1": []*validation.PriorityStream{
			{
				Selector: `{service="foo"}`,
				Priority: 1,
			},
		},
	}

	// Validate the mapping to populate Matchers
	err := policyMapping.Validate()
	require.NoError(t, err)

	mockOverrides := &mockLimits{
		policyMapping: policyMapping,
	}

	logger := log.NewNopLogger()

	// Create a request-scoped policy resolver with the initial configuration
	resolver := NewRequestScopedPolicyResolver(mockOverrides, logger)

	// Create labels for testing
	lbls := labels.FromStrings("service", "foo")

	// First policy resolution with the initial configuration
	policy := resolver.ResolvePolicy("user-1", lbls)
	assert.Equal(t, "policy-1", policy, "Expected policy-1 to be returned for service=foo")

	// Change the policy mapping in the underlying configuration
	newPolicyMapping := validation.PolicyStreamMapping{
		"policy-2": []*validation.PriorityStream{
			{
				Selector: `{service="foo"}`,
				Priority: 1,
			},
		},
	}

	// Validate the new mapping
	err = newPolicyMapping.Validate()
	require.NoError(t, err)

	mockOverrides.policyMapping = newPolicyMapping

	// The second policy resolution should return the same result,
	// since the resolver caches the result during its lifetime
	policy = resolver.ResolvePolicy("user-1", lbls)
	assert.Equal(t, "policy-1", policy, "Expected policy-1 to still be returned after config change")

	// Create a new resolver with the updated configuration
	newResolver := NewRequestScopedPolicyResolver(mockOverrides, logger)

	// The new resolver should use the updated configuration
	policy = newResolver.ResolvePolicy("user-1", lbls)
	assert.Equal(t, "policy-2", policy, "Expected policy-2 to be returned with the new resolver")
}

func TestRequestScopedRetentionResolver_StabilityAcrossConfigChanges(t *testing.T) {
	lbs := labels.FromStrings("service", "foo")

	mockOverrides := &mockLimits{
		retentions: []validation.StreamRetention{},
	}

	var matchers []*labels.Matcher
	for _, lb := range lbs {
		matchers = append(matchers, labels.MustNewMatcher(labels.MatchEqual, lb.Name, lb.Value))
	}

	mockOverrides.retentions = []validation.StreamRetention{
		{
			Matchers: matchers,
			Priority: 1,
			Period:   model.Duration(time.Hour * 24),
		},
	}

	tenantsRetention := retention.NewTenantsRetention(mockOverrides)

	// Create a request-scoped retention resolver with the initial configuration
	resolver := NewRequestScopedRetentionResolver(tenantsRetention)

	// Resolve the retention period for the labels.
	retentionPeriod := resolver.RetentionPeriodFor("" /* not used on this test */, lbs)
	assert.Equal(t, time.Hour*24, retentionPeriod)

	// Change the duration mapped in runtime.
	mockOverrides.retentions[0].Period = model.Duration(time.Hour * 48)

	// The second retention period resolution should return the same result,
	// since the resolver caches the result during its lifetime
	retentionPeriod = resolver.RetentionPeriodFor("" /* not used on this test */, lbs)
	assert.Equal(t, time.Hour*24, retentionPeriod)

	// Create a new resolver with the updated configuration
	newResolver := NewRequestScopedRetentionResolver(tenantsRetention)

	// The new resolver should use the updated configuration
	retentionPeriod = newResolver.RetentionPeriodFor("" /* not used on this test */, lbs)
	assert.Equal(t, time.Hour*48, retentionPeriod)
}

func TestRequestScopedPolicyResolver_MultipleLabels(t *testing.T) {
	// Create a logger
	logger := log.NewNopLogger()

	// Set up policy mapping with multiple potential matches
	policyMapping := validation.PolicyStreamMapping{
		"policy1": []*validation.PriorityStream{
			{
				Selector: `{app="foo"}`,
				Priority: 3,
			},
		},
		"policy2": []*validation.PriorityStream{
			{
				Selector: `{env="prod"}`,
				Priority: 2,
			},
		},
		"policy3": []*validation.PriorityStream{
			{
				Selector: `{app="foo", env="prod"}`,
				Priority: 1,
			},
		},
	}
	require.NoError(t, policyMapping.Validate())

	// Create a mock limits implementation
	mockOverrides := &mockLimits{
		policyMapping: policyMapping,
	}

	// Create a policy resolver
	resolver := NewRequestScopedPolicyResolver(mockOverrides, logger)

	// Test case 1: Match app=foo
	lbs1 := labels.FromStrings("app", "foo")
	policy1 := resolver.ResolvePolicy("test_user", lbs1)
	assert.Equal(t, "policy1", policy1)

	// Test case 2: Match env=prod
	lbs2 := labels.FromStrings("env", "prod")
	policy2 := resolver.ResolvePolicy("test_user", lbs2)
	assert.Equal(t, "policy2", policy2)

	// Test case 3: No match
	lbs3 := labels.FromStrings("app", "bar", "env", "dev")
	policy3 := resolver.ResolvePolicy("test_user", lbs3)
	assert.Equal(t, "", policy3, "Expected no policy match")

	// Request the policies again to verify caching
	assert.Equal(t, policy1, resolver.ResolvePolicy("test_user", lbs1))
	assert.Equal(t, policy2, resolver.ResolvePolicy("test_user", lbs2))
	assert.Equal(t, policy3, resolver.ResolvePolicy("test_user", lbs3))
}
