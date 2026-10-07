package tsdb

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/indexgateway"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	shipperindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"
)

// These tests check that an index gateway client with per_index sharding,
// which splits each request by table and merges the answers, returns what one
// request answered by one index gateway returns. The index gateways are real,
// over real TSDB files, one set per table, built the way ingesters build them:
// each chunk is indexed in every table its time range overlaps.

const (
	fanoutTenant = "fake"
	fanoutBase   = 20000 // first table number of the fixture
)

var fanoutDay = model.Time(config.ObjectStorageIndexRequiredPeriod / time.Millisecond)

func fanoutDayStart(n int) model.Time { return model.Time(fanoutBase+n) * fanoutDay }

var (
	fanoutHour   = model.Time(time.Hour / time.Millisecond)
	fanoutMinute = model.Time(time.Minute / time.Millisecond)
)

// fanoutParts returns the part of [from, through] each table that
// IndexBuckets picks covers: what the per_index client should send.
func fanoutParts(from, through model.Time, tableRange config.TableRange) [][2]model.Time {
	var parts [][2]model.Time
	for _, b := range IndexBuckets(from, through, config.TableRanges{tableRange}) {
		parts = append(parts, [2]model.Time{max(from, b.BucketStart), min(through, b.BucketStart+fanoutDay-1)})
	}
	return parts
}

// fanoutSeries is the fixture: four days, with chunks within a day, chunks
// spanning one or two midnights, a chunk starting exactly at midnight and a
// single-sample chunk exactly at midnight.
func fanoutSeries() []LoadableSeries {
	var checksum uint32
	chk := func(from, through model.Time) index.ChunkMeta {
		checksum++
		return index.ChunkMeta{MinTime: int64(from), MaxTime: int64(through), Checksum: checksum, KB: 10 + checksum, Entries: 100 + checksum}
	}
	d := fanoutDayStart
	return []LoadableSeries{
		{
			Labels: labels.FromStrings("app", "a", "env", "prod"),
			Chunks: index.ChunkMetas{
				chk(d(0)+fanoutHour, d(0)+2*fanoutHour),
				chk(d(0)+23*fanoutHour, d(1)+fanoutHour), // spans midnight
				chk(d(1)+5*fanoutHour, d(1)+6*fanoutHour),
				chk(d(2)+5*fanoutHour, d(2)+6*fanoutHour),
				chk(d(3)+5*fanoutHour, d(3)+6*fanoutHour),
			},
		},
		{
			Labels: labels.FromStrings("app", "b", "env", "dev"),
			Chunks: index.ChunkMetas{
				chk(d(1), d(1)),            // a single sample at midnight
				chk(d(2), d(2)+fanoutHour), // starts at midnight
				chk(d(0)+3*fanoutHour, d(0)+4*fanoutHour),
			},
		},
		{
			Labels: labels.FromStrings("app", "c", "env", "prod", "pod", "x"),
			Chunks: index.ChunkMetas{
				chk(d(3)+fanoutHour, d(3)+2*fanoutHour),
			},
		},
		{
			Labels: labels.FromStrings("app", "a", "env", "dev"),
			Chunks: index.ChunkMetas{
				chk(d(1)+23*fanoutHour, d(3)+fanoutHour), // spans two midnights
			},
		},
	}
}

func fanoutTableRange() config.TableRange {
	return config.TableRange{
		Start: fanoutBase - 10,
		End:   fanoutBase + 10,
		PeriodConfig: &config.PeriodConfig{
			IndexType: "tsdb",
			Schema:    "v13",
			IndexTables: config.IndexPeriodicTableConfig{
				PeriodicTableConfig: config.PeriodicTableConfig{Prefix: "index_", Period: config.ObjectStorageIndexRequiredPeriod},
			},
		},
	}
}

// fanoutShipper serves the fixture's TSDB files by table.
type fanoutShipper map[string][]*TSDBFile

func (s fanoutShipper) ForEachConcurrent(_ context.Context, table, _ string, callback shipperindex.ForEachIndexCallback) error {
	for _, f := range s[table] {
		if err := callback(false, f); err != nil {
			return err
		}
	}
	return nil
}

// buildFanoutShipper indexes each chunk of series in every table its time
// range overlaps, as ingesters do.
func buildFanoutShipper(t *testing.T, series []LoadableSeries, tableRange config.TableRange) fanoutShipper {
	byTable := map[string][]LoadableSeries{}
	for _, s := range series {
		perTable := map[string]index.ChunkMetas{}
		for _, c := range s.Chunks {
			for _, b := range IndexBuckets(model.Time(c.MinTime), model.Time(c.MaxTime), config.TableRanges{tableRange}) {
				perTable[b.Prefix] = append(perTable[b.Prefix], c)
			}
		}
		for table, chks := range perTable {
			byTable[table] = append(byTable[table], LoadableSeries{Labels: s.Labels, Chunks: chks})
		}
	}

	shipper := fanoutShipper{}
	for table, ss := range byTable {
		shipper[table] = []*TSDBFile{BuildIndex(t, t.TempDir(), ss)}
	}
	return shipper
}

// fanoutQuerier is the index gateway's view of the index, as the TSDB store
// builds it: an IndexClient over an indexShipperQuerier, which reads the tables
// IndexBuckets picks, with GetChunks keeping the chunks the store's time range
// filter keeps.
type fanoutQuerier struct {
	*IndexClient
}

func (q fanoutQuerier) GetChunks(ctx context.Context, userID string, from, through model.Time, predicate chunk.Predicate, _ *logproto.ChunkRefGroup) ([][]chunk.Chunk, []*fetcher.Fetcher, error) {
	refs, err := q.GetChunkRefs(ctx, userID, from, through, predicate)
	if err != nil {
		return nil, nil, err
	}
	// Same rule as filterForTimeRange in pkg/storage/stores.
	chunks := make([]chunk.Chunk, 0, len(refs))
	for _, ref := range refs {
		if (through >= ref.From && from < ref.Through) || (ref.From == from && ref.Through == from) {
			chunks = append(chunks, chunk.Chunk{ChunkRef: ref})
		}
	}
	return [][]chunk.Chunk{chunks}, []*fetcher.Fetcher{nil}, nil
}

func (q fanoutQuerier) Stop() {}

// fanoutCalls records which index gateway received which request.
type fanoutCalls struct {
	mu    sync.Mutex
	calls []fanoutCall
}

type fanoutCall struct {
	addr, method  string
	from, through model.Time
}

func (c *fanoutCalls) record(addr, method string, req any) {
	call := fanoutCall{addr: addr, method: method}
	switch r := req.(type) {
	case *logproto.GetChunkRefRequest:
		call.from, call.through = r.From, r.Through
	case *logproto.GetSeriesRequest:
		call.from, call.through = r.From, r.Through
	case *logproto.LabelNamesForMetricNameRequest:
		call.from, call.through = r.From, r.Through
	case *logproto.LabelValuesForMetricNameRequest:
		call.from, call.through = r.From, r.Through
	case *logproto.IndexStatsRequest:
		call.from, call.through = r.From, r.Through
	case *logproto.VolumeRequest:
		call.from, call.through = r.From, r.Through
	case *logproto.ShardsRequest:
		call.from, call.through = r.From, r.Through
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, call)
}

func (c *fanoutCalls) take() []fanoutCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	calls := c.calls
	c.calls = nil
	return calls
}

type fanoutEnv struct {
	perIndex, single *indexgateway.GatewayClient
	ownership        *indexgateway.IndexOwnership
	calls            *fanoutCalls
	tableRange       config.TableRange
}

func newFanoutEnv(t *testing.T) *fanoutEnv {
	t.Helper()
	logger := log.NewNopLogger()
	tableRange := fanoutTableRange()

	limits, err := validation.NewOverrides(validation.Limits{IndexGatewayShardSize: 0, VolumeMaxSeries: 1000, TSDBMaxBytesPerShard: 1 << 30}, nil)
	require.NoError(t, err)

	idx := newIndexShipperQuerier(buildFanoutShipper(t, fanoutSeries(), tableRange), tableRange)
	gw, err := indexgateway.NewIndexGateway(indexgateway.Config{}, limits, logger, nil, fanoutQuerier{NewIndexClient(idx, DefaultIndexClientOptions(), limits)}, nil, nil)
	require.NoError(t, err)

	// Six index gateways, all answering from the same index.
	calls := &fanoutCalls{}
	var addrs []string
	for range 6 {
		lis, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		addr := lis.Addr().String()
		srv := grpc.NewServer(
			grpc.ChainUnaryInterceptor(middleware.ServerUserHeaderInterceptor, func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
				if info.FullMethod != grpc_health_v1.Health_Check_FullMethodName {
					calls.record(addr, info.FullMethod, req)
				}
				return handler(ctx, req)
			}),
			grpc.ChainStreamInterceptor(middleware.StreamServerUserHeaderInterceptor, func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
				return handler(srv, &recordingStream{ServerStream: ss, record: func(req any) { calls.record(addr, info.FullMethod, req) }})
			}),
		)
		logproto.RegisterIndexGatewayServer(srv, gw)
		grpc_health_v1.RegisterHealthServer(srv, servingHealth{})
		go func() { _ = srv.Serve(lis) }()
		t.Cleanup(srv.Stop)
		addrs = append(addrs, addr)
	}

	r := newFanoutRing(t, addrs)
	newClient := func(sharding string) *indexgateway.GatewayClient {
		cfg := indexgateway.ClientConfig{}
		flagext.DefaultValues(&cfg)
		cfg.Mode = indexgateway.RingMode
		cfg.Ring = r
		cfg.Sharding = sharding
		cfg.TableRange = tableRange
		c, err := indexgateway.NewGatewayClient(cfg, prometheus.NewRegistry(), limits, logger, constants.Loki)
		require.NoError(t, err)
		t.Cleanup(c.Stop)
		return c
	}

	return &fanoutEnv{
		perIndex:   newClient(indexgateway.ShardingPerIndex),
		single:     newClient(indexgateway.ShardingDefault),
		ownership:  indexgateway.NewIndexOwnership(r),
		calls:      calls,
		tableRange: tableRange,
	}
}

// servingHealth reports every service as serving.
type servingHealth struct {
	grpc_health_v1.UnimplementedHealthServer
}

func (servingHealth) Check(context.Context, *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

type recordingStream struct {
	grpc.ServerStream
	record func(any)
}

func (s *recordingStream) RecvMsg(m any) error {
	err := s.ServerStream.RecvMsg(m)
	if err == nil {
		s.record(m)
	}
	return err
}

func newFanoutRing(t *testing.T, addrs []string) *ring.Ring {
	t.Helper()
	logger := log.NewNopLogger()
	descs := map[string]ring.InstanceDesc{}
	rnd := rand.New(rand.NewSource(1))
	for _, addr := range addrs {
		tokens := make([]uint32, 0, 16)
		for range 16 {
			tokens = append(tokens, rnd.Uint32())
		}
		slices.Sort(tokens)
		descs[addr] = ring.InstanceDesc{
			Addr:                addr,
			State:               ring.ACTIVE,
			Timestamp:           time.Now().Unix(),
			RegisteredTimestamp: time.Now().Add(-time.Hour).Unix(),
			Tokens:              tokens,
		}
	}

	kvStore, closer := consul.NewInMemoryClient(ring.GetCodec(), logger, nil)
	t.Cleanup(func() { _ = closer.Close() })
	require.NoError(t, kvStore.CAS(context.Background(), "ring", func(any) (any, bool, error) {
		return &ring.Desc{Ingesters: descs}, true, nil
	}))
	r, err := ring.New(ring.Config{KVStore: kv.Config{Mock: kvStore}, HeartbeatTimeout: time.Hour, ReplicationFactor: 2}, "indexgateway", "ring", logger, nil)
	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), r))
	t.Cleanup(func() { _ = services.StopAndAwaitTerminated(context.Background(), r) })
	require.Eventually(t, func() bool { return r.InstancesCount() == len(addrs) }, time.Minute, 10*time.Millisecond)
	return r
}

// fanoutRanges are the query ranges every RPC is checked over.
func fanoutRanges() map[string][2]model.Time {
	d := fanoutDayStart
	return map[string][2]model.Time{
		"within a day":                    {d(1) + 4*fanoutHour, d(1) + 8*fanoutHour},
		"whole day, end exclusive":        {d(1), d(2) - 1},
		"day ending at midnight":          {d(1), d(2)}, // reads table 2 only for the chunk starting at d(2)
		"midnight only":                   {d(1), d(1)},
		"across midnights":                {d(0) + 22*fanoutHour, d(3) + 2*fanoutHour},
		"every table, ending at midnight": {d(0), d(4)},
		"from inside a spanning chunk":    {d(1) + 30*fanoutMinute, d(2) + 12*fanoutHour},
	}
}

var fanoutMatchers = []string{`{app=~".+"}`, `{app="a"}`, `{env="prod"}`, `{app="b"}`}

// checkCallsGoToOwners checks that the per_index client sent each part to an
// owner of its table, and returns the number of calls.
func (e *fanoutEnv) checkCallsGoToOwners(t *testing.T) int {
	t.Helper()
	calls := e.calls.take()
	for _, c := range calls {
		table := fmt.Sprintf("index_%d", int64(c.from/fanoutDay))
		rs, err := e.ownership.Owners(fanoutTenant, table, indexgateway.IndexOwnersRead)
		require.NoError(t, err)
		require.True(t, rs.Includes(c.addr), "%s [%d, %d] for %s sent to %s, not an owner", c.method, c.from, c.through, table, c.addr)
	}
	return len(calls)
}

func TestGatewayClientPerIndex_ChunkRefsMatchSingleCall(t *testing.T) {
	e := newFanoutEnv(t)
	ctx := user.InjectOrgID(context.Background(), fanoutTenant)

	for name, r := range fanoutRanges() {
		for _, m := range fanoutMatchers {
			t.Run(name+"/"+m, func(t *testing.T) {
				req := &logproto.GetChunkRefRequest{From: r[0], Through: r[1], Matchers: m}
				want, err := e.single.GetChunkRef(ctx, req)
				require.NoError(t, err)
				e.calls.take()

				got, err := e.perIndex.GetChunkRef(ctx, req)
				require.NoError(t, err)
				require.ElementsMatch(t, want.Refs, got.Refs)
				require.Equal(t, int64(len(want.Refs)), got.Stats.TotalChunks)
				require.Equal(t, int64(len(want.Refs)), got.Stats.PostFilterChunks)
				require.Equal(t, len(fanoutParts(r[0], r[1], e.tableRange)), e.checkCallsGoToOwners(t))
			})
		}
	}
}

func TestGatewayClientPerIndex_SeriesAndLabelsMatchSingleCall(t *testing.T) {
	e := newFanoutEnv(t)
	ctx := user.InjectOrgID(context.Background(), fanoutTenant)

	for name, r := range fanoutRanges() {
		for _, m := range fanoutMatchers {
			t.Run(name+"/"+m, func(t *testing.T) {
				seriesReq := &logproto.GetSeriesRequest{From: r[0], Through: r[1], Matchers: m}
				wantSeries, err := e.single.GetSeries(ctx, seriesReq)
				require.NoError(t, err)
				gotSeries, err := e.perIndex.GetSeries(ctx, seriesReq)
				require.NoError(t, err)
				require.ElementsMatch(t, wantSeries.Series, gotSeries.Series)

				namesReq := &logproto.LabelNamesForMetricNameRequest{From: r[0], Through: r[1], MetricName: "logs", Matchers: m}
				wantNames, err := e.single.LabelNamesForMetricName(ctx, namesReq)
				require.NoError(t, err)
				gotNames, err := e.perIndex.LabelNamesForMetricName(ctx, namesReq)
				require.NoError(t, err)
				require.ElementsMatch(t, wantNames.Values, gotNames.Values)

				valuesReq := &logproto.LabelValuesForMetricNameRequest{From: r[0], Through: r[1], MetricName: "logs", LabelName: "env", Matchers: m}
				wantValues, err := e.single.LabelValuesForMetricName(ctx, valuesReq)
				require.NoError(t, err)
				gotValues, err := e.perIndex.LabelValuesForMetricName(ctx, valuesReq)
				require.NoError(t, err)
				require.ElementsMatch(t, wantValues.Values, gotValues.Values)
			})
		}
	}
	e.calls.take()
}

// Stats and volumes are summed over tables, so a stream or chunk found in
// several tables is counted once per table. They equal the sum of single
// calls over each table's part.
func TestGatewayClientPerIndex_StatsAndVolumeSumPerTable(t *testing.T) {
	e := newFanoutEnv(t)
	ctx := user.InjectOrgID(context.Background(), fanoutTenant)

	for name, r := range fanoutRanges() {
		for _, m := range fanoutMatchers {
			t.Run(name+"/"+m, func(t *testing.T) {
				parts := fanoutParts(r[0], r[1], e.tableRange)

				single, err := e.single.GetStats(ctx, &logproto.IndexStatsRequest{From: r[0], Through: r[1], Matchers: m})
				require.NoError(t, err)
				var wantStats logproto.IndexStatsResponse
				var partVolumes []*logproto.VolumeResponse
				for _, p := range parts {
					s, err := e.single.GetStats(ctx, &logproto.IndexStatsRequest{From: p[0], Through: p[1], Matchers: m})
					require.NoError(t, err)
					wantStats.Streams += s.Streams
					wantStats.Chunks += s.Chunks
					wantStats.Bytes += s.Bytes
					wantStats.Entries += s.Entries

					v, err := e.single.GetVolume(ctx, &logproto.VolumeRequest{From: p[0], Through: p[1], Matchers: m, Limit: 100, AggregateBy: "series"})
					require.NoError(t, err)
					partVolumes = append(partVolumes, v)
				}

				got, err := e.perIndex.GetStats(ctx, &logproto.IndexStatsRequest{From: r[0], Through: r[1], Matchers: m})
				require.NoError(t, err)
				if len(parts) == 1 {
					require.Equal(t, single, got)
				} else {
					require.Equal(t, &wantStats, got)
				}
				// TSDB weighs each chunk's stats by the share of its time range
				// inside the query. The part for a through exactly at midnight is
				// one millisecond long, so it counts nothing for chunks that start
				// there, where a single request counts them with next to no bytes
				// and entries. Apart from that case, only streams differ.
				if through := r[1]; through%fanoutDay != 0 || len(parts) == 1 {
					require.Equal(t, single.Chunks, got.Chunks)
					require.Equal(t, single.Bytes, got.Bytes)
					require.Equal(t, single.Entries, got.Entries)
					require.GreaterOrEqual(t, got.Streams, single.Streams)
				}

				gotVolume, err := e.perIndex.GetVolume(ctx, &logproto.VolumeRequest{From: r[0], Through: r[1], Matchers: m, Limit: 100, AggregateBy: "series"})
				require.NoError(t, err)
				wantVolume := partVolumes[0]
				if len(parts) > 1 {
					wantVolume = seriesvolume.Merge(partVolumes, 100)
				}
				require.Equal(t, wantVolume.Volumes, gotVolume.Volumes)
			})
		}
	}

	t.Run("a stream spanning days is counted once per day", func(t *testing.T) {
		req := &logproto.IndexStatsRequest{From: fanoutDayStart(0), Through: fanoutDayStart(2) - 1, Matchers: `{app="a", env="prod"}`}
		single, err := e.single.GetStats(ctx, req)
		require.NoError(t, err)
		got, err := e.perIndex.GetStats(ctx, req)
		require.NoError(t, err)
		require.Equal(t, uint64(1), single.Streams)
		require.Equal(t, uint64(2), got.Streams)
	})
	e.calls.take()
}

func TestGatewayClientPerIndex_ShardsGoWholeToFirstTableOwners(t *testing.T) {
	e := newFanoutEnv(t)
	ctx := user.InjectOrgID(context.Background(), fanoutTenant)

	req := &logproto.ShardsRequest{From: fanoutDayStart(0) + fanoutHour, Through: fanoutDayStart(3), Query: `{app=~".+"}`, TargetBytesPerShard: 1 << 20}
	want, err := e.single.GetShards(ctx, req)
	require.NoError(t, err)
	e.calls.take()

	got, err := e.perIndex.GetShards(ctx, req)
	require.NoError(t, err)
	// Timings differ between calls.
	want.Statistics.Index.ShardsDuration, got.Statistics.Index.ShardsDuration = 0, 0
	require.Equal(t, want, got)

	calls := e.calls.take()
	require.Len(t, calls, 1)
	require.Equal(t, req.From, calls[0].from)
	require.Equal(t, req.Through, calls[0].through)
	rs, err := e.ownership.Owners(fanoutTenant, fmt.Sprintf("index_%d", fanoutBase), indexgateway.IndexOwnersRead)
	require.NoError(t, err)
	require.True(t, rs.Includes(calls[0].addr))
}
