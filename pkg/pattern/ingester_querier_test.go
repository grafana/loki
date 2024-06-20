package pattern

import (
	"bufio"
	"context"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices"

	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/pattern/metric"
)

func Test_prunePatterns(t *testing.T) {
	file, err := os.Open(`testdata/patterns.txt`)
	require.NoError(t, err)
	defer file.Close()

	resp := new(logproto.QueryPatternsResponse)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		resp.Series = append(resp.Series, logproto.NewPatternSeries(scanner.Text(), []*logproto.PatternSample{}))
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
		patterns = append(patterns, p.GetPattern())
	}
	slices.Sort(patterns)

	require.Equal(t, expectedPatterns, patterns)
	require.Less(t, len(patterns), startingPatterns, "prunePatterns should remove duplicates")
}

func Test_Samples(t *testing.T) {
	t.Run("it rejects metric queries with filters", func(t *testing.T) {
		q := &IngesterQuerier{
			cfg: Config{
				MetricAggregation: metric.AggregationConfig{
					Enabled: true,
				},
			},
			logger:     log.NewNopLogger(),
			ringClient: &fakeRingClient{},
			registerer: nil,
		}
		for _, query := range []string{
			`count_over_time({foo="bar"} |= "baz" [5m])`,
			`count_over_time({foo="bar"} != "baz" [5m])`,
			`count_over_time({foo="bar"} |~ "baz" [5m])`,
			`count_over_time({foo="bar"} !~ "baz" [5m])`,
			`count_over_time({foo="bar"} | logfmt | color="blue" [5m])`,
			`sum(count_over_time({foo="bar"} |= "baz" [5m]))`,
			`sum by (label)(count_over_time({foo="bar"} |= "baz" [5m]))`,
			`bytes_over_time({foo="bar"} |= "baz" [5m])`,
		} {
			_, err := q.Samples(
				context.Background(),
				&logproto.QuerySamplesRequest{
					Query: query,
				},
			)
			require.Error(t, err, query)
			require.ErrorIs(t, err, ErrParseQuery, query)
		}
	})

	t.Run("it rejects log selector queries", func(t *testing.T) {
		q := &IngesterQuerier{
			cfg: Config{
				MetricAggregation: metric.AggregationConfig{
					Enabled: true,
				},
			},
			logger:     log.NewNopLogger(),
			ringClient: &fakeRingClient{},
			registerer: nil,
		}
		for _, query := range []string{
			`{foo="bar"}`,
		} {
			_, err := q.Samples(
				context.Background(),
				&logproto.QuerySamplesRequest{
					Query: query,
				},
			)
			require.Error(t, err, query)
			require.Equal(t, "only sample expression supported", err.Error(), query)
		}
	})

	t.Run("accepts count and bytes metric queries", func(t *testing.T) {
		q := &IngesterQuerier{
			cfg: Config{
				MetricAggregation: metric.AggregationConfig{
					Enabled: true,
				},
			},
			logger:     log.NewNopLogger(),
			ringClient: &fakeRingClient{},
			registerer: nil,
		}
		for _, query := range []string{
			`count_over_time({foo="bar"}[5m])`,
			`bytes_over_time({foo="bar"}[5m])`,
			`sum(count_over_time({foo="bar"}[5m]))`,
			`sum(bytes_over_time({foo="bar"}[5m]))`,
			`sum by (level)(count_over_time({foo="bar"}[5m]))`,
			`sum by (level)(bytes_over_time({foo="bar"}[5m]))`,
		} {
			_, err := q.Samples(
				context.Background(),
				&logproto.QuerySamplesRequest{
					Query: query,
				},
			)
			require.NoError(t, err, query)
		}
	})
}

type fakeRingClient struct{}

func (f *fakeRingClient) Pool() *ring_client.Pool {
	panic("not implemented")
}

func (f *fakeRingClient) StartAsync(_ context.Context) error {
	panic("not implemented")
}

func (f *fakeRingClient) AwaitRunning(_ context.Context) error {
	panic("not implemented")
}

func (f *fakeRingClient) StopAsync() {
	panic("not implemented")
}

func (f *fakeRingClient) AwaitTerminated(_ context.Context) error {
	panic("not implemented")
}

func (f *fakeRingClient) FailureCase() error {
	panic("not implemented")
}

func (f *fakeRingClient) State() services.State {
	panic("not implemented")
}

func (f *fakeRingClient) AddListener(_ services.Listener) {
	panic("not implemented")
}

func (f *fakeRingClient) Ring() ring.ReadRing {
	return &fakeRing{}
}

type fakeRing struct{}

// InstancesWithTokensCount returns the number of instances in the ring that have tokens.
func (f *fakeRing) InstancesWithTokensCount() int {
	panic("not implemented") // TODO: Implement
}

// InstancesInZoneCount returns the number of instances in the ring that are registered in given zone.
func (f *fakeRing) InstancesInZoneCount(_ string) int {
	panic("not implemented") // TODO: Implement
}

// InstancesWithTokensInZoneCount returns the number of instances in the ring that are registered in given zone and have tokens.
func (f *fakeRing) InstancesWithTokensInZoneCount(_ string) int {
	panic("not implemented") // TODO: Implement
}

// ZonesCount returns the number of zones for which there's at least 1 instance registered in the ring.
func (f *fakeRing) ZonesCount() int {
	panic("not implemented") // TODO: Implement
}

func (f *fakeRing) Get(
	_ uint32,
	_ ring.Operation,
	_ []ring.InstanceDesc,
	_ []string,
	_ []string,
) (ring.ReplicationSet, error) {
	panic("not implemented")
}

func (f *fakeRing) GetAllHealthy(_ ring.Operation) (ring.ReplicationSet, error) {
	panic("not implemented")
}

func (f *fakeRing) GetReplicationSetForOperation(_ ring.Operation) (ring.ReplicationSet, error) {
	return ring.ReplicationSet{}, nil
}

func (f *fakeRing) ReplicationFactor() int {
	panic("not implemented")
}

func (f *fakeRing) InstancesCount() int {
	panic("not implemented")
}

func (f *fakeRing) ShuffleShard(_ string, _ int) ring.ReadRing {
	panic("not implemented")
}

func (f *fakeRing) GetInstanceState(_ string) (ring.InstanceState, error) {
	panic("not implemented")
}

func (f *fakeRing) ShuffleShardWithLookback(
	_ string,
	_ int,
	_ time.Duration,
	_ time.Time,
) ring.ReadRing {
	panic("not implemented")
}

func (f *fakeRing) HasInstance(_ string) bool {
	panic("not implemented")
}

func (f *fakeRing) CleanupShuffleShardCache(_ string) {
	panic("not implemented")
}

func (f *fakeRing) GetTokenRangesForInstance(_ string) (ring.TokenRanges, error) {
	panic("not implemented")
}
