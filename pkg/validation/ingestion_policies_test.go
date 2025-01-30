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
				Priority: 1,
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
				Priority: 1,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchRegexp, "qab", "qzx.*"),
				},
			},
		},
	}

	require.Equal(t, "policy1", mapping.PolicyFor(labels.FromStrings("foo", "bar", "env", "prod")))
	// matches both policy2 and policy1 but policy1 has higher priority.
	require.Equal(t, "policy1", mapping.PolicyFor(labels.FromStrings("foo", "bar", "daz", "baz", "env", "prod")))
	// matches policy3 and policy4 but policy3 appears first.
	require.Equal(t, "policy3", mapping.PolicyFor(labels.FromStrings("qyx", "qzx", "qox", "qox")))
	// matches no policy.
	require.Equal(t, "", mapping.PolicyFor(labels.FromStrings("foo", "fooz", "daz", "qux", "quux", "corge", "env", "prod")))
	// matches policy5 through regex.
	require.Equal(t, "policy5", mapping.PolicyFor(labels.FromStrings("qab", "qzxqox")))
}
