package querier

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/grafana/loki/v3/pkg/logql/log"

	"github.com/grafana/loki/v3/pkg/loghttp"

	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	grpc_metadata "google.golang.org/grpc/metadata"

	"github.com/grafana/loki/v3/pkg/distributor/clientpool"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/validation"
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
	res := args.Get(0)
	if res == nil {
		return (logproto.Querier_QueryClient)(nil), args.Error(1)
	}
	return res.(logproto.Querier_QueryClient), args.Error(1)
}

func (c *querierClientMock) QuerySample(ctx context.Context, in *logproto.SampleQueryRequest, opts ...grpc.CallOption) (logproto.Querier_QuerySampleClient, error) {
	args := c.Called(ctx, in, opts)
	res := args.Get(0)
	if res == nil {
		return (logproto.Querier_QuerySampleClient)(nil), args.Error(1)
	}
	return res.(logproto.Querier_QuerySampleClient), args.Error(1)
}

func (c *querierClientMock) Label(ctx context.Context, in *logproto.LabelRequest, opts ...grpc.CallOption) (*logproto.LabelResponse, error) {
	args := c.Called(ctx, in, opts)
	res := args.Get(0)
	if res == nil {
		return (*logproto.LabelResponse)(nil), args.Error(1)
	}
	return res.(*logproto.LabelResponse), args.Error(1)
}

func (c *querierClientMock) Tail(ctx context.Context, in *logproto.TailRequest, opts ...grpc.CallOption) (logproto.Querier_TailClient, error) {
	args := c.Called(ctx, in, opts)
	res := args.Get(0)
	if res == nil {
		return (logproto.Querier_TailClient)(nil), args.Error(1)
	}
	return res.(logproto.Querier_TailClient), args.Error(1)
}

func (c *querierClientMock) Series(ctx context.Context, in *logproto.SeriesRequest, _ ...grpc.CallOption) (*logproto.SeriesResponse, error) {
	args := c.Called(ctx, in)
	res := args.Get(0)
	if res == nil {
		return (*logproto.SeriesResponse)(nil), args.Error(1)
	}
	return res.(*logproto.SeriesResponse), args.Error(1)
}

func (c *querierClientMock) TailersCount(ctx context.Context, in *logproto.TailersCountRequest, opts ...grpc.CallOption) (*logproto.TailersCountResponse, error) {
	args := c.Called(ctx, in, opts)
	res := args.Get(0)
	if res == nil {
		return (*logproto.TailersCountResponse)(nil), args.Error(1)
	}
	return res.(*logproto.TailersCountResponse), args.Error(1)
}

func (c *querierClientMock) GetChunkIDs(ctx context.Context, in *logproto.GetChunkIDsRequest, opts ...grpc.CallOption) (*logproto.GetChunkIDsResponse, error) {
	args := c.Called(ctx, in, opts)
	res := args.Get(0)
	if res == nil {
		return (*logproto.GetChunkIDsResponse)(nil), args.Error(1)
	}
	return res.(*logproto.GetChunkIDsResponse), args.Error(1)
}

func (c *querierClientMock) GetDetectedLabels(ctx context.Context, in *logproto.DetectedLabelsRequest, opts ...grpc.CallOption) (*logproto.LabelToValuesResponse, error) {
	args := c.Called(ctx, in, opts)
	res := args.Get(0)
	if res == nil {
		return (*logproto.LabelToValuesResponse)(nil), args.Error(1)
	}
	return res.(*logproto.LabelToValuesResponse), args.Error(1)

}

func (c *querierClientMock) GetVolume(ctx context.Context, in *logproto.VolumeRequest, opts ...grpc.CallOption) (*logproto.VolumeResponse, error) {
	args := c.Called(ctx, in, opts)
	res := args.Get(0)
	if res == nil {
		return (*logproto.VolumeResponse)(nil), args.Error(1)
	}
	return res.(*logproto.VolumeResponse), args.Error(1)
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
	return ring_client.PoolAddrFunc(func(addr string) (ring_client.PoolClient, error) {
		return c, nil
	})
}

// mockIngesterClientConfig returns an ingester client config suitable for testing
func mockIngesterClientConfig() client.Config {
	return client.Config{
		PoolConfig: clientpool.PoolConfig{
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

func (c *queryClientMock) SendMsg(_ interface{}) error {
	return nil
}

func (c *queryClientMock) RecvMsg(_ interface{}) error {
	return nil
}

func (c *queryClientMock) Context() context.Context {
	return context.Background()
}

// queryClientMock is a mockable version of Querier_QueryClient
type querySampleClientMock struct {
	util.ExtendedMock
	logproto.Querier_QueryClient
}

func newQuerySampleClientMock() *querySampleClientMock {
	return &querySampleClientMock{}
}

func (c *querySampleClientMock) Recv() (*logproto.SampleQueryResponse, error) {
	args := c.Called()
	res := args.Get(0)
	if res == nil {
		return (*logproto.SampleQueryResponse)(nil), args.Error(1)
	}
	return res.(*logproto.SampleQueryResponse), args.Error(1)
}

func (c *querySampleClientMock) Header() (grpc_metadata.MD, error) {
	return nil, nil
}

func (c *querySampleClientMock) Trailer() grpc_metadata.MD {
	return nil
}

func (c *querySampleClientMock) CloseSend() error {
	return nil
}

func (c *querySampleClientMock) SendMsg(_ interface{}) error {
	return nil
}

func (c *querySampleClientMock) RecvMsg(_ interface{}) error {
	return nil
}

func (c *querySampleClientMock) Context() context.Context {
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

func (c *tailClientMock) SendMsg(_ interface{}) error {
	return nil
}

func (c *tailClientMock) RecvMsg(_ interface{}) error {
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
func (s *storeMock) SetChunkFilterer(chunk.RequestChunkFilterer)    {}
func (s *storeMock) SetExtractorWrapper(log.SampleExtractorWrapper) {}
func (s *storeMock) SetPipelineWrapper(log.PipelineWrapper)         {}

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

func (s *storeMock) GetChunks(ctx context.Context, userID string, from, through model.Time, predicate chunk.Predicate) ([][]chunk.Chunk, []*fetcher.Fetcher, error) {
	args := s.Called(ctx, userID, from, through, predicate)
	return args.Get(0).([][]chunk.Chunk), args.Get(0).([]*fetcher.Fetcher), args.Error(2)
}

func (s *storeMock) Put(_ context.Context, _ []chunk.Chunk) error {
	return errors.New("storeMock.Put() has not been mocked")
}

func (s *storeMock) PutOne(_ context.Context, _, _ model.Time, _ chunk.Chunk) error {
	return errors.New("storeMock.PutOne() has not been mocked")
}

func (s *storeMock) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, _ ...*labels.Matcher) ([]string, error) {
	args := s.Called(ctx, userID, from, through, metricName, labelName)
	return args.Get(0).([]string), args.Error(1)
}

func (s *storeMock) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error) {
	args := s.Called(ctx, userID, from, through, metricName)
	return args.Get(0).([]string), args.Error(1)
}

func (s *storeMock) GetChunkFetcher(_ model.Time) *fetcher.Fetcher {
	panic("don't call me please")
}

func (s *storeMock) GetSchemaConfigs() []config.PeriodConfig {
	panic("don't call me please")
}

func (s *storeMock) SelectSeries(ctx context.Context, req logql.SelectLogParams) ([]logproto.SeriesIdentifier, error) {
	args := s.Called(ctx, req)
	res := args.Get(0)
	if res == nil {
		return []logproto.SeriesIdentifier(nil), args.Error(1)
	}
	return res.([]logproto.SeriesIdentifier), args.Error(1)
}

func (s *storeMock) GetSeries(_ context.Context, _ string, _, _ model.Time, _ ...*labels.Matcher) ([]labels.Labels, error) {
	panic("don't call me please")
}

func (s *storeMock) Stats(_ context.Context, _ string, _, _ model.Time, _ ...*labels.Matcher) (*stats.Stats, error) {
	return nil, nil
}

func (s *storeMock) GetShards(_ context.Context, _ string, _, _ model.Time, _ uint64, _ chunk.Predicate) (*logproto.ShardsResponse, error) {
	return nil, nil
}

func (s *storeMock) HasForSeries(_, _ model.Time) (sharding.ForSeries, bool) {
	return nil, false
}

func (s *storeMock) Volume(ctx context.Context, userID string, from, through model.Time, _ int32, targetLabels []string, _ string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	args := s.Called(ctx, userID, from, through, targetLabels, matchers)
	return args.Get(0).(*logproto.VolumeResponse), args.Error(1)
}

func (s *storeMock) Stop() {
}

// readRingMock is a mocked version of a ReadRing, used in querier unit tests
// to control the pool of ingesters available
type readRingMock struct {
	replicationSet ring.ReplicationSet
}

func newReadRingMock(ingesters []ring.InstanceDesc, maxErrors int) *readRingMock {
	return &readRingMock{
		replicationSet: ring.ReplicationSet{
			Instances: ingesters,
			MaxErrors: maxErrors,
		},
	}
}

func (r *readRingMock) Describe(_ chan<- *prometheus.Desc) {
}

func (r *readRingMock) Collect(_ chan<- prometheus.Metric) {
}

func (r *readRingMock) Get(_ uint32, _ ring.Operation, _ []ring.InstanceDesc, _ []string, _ []string) (ring.ReplicationSet, error) {
	return r.replicationSet, nil
}

func (r *readRingMock) ShuffleShard(_ string, size int) ring.ReadRing {
	// pass by value to copy
	return func(r readRingMock) *readRingMock {
		r.replicationSet.Instances = r.replicationSet.Instances[:size]
		return &r
	}(*r)
}

func (r *readRingMock) BatchGet(_ []uint32, _ ring.Operation) ([]ring.ReplicationSet, error) {
	return []ring.ReplicationSet{r.replicationSet}, nil
}

func (r *readRingMock) GetAllHealthy(_ ring.Operation) (ring.ReplicationSet, error) {
	return r.replicationSet, nil
}

func (r *readRingMock) GetReplicationSetForOperation(_ ring.Operation) (ring.ReplicationSet, error) {
	return r.replicationSet, nil
}

func (r *readRingMock) ReplicationFactor() int {
	return 1
}

func (r *readRingMock) InstancesCount() int {
	return len(r.replicationSet.Instances)
}

func (r *readRingMock) Subring(_ uint32, _ int) ring.ReadRing {
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

func (r *readRingMock) ShuffleShardWithLookback(_ string, _ int, _ time.Duration, _ time.Time) ring.ReadRing {
	return r
}

func (r *readRingMock) CleanupShuffleShardCache(_ string) {}

func (r *readRingMock) GetInstanceState(_ string) (ring.InstanceState, error) {
	return 0, nil
}

func (r *readRingMock) GetTokenRangesForInstance(_ string) (ring.TokenRanges, error) {
	tr := ring.TokenRanges{0, math.MaxUint32}
	return tr, nil
}

func mockReadRingWithOneActiveIngester() *readRingMock {
	return newReadRingMock([]ring.InstanceDesc{
		{Addr: "test", Timestamp: time.Now().UnixNano(), State: ring.ACTIVE, Tokens: []uint32{1, 2, 3}},
	}, 0)
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

// mockSampleIterator returns an iterator with 1 stream and quantity entries,
// where entries timestamp and line string are constructed as sequential numbers
// starting at from
func mockSampleIterator(client iter.QuerySampleClient) iter.SampleIterator {
	return iter.NewSampleQueryClientIterator(client)
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

type querierMock struct {
	util.ExtendedMock
}

func newQuerierMock() *querierMock {
	return &querierMock{}
}

func (q *querierMock) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {
	args := q.Called(ctx, params)
	return args.Get(0).(func() iter.EntryIterator)(), args.Error(1)
}

func (q *querierMock) SelectSamples(ctx context.Context, params logql.SelectSampleParams) (iter.SampleIterator, error) {
	args := q.Called(ctx, params)
	return args.Get(0).(func() iter.SampleIterator)(), args.Error(1)
}

func (q *querierMock) Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	args := q.Called(ctx, req)
	return args.Get(0).(*logproto.LabelResponse), args.Error(1)
}

func (q *querierMock) Series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	args := q.Called(ctx, req)
	return args.Get(0).(func() *logproto.SeriesResponse)(), args.Error(1)
}

func (q *querierMock) Tail(_ context.Context, _ *logproto.TailRequest, _ bool) (*Tailer, error) {
	return nil, errors.New("querierMock.Tail() has not been mocked")
}

func (q *querierMock) IndexStats(_ context.Context, _ *loghttp.RangeQuery) (*stats.Stats, error) {
	return nil, nil
}

func (q *querierMock) GetShards(_ context.Context, _ string, _, _ model.Time, _ uint64, _ chunk.Predicate) ([]logproto.Shard, error) {
	return nil, nil
}

func (q *querierMock) HasForSeries(_, _ model.Time) (sharding.ForSeries, bool) {
	return nil, false
}

func (q *querierMock) IndexShards(_ context.Context, _ *loghttp.RangeQuery, _ uint64) (*logproto.ShardsResponse, error) {
	return nil, errors.New("unimplemented")
}

func (q *querierMock) Volume(ctx context.Context, req *logproto.VolumeRequest) (*logproto.VolumeResponse, error) {
	args := q.MethodCalled("Volume", ctx, req)

	resp := args.Get(0)
	err := args.Error(1)
	if resp == nil {
		return nil, err
	}

	return resp.(*logproto.VolumeResponse), err
}

func (q *querierMock) DetectedFields(ctx context.Context, req *logproto.DetectedFieldsRequest) (*logproto.DetectedFieldsResponse, error) {
	args := q.MethodCalled("DetectedFields", ctx, req)

	resp := args.Get(0)
	err := args.Error(1)
	if resp == nil {
		return nil, err
	}

	return resp.(*logproto.DetectedFieldsResponse), err
}

func (q *querierMock) Patterns(ctx context.Context, req *logproto.QueryPatternsRequest) (*logproto.QueryPatternsResponse, error) {
	args := q.MethodCalled("Patterns", ctx, req)

	resp := args.Get(0)
	err := args.Error(1)
	if resp == nil {
		return nil, err
	}

	return resp.(*logproto.QueryPatternsResponse), err
}

func (q *querierMock) DetectedLabels(ctx context.Context, req *logproto.DetectedLabelsRequest) (*logproto.DetectedLabelsResponse, error) {
	args := q.MethodCalled("DetectedFields", ctx, req)

	resp := args.Get(0)
	err := args.Error(1)
	if resp == nil {
		return nil, err
	}

	return resp.(*logproto.DetectedLabelsResponse), err
}

type engineMock struct {
	util.ExtendedMock
}

func newEngineMock() *engineMock {
	return &engineMock{}
}

func (e *engineMock) Query(p logql.Params) logql.Query {
	args := e.Called(p)
	return args.Get(0).(logql.Query)
}

type queryMock struct {
	result logqlmodel.Result
}

func (q queryMock) Exec(_ context.Context) (logqlmodel.Result, error) {
	return q.result, nil
}

type mockTenantLimits map[string]*validation.Limits

func (tl mockTenantLimits) TenantLimits(userID string) *validation.Limits {
	limits, ok := tl[userID]
	if !ok {
		return &validation.Limits{}
	}

	return limits
}

func (tl mockTenantLimits) AllByUserID() map[string]*validation.Limits {
	return tl
}
