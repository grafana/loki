package labelaccess

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

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
		Selector: []*labels.Matcher{
			labels.MustNewMatcher(labels.MatchNotRegexp, "foo", strings.Join(values, "|")),
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
