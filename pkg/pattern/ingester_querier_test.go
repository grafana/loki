package pattern

import (
	"bufio"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func Test_prunePatterns(t *testing.T) {
	file, err := os.Open("testdata/patterns.txt")
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
	prunePatterns(resp, 0)

	expectedPatterns := []string{
		`<_> caller=wrapper.go:48 level=info component=distributor msg="sample remote write" eventType=bi <_>`,
		`<_> caller=offset_committer.go:174 level=info msg="partition offset committer committed offset" topic=cortex-dev-01-aggregations <_> +0000 UTC" <_> +0000 UTC" <_> currentBuckets="unsupported value type"`,
		`<_> caller=aggregator.go:139 level=info msg="received kafka message" topic=cortex-dev-01-aggregations <_>`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" result="user=9960, <_> sampleTimestamp=2024-04-03 <_> +0000 UTC, <_>`,
		`<_> caller=offset_committer.go:174 level=info msg="partition offset committer committed offset" topic=cortex-dev-01-aggregations <_> handledMessageTime="2024-04-03 <_> +0000 UTC" <_> +0000 UTC" <_> currentBuckets="unsupported value type"`,
		`<_> caller=batcher.go:155 level=info msg="batcher: processing aggregation result" <_> +0000 UTC, <_>`,
	}

	patterns := make([]string, 0, len(resp.Series))
	for _, p := range resp.Series {
		patterns = append(patterns, p.Pattern)
	}

	require.Equal(t, expectedPatterns, patterns)
}
