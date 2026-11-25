package labelaccess

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/grafana/loki/v3/pkg/labelaccess/types"
)

const (
	tenantID  = "tenant"
	numSeries = 1000
	// numExcluded is how many foo values benchExcludePolicy excludes. Series spread over twice
	// as many, so half of them are excluded.
	numExcluded = 100
)

func benchSeries() []labels.Labels {
	series := make([]labels.Labels, 0, numSeries)
	for i := 0; i < numSeries; i++ {
		series = append(series, labels.FromStrings(
			"app", fmt.Sprintf("app-%d", i%7),
			"env", "prod",
			"foo", fmt.Sprintf("foo-%03d", i%(2*numExcluded)),
			"pod", fmt.Sprintf("app-%d-%08x", i%7, i),
			"stream", "stdout",
		))
	}
	return series
}

// benchExcludePolicy returns a policy with a single negative regex matcher {foo!~"foo-000|foo-001|...|foo-099"}.
func benchExcludePolicy() []*types.LabelPolicy {
	values := make([]string, 0, numExcluded)
	for j := 0; j < numExcluded; j++ {
		values = append(values, fmt.Sprintf("foo-%03d", j))
	}
	return []*types.LabelPolicy{{
		Selector: []*types.LabelMatcher{
			{Type: types.LABEL_MATCHER_TYPE_NRE, Name: "foo", Value: strings.Join(values, "|")},
		},
	}}
}

func BenchmarkChunkFilterer_ShouldFilterSeries(b *testing.B) {
	series := benchSeries()
	ctx := user.InjectOrgID(context.Background(), tenantID)
	ctx = InjectLabelMatchersContext(ctx, LabelPolicySet{tenantID: benchExcludePolicy()})
	cf := (&RequestChunkFilterer{}).ForRequest(ctx)
	require.NotNil(b, cf)

	b.ReportAllocs()
	for b.Loop() {
		for _, s := range series {
			_ = cf.ShouldFilter(s)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*numSeries), "ns/series")
}

// benchPolicies returns numPolicies policies, each with numMatchers label matchers that cycle
// through equality, negative equality, regex and negative regex. The regex values are
// alternations of regexValues values, as access policies commonly allow a list of values.
func benchPolicies(numPolicies, numMatchers, regexValues int) []*types.LabelPolicy {
	matcherTypes := []types.LabelMatcherType{
		types.LABEL_MATCHER_TYPE_EQ,
		types.LABEL_MATCHER_TYPE_RE,
		types.LABEL_MATCHER_TYPE_NEQ,
		types.LABEL_MATCHER_TYPE_NRE,
	}
	policies := make([]*types.LabelPolicy, 0, numPolicies)
	for p := 0; p < numPolicies; p++ {
		selector := make([]*types.LabelMatcher, 0, numMatchers)
		for m := 0; m < numMatchers; m++ {
			typ := matcherTypes[m%len(matcherTypes)]
			value := fmt.Sprintf("value-%d-%d", p, m)
			if typ == types.LABEL_MATCHER_TYPE_RE || typ == types.LABEL_MATCHER_TYPE_NRE {
				values := make([]string, 0, regexValues)
				for v := 0; v < regexValues; v++ {
					values = append(values, fmt.Sprintf("value-%d-%d-%d", p, m, v))
				}
				value = strings.Join(values, "|")
			}
			selector = append(selector, &types.LabelMatcher{Type: typ, Name: fmt.Sprintf("label_%d", m), Value: value})
		}
		policies = append(policies, &types.LabelPolicy{Selector: selector})
	}
	return policies
}

// benchServerContext returns a context as ServerLBACInterceptor hands it to a gRPC handler: the
// policies arrive as gRPC metadata and are re-injected as the label policy header.
func benchServerContext(b *testing.B, policies []*types.LabelPolicy) context.Context {
	b.Helper()
	encoded := make([]string, 0, len(policies))
	for _, p := range policies {
		v, err := policyToHeaderValue(tenantID, p)
		require.NoError(b, err)
		encoded = append(encoded, v)
	}
	ctx := user.InjectOrgID(context.Background(), tenantID)
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(lowerLBACHeaderName, strings.Join(encoded, ",")))
	ctx, err := extractFromGRPCRequest(ctx)
	require.NoError(b, err)
	return ctx
}

// BenchmarkRequestChunkFilterer_ForRequest measures one ForRequest call. The index gateway makes
// one such call for every TSDB index file a request reads, so a request makes many of them.
func BenchmarkRequestChunkFilterer_ForRequest(b *testing.B) {
	for _, tc := range []struct {
		name                                  string
		numPolicies, numMatchers, regexValues int
	}{
		{name: "small", numPolicies: 1, numMatchers: 1, regexValues: 1},
		{name: "medium", numPolicies: 3, numMatchers: 3, regexValues: 5},
		{name: "large", numPolicies: 10, numMatchers: 5, regexValues: 20},
	} {
		b.Run(tc.name, func(b *testing.B) {
			ctx := benchServerContext(b, benchPolicies(tc.numPolicies, tc.numMatchers, tc.regexValues))
			filterer := &RequestChunkFilterer{}
			require.NotNil(b, filterer.ForRequest(ctx))

			b.ReportAllocs()
			for b.Loop() {
				_ = filterer.ForRequest(ctx)
			}
		})
	}
}
