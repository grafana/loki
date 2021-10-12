package querier

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/distributor"
	"github.com/cortexproject/cortex/pkg/ring"
	ring_client "github.com/cortexproject/cortex/pkg/ring/client"
	"github.com/grafana/dskit/grpcclient"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	grpc_metadata "google.golang.org/grpc/metadata"

	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/util"
)

// querierClientMock is a mockable version of QuerierClient, used in querier
// unit tests to control the behaviour of a remote ingester
type querierClientMock struct {
	util.ExtendedMock
	grpc_health_v1.HealthClient
	logproto.QuerierClient
}

func newQuerierClientMock() *querierClientMock {
	return &querierClientMock{}
}

func (c *querierClientMock) Query(ctx context.Context, in *logproto.QueryRequest, opts ...grpc.CallOption) (logproto.Querier_QueryClient, error) {
	args := c.Called(ctx, in, opts)
	return args.Get(0).(logproto.Querier_QueryClient), args.Error(1)
}

func (c *querierClientMock) Label(ctx context.Context, in *logproto.LabelRequest, opts ...grpc.CallOption) (*logproto.LabelResponse, error) {
	args := c.Called(ctx, in, opts)
	return args.Get(0).(*logproto.LabelResponse), args.Error(1)
}

func (c *querierClientMock) Tail(ctx context.Context, in *logproto.TailRequest, opts ...grpc.CallOption) (logproto.Querier_TailClient, error) {
	args := c.Called(ctx, in, opts)
	return args.Get(0).(logproto.Querier_TailClient), args.Error(1)
}

func (c *querierClientMock) Series(ctx context.Context, in *logproto.SeriesRequest, opts ...grpc.CallOption) (*logproto.SeriesResponse, error) {
	args := c.Called(ctx, in)
	res := args.Get(0)
	if res == nil {
		return (*logproto.SeriesResponse)(nil), args.Error(1)
	}
	return res.(*logproto.SeriesResponse), args.Error(1)
}

func (c *querierClientMock) TailersCount(ctx context.Context, in *logproto.TailersCountRequest, opts ...grpc.CallOption) (*logproto.TailersCountResponse, error) {
	args := c.Called(ctx, in, opts)
	return args.Get(0).(*logproto.TailersCountResponse), args.Error(1)
}

func (c *querierClientMock) Context() context.Context {
	return context.Background()
}

func (c *querierClientMock) Close() error {
	return nil
}

// newIngesterClientMockFactory creates a factory function always returning
// the input querierClientMock
func newIngesterClientMockFactory(c *querierClientMock) ring_client.PoolFactory {
	return func(addr string) (ring_client.PoolClient, error) {
		return c, nil
	}
}

// mockIngesterClientConfig returns an ingester client config suitable for testing
func mockIngesterClientConfig() client.Config {
	return client.Config{
		PoolConfig: distributor.PoolConfig{
			ClientCleanupPeriod:  1 * time.Minute,
			HealthCheckIngesters: false,
			RemoteTimeout:        1 * time.Second,
		},
		GRPCClientConfig: grpcclient.Config{
			MaxRecvMsgSize: 1024,
		},
		RemoteTimeout: 1 * time.Second,
	}
}

// queryClientMock is a mockable version of Querier_QueryClient
type queryClientMock struct {
	util.ExtendedMock
	logproto.Querier_QueryClient
}

func newQueryClientMock() *queryClientMock {
	return &queryClientMock{}
}

func (c *queryClientMock) Recv() (*logproto.QueryResponse, error) {
	args := c.Called()
	res := args.Get(0)
	if res == nil {
		return (*logproto.QueryResponse)(nil), args.Error(1)
	}
	return res.(*logproto.QueryResponse), args.Error(1)
}

func (c *queryClientMock) Header() (grpc_metadata.MD, error) {
	return nil, nil
}

func (c *queryClientMock) Trailer() grpc_metadata.MD {
	return nil
}

func (c *queryClientMock) CloseSend() error {
	return nil
}

func (c *queryClientMock) SendMsg(m interface{}) error {
	return nil
}

func (c *queryClientMock) RecvMsg(m interface{}) error {
	return nil
}

func (c *queryClientMock) Context() context.Context {
	return context.Background()
}

// tailClientMock is mockable version of Querier_TailClient
type tailClientMock struct {
	util.ExtendedMock
	logproto.Querier_TailClient
	recvTrigger chan time.Time
}

func newTailClientMock() *tailClientMock {
	return &tailClientMock{
		recvTrigger: make(chan time.Time, 10),
	}
}

func (c *tailClientMock) Recv() (*logproto.TailResponse, error) {
	args := c.Called()
	return args.Get(0).(*logproto.TailResponse), args.Error(1)
}

func (c *tailClientMock) Header() (grpc_metadata.MD, error) {
	return nil, nil
}

func (c *tailClientMock) Trailer() grpc_metadata.MD {
	return nil
}

func (c *tailClientMock) CloseSend() error {
	return nil
}

func (c *tailClientMock) Context() context.Context {
	return context.Background()
}

func (c *tailClientMock) SendMsg(m interface{}) error {
	return nil
}

func (c *tailClientMock) RecvMsg(m interface{}) error {
	return nil
}

func (c *tailClientMock) mockRecvWithTrigger(response *logproto.TailResponse) *tailClientMock {
	c.On("Recv").WaitUntil(c.recvTrigger).Return(response, nil)

	return c
}

// triggerRecv triggers the Recv() mock to return from the next invocation
// or from the current invocation if was already called and waiting for the
// trigger. This method works if and only if the Recv() has been mocked with
// mockRecvWithTrigger().
func (c *tailClientMock) triggerRecv() {
	c.recvTrigger <- time.Now()
}

// storeMock is a mockable version of Loki's storage, used in querier unit tests
// to control the behaviour of the store without really hitting any storage backend
type storeMock struct {
	util.ExtendedMock
}

func newStoreMock() *storeMock {
	return &storeMock{}
}

func (s *storeMock) SetChunkFilterer(storage.RequestChunkFilterer) {}

func (s *storeMock) SelectLogs(ctx context.Context, req logql.SelectLogParams) (iter.EntryIterator, error) {
	args := s.Called(ctx, req)
	res := args.Get(0)
	if res == nil {
		return iter.EntryIterator(nil), args.Error(1)
	}
	return res.(iter.EntryIterator), args.Error(1)
}

func (s *storeMock) SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error) {
	args := s.Called(ctx, req)
	res := args.Get(0)
	if res == nil {
		return iter.SampleIterator(nil), args.Error(1)
	}
	return res.(iter.SampleIterator), args.Error(1)
}

func (s *storeMock) Get(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]chunk.Chunk, error) {
	args := s.Called(ctx, userID, from, through, matchers)
	return args.Get(0).([]chunk.Chunk), args.Error(1)
}

func (s *storeMock) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([][]chunk.Chunk, []*chunk.Fetcher, error) {
	args := s.Called(ctx, userID, from, through, matchers)
	return args.Get(0).([][]chunk.Chunk), args.Get(0).([]*chunk.Fetcher), args.Error(2)
}

func (s *storeMock) Put(ctx context.Context, chunks []chunk.Chunk) error {
	return errors.New("storeMock.Put() has not been mocked")
}

func (s *storeMock) PutOne(ctx context.Context, from, through model.Time, chunk chunk.Chunk) error {
	return errors.New("storeMock.PutOne() has not been mocked")
}

func (s *storeMock) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string) ([]string, error) {
	args := s.Called(ctx, userID, from, through, metricName, labelName)
	return args.Get(0).([]string), args.Error(1)
}

func (s *storeMock) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error) {
	args := s.Called(ctx, userID, from, through, metricName)
	return args.Get(0).([]string), args.Error(1)
}

func (s *storeMock) DeleteChunk(ctx context.Context, from, through model.Time, userID, chunkID string, metric labels.Labels, partiallyDeletedInterval *model.Interval) error {
	panic("don't call me please")
}

func (s *storeMock) DeleteSeriesIDs(ctx context.Context, from, through model.Time, userID string, metric labels.Labels) error {
	panic("don't call me please")
}

func (s *storeMock) GetChunkFetcher(_ model.Time) *chunk.Fetcher {
	panic("don't call me please")
}

func (s *storeMock) GetSchemaConfigs() []chunk.PeriodConfig {
	panic("don't call me please")
}

func (s *storeMock) GetSeries(ctx context.Context, req logql.SelectLogParams) ([]logproto.SeriesIdentifier, error) {
	args := s.Called(ctx, req)
	res := args.Get(0)
	if res == nil {
		return []logproto.SeriesIdentifier(nil), args.Error(1)
	}
	return res.([]logproto.SeriesIdentifier), args.Error(1)
}

func (s *storeMock) Stop() {
}

// readRingMock is a mocked version of a ReadRing, used in querier unit tests
// to control the pool of ingesters available
type readRingMock struct {
	replicationSet ring.ReplicationSet
}

func newReadRingMock(ingesters []ring.InstanceDesc) *readRingMock {
	return &readRingMock{
		replicationSet: ring.ReplicationSet{
			Instances: ingesters,
			MaxErrors: 0,
		},
	}
}

func (r *readRingMock) Describe(ch chan<- *prometheus.Desc) {
}

func (r *readRingMock) Collect(ch chan<- prometheus.Metric) {
}

func (r *readRingMock) Get(key uint32, op ring.Operation, buf []ring.InstanceDesc, _ []string, _ []string) (ring.ReplicationSet, error) {
	return r.replicationSet, nil
}

func (r *readRingMock) ShuffleShard(identifier string, size int) ring.ReadRing {
	// pass by value to copy
	return func(r readRingMock) *readRingMock {
		r.replicationSet.Instances = r.replicationSet.Instances[:size]
		return &r
	}(*r)
}

func (r *readRingMock) BatchGet(keys []uint32, op ring.Operation) ([]ring.ReplicationSet, error) {
	return []ring.ReplicationSet{r.replicationSet}, nil
}

func (r *readRingMock) GetAllHealthy(op ring.Operation) (ring.ReplicationSet, error) {
	return r.replicationSet, nil
}

func (r *readRingMock) GetReplicationSetForOperation(op ring.Operation) (ring.ReplicationSet, error) {
	return r.replicationSet, nil
}

func (r *readRingMock) ReplicationFactor() int {
	return 1
}

func (r *readRingMock) InstancesCount() int {
	return len(r.replicationSet.Instances)
}

func (r *readRingMock) Subring(key uint32, n int) ring.ReadRing {
	return r
}

func (r *readRingMock) HasInstance(instanceID string) bool {
	for _, ing := range r.replicationSet.Instances {
		if ing.Addr != instanceID {
			return true
		}
	}
	return false
}

func (r *readRingMock) ShuffleShardWithLookback(identifier string, size int, lookbackPeriod time.Duration, now time.Time) ring.ReadRing {
	return r
}

func (r *readRingMock) CleanupShuffleShardCache(identifier string) {}

func (r *readRingMock) GetInstanceState(instanceID string) (ring.InstanceState, error) {
	return 0, nil
}

func mockReadRingWithOneActiveIngester() *readRingMock {
	return newReadRingMock([]ring.InstanceDesc{
		{Addr: "test", Timestamp: time.Now().UnixNano(), State: ring.ACTIVE, Tokens: []uint32{1, 2, 3}},
	})
}

func mockInstanceDesc(addr string, state ring.InstanceState) ring.InstanceDesc {
	return ring.InstanceDesc{
		Addr:      addr,
		Timestamp: time.Now().UnixNano(),
		State:     state,
		Tokens:    []uint32{1, 2, 3},
	}
}

// mockStreamIterator returns an iterator with 1 stream and quantity entries,
// where entries timestamp and line string are constructed as sequential numbers
// starting at from
func mockStreamIterator(from int, quantity int) iter.EntryIterator {
	return iter.NewStreamIterator(mockStream(from, quantity))
}

// mockStream return a stream with quantity entries, where entries timestamp and
// line string are constructed as sequential numbers starting at from
func mockStream(from int, quantity int) logproto.Stream {
	return mockStreamWithLabels(from, quantity, `{type="test"}`)
}

func mockStreamWithLabels(from int, quantity int, labels string) logproto.Stream {
	entries := make([]logproto.Entry, 0, quantity)

	for i := from; i < from+quantity; i++ {
		entries = append(entries, logproto.Entry{
			Timestamp: time.Unix(int64(i), 0),
			Line:      fmt.Sprintf("line %d", i),
		})
	}

	return logproto.Stream{
		Entries: entries,
		Labels:  labels,
	}
}
