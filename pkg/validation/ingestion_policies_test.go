package validation

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func Test_PolicyStreamMapping_PolicyFor(t *testing.T) {
	mapping := PolicyStreamMapping{
		"policy1": []*PriorityStream{
			{
				Selector: `{foo="bar"}`,
				Priority: 2,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
				},
			},
		},
		"policy2": []*PriorityStream{
			{
				Selector: `{foo="bar", daz="baz"}`,
				Priority: 1,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
					labels.MustNewMatcher(labels.MatchEqual, "daz", "baz"),
				},
			},
		},
		"policy3": []*PriorityStream{
			{
				Selector: `{qyx="qzx", qox="qox"}`,
				Priority: 2,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchEqual, "qyx", "qzx"),
					labels.MustNewMatcher(labels.MatchEqual, "qox", "qox"),
				},
			},
		},
		"policy4": []*PriorityStream{
			{
				Selector: `{qyx="qzx", qox="qox"}`,
				Priority: 1,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchEqual, "qyx", "qzx"),
					labels.MustNewMatcher(labels.MatchEqual, "qox", "qox"),
				},
			},
		},
		"policy5": []*PriorityStream{
			{
				Selector: `{qab=~"qzx.*"}`,
				Priority: 2,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchRegexp, "qab", "qzx.*"),
				},
			},
		},
		"policy6": []*PriorityStream{
			{
				Selector: `{env="prod"}`,
				Priority: 2,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
				},
			},
			{
				Selector: `{env=~"prod|staging"}`,
				Priority: 1,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchRegexp, "env", "prod|staging"),
				},
			},
			{
				Selector: `{team="finance"}`,
				Priority: 4,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchEqual, "team", "finance"),
				},
			},
		},
		"policy7": []*PriorityStream{
			{
				Selector: `{env=~"prod|dev"}`,
				Priority: 3,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchRegexp, "env", "prod|dev"),
				},
			},
		},
		"policy8": []*PriorityStream{
			{
				Selector: `{env=~"prod|test"}`,
				Priority: 3,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchRegexp, "env", "prod|test"),
				},
			},
		},
	}

	require.NoError(t, mapping.Validate())

	require.Equal(t, []string{"policy1"}, mapping.PolicyFor(labels.FromStrings("foo", "bar")))
	// matches both policy2 and policy1 but policy1 has higher priority.
	require.Equal(t, []string{"policy1"}, mapping.PolicyFor(labels.FromStrings("foo", "bar", "daz", "baz")))
	// matches policy3 and policy4 but policy3 has higher priority..
	require.Equal(t, []string{"policy3"}, mapping.PolicyFor(labels.FromStrings("qyx", "qzx", "qox", "qox")))
	// matches no policy.
	require.Empty(t, mapping.PolicyFor(labels.FromStrings("foo", "fooz", "daz", "qux", "quux", "corge")))
	// matches policy5 through regex.
	require.Equal(t, []string{"policy5"}, mapping.PolicyFor(labels.FromStrings("qab", "qzxqox")))

	require.Equal(t, []string{"policy6"}, mapping.PolicyFor(labels.FromStrings("env", "prod", "team", "finance")))
	// Matches policy7 and policy8 which have the same priority.
	require.Equal(t, []string{"policy7", "policy8"}, mapping.PolicyFor(labels.FromStrings("env", "prod")))
}

func TestPolicyStreamMapping_ApplyDefaultPolicyStreamMappings(t *testing.T) {
	tests := []struct {
		name          string
		existing      PolicyStreamMapping
		defaults      PolicyStreamMapping
		expected      PolicyStreamMapping
		expectedError bool
	}{
		{
			name:     "nil existing, nil defaults",
			existing: nil,
			defaults: nil,
			expected: nil,
		},
		{
			name:     "nil existing, with defaults",
			existing: nil,
			defaults: PolicyStreamMapping{
				"policy1": {
					{Priority: 1, Selector: "{app=\"test\"}"},
				},
			},
			expected: PolicyStreamMapping{
				"policy1": {
					{Priority: 1, Selector: "{app=\"test\"}"},
				},
			},
		},
		{
			name: "existing with defaults, no overlap",
			existing: PolicyStreamMapping{
				"policy1": {
					{Priority: 2, Selector: "{app=\"existing\"}"},
				},
			},
			defaults: PolicyStreamMapping{
				"policy2": {
					{Priority: 1, Selector: "{app=\"default\"}"},
				},
			},
			expected: PolicyStreamMapping{
				"policy1": {
					{Priority: 2, Selector: "{app=\"existing\"}"},
				},
				"policy2": {
					{Priority: 1, Selector: "{app=\"default\"}"},
				},
			},
		},
		{
			name: "existing with defaults, with overlap - existing takes precedence",
			existing: PolicyStreamMapping{
				"policy1": {
					{Priority: 2, Selector: "{app=\"existing\"}"},
				},
			},
			defaults: PolicyStreamMapping{
				"policy1": {
					{Priority: 1, Selector: "{app=\"existing\"}"}, // Same selector, different priority
				},
				"policy2": {
					{Priority: 1, Selector: "{app=\"default\"}"},
				},
			},
			expected: PolicyStreamMapping{
				"policy1": {
					{Priority: 2, Selector: "{app=\"existing\"}"}, // Existing priority preserved
				},
				"policy2": {
					{Priority: 1, Selector: "{app=\"default\"}"},
				},
			},
		},
		{
			name: "existing with defaults, merge different selectors",
			existing: PolicyStreamMapping{
				"policy1": {
					{Priority: 2, Selector: "{app=\"existing\"}"},
				},
			},
			defaults: PolicyStreamMapping{
				"policy1": {
					{Priority: 1, Selector: "{app=\"default\"}"}, // Different selector
				},
			},
			expected: PolicyStreamMapping{
				"policy1": {
					{Priority: 2, Selector: "{app=\"existing\"}"},
					{Priority: 1, Selector: "{app=\"default\"}"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of existing for the test
			var existingCopy PolicyStreamMapping
			if tt.existing != nil {
				existingCopy = make(PolicyStreamMapping)
				for k, v := range tt.existing {
					streamsCopy := make([]*PriorityStream, len(v))
					for i, stream := range v {
						streamsCopy[i] = &PriorityStream{
							Priority: stream.Priority,
							Selector: stream.Selector,
							Matchers: stream.Matchers,
						}
					}
					existingCopy[k] = streamsCopy
				}
			}

			// Apply defaults
			err := existingCopy.ApplyDefaultPolicyStreamMappings(tt.defaults)
			if tt.expectedError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Validate the result
			if tt.expected == nil {
				require.Nil(t, existingCopy)
			} else {
				require.NotNil(t, existingCopy)
				require.Equal(t, len(tt.expected), len(existingCopy))

				for policyName, expectedStreams := range tt.expected {
					actualStreams, exists := existingCopy[policyName]
					require.True(t, exists, "Policy %s should exist", policyName)
					require.Equal(t, len(expectedStreams), len(actualStreams))

					// Check each stream
					for i, expectedStream := range expectedStreams {
						require.Less(t, i, len(actualStreams))
						actualStream := actualStreams[i]
						require.Equal(t, expectedStream.Priority, actualStream.Priority)
						require.Equal(t, expectedStream.Selector, actualStream.Selector)
					}
				}
			}
		})
	}
}

func TestPolicyStreamMapping_ApplyDefaultPolicyStreamMappings_Validation(t *testing.T) {
	// Test that validation is called after merging
	existing := PolicyStreamMapping{
		"policy1": {
			{Priority: 2, Selector: "{app=\"existing\"}"},
		},
	}

	defaults := PolicyStreamMapping{
		"policy1": {
			{Priority: 1, Selector: "{app=\"default\"}"},
		},
	}

	// This should not panic and should call Validate()
	err := existing.ApplyDefaultPolicyStreamMappings(defaults)
	require.NoError(t, err)

	// Verify the result is valid
	require.NoError(t, existing.Validate())
}
