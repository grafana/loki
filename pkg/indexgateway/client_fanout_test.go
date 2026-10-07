package indexgateway

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/ring"
	dskitclient "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"
)

const day = model.Time(24 * time.Hour / time.Millisecond)

// dayStart returns the start of table n's day.
func dayStart(n int) model.Time { return model.Time(n) * day }

func testTableRange(start, end int64) config.TableRange {
	return config.TableRange{
		Start: start,
		End:   end,
		PeriodConfig: &config.PeriodConfig{
			IndexTables: config.IndexPeriodicTableConfig{
				PeriodicTableConfig: config.PeriodicTableConfig{Prefix: "index_", Period: 24 * time.Hour},
			},
		},
	}
}

func TestSplitByTable(t *testing.T) {
	const n = 20000
	tr := testTableRange(n-100, n+100)
	hour := model.Time(time.Hour / time.Millisecond)

	for _, tc := range []struct {
		name          string
		from, through model.Time
		tableRange    config.TableRange
		want          []tablePart
	}{
		{
			name: "within one day",
			from: dayStart(n) + hour, through: dayStart(n) + 2*hour,
			tableRange: tr,
			want:       []tablePart{{"index_20000", dayStart(n) + hour, dayStart(n) + 2*hour}},
		},
		{
			name: "whole day, end inclusive",
			from: dayStart(n), through: dayStart(n+1) - 1,
			tableRange: tr,
			want:       []tablePart{{"index_20000", dayStart(n), dayStart(n+1) - 1}},
		},
		{
			name: "through exactly at midnight reads the next table",
			from: dayStart(n), through: dayStart(n + 1),
			tableRange: tr,
			want: []tablePart{
				{"index_20000", dayStart(n), dayStart(n+1) - 1},
				{"index_20001", dayStart(n + 1), dayStart(n + 1)},
			},
		},
		{
			name: "from at midnight",
			from: dayStart(n + 1), through: dayStart(n+1) + hour,
			tableRange: tr,
			want:       []tablePart{{"index_20001", dayStart(n + 1), dayStart(n+1) + hour}},
		},
		{
			name: "several days",
			from: dayStart(n) + 12*hour, through: dayStart(n+2) + 6*hour,
			tableRange: tr,
			want: []tablePart{
				{"index_20000", dayStart(n) + 12*hour, dayStart(n+1) - 1},
				{"index_20001", dayStart(n + 1), dayStart(n+2) - 1},
				{"index_20002", dayStart(n + 2), dayStart(n+2) + 6*hour},
			},
		},
		{
			name: "only tables of the period",
			from: dayStart(n-1) + hour, through: dayStart(n+2) + hour,
			tableRange: testTableRange(n, n+1),
			want: []tablePart{
				{"index_20000", dayStart(n), dayStart(n+1) - 1},
				{"index_20001", dayStart(n + 1), dayStart(n+2) - 1},
			},
		},
		{
			name: "outside the period",
			from: dayStart(n), through: dayStart(n) + hour,
			tableRange: testTableRange(n+10, n+20),
			want:       nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, splitByTable(tc.from, tc.through, tc.tableRange))
		})
	}
}

// fanoutCall is one RPC a recordingGateways connection received.
type fanoutCall struct {
	addr, op      string
	from, through model.Time
}

// recordingGateways fakes index gateways: it records every call and answers
// with responses derived from the request's time range.
type recordingGateways struct {
	failing map[string]bool
	block   chan struct{} // when set, calls wait on it

	mu          sync.Mutex
	calls       []fanoutCall
	inFlight    int
	maxInFlight int
}

func (g *recordingGateways) record(addr, op string, from, through model.Time) error {
	g.mu.Lock()
	g.calls = append(g.calls, fanoutCall{addr: addr, op: op, from: from, through: through})
	g.inFlight++
	g.maxInFlight = max(g.maxInFlight, g.inFlight)
	block := g.block
	g.mu.Unlock()

	if block != nil {
		<-block
	}

	g.mu.Lock()
	g.inFlight--
	g.mu.Unlock()

	if g.failing[addr] {
		return fmt.Errorf("gateway %s failed", addr)
	}
	return nil
}

func (g *recordingGateways) recorded() []fanoutCall {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]fanoutCall(nil), g.calls...)
}

func (g *recordingGateways) reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls, g.maxInFlight = nil, 0
}

type recordingConn struct {
	logproto.IndexGatewayClient
	grpc_health_v1.HealthClient
	g    *recordingGateways
	addr string
}

func (c *recordingConn) GetChunkRef(_ context.Context, in *logproto.GetChunkRefRequest, _ ...grpc.CallOption) (*logproto.GetChunkRefResponse, error) {
	if err := c.g.record(c.addr, "GetChunkRef", in.From, in.Through); err != nil {
		return nil, err
	}
	// One chunk per part, plus one shared by every part.
	return &logproto.GetChunkRefResponse{
		Refs: []*logproto.ChunkRef{
			{Fingerprint: uint64(in.From), UserID: "tenant", From: in.From, Through: in.Through},
			{Fingerprint: 1, UserID: "tenant", From: 1, Through: 2},
		},
		Stats: stats.Index{TotalChunks: 2, PostFilterChunks: 2, TotalStreams: 2},
	}, nil
}

func (c *recordingConn) GetSeries(_ context.Context, in *logproto.GetSeriesRequest, _ ...grpc.CallOption) (*logproto.GetSeriesResponse, error) {
	if err := c.g.record(c.addr, "GetSeries", in.From, in.Through); err != nil {
		return nil, err
	}
	return &logproto.GetSeriesResponse{}, nil
}

func (c *recordingConn) GetStats(_ context.Context, in *logproto.IndexStatsRequest, _ ...grpc.CallOption) (*logproto.IndexStatsResponse, error) {
	if err := c.g.record(c.addr, "GetStats", in.From, in.Through); err != nil {
		return nil, err
	}
	return &logproto.IndexStatsResponse{Streams: 1, Chunks: 2, Bytes: 3, Entries: 4}, nil
}

func (c *recordingConn) GetShards(_ context.Context, in *logproto.ShardsRequest, _ ...grpc.CallOption) (logproto.IndexGateway_GetShardsClient, error) {
	if err := c.g.record(c.addr, "GetShards", in.From, in.Through); err != nil {
		return nil, err
	}
	return &mockShardsClient{}, nil
}

func (c *recordingConn) Check(context.Context, *grpc_health_v1.HealthCheckRequest, ...grpc.CallOption) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

func (c *recordingConn) Close() error { return nil }

type fanoutFixture struct {
	client    *GatewayClient
	gateways  *recordingGateways
	ownership *IndexOwnership
	reg       *prometheus.Registry
}

func newFanoutFixture(t *testing.T, mutate func(*ClientConfig)) *fanoutFixture {
	t.Helper()

	instances := newTestInstances(0, 6)
	addrs := make([]string, 0, len(instances))
	for _, inst := range instances {
		addrs = append(addrs, inst.id)
	}
	r := newTestRing(t, instances)

	cfg := ClientConfig{}
	flagext.DefaultValues(&cfg)
	cfg.Mode = RingMode
	cfg.Ring = r
	cfg.Sharding = ShardingPerIndex
	cfg.TableRange = testTableRange(0, 1<<20)
	if mutate != nil {
		mutate(&cfg)
	}

	o, err := validation.NewOverrides(validation.Limits{IndexGatewayShardSize: 3}, nil)
	require.NoError(t, err)
	reg := prometheus.NewRegistry()
	client, err := NewGatewayClient(cfg, reg, o, log.NewNopLogger(), constants.Loki)
	require.NoError(t, err)
	t.Cleanup(client.Stop)

	gateways := &recordingGateways{}
	pool := dskitclient.NewPool("test", dskitclient.PoolConfig{CheckInterval: time.Hour},
		func() ([]string, error) { return addrs, nil },
		dskitclient.PoolAddrFunc(func(addr string) (dskitclient.PoolClient, error) {
			return &recordingConn{g: gateways, addr: addr}, nil
		}),
		nil, log.NewNopLogger())
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), pool))
	require.NoError(t, services.StopAndAwaitTerminated(context.Background(), client.pool))
	client.pool = pool

	return &fanoutFixture{client: client, gateways: gateways, ownership: NewIndexOwnership(r), reg: reg}
}

func (f *fanoutFixture) owners(t *testing.T, table string) ring.ReplicationSet {
	t.Helper()
	rs, err := f.ownership.Owners("tenant", table, IndexOwnersRead)
	require.NoError(t, err)
	require.Len(t, rs.Instances, ReplicationFactor)
	return rs
}

func TestPerIndex_RoutesEachPartToItsTableOwners(t *testing.T) {
	f := newFanoutFixture(t, nil)
	ctx := user.InjectOrgID(context.Background(), "tenant")
	const n = 20000

	// Three whole days, ending exactly at midnight: four tables.
	from, through := dayStart(n), dayStart(n+3)
	resp, err := f.client.GetChunkRef(ctx, &logproto.GetChunkRefRequest{From: from, Through: through, Matchers: `{a="b"}`})
	require.NoError(t, err)

	calls := f.gateways.recorded()
	require.Len(t, calls, 4)
	byTable := map[string]fanoutCall{}
	for _, c := range calls {
		require.Equal(t, "GetChunkRef", c.op)
		table := fmt.Sprintf("index_%d", int64(c.from/day))
		require.True(t, f.owners(t, table).Includes(c.addr), "%s sent to %s, which does not own it", table, c.addr)
		byTable[table] = c
	}
	for i, part := range splitByTable(from, through, f.client.cfg.TableRange) {
		require.Equal(t, part.From, byTable[part.Table].from, i)
		require.Equal(t, part.Through, byTable[part.Table].through, i)
	}

	// One ref per part, plus the one every part returned, deduplicated.
	require.Len(t, resp.Refs, 5)
	require.Equal(t, int64(5), resp.Stats.TotalChunks)
	require.Equal(t, int64(5), resp.Stats.PostFilterChunks)
	require.Equal(t, int64(8), resp.Stats.TotalStreams)

	tables := findMetric(t, f.reg, "loki_index_gateway_client_request_tables", map[string]string{"operation": "GetChunkRef"})
	require.NotNil(t, tables)
	require.Equal(t, uint64(1), tables.GetHistogram().GetSampleCount())
	require.Equal(t, float64(4), tables.GetHistogram().GetSampleSum())
	boundary := findMetric(t, f.reg, "loki_index_gateway_client_boundary_parts_total", map[string]string{"operation": "GetChunkRef"})
	require.NotNil(t, boundary)
	require.Equal(t, float64(1), boundary.GetCounter().GetValue())
}

func TestPerIndex_SingleTableSendsRequestUnchanged(t *testing.T) {
	const n = 20000
	// Table n-1 is outside this client's period, so only table n is read.
	f := newFanoutFixture(t, func(cfg *ClientConfig) { cfg.TableRange = testTableRange(n, n+10) })
	ctx := user.InjectOrgID(context.Background(), "tenant")

	from, through := dayStart(n)-1000, dayStart(n)+1000
	resp, err := f.client.GetStats(ctx, &logproto.IndexStatsRequest{From: from, Through: through, Matchers: `{a="b"}`})
	require.NoError(t, err)
	require.Equal(t, &logproto.IndexStatsResponse{Streams: 1, Chunks: 2, Bytes: 3, Entries: 4}, resp)

	calls := f.gateways.recorded()
	require.Len(t, calls, 1)
	require.Equal(t, from, calls[0].from)
	require.Equal(t, through, calls[0].through)
	require.True(t, f.owners(t, "index_20000").Includes(calls[0].addr))
	require.Nil(t, findMetric(t, f.reg, "loki_index_gateway_client_boundary_parts_total", nil))
}

func TestPerIndex_OutsideThePeriodUsesDefaultRouting(t *testing.T) {
	f := newFanoutFixture(t, func(cfg *ClientConfig) { cfg.TableRange = testTableRange(100, 200) })
	ctx := user.InjectOrgID(context.Background(), "tenant")

	_, err := f.client.GetSeries(ctx, &logproto.GetSeriesRequest{From: dayStart(20000), Through: dayStart(20001), Matchers: `{a="b"}`})
	require.NoError(t, err)
	require.Len(t, f.gateways.recorded(), 1)
	require.Nil(t, findMetric(t, f.reg, "loki_index_gateway_client_request_tables", nil))
}

func TestPerIndex_DefaultShardingIsUnchanged(t *testing.T) {
	f := newFanoutFixture(t, func(cfg *ClientConfig) { cfg.Sharding = ShardingDefault })
	ctx := user.InjectOrgID(context.Background(), "tenant")

	_, err := f.client.GetChunkRef(ctx, &logproto.GetChunkRefRequest{From: dayStart(20000), Through: dayStart(20003), Matchers: `{a="b"}`})
	require.NoError(t, err)
	calls := f.gateways.recorded()
	require.Len(t, calls, 1)
	require.Equal(t, dayStart(20000), calls[0].from)
	require.Equal(t, dayStart(20003), calls[0].through)

	addrs, err := f.client.getServerAddresses("tenant")
	require.NoError(t, err)
	require.Contains(t, addrs, calls[0].addr)

	require.Nil(t, findMetric(t, f.reg, "loki_index_gateway_client_request_tables", nil))
	require.Nil(t, findMetric(t, f.reg, "loki_index_gateway_client_get_shards_requests_total", nil))
}

func TestPerIndex_RetriesStayWithinOwners(t *testing.T) {
	f := newFanoutFixture(t, nil)
	ctx := user.InjectOrgID(context.Background(), "tenant")
	const n = 20000
	owners := f.owners(t, "index_20000").GetAddresses()

	t.Run("one owner down", func(t *testing.T) {
		f.gateways.reset()
		f.gateways.failing = map[string]bool{owners[0]: true}
		for range 20 {
			_, err := f.client.GetStats(ctx, &logproto.IndexStatsRequest{From: dayStart(n), Through: dayStart(n) + 1000, Matchers: `{a="b"}`})
			require.NoError(t, err)
		}
		for _, c := range f.gateways.recorded() {
			require.Contains(t, owners, c.addr)
		}
	})

	t.Run("every owner down", func(t *testing.T) {
		f.gateways.reset()
		f.gateways.failing = map[string]bool{}
		for _, o := range owners {
			f.gateways.failing[o] = true
		}
		_, err := f.client.GetStats(ctx, &logproto.IndexStatsRequest{From: dayStart(n), Through: dayStart(n) + 1000, Matchers: `{a="b"}`})
		require.Error(t, err)
		calls := f.gateways.recorded()
		require.Len(t, calls, len(owners))
		for _, c := range calls {
			require.Contains(t, owners, c.addr)
		}
	})

	t.Run("a failed part fails the request", func(t *testing.T) {
		f.gateways.reset()
		// Every owner of index_20000 is still down.
		_, err := f.client.GetStats(ctx, &logproto.IndexStatsRequest{From: dayStart(n - 2), Through: dayStart(n+2) + 1000, Matchers: `{a="b"}`})
		require.Error(t, err)
	})
}

func TestPerIndex_ConcurrencyIsCapped(t *testing.T) {
	f := newFanoutFixture(t, func(cfg *ClientConfig) { cfg.PerIndexMaxConcurrency = 2 })
	ctx := user.InjectOrgID(context.Background(), "tenant")
	const n = 20000

	block := make(chan struct{})
	f.gateways.block = block
	done := make(chan error)
	go func() {
		_, err := f.client.GetStats(ctx, &logproto.IndexStatsRequest{From: dayStart(n), Through: dayStart(n+5) - 1, Matchers: `{a="b"}`})
		done <- err
	}()

	require.Eventually(t, func() bool { return len(f.gateways.recorded()) == 2 }, 5*time.Second, time.Millisecond)
	// No third part starts while two are in flight.
	time.Sleep(50 * time.Millisecond)
	require.Len(t, f.gateways.recorded(), 2)
	close(block)

	require.NoError(t, <-done)
	require.Len(t, f.gateways.recorded(), 5)
	require.Equal(t, 2, f.gateways.maxInFlight)
}

func TestPerIndex_InFlightGateCountsParts(t *testing.T) {
	f := newFanoutFixture(t, func(cfg *ClientConfig) { cfg.MaxInFlightRequests = 2 })
	ctx := user.InjectOrgID(context.Background(), "tenant")
	const n = 20000

	// Hold the first two parts in flight, so the gate rejects the others.
	block := make(chan struct{})
	f.gateways.block = block
	done := make(chan error)
	go func() {
		_, err := f.client.GetStats(ctx, &logproto.IndexStatsRequest{From: dayStart(n), Through: dayStart(n+5) - 1, Matchers: `{a="b"}`})
		done <- err
	}()
	require.Eventually(t, func() bool { return len(f.gateways.recorded()) == 2 }, 5*time.Second, time.Millisecond)
	close(block)

	err := <-done
	require.Error(t, err)
	require.True(t, isServiceUnavailable(err), err)
	require.Len(t, f.gateways.recorded(), 2)
}

func TestPerIndex_GetShardsGoesToFirstTableOwners(t *testing.T) {
	f := newFanoutFixture(t, nil)
	ctx := user.InjectOrgID(context.Background(), "tenant")
	const n = 20000

	_, err := f.client.GetShards(ctx, &logproto.ShardsRequest{From: dayStart(n) + 1000, Through: dayStart(n + 3), Query: `{a="b"}`})
	require.NoError(t, err)
	calls := f.gateways.recorded()
	require.Len(t, calls, 1)
	require.Equal(t, dayStart(n)+1000, calls[0].from)
	require.Equal(t, dayStart(n+3), calls[0].through)
	require.True(t, f.owners(t, "index_20000").Includes(calls[0].addr))

	multi := findMetric(t, f.reg, "loki_index_gateway_client_get_shards_requests_total", map[string]string{"tables": "multi"})
	require.NotNil(t, multi)
	require.Equal(t, float64(1), multi.GetCounter().GetValue())
}

func TestPerIndex_RequiresRingMode(t *testing.T) {
	cfg := ClientConfig{}
	flagext.DefaultValues(&cfg)
	cfg.Mode = SimpleMode
	cfg.Address = "index-gateway"
	cfg.Sharding = ShardingPerIndex
	cfg.TableRange = testTableRange(0, 10)

	o, err := validation.NewOverrides(validation.Limits{}, nil)
	require.NoError(t, err)
	_, err = NewGatewayClient(cfg, nil, o, log.NewNopLogger(), constants.Loki)
	require.ErrorContains(t, err, "requires index-gateway.mode=ring")
}

func TestMergeChunkRefResponses(t *testing.T) {
	a := &logproto.ChunkRef{Fingerprint: 1, UserID: "t", From: 1, Through: 10, Checksum: 1}
	b := &logproto.ChunkRef{Fingerprint: 2, UserID: "t", From: 1, Through: 10, Checksum: 2}
	spanning := &logproto.ChunkRef{Fingerprint: 1, UserID: "t", From: 5, Through: 20, Checksum: 3}

	got := mergeChunkRefResponses([]*logproto.GetChunkRefResponse{
		{Refs: []*logproto.ChunkRef{a, spanning}, Stats: stats.Index{TotalChunks: 3, PostFilterChunks: 2, TotalStreams: 1, UsedBloomFilters: true}},
		{Refs: []*logproto.ChunkRef{b, {Fingerprint: 1, UserID: "t", From: 5, Through: 20, Checksum: 3}}, Stats: stats.Index{TotalChunks: 2, PostFilterChunks: 2, TotalStreams: 2}},
	})
	require.Equal(t, []*logproto.ChunkRef{a, spanning, b}, got.Refs)
	require.Equal(t, int64(4), got.Stats.TotalChunks)
	require.Equal(t, int64(3), got.Stats.PostFilterChunks)
	require.Equal(t, int64(3), got.Stats.TotalStreams)
	require.True(t, got.Stats.UsedBloomFilters)
}

func TestMergeSeriesResponses(t *testing.T) {
	s := func(kv ...string) logproto.IndexSeries {
		var ls []logproto.LabelAdapter
		for i := 0; i < len(kv); i += 2 {
			ls = append(ls, logproto.LabelAdapter{Name: kv[i], Value: kv[i+1]})
		}
		return logproto.IndexSeries{Labels: ls}
	}
	got := mergeSeriesResponses([]*logproto.GetSeriesResponse{
		{Series: []logproto.IndexSeries{s("a", "1"), s("a", "2")}},
		{Series: []logproto.IndexSeries{s("a", "2"), s("a", "3")}},
	})
	require.Equal(t, []logproto.IndexSeries{s("a", "1"), s("a", "2"), s("a", "3")}, got.Series)
}

func TestMergeStatsAndVolumeResponses(t *testing.T) {
	require.Equal(t, &logproto.IndexStatsResponse{Streams: 3, Chunks: 5, Bytes: 7, Entries: 9}, mergeStatsResponses([]*logproto.IndexStatsResponse{
		{Streams: 1, Chunks: 2, Bytes: 3, Entries: 4},
		{Streams: 2, Chunks: 3, Bytes: 4, Entries: 5},
	}))

	got := mergeVolumeResponses([]*logproto.VolumeResponse{
		{Volumes: []logproto.Volume{{Name: `{a="1"}`, Volume: 10}, {Name: `{a="2"}`, Volume: 5}}, Limit: 2},
		{Volumes: []logproto.Volume{{Name: `{a="3"}`, Volume: 8}, {Name: `{a="2"}`, Volume: 4}}, Limit: 2},
	}, 2)
	require.Equal(t, []logproto.Volume{{Name: `{a="1"}`, Volume: 10}, {Name: `{a="2"}`, Volume: 9}}, got.Volumes)
}
