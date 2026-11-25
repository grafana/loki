package labelaccess

import (
	"context"
	"testing"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/labelaccess/types"
)

func TestChunkFiltererShouldFilter(t *testing.T) {
	tests := []struct {
		name     string
		policies []*types.LabelPolicy
		metric   labels.Labels
		want     bool
	}{
		{
			"no selectors",
			[]*types.LabelPolicy{},
			labels.EmptyLabels(),
			false,
		},
		{
			"matching selector with 1 matcher",
			[]*types.LabelPolicy{
				{
					Selector: []*labels.Matcher{
						labels.MustNewMatcher(labels.MatchEqual, "env", "dev"),
					},
				},
			},
			labels.FromStrings("host", "host1", "env", "dev"),
			false,
		},
		{
			"matching selector with 1 negative matcher",
			[]*types.LabelPolicy{
				{
					Selector: []*labels.Matcher{
						labels.MustNewMatcher(labels.MatchNotEqual, "env", "dev"),
					},
				},
			},
			labels.FromStrings("host", "host1", "env", "dev"),
			true,
		},
		{
			"non matching selector with multiple matchers",
			[]*types.LabelPolicy{
				{
					Selector: []*labels.Matcher{
						labels.MustNewMatcher(labels.MatchEqual, "env", "dev"),
						labels.MustNewMatcher(labels.MatchEqual, "host", "host2"),
					},
				},
			},
			labels.FromStrings("host", "host1", "env", "dev"),
			true,
		},
		{
			"one of multiple selectors matching",
			[]*types.LabelPolicy{
				{
					Selector: []*labels.Matcher{
						labels.MustNewMatcher(labels.MatchNotEqual, "env", "prod"),
					},
				},
				{
					Selector: []*labels.Matcher{
						labels.MustNewMatcher(labels.MatchEqual, "env", "dev"),
						labels.MustNewMatcher(labels.MatchEqual, "host", "host1"),
					},
				},
			},
			labels.FromStrings("host", "host3", "env", "dev"),
			false,
		},
		{
			"non matching selector",
			[]*types.LabelPolicy{
				{
					Selector: []*labels.Matcher{
						labels.MustNewMatcher(labels.MatchEqual, "env", "dev"),
					},
				},
			},
			labels.FromStrings("host", "host1", "env", "prod"),
			true,
		},
		{
			"regex selector with no matching labels",
			[]*types.LabelPolicy{
				{
					Selector: []*labels.Matcher{
						labels.MustNewMatcher(labels.MatchRegexp, "environment", "dev.*"),
					},
				},
			},
			labels.FromStrings("host", "host1", "env", "prod"),
			true,
		},
		{
			"negative regex selector without matching labels",
			[]*types.LabelPolicy{
				{
					Selector: []*labels.Matcher{
						labels.MustNewMatcher(labels.MatchNotRegexp, "environment", "dev.*"),
					},
				},
			},
			labels.FromStrings("host", "host1", "env", "prod"),
			false,
		},
		{
			"selector with no matchers",
			[]*types.LabelPolicy{
				{Selector: []*labels.Matcher{}},
			},
			labels.FromStrings("host", "host1", "env", "prod"),
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ChunkFilterer{
				policies: tt.policies,
			}
			if got := c.ShouldFilter(tt.metric); got != tt.want {
				t.Errorf("ChunkFilterer.ShouldFilter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func BenchmarkShouldFilter(b *testing.B) {
	lps := make(LabelPolicySet)
	lps["tenant"] = []*types.LabelPolicy{
		{
			Selector: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchNotRegexp, "environment", "dev.*"),
			},
		},
	}

	ctx := context.Background()
	ctx = InjectLabelMatchersContext(ctx, lps)
	ctx = user.InjectOrgID(ctx, "tenant")

	rcf := RequestChunkFilterer{}
	cf := rcf.ForRequest(ctx)

	lbls := labels.FromStrings("host", "host1", "env", "prod")

	for n := 0; n < b.N; n++ {
		_ = cf.ShouldFilter(lbls)
	}
}

func TestChunkFilterer_RequiredLabelNames(t *testing.T) {
	tests := []struct {
		name     string
		policies []*types.LabelPolicy
		metric   labels.Labels
		want     []string
	}{
		{
			"no selectors",
			[]*types.LabelPolicy{},
			labels.EmptyLabels(),
			nil,
		},
		{
			"policy with one selector",
			[]*types.LabelPolicy{
				{
					Selector: []*labels.Matcher{
						labels.MustNewMatcher(labels.MatchEqual, "env", "dev"),
					},
				},
			},
			labels.FromStrings("host", "host1", "env", "dev"),
			[]string{"env"},
		},
		{
			"policy with multiple selectors",
			[]*types.LabelPolicy{
				{
					Selector: []*labels.Matcher{
						labels.MustNewMatcher(labels.MatchEqual, "env", "dev"),
						labels.MustNewMatcher(labels.MatchEqual, "host", "host2"),
					},
				},
			},
			labels.FromStrings("host", "host1", "env", "dev"),
			[]string{"env", "host"},
		},
		{
			"multiple policies",
			[]*types.LabelPolicy{
				{
					Selector: []*labels.Matcher{
						labels.MustNewMatcher(labels.MatchNotEqual, "env", "prod"),
					},
				},
				{
					Selector: []*labels.Matcher{
						labels.MustNewMatcher(labels.MatchEqual, "env", "dev"),
						labels.MustNewMatcher(labels.MatchEqual, "host", "host1"),
					},
				},
			},
			labels.FromStrings("host", "host3", "env", "dev"),
			[]string{"env", "host"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ChunkFilterer{
				policies: tt.policies,
			}
			got := c.RequiredLabelNames()
			require.ElementsMatch(t, got, tt.want)
		})
	}
}

func TestChunkFilterer_PushableLabelMatchers(t *testing.T) {
	policy := func(matchers ...*labels.Matcher) *types.LabelPolicy {
		return &types.LabelPolicy{Selector: matchers}
	}
	envDev := labels.MustNewMatcher(labels.MatchEqual, "env", "dev")
	secretNotTrue := labels.MustNewMatcher(labels.MatchNotEqual, "secret", "true")
	appRegexp := labels.MustNewMatcher(labels.MatchRegexp, "app", "a|d")
	teamAny := labels.MustNewMatcher(labels.MatchRegexp, "team", ".*")

	testCases := []struct {
		name     string
		policies []*types.LabelPolicy
		want     []string
	}{
		{
			name:     "returns no matcher when the request has no policy",
			policies: nil,
		},
		{
			name:     "returns the matchers of the only policy",
			policies: []*types.LabelPolicy{policy(envDev, secretNotTrue, appRegexp)},
			want:     []string{`env="dev"`, `secret!="true"`, `app=~"a|d"`},
		},
		{
			name:     "returns no matcher when the request has several policies",
			policies: []*types.LabelPolicy{policy(envDev), policy(secretNotTrue)},
		},
		{
			name:     "returns a matcher that matches every value",
			policies: []*types.LabelPolicy{policy(teamAny, envDev)},
			want:     []string{`team=~".*"`, `env="dev"`},
		},
		{
			name:     "returns no matcher when the only policy has no matcher",
			policies: []*types.LabelPolicy{policy()},
		},
	}

	streams := []labels.Labels{
		labels.FromStrings("app", "a", "env", "dev"),
		labels.FromStrings("app", "b", "env", "dev", "secret", "true"),
		labels.FromStrings("app", "d", "env", "prod"),
		labels.FromStrings("app", "d", "team", "payments"),
		labels.FromStrings("env", "dev", "team", "payments"),
		labels.EmptyLabels(),
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			filterer := &ChunkFilterer{policies: testCase.policies}

			matchers := filterer.PushableLabelMatchers()
			var got []string
			for _, matcher := range matchers {
				got = append(got, matcher.String())
			}
			require.Equal(t, testCase.want, got)

			for _, stream := range streams {
				if filterer.ShouldFilter(stream) {
					continue
				}
				for _, matcher := range matchers {
					require.True(t, matcher.Matches(stream.Get(matcher.Name)), "the filter admits %s, but it does not match %s", stream, matcher)
				}
			}
		})
	}
}

// TestRequestChunkFilterer_ForRequest tests that ForRequest returns a proper nil interface
// when no LBAC policies are present, and a non-nil filterer when policies exist.
// The nil interface check is critical because a typed nil (*ChunkFilterer)(nil)
// wrapped in an interface would cause `filterer != nil` checks to pass,
// leading to nil pointer dereferences.
func TestRequestChunkFilterer_ForRequest(t *testing.T) {
	for _, tc := range []struct {
		name      string
		policies  LabelPolicySet
		expectNil bool
	}{
		{
			name:      "no LBAC policies returns nil interface",
			policies:  nil,
			expectNil: true,
		},
		{
			name:      "empty LBAC policies returns nil interface",
			policies:  LabelPolicySet{},
			expectNil: true,
		},
		{
			name: "with LBAC policies returns non-nil filterer",
			policies: LabelPolicySet{
				"test-tenant": {
					{Selector: []*labels.Matcher{
						labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
					}},
				},
			},
			expectNil: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &RequestChunkFilterer{}

			ctx := t.Context()
			if tc.policies != nil {
				ctx = InjectLabelMatchersContext(ctx, tc.policies)
			}
			ctx = user.InjectOrgID(ctx, "test-tenant")

			filterer := r.ForRequest(ctx)

			if tc.expectNil {
				// The interface itself must be nil, not just the underlying value.
				// If we returned a typed nil, `filterer != nil` would be true
				// but filterer.ShouldFilter() would panic.
				require.True(t, filterer == nil, "ForRequest should return nil interface")
			} else {
				require.True(t, filterer != nil, "ForRequest should return non-nil filterer")
			}
		})
	}
}
