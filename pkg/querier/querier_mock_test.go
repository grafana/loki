package querier

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	grpc_metadata "google.golang.org/grpc/metadata"

	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/distributor/clientpool"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/querier/pattern"
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

type mockIngesterClientFactory struct {
	requestedClients map[string]int
}

// newIngesterClientMockFactory creates a factory function always returning
// the input querierClientMock
func (f mockIngesterClientFactory) newIngesterClientMockFactory(c *querierClientMock) ring_client.PoolFactory {
	return ring_client.PoolAddrFunc(func(addr string) (ring_client.PoolClient, error) {
		f.requestedClients[addr]++
		return c, nil
	})
}

// newIngesterClientMockFactory creates a factory function always returning
// the input querierClientMock
func newIngesterClientMockFactory(c ring_client.PoolClient) ring_client.PoolFactory {
	return ring_client.PoolAddrFunc(func(_ string) (ring_client.PoolClient, error) {
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

func (s *storeMock) GetChunks(ctx context.Context, userID string, from, through model.Time, predicate chunk.Predicate, _ *logproto.ChunkRefGroup) ([][]chunk.Chunk, []*fetcher.Fetcher, error) {
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
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

func (s *storeMock) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, m ...*labels.Matcher) ([]string, error) {
	args := s.Called(ctx, userID, from, through, metricName, m)
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

func (r *readRingMock) GetInstance(addr string) (ring.InstanceDesc, error) {
	for _, ing := range r.replicationSet.Instances {
		if ing.Addr == addr {
			return ing, nil
		}
	}
	return ring.InstanceDesc{}, errors.New("instance not found")
}

// partitionRingMock is a mocked version of a ReadRing, used in querier unit tests
// to control the pool of ingesters available
type partitionRingMock struct {
	ring *ring.PartitionRing
}

func (p partitionRingMock) PartitionRing() *ring.PartitionRing {
	return p.ring
}

func newPartitionInstanceRingMock(ingesterRing ring.InstanceRingReader, ingesters []ring.InstanceDesc, numPartitions int, ingestersPerPartition int) *ring.PartitionInstanceRing {
	partitions := make(map[int32]ring.PartitionDesc)
	owners := make(map[string]ring.OwnerDesc)
	for i := 0; i < numPartitions; i++ {
		partitions[int32(i)] = ring.PartitionDesc{
			Id:     int32(i),
			State:  ring.PartitionActive,
			Tokens: []uint32{uint32(i)},
		}

		for j := 0; j < ingestersPerPartition; j++ {
			ingesterIdx := i*ingestersPerPartition + j
			if ingesterIdx < len(ingesters) {
				owners[ingesters[ingesterIdx].Id] = ring.OwnerDesc{
					OwnedPartition: int32(i),
					State:          ring.OwnerActive,
				}
			}
		}
	}
	partitionRing := partitionRingMock{
		ring: ring.NewPartitionRing(ring.PartitionRingDesc{
			Partitions: partitions,
			Owners:     owners,
		}),
	}
	return ring.NewPartitionInstanceRing(partitionRing, ingesterRing, time.Hour)
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

func (r *readRingMock) InstancesInZoneCount(_ string) int {
	return len(r.replicationSet.Instances)
}

func (r *readRingMock) InstancesWithTokensCount() int {
	return len(r.replicationSet.Instances)
}

func (r *readRingMock) InstancesWithTokensInZoneCount(_ string) int {
	return len(r.replicationSet.Instances)
}

func (r *readRingMock) ZonesCount() int {
	return 1
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

// WritableInstancesWithTokensCount returns the number of writable instances in the ring that have tokens.
func (r *readRingMock) WritableInstancesWithTokensCount() int {
	return len(r.replicationSet.Instances)
}

// WritableInstancesWithTokensInZoneCount returns the number of writable instances in the ring that are registered in given zone and have tokens.
func (r *readRingMock) WritableInstancesWithTokensInZoneCount(_ string) int {
	return len(r.replicationSet.Instances)
}

func (r *readRingMock) GetWithOptions(_ uint32, _ ring.Operation, _ ...ring.Option) (ring.ReplicationSet, error) {
	return r.replicationSet, nil
}

func mockReadRingWithOneActiveIngester() *readRingMock {
	return newReadRingMock([]ring.InstanceDesc{
		{Addr: "test", Timestamp: time.Now().UnixNano(), State: ring.ACTIVE, Tokens: []uint32{1, 2, 3}},
	}, 0)
}

func mockInstanceDesc(addr string, state ring.InstanceState) ring.InstanceDesc {
	return mockInstanceDescWithZone(addr, state, "")
}

func mockInstanceDescWithZone(addr string, state ring.InstanceState, zone string) ring.InstanceDesc {
	return ring.InstanceDesc{
		Id:        addr,
		Addr:      addr,
		Timestamp: time.Now().UnixNano(),
		State:     state,
		Tokens:    []uint32{1, 2, 3},
		Zone:      zone,
	}
}

// mockStreamIterator returns an iterator with 1 stream and quantity entries,
// where entries timestamp and line string are constructed as sequential numbers
// starting at from
func mockStreamIterator(from int, quantity int) iter.EntryIterator {
	return iter.NewStreamIterator(mockStream(from, quantity))
}

// mockLogfmtStreamIterator returns an iterator with 1 stream and quantity entries,
// where entries timestamp and line string are constructed as sequential numbers
// starting at from, and the line is in logfmt format with the fields message, count and fake
func mockLogfmtStreamIterator(from int, quantity int) iter.EntryIterator {
	return iter.NewStreamIterator(mockLogfmtStream(from, quantity))
}

// mockLogfmtStreamIterator returns an iterator with 1 stream and quantity entries,
// where entries timestamp and line string are constructed as sequential numbers
// starting at from, and the line is in logfmt format with the fields message, count and fake
func mockLogfmtStreamIteratorWithStructuredMetadata(from int, quantity int) iter.EntryIterator {
	return iter.NewStreamIterator(mockLogfmtStreamWithStructuredMetadata(from, quantity))
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

func mockLogfmtStream(from int, quantity int) logproto.Stream {
	return mockLogfmtStreamWithLabels(from, quantity, `{type="test", name="foo"}`)
}

func mockLogfmtStreamWithLabels(_ int, quantity int, lbls string) logproto.Stream {
	entries := make([]logproto.Entry, 0, quantity)
	streamLabels, err := syntax.ParseLabels(lbls)
	if err != nil {
		streamLabels = labels.EmptyLabels()
	}

	lblBuilder := log.NewBaseLabelsBuilder().ForLabels(streamLabels, streamLabels.Hash())
	logFmtParser := log.NewLogfmtParser(false, false)

	// used for detected fields queries which are always BACKWARD
	for i := quantity; i > 0; i-- {
		line := fmt.Sprintf(
			`message="line %d" count=%d fake=true bytes=%dMB duration=%dms percent=%f even=%t name=bar`,
			i,
			i,
			(i * 10),
			(i * 256),
			float32(i*10.0),
			(i%2 == 0))

		entry := logproto.Entry{
			Timestamp: time.Unix(int64(i), 0),
			Line:      line,
		}
		_, logfmtSuccess := logFmtParser.Process(0, []byte(line), lblBuilder)
		if logfmtSuccess {
			entry.Parsed = logproto.FromLabelsToLabelAdapters(lblBuilder.LabelsResult().Parsed())
		}
		entries = append(entries, entry)
	}

	return logproto.Stream{
		Entries: entries,
		Labels:  lblBuilder.LabelsResult().String(),
	}
}

func mockLogfmtStreamWithStructuredMetadata(from int, quantity int) logproto.Stream {
	return mockLogfmtStreamWithLabelsAndStructuredMetadata(from, quantity, `{type="test"}`)
}

func mockLogfmtStreamWithLabelsAndStructuredMetadata(
	from int,
	quantity int,
	lbls string,
) logproto.Stream {
	var entries []logproto.Entry
	metadata := push.LabelsAdapter{
		{
			Name:  "constant",
			Value: "constant",
		},
	}

	for i := from; i < from+quantity; i++ {
		metadata = append(metadata, push.LabelAdapter{
			Name:  "variable",
			Value: fmt.Sprintf("value%d", i),
		})
	}

	streamLabels, err := syntax.ParseLabels(lbls)
	if err != nil {
		streamLabels = labels.EmptyLabels()
	}

	lblBuilder := log.NewBaseLabelsBuilder().ForLabels(streamLabels, streamLabels.Hash())
	logFmtParser := log.NewLogfmtParser(false, false)

	for i := quantity; i > 0; i-- {
		line := fmt.Sprintf(`message="line %d" count=%d fake=true`, i, i)
		entry := logproto.Entry{
			Timestamp:          time.Unix(int64(i), 0),
			Line:               line,
			StructuredMetadata: metadata,
		}
		_, logfmtSuccess := logFmtParser.Process(0, []byte(line), lblBuilder)
		if logfmtSuccess {
			entry.Parsed = logproto.FromLabelsToLabelAdapters(lblBuilder.LabelsResult().Parsed())
		}
		entries = append(entries, entry)
	}
	return logproto.Stream{
		Labels:  lbls,
		Entries: entries,
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

func (q *querierMock) WithPatternQuerier(_ pattern.PatterQuerier) {}

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

type mockDeleteGettter struct {
	user    string
	results []deletion.DeleteRequest
}

func (d *mockDeleteGettter) GetAllDeleteRequestsForUser(_ context.Context, userID string) ([]deletion.DeleteRequest, error) {
	d.user = userID
	return d.results, nil
}
