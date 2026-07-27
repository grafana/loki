package labelaccess

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/labelaccess/types"
)

func TestLabelPolicySetString(t *testing.T) {
	instancePolicyMap := LabelPolicySet{}

	// One tenant with multiple label matchers
	m1, err := labels.NewMatcher(labels.MatchEqual, "env", "dev")
	require.Nil(t, err)

	m2, err := labels.NewMatcher(labels.MatchNotEqual, "level", "info")
	require.Nil(t, err)

	policy := &types.LabelPolicy{
		Selector: make([]*types.LabelMatcher, 2),
	}
	policy.Selector[0], err = types.LabelMatcherFromPromLabel(m1)
	require.Nil(t, err)
	policy.Selector[1], err = types.LabelMatcherFromPromLabel(m2)
	require.Nil(t, err)

	instancePolicyMap["tenant1"] = []*types.LabelPolicy{policy}

	require.Equal(t, "tenant1:[{env=\"dev\",level!=\"info\",},] ", instancePolicyMap.String())

	// Add another tenant with one label matchers
	m3, err := labels.NewMatcher(labels.MatchNotRegexp, "env", "p.*")
	require.Nil(t, err)

	policy2 := &types.LabelPolicy{
		Selector: make([]*types.LabelMatcher, 1),
	}
	policy2.Selector[0], err = types.LabelMatcherFromPromLabel(m3)
	require.Nil(t, err)

	m4, err := labels.NewMatcher(labels.MatchRegexp, "blah", "p.*")
	require.Nil(t, err)

	policy3 := &types.LabelPolicy{
		Selector: make([]*types.LabelMatcher, 1),
	}
	policy3.Selector[0], err = types.LabelMatcherFromPromLabel(m4)

	require.Nil(t, err)

	instancePolicyMap["tenant2"] = []*types.LabelPolicy{policy2, policy3}

	require.Equal(t, "tenant1:[{env=\"dev\",level!=\"info\",},] tenant2:[{env!~\"p.*\",},{blah=~\"p.*\",},] ", instancePolicyMap.String())
}

// TestLabelPolicySetHashOrderIndependent guards against the cache-key
// fragmentation bug in grafana/loki-private#2522: a multi-team user's policies
// arrive in the X-Prom-Label-Policy header in an unstable order, and the same
// logical access set must always produce the same hash (and String) so it maps
// to a single results-cache bucket.
func mustPolicy(t *testing.T, matchType labels.MatchType, name, value string) *types.LabelPolicy {
	t.Helper()
	m, err := labels.NewMatcher(matchType, name, value)
	require.NoError(t, err)
	lm, err := types.LabelMatcherFromPromLabel(m)
	require.NoError(t, err)
	return &types.LabelPolicy{Selector: []*types.LabelMatcher{lm}}
}

func TestLabelPolicySetHashOrderIndependent(t *testing.T) {
	const tenant = "tenant1"

	pA := mustPolicy(t, labels.MatchEqual, "team", "alpha")
	pB := mustPolicy(t, labels.MatchEqual, "team", "bravo")
	pC := mustPolicy(t, labels.MatchEqual, "team", "charlie")

	permutations := [][]*types.LabelPolicy{
		{pA, pB, pC},
		{pA, pC, pB},
		{pB, pA, pC},
		{pB, pC, pA},
		{pC, pA, pB},
		{pC, pB, pA},
	}

	wantHash := LabelPolicySet{tenant: permutations[0]}.hash()
	wantString := LabelPolicySet{tenant: permutations[0]}.String()

	for i, perm := range permutations {
		set := LabelPolicySet{tenant: perm}
		require.Equalf(t, wantHash, set.hash(), "hash differs for permutation %d: %v", i, perm)
		require.Equalf(t, wantString, set.String(), "String differs for permutation %d: %v", i, perm)
	}
}

// TestLabelPolicySetHashDistinct asserts the hash actually discriminates:
// different access sets must produce different keys, otherwise distinct
// permissions would collide on a single cache bucket. It also exercises the
// multi-tenant path (tenant sort + per-tenant canonicalization together), which
// the single-tenant order-independence test does not.
func TestLabelPolicySetHashDistinct(t *testing.T) {
	pAlpha := mustPolicy(t, labels.MatchEqual, "team", "alpha")
	pBravo := mustPolicy(t, labels.MatchEqual, "team", "bravo")
	pCharlie := mustPolicy(t, labels.MatchEqual, "team", "charlie")

	// Two tenants, two policies each, built in different orders. Both describe
	// the same logical access set, so they must hash identically.
	setAB := LabelPolicySet{
		"tenant1": {pAlpha, pBravo},
		"tenant2": {pAlpha, pBravo},
	}
	setBA := LabelPolicySet{
		"tenant2": {pBravo, pAlpha},
		"tenant1": {pBravo, pAlpha},
	}
	require.Equal(t, setAB.hash(), setBA.hash(), "same multi-tenant access set must hash identically regardless of order")

	// Distinct sets must hash distinctly. Each differs from setAB in exactly one
	// dimension: policy content, policy count, and which tenant holds them.
	others := map[string]LabelPolicySet{
		"different policy value": {
			"tenant1": {pAlpha, pCharlie},
			"tenant2": {pAlpha, pBravo},
		},
		"fewer policies": {
			"tenant1": {pAlpha},
			"tenant2": {pAlpha, pBravo},
		},
		"different tenant set": {
			"tenant1": {pAlpha, pBravo},
			"tenant3": {pAlpha, pBravo},
		},
	}
	for name, other := range others {
		require.NotEqualf(t, setAB.hash(), other.hash(), "expected distinct hash for %q", name)
	}
}

// TestLabelPolicySetHashDeduplicates asserts a repeated selector does not change
// the key: a user in multiple teams that contribute identical selectors must map
// to the same cache bucket as if the selector appeared once.
func TestLabelPolicySetHashDeduplicates(t *testing.T) {
	pAlpha := mustPolicy(t, labels.MatchEqual, "team", "alpha")
	pBravo := mustPolicy(t, labels.MatchEqual, "team", "bravo")

	withDup := LabelPolicySet{"tenant1": {pAlpha, pBravo, pBravo}}
	withoutDup := LabelPolicySet{"tenant1": {pAlpha, pBravo}}

	require.Equal(t, withoutDup.hash(), withDup.hash(), "duplicate policy must not change the hash")
	require.Equal(t, withoutDup.String(), withDup.String(), "duplicate policy must not change the String output")
}

func TestInjectAndExtractLabelMatchersContext(t *testing.T) {
	for _, tc := range []struct {
		name        string
		matchers    LabelPolicySet
		expectedErr error
	}{
		{
			name: "single policy is injected and extracted correctly",
			matchers: LabelPolicySet{
				"tenant1": {
					{Selector: []*types.LabelMatcher{{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "prod"}}},
				},
			},
		},
		{
			name: "multiple policies are injected and extracted correctly",
			matchers: LabelPolicySet{
				"tenant1": {
					{Selector: []*types.LabelMatcher{{Type: types.LABEL_MATCHER_TYPE_RE, Name: "env", Value: ".*"}}},
					{Selector: []*types.LabelMatcher{{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "classification", Value: "secret"}}},
				},
			},
		},
		{
			name: "multiple tenants are injected and extracted correctly",
			matchers: LabelPolicySet{
				"tenant1": {
					{Selector: []*types.LabelMatcher{
						{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "dev"},
						{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "classification", Value: "secret"},
					}},
					{Selector: []*types.LabelMatcher{{Type: types.LABEL_MATCHER_TYPE_NEQ, Name: "classification", Value: "secret"}}},
				},
				"tenant2": {
					{Selector: []*types.LabelMatcher{{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "level", Value: "info"}}},
				},
			},
		},
		{
			name:        "empty matchers do not inject anything",
			matchers:    LabelPolicySet{},
			expectedErr: errNoMatcherSource,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			ctx = InjectLabelMatchersContext(ctx, tc.matchers)

			extracted, err := ExtractLabelMatchersContext(ctx)

			if tc.expectedErr != nil {
				require.ErrorIs(t, err, tc.expectedErr)
				return
			}

			require.NoError(t, err)

			require.Equal(t, len(tc.matchers), len(extracted))
			for tenant, expectedPolicies := range tc.matchers {
				require.ElementsMatch(t, expectedPolicies, extracted[tenant])
			}
		})
	}

	t.Run("extracting from empty context returns error", func(t *testing.T) {
		_, err := ExtractLabelMatchersContext(t.Context())
		require.ErrorIs(t, err, errNoMatcherSource)
	})
}
