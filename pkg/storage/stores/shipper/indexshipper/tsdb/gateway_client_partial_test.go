package tsdb

import (
	"context"
	"fmt"
	"math"
	"net"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/indexgateway"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"
)

// Unlike the shared-index fixtures, each gateway here has only its own files.
// A separate reference gateway has all files and is excluded from the ring.
func newPartialFanoutEnv(t *testing.T, k int) *fanoutEnv {
	t.Helper()
	logger := log.NewNopLogger()
	tableRange := fanoutTableRange()
	limits, err := validation.NewOverrides(validation.Limits{IndexGatewayShardSize: 0, VolumeMaxSeries: 1000, TSDBMaxBytesPerShard: 1 << 30, TSDBIndexShardFactor: k, TSDBPrecomputeChunks: true}, nil)
	require.NoError(t, err)
	files := buildFanoutShipperWithShards(t, fanoutSeries(), tableRange, k)
	var listeners []net.Listener
	var addrs []string
	for range 7 {
		lis, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		listeners = append(listeners, lis)
		addrs = append(addrs, lis.Addr().String())
	}
	r := newFanoutRing(t, addrs[:6])
	ownership := indexgateway.NewIndexOwnership(r)
	calls := &fanoutCalls{}
	missing := 0
	for gi, lis := range listeners {
		local := fanoutShipper{}
		for table, indices := range files {
			for _, file := range indices {
				owned := gi == 6
				if !owned {
					shard := file.shard
					if k == 1 {
						shard = indexgateway.WholeIndexShard
					}
					owned, err = ownership.Owns(addrs[gi], fanoutTenant, table, shard, indexgateway.IndexOwnersRead)
					require.NoError(t, err)
				}
				if owned {
					local[table] = append(local[table], file)
				} else {
					missing++
				}
			}
		}
		idx := newIndexShipperQuerier(local, tableRange)
		gw, err := indexgateway.NewIndexGateway(indexgateway.Config{}, limits, logger, nil, fanoutQuerier{NewIndexClient(idx, DefaultIndexClientOptions(), limits)}, nil, nil)
		require.NoError(t, err)
		addr := addrs[gi]
		srv := grpc.NewServer(grpc.ChainUnaryInterceptor(middleware.ServerUserHeaderInterceptor), grpc.ChainStreamInterceptor(middleware.StreamServerUserHeaderInterceptor, func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			return handler(srv, &recordingStream{ServerStream: ss, record: func(req any) { calls.record(addr, info.FullMethod, req) }})
		}))
		logproto.RegisterIndexGatewayServer(srv, gw)
		grpc_health_v1.RegisterHealthServer(srv, servingHealth{})
		go func() { _ = srv.Serve(lis) }()
		t.Cleanup(srv.Stop)
	}
	require.Positive(t, missing, "fixture must have genuinely partial local indexes")
	makeClient := func(reference bool) *indexgateway.GatewayClient {
		cfg := indexgateway.ClientConfig{}
		flagext.DefaultValues(&cfg)
		cfg.Mode = indexgateway.RingMode
		cfg.Ring = r
		cfg.Sharding = indexgateway.ShardingPerIndex
		cfg.TableRange = tableRange
		if reference {
			cfg.Mode = indexgateway.SimpleMode
			cfg.Sharding = indexgateway.ShardingDefault
			cfg.Address = addrs[6]
		}
		c, err := indexgateway.NewGatewayClient(cfg, prometheus.NewRegistry(), limits, logger, constants.Loki)
		require.NoError(t, err)
		t.Cleanup(c.Stop)
		return c
	}
	return &fanoutEnv{perIndex: makeClient(false), single: makeClient(true), ownership: ownership, calls: calls, tableRange: tableRange, shardFactor: k}
}

func TestGatewayClientMultiTablePlanWithPartialLocalIndexes(t *testing.T) {
	for _, k := range []int{1, 4} {
		t.Run(fmt.Sprintf("factor=%d", k), func(t *testing.T) {
			e := newPartialFanoutEnv(t, k)
			ctx := user.InjectOrgID(context.Background(), fanoutTenant)
			for _, target := range []uint64{1, 1 << 30} {
				t.Run(fmt.Sprintf("target=%d", target), func(t *testing.T) {
					req := &logproto.ShardsRequest{From: fanoutDayStart(0), Through: fanoutDayStart(3), Query: `{app=~".+"}`, TargetBytesPerShard: target}
					var want logproto.IndexStatsResponse
					for _, part := range fanoutParts(req.From, req.Through, e.tableRange) {
						r := *req
						r.From, r.Through = part[0], part[1]
						plan, err := e.single.GetShards(ctx, &r)
						require.NoError(t, err)
						sum := sumShardStats(plan.Shards)
						want.Streams += sum.Streams
						want.Chunks += sum.Chunks
						want.Bytes += sum.Bytes
						want.Entries += sum.Entries
					}
					e.calls.take()
					plan, err := e.perIndex.GetShards(ctx, req)
					require.NoError(t, err)
					require.Equal(t, want, sumShardStats(plan.Shards))
					require.Empty(t, plan.ChunkGroups, "partial precomputed references must not escape into execution")
					require.Len(t, e.calls.take(), len(fanoutParts(req.From, req.Through, e.tableRange))*k)
					require.NotEmpty(t, plan.Shards)
					require.Equal(t, model.Fingerprint(0), plan.Shards[0].Bounds.Min)
					require.Equal(t, model.Fingerprint(math.MaxUint64), plan.Shards[len(plan.Shards)-1].Bounds.Max)
					for i := 1; i < len(plan.Shards); i++ {
						require.Equal(t, plan.Shards[i-1].Bounds.Max+1, plan.Shards[i].Bounds.Min)
					}
					whole, err := e.single.GetChunkRef(ctx, &logproto.GetChunkRefRequest{From: req.From, Through: req.Through, Matchers: req.Query})
					require.NoError(t, err)
					var got []*logproto.ChunkRef
					for _, shard := range plan.Shards {
						refs, err := e.perIndex.GetChunkRef(ctx, &logproto.GetChunkRefRequest{From: req.From, Through: req.Through, Matchers: fmt.Sprintf(`{app=~".+",__cortex_shard__=%q}`, logql.NewBoundedShard(shard).String())})
						require.NoError(t, err)
						got = append(got, refs.Refs...)
					}
					require.ElementsMatch(t, whole.Refs, got)
				})
			}
		})
	}
}
