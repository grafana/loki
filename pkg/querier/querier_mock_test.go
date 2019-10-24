package querier

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/util/grpcclient"

	"github.com/cortexproject/cortex/pkg/chunk"
	cortex_client "github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	grpc_metadata "google.golang.org/grpc/metadata"
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

// newIngesterClientMockFactory creates a factory function always returning
// the input querierClientMock
func newIngesterClientMockFactory(c *querierClientMock) cortex_client.Factory {
	return func(addr string) (grpc_health_v1.HealthClient, error) {
		return c, nil
	}
}

// mockIngesterClientConfig returns an ingester client config suitable for testing
func mockIngesterClientConfig() client.Config {
	return client.Config{
		PoolConfig: cortex_client.PoolConfig{
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
	return args.Get(0).(*logproto.QueryResponse), args.Error(1)
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

func (s *storeMock) LazyQuery(ctx context.Context, req logql.SelectParams) (iter.EntryIterator, error) {
	args := s.Called(ctx, req)
	return args.Get(0).(iter.EntryIterator), args.Error(1)
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

func (s *storeMock) Stop() {

}

// readRingMock is a mocked version of a ReadRing, used in querier unit tests
// to control the pool of ingesters available
type readRingMock struct {
	replicationSet ring.ReplicationSet
}

func newReadRingMock(ingesters []ring.IngesterDesc) *readRingMock {
	return &readRingMock{
		replicationSet: ring.ReplicationSet{
			Ingesters: ingesters,
			MaxErrors: 0,
		},
	}
}

func (r *readRingMock) Describe(ch chan<- *prometheus.Desc) {
}

func (r *readRingMock) Collect(ch chan<- prometheus.Metric) {
}

func (r *readRingMock) Get(key uint32, op ring.Operation, buf []ring.IngesterDesc) (ring.ReplicationSet, error) {
	return r.replicationSet, nil
}

func (r *readRingMock) BatchGet(keys []uint32, op ring.Operation) ([]ring.ReplicationSet, error) {
	return []ring.ReplicationSet{r.replicationSet}, nil
}

func (r *readRingMock) GetAll() (ring.ReplicationSet, error) {
	return r.replicationSet, nil
}

func (r *readRingMock) ReplicationFactor() int {
	return 1
}

func (r *readRingMock) IngesterCount() int {
	return len(r.replicationSet.Ingesters)
}

func mockReadRingWithOneActiveIngester() *readRingMock {
	return newReadRingMock([]ring.IngesterDesc{
		{Addr: "test", Timestamp: time.Now().UnixNano(), State: ring.ACTIVE, Tokens: []uint32{1, 2, 3}},
	})
}

func mockIngesterDesc(addr string, state ring.IngesterState) ring.IngesterDesc {
	return ring.IngesterDesc{
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
func mockStream(from int, quantity int) *logproto.Stream {
	entries := make([]logproto.Entry, 0, quantity)

	for i := from; i < from+quantity; i++ {
		entries = append(entries, logproto.Entry{
			Timestamp: time.Unix(int64(i), 0),
			Line:      fmt.Sprintf("line %d", i),
		})
	}

	return &logproto.Stream{
		Entries: entries,
		Labels:  `{type="test"}`,
	}
}
