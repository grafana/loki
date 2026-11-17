package types

import (
	"encoding/json"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
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
			policy: &LabelPolicy{Selector: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "env", "dev"),
			}},
			want: "&LabelPolicy{Selector:[]*LabelMatcher{&LabelMatcher{Type:LABEL_MATCHER_TYPE_EQ,Name:env,Value:dev,},},}",
		},
		{
			name: "multiple matchers preserve order and trailing comma",
			policy: &LabelPolicy{Selector: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "env", "dev"),
				labels.MustNewMatcher(labels.MatchNotRegexp, "class", "secre.*"),
			}},
			want: "&LabelPolicy{Selector:[]*LabelMatcher{&LabelMatcher{Type:LABEL_MATCHER_TYPE_EQ,Name:env,Value:dev,},&LabelMatcher{Type:LABEL_MATCHER_TYPE_NRE,Name:class,Value:secre.*,},},}",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.policy.String())
		})
	}
}

// goldenLabelPolicyJSON is how a policy was encoded before LabelPolicy held compiled matchers. Queriers
// and frontends of different versions exchange this encoding during a rollout, so it must not change.
const goldenLabelPolicyJSON = `{"selector":[{"type":1,"name":"env","value":"dev"},{"type":2,"name":"level","value":"info"},{"type":3,"name":"app","value":"api|web"},{"type":4,"name":"class","value":"secre.*"},{"type":1,"name":"q","value":"a\"b"}]}`

func TestLabelPolicyJSONRoundTrip(t *testing.T) {
	var policy LabelPolicy
	require.NoError(t, json.Unmarshal([]byte(goldenLabelPolicyJSON), &policy))
	require.Len(t, policy.Selector, 5)

	encoded, err := json.Marshal(&policy)
	require.NoError(t, err)
	require.JSONEq(t, goldenLabelPolicyJSON, string(encoded))
	require.Equal(t, goldenLabelPolicyJSON, string(encoded))

	encoded, err = json.Marshal(LabelPolicy{})
	require.NoError(t, err)
	require.Equal(t, `{}`, string(encoded))
}

func TestLabelPolicyUnmarshalJSONRejectsInvalidMatchers(t *testing.T) {
	for _, tc := range []struct {
		name string
		json string
	}{
		{name: "unspecified match type", json: `{"selector":[{"name":"env","value":"dev"}]}`},
		{name: "unknown match type", json: `{"selector":[{"type":9,"name":"env","value":"dev"}]}`},
		{name: "invalid regex", json: `{"selector":[{"type":3,"name":"env","value":"("}]}`},
		{name: "invalid negated regex", json: `{"selector":[{"type":4,"name":"env","value":"("}]}`},
		{name: "null matcher", json: `{"selector":[null]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var policy LabelPolicy
			require.Error(t, json.Unmarshal([]byte(tc.json), &policy))
		})
	}
}

func TestLabelPolicyMarshalJSONRejectsNilMatcher(t *testing.T) {
	_, err := json.Marshal(&LabelPolicy{Selector: []*labels.Matcher{nil}})
	require.Error(t, err)
}
