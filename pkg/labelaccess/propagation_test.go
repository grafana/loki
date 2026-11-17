package labelaccess

import (
	"encoding/json"
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

	policy := &types.LabelPolicy{Selector: []*labels.Matcher{m1, m2}}

	instancePolicyMap["tenant1"] = []*types.LabelPolicy{policy}

	require.Equal(t, "tenant1:[{env=\"dev\",level!=\"info\",},] ", instancePolicyMap.String())

	// Add another tenant with one label matchers
	m3, err := labels.NewMatcher(labels.MatchNotRegexp, "env", "p.*")
	require.Nil(t, err)

	policy2 := &types.LabelPolicy{Selector: []*labels.Matcher{m3}}

	m4, err := labels.NewMatcher(labels.MatchRegexp, "blah", "p.*")
	require.Nil(t, err)

	policy3 := &types.LabelPolicy{Selector: []*labels.Matcher{m4}}

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
	return &types.LabelPolicy{Selector: []*labels.Matcher{m}}
}

// withoutCompiledRegex copies policies, leaving out each matcher's compiled regex. It holds a func,
// which reflect.DeepEqual never considers equal, so policies with regex matchers can't be compared
// otherwise.
func withoutCompiledRegex(policies []*types.LabelPolicy) []*types.LabelPolicy {
	out := make([]*types.LabelPolicy, len(policies))
	for i, p := range policies {
		selector := make([]*labels.Matcher, len(p.Selector))
		for j, m := range p.Selector {
			selector[j] = &labels.Matcher{Type: m.Type, Name: m.Name, Value: m.Value}
		}
		out[i] = &types.LabelPolicy{Selector: selector}
	}
	return out
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
					{Selector: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "env", "prod")}},
				},
			},
		},
		{
			name: "multiple policies are injected and extracted correctly",
			matchers: LabelPolicySet{
				"tenant1": {
					{Selector: []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "env", ".*")}},
					{Selector: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "classification", "secret")}},
				},
			},
		},
		{
			name: "multiple tenants are injected and extracted correctly",
			matchers: LabelPolicySet{
				"tenant1": {
					{Selector: []*labels.Matcher{
						labels.MustNewMatcher(labels.MatchEqual, "env", "dev"),
						labels.MustNewMatcher(labels.MatchEqual, "classification", "secret"),
					}},
					{Selector: []*labels.Matcher{labels.MustNewMatcher(labels.MatchNotEqual, "classification", "secret")}},
				},
				"tenant2": {
					{Selector: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "level", "info")}},
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
				require.ElementsMatch(t, withoutCompiledRegex(expectedPolicies), withoutCompiledRegex(extracted[tenant]))
			}
		})
	}

	t.Run("extracting from empty context returns error", func(t *testing.T) {
		_, err := ExtractLabelMatchersContext(t.Context())
		require.ErrorIs(t, err, errNoMatcherSource)
	})
}

// TestLabelPolicySetStringAndHashGolden pins the output of String and hash. hash is part of the
// results cache key, so a change to either would cause cache misses when rolled out.
func TestLabelPolicySetStringAndHashGolden(t *testing.T) {
	const encoded = `{"tenant1":[{"selector":[{"type":1,"name":"env","value":"dev"},{"type":2,"name":"level","value":"info"}]},{"selector":[{"type":3,"name":"app","value":"api|web"},{"type":4,"name":"class","value":"secre.*"}]}],"tenant2":[{"selector":[{"type":1,"name":"q","value":"a\"b"}]},{}]}`
	var set LabelPolicySet
	require.NoError(t, json.Unmarshal([]byte(encoded), &set))

	require.Equal(t, "tenant1:[{env=\"dev\",level!=\"info\",},{app=~\"api|web\",class!~\"secre.*\",},] tenant2:[{q=\"a\\\"b\",},{},] ", set.String())
	require.Equal(t, "12049742001303896576", set.hash())
	require.Equal(t,
		"&LabelPolicy{Selector:[]*LabelMatcher{&LabelMatcher{Type:LABEL_MATCHER_TYPE_EQ,Name:env,Value:dev,},&LabelMatcher{Type:LABEL_MATCHER_TYPE_NEQ,Name:level,Value:info,},},}",
		set["tenant1"][0].String())
	require.Equal(t,
		"&LabelPolicy{Selector:[]*LabelMatcher{&LabelMatcher{Type:LABEL_MATCHER_TYPE_RE,Name:app,Value:api|web,},&LabelMatcher{Type:LABEL_MATCHER_TYPE_NRE,Name:class,Value:secre.*,},},}",
		set["tenant1"][1].String())

	reencoded, err := json.Marshal(set)
	require.NoError(t, err)
	require.Equal(t, encoded, string(reencoded))
}
