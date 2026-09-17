package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLabelPolicyString(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy *LabelPolicy
		want   string
	}{
		{
			name:   "nil policy",
			policy: nil,
			want:   "nil",
		},
		{
			name:   "empty selector",
			policy: &LabelPolicy{},
			want:   "&LabelPolicy{Selector:[]*LabelMatcher{},}",
		},
		{
			name: "single matcher",
			policy: &LabelPolicy{Selector: []*LabelMatcher{
				{Type: LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "dev"},
			}},
			want: "&LabelPolicy{Selector:[]*LabelMatcher{&LabelMatcher{Type:LABEL_MATCHER_TYPE_EQ,Name:env,Value:dev,},},}",
		},
		{
			name: "multiple matchers preserve order and trailing comma",
			policy: &LabelPolicy{Selector: []*LabelMatcher{
				{Type: LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "dev"},
				{Type: LABEL_MATCHER_TYPE_NRE, Name: "class", Value: "secre.*"},
			}},
			want: "&LabelPolicy{Selector:[]*LabelMatcher{&LabelMatcher{Type:LABEL_MATCHER_TYPE_EQ,Name:env,Value:dev,},&LabelMatcher{Type:LABEL_MATCHER_TYPE_NRE,Name:class,Value:secre.*,},},}",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.policy.String())
		})
	}
}
