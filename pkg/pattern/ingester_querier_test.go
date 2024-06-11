package pattern

import (
	"bufio"
	"os"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func Test_prunePatterns(t *testing.T) {
	file, err := os.Open(`testdata/patterns.txt`)
	require.NoError(t, err)
	defer file.Close()

	resp := new(logproto.QueryPatternsResponse)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		resp.Series = append(resp.Series, &logproto.PatternSeries{
			Pattern: scanner.Text(),
		})
	}
	require.NoError(t, scanner.Err())

	startingPatterns := len(resp.Series)
	prunePatterns(resp, 0, newIngesterQuerierMetrics(prometheus.DefaultRegisterer, "test"))

	expectedPatterns := []string{
		`<_> caller=aggregator.go:139 level=info msg="received kafka message" topic=cortex-dev-01-aggregations partition=0 <_> <_>`,
		`<_> caller=aggregator.go:139 level=info msg="received kafka message" topic=cortex-dev-01-aggregations partition=1 <_> <_>`,
		`<_> caller=aggregator.go:139 level=info msg="received kafka message" topic=cortex-dev-01-aggregations partition=2 <_> <_>`,
		`<_> caller=aggregator.go:139 level=info msg="received kafka message" topic=cortex-dev-01-aggregations partition=3 <_> <_>`,
		`<_> caller=aggregator.go:139 level=info msg="received kafka message" topic=cortex-dev-01-aggregations partition=4 <_> <_>`,
		`<_> caller=aggregator.go:139 level=info msg="received kafka message" topic=cortex-dev-01-aggregations partition=5 <_> <_>`,
		`<_> caller=aggregator.go:139 level=info msg="received kafka message" topic=cortex-dev-01-aggregations partition=6 <_> <_>`,
		`<_> caller=aggregator.go:139 level=info msg="received kafka message" topic=cortex-dev-01-aggregations partition=7 <_> <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" <_> partitionID=0, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" <_> partitionID=0, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" <_> partitionID=7, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=0, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=0, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=0, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=0, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=0, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=1, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=1, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=1, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=1, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=1, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=1, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=1, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=1, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=2, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=2, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=2, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=2, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=2, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=2, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=2, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=2, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=3, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=3, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=3, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> sampleTimestamp=2024-04-03 <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=3, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> sampleTimestamp=2024-04-03 <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=3, <_> <_> <_> <_> <_> <_> <_> <_> <_> sampleTimestamp=2024-04-03 <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=4, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=4, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=4, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> sampleTimestamp=2024-04-03 <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=4, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> sampleTimestamp=2024-04-03 <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=4, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> sampleTimestamp=2024-04-03 <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=4, <_> <_> <_> <_> <_> <_> <_> <_> <_> sampleTimestamp=2024-04-03 <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=4, <_> <_> <_> <_> <_> <_> <_> <_> sampleTimestamp=2024-04-03 <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=5, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=5, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=5, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> sampleTimestamp=2024-04-03 <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=5, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> sampleTimestamp=2024-04-03 <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=5, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> sampleTimestamp=2024-04-03 <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=5, <_> <_> <_> <_> <_> <_> <_> <_> <_> sampleTimestamp=2024-04-03 <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=6, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=6, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=6, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=6, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=6, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=6, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=6, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=6, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=7, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=7, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=7, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=7, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=7, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=7, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, partitionID=7, <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> <_> +0000 UTC, <_>`,
		`<_> caller=offset_committer.go:174 level=info msg="partition offset committer committed offset" topic=cortex-dev-01-aggregations partition=0 <_> <_> +0000 UTC" <_> <_> +0000 UTC" <_> currentBuckets="unsupported value type"`,
		`<_> caller=offset_committer.go:174 level=info msg="partition offset committer committed offset" topic=cortex-dev-01-aggregations partition=1 <_> <_> +0000 UTC" <_> <_> +0000 UTC" <_> currentBuckets="unsupported value type"`,
		`<_> caller=offset_committer.go:174 level=info msg="partition offset committer committed offset" topic=cortex-dev-01-aggregations partition=2 <_> <_> +0000 UTC" <_> <_> +0000 UTC" <_> currentBuckets="unsupported value type"`,
		`<_> caller=offset_committer.go:174 level=info msg="partition offset committer committed offset" topic=cortex-dev-01-aggregations partition=3 handledMessageTime="2024-04-03 <_> +0000 UTC" <_> <_> +0000 UTC" <_> currentBuckets="unsupported value type"`,
		`<_> caller=offset_committer.go:174 level=info msg="partition offset committer committed offset" topic=cortex-dev-01-aggregations partition=4 handledMessageTime="2024-04-03 <_> +0000 UTC" <_> <_> +0000 UTC" <_> currentBuckets="unsupported value type"`,
		`<_> caller=offset_committer.go:174 level=info msg="partition offset committer committed offset" topic=cortex-dev-01-aggregations partition=5 handledMessageTime="2024-04-03 <_> +0000 UTC" <_> <_> +0000 UTC" <_> currentBuckets="unsupported value type"`,
		`<_> caller=offset_committer.go:174 level=info msg="partition offset committer committed offset" topic=cortex-dev-01-aggregations partition=6 <_> <_> +0000 UTC" <_> <_> +0000 UTC" <_> currentBuckets="unsupported value type"`,
		`<_> caller=offset_committer.go:174 level=info msg="partition offset committer committed offset" topic=cortex-dev-01-aggregations partition=7 <_> <_> +0000 UTC" <_> <_> +0000 UTC" <_> currentBuckets="unsupported value type"`,
		`<_> caller=wrapper.go:48 level=info component=distributor msg="sample remote write" eventType=bi <_> <_> <_>`,
	}

	patterns := make([]string, 0, len(resp.Series))
	for _, p := range resp.Series {
		patterns = append(patterns, p.Pattern)
	}
	slices.Sort(patterns)

	require.Equal(t, expectedPatterns, patterns)
	require.Less(t, len(patterns), startingPatterns, "prunePatterns should remove duplicates")
}
