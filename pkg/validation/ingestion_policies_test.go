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
