package pattern

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/pattern/metric"
)

func TestPrunePatterns(t *testing.T) {
	metrics := newIngesterQuerierMetrics(prometheus.NewRegistry(), "test")
	testCases := []struct {
		name             string
		inputSeries      []*logproto.PatternSeries
		minClusterSize   int64
		expectedSeries   []*logproto.PatternSeries
		expectedPruned   int
		expectedRetained int
	}{
		{
			name: "No pruning needed",
			inputSeries: []*logproto.PatternSeries{
				{Pattern: `{app="test1"}`, Samples: []*logproto.PatternSample{{Value: 40}}},
				{Pattern: `{app="test2"}`, Samples: []*logproto.PatternSample{{Value: 35}}},
			},
			minClusterSize: 20,
			expectedSeries: []*logproto.PatternSeries{
				{Pattern: `{app="test1"}`, Samples: []*logproto.PatternSample{{Value: 40}}},
				{Pattern: `{app="test2"}`, Samples: []*logproto.PatternSample{{Value: 35}}},
			},
			expectedPruned:   0,
			expectedRetained: 2,
		},
		{
			name: "Pruning some patterns",
			inputSeries: []*logproto.PatternSeries{
				{Pattern: `{app="test1"}`, Samples: []*logproto.PatternSample{{Value: 10}}},
				{Pattern: `{app="test2"}`, Samples: []*logproto.PatternSample{{Value: 5}}},
				{Pattern: `{app="test3"}`, Samples: []*logproto.PatternSample{{Value: 50}}},
			},
			minClusterSize: 20,
			expectedSeries: []*logproto.PatternSeries{
				{Pattern: `{app="test3"}`, Samples: []*logproto.PatternSample{{Value: 50}}},
			},
			expectedPruned:   2,
			expectedRetained: 1,
		},
		{
			name: "Limit patterns to maxPatterns",
			inputSeries: func() []*logproto.PatternSeries {
				series := make([]*logproto.PatternSeries, maxPatterns+10)
				for i := 0; i < maxPatterns+10; i++ {
					series[i] = &logproto.PatternSeries{
						Pattern: `{app="test"}`,
						Samples: []*logproto.PatternSample{{Value: int64(maxPatterns + 10 - i)}},
					}
				}
				return series
			}(),
			minClusterSize: 0,
			expectedSeries: func() []*logproto.PatternSeries {
				series := make([]*logproto.PatternSeries, maxPatterns)
				for i := 0; i < maxPatterns; i++ {
					series[i] = &logproto.PatternSeries{
						Pattern: `{app="test"}`,
						Samples: []*logproto.PatternSample{{Value: int64(maxPatterns + 10 - i)}},
					}
				}
				return series
			}(),
			expectedPruned:   10,
			expectedRetained: maxPatterns,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &logproto.QueryPatternsResponse{
				Series: tc.inputSeries,
			}
			result := prunePatterns(resp, tc.minClusterSize, metrics)

			require.Equal(t, len(tc.expectedSeries), len(result.Series))
			require.Equal(t, tc.expectedSeries, result.Series)
		})
	}
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
	return ring.ReplicationSet{}, nil
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
