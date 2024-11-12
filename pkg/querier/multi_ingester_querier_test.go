package querier

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func buildMultiIngesterConfig() MultiIngesterConfig {
	mic := MultiIngesterConfig{
		MultiIngesterConfig: []ring.LifecyclerConfig{
			{
				RingConfig: ring.Config{
					KVStore: kv.Config{
						Store:  "consul",
						Prefix: "collectors/",
						StoreConfig: kv.StoreConfig{
							Consul: consul.Config{
								Host:              "127.0.0.1:8500",
								HTTPClientTimeout: time.Second * 20,
								ConsistentReads:   true,
							},
						},
					},
					ReplicationFactor: 1,
				},
				FinalSleep: 0,
			}, {
				RingConfig: ring.Config{
					KVStore: kv.Config{
						Store:  "consul",
						Prefix: "collectors/",
						StoreConfig: kv.StoreConfig{
							Consul: consul.Config{
								Host:              "127.0.0.1:8501",
								HTTPClientTimeout: time.Second * 20,
								ConsistentReads:   true,
							},
						},
					},
					ReplicationFactor: 1,
				},
				FinalSleep: 0,
			},
		},
	}
	return mic
}

func TestLabels(t *testing.T) {
	mic := buildMultiIngesterConfig()
	cc := client.Config{}
	iq, err := NewMultiIngesterQuerier(mic, cc, 0, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	bs := services.NewIdleService(func(ctx context.Context) error {
		return services.StartManagerAndAwaitHealthy(ctx, iq.(*MultiIngesterQuerier).GetSubServices())
	}, func(failureCase error) error {
		return nil
	})
	bs.StartAsync(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	now := time.Now()
	before := now.Add(-30 * time.Minute)
	req := &logproto.LabelRequest{
		Start: &before,
		End:   &now,
	}
	labels, err := iq.Label(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("labels: %+v\n", labels)
}

// MockIngesterQuerier is a mock implementation of the IIngesterQuerier interface
type MockIngesterQuerier struct {
	mock.Mock
}

func (m *MockIngesterQuerier) SelectLogs(ctx context.Context, params logql.SelectLogParams) ([]iter.EntryIterator, error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockIngesterQuerier) SelectSample(ctx context.Context, params logql.SelectSampleParams) ([]iter.SampleIterator, error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockIngesterQuerier) Label(ctx context.Context, req *logproto.LabelRequest) ([][]string, error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockIngesterQuerier) Tail(ctx context.Context, req *logproto.TailRequest) (map[string]logproto.Querier_TailClient, error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockIngesterQuerier) TailDisconnectedIngesters(ctx context.Context, req *logproto.TailRequest, connectedIngestersAddr []string) (map[string]logproto.Querier_TailClient, error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockIngesterQuerier) Series(ctx context.Context, req *logproto.SeriesRequest) ([][]logproto.SeriesIdentifier, error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockIngesterQuerier) TailersCount(ctx context.Context) ([]uint32, error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockIngesterQuerier) GetChunkIDs(ctx context.Context, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockIngesterQuerier) Volume(ctx context.Context, userID string, from, through model.Time, limit int32, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockIngesterQuerier) DetectedLabel(ctx context.Context, req *logproto.DetectedLabelsRequest) (*logproto.LabelToValuesResponse, error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockIngesterQuerier) Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*stats.Stats, error) {
	args := m.Called(ctx, userID, from, through, matchers)
	time.Sleep(time.Second)
	return args.Get(0).(*stats.Stats), args.Error(1)
}

func TestStats(t *testing.T) {
	// Create mock ingester queriers
	mockQuerier1 := new(MockIngesterQuerier)
	mockQuerier2 := new(MockIngesterQuerier)

	// Set up expected responses
	mockStats1 := &stats.Stats{Chunks: 10}
	mockStats2 := &stats.Stats{Chunks: 20}

	mockQuerier1.On("Stats", mock.Anything, "test-user", mock.Anything, mock.Anything, mock.Anything).Return(mockStats1, nil)
	mockQuerier2.On("Stats", mock.Anything, "test-user", mock.Anything, mock.Anything, mock.Anything).Return(mockStats2, nil)

	// Initialize MultiIngesterQuerier with mock ingester queriers
	miq := &MultiIngesterQuerier{
		ingesterQueriers: []storage.IIngesterQuerier{mockQuerier1, mockQuerier2},
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	from := model.TimeFromUnix(time.Now().Add(-1 * time.Hour).Unix())
	through := model.TimeFromUnix(time.Now().Unix())
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
	}

	// Call the Stats method
	resp, err := miq.Stats(ctx, "test-user", from, through, matchers...)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Validate the merged stats
	require.Equal(t, int64(30), int64(resp.Chunks))
}
