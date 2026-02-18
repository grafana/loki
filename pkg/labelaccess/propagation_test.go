package labelaccess

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/labelaccess/types"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
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
