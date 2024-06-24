package distributor

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/distributor/shardstreams"
	"github.com/grafana/loki/v3/pkg/validation"

	"github.com/stretchr/testify/require"

	client2 "github.com/grafana/loki/v3/pkg/ingester/client"

	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/ring/client"
)

func TestRateStore(t *testing.T) {
	t.Run("it reports rates and pushes per second from all of the ingesters", func(t *testing.T) {
		tc := setup(true)
		tc.ring.replicationSet = ring.ReplicationSet{
			Instances: []ring.InstanceDesc{
				{Addr: "ingester0"},
				{Addr: "ingester1"},
				{Addr: "ingester2"},
				{Addr: "ingester3"},
			},
		}

		tc.clientPool.clients = map[string]client.PoolClient{
			"ingester0": newRateClient([]*logproto.StreamRate{
				{Tenant: "tenant 1", StreamHash: 0, StreamHashNoShard: 0, Rate: 15, Pushes: 10},
				{Tenant: "tenant 2", StreamHash: 0, StreamHashNoShard: 0, Rate: 15, Pushes: 10},
			}),
			"ingester1": newRateClient([]*logproto.StreamRate{
				{Tenant: "tenant 1", StreamHash: 1, StreamHashNoShard: 1, Rate: 25, Pushes: 20},
				{Tenant: "tenant 2", StreamHash: 1, StreamHashNoShard: 1, Rate: 25, Pushes: 20},
			}),
			"ingester2": newRateClient([]*logproto.StreamRate{
				{Tenant: "tenant 1", StreamHash: 2, StreamHashNoShard: 2, Rate: 35, Pushes: 30},
				{Tenant: "tenant 2", StreamHash: 2, StreamHashNoShard: 2, Rate: 35, Pushes: 30},
			}),
			"ingester3": newRateClient([]*logproto.StreamRate{
				{Tenant: "tenant 1", StreamHash: 3, StreamHashNoShard: 3, Rate: 45, Pushes: 40},
				{Tenant: "tenant 2", StreamHash: 3, StreamHashNoShard: 3, Rate: 45, Pushes: 40},
			}),
		}

		require.NoError(t, tc.rateStore.instrumentedUpdateAllRates(context.Background()))

		requireRatesAndPushesEqual(t, 15, 10, tc.rateStore, "tenant 1", 0)
		requireRatesAndPushesEqual(t, 25, 20, tc.rateStore, "tenant 1", 1)
		requireRatesAndPushesEqual(t, 35, 30, tc.rateStore, "tenant 1", 2)
		requireRatesAndPushesEqual(t, 45, 40, tc.rateStore, "tenant 1", 3)

		requireRatesAndPushesEqual(t, 15, 10, tc.rateStore, "tenant 2", 0)
		requireRatesAndPushesEqual(t, 25, 20, tc.rateStore, "tenant 2", 1)
		requireRatesAndPushesEqual(t, 35, 30, tc.rateStore, "tenant 2", 2)
		requireRatesAndPushesEqual(t, 45, 40, tc.rateStore, "tenant 2", 3)
	})

	t.Run("it reports the highest rate from replicas", func(t *testing.T) {
		tc := setup(true)
		tc.ring.replicationSet = ring.ReplicationSet{
			Instances: []ring.InstanceDesc{
				{Addr: "ingester0"},
				{Addr: "ingester1"},
				{Addr: "ingester2"},
			},
		}

		tc.clientPool.clients = map[string]client.PoolClient{
			"ingester0": newRateClient([]*logproto.StreamRate{
				{Tenant: "tenant 1", StreamHash: 0, StreamHashNoShard: 0, Rate: 25, Pushes: 35},
				{Tenant: "tenant 2", StreamHash: 0, StreamHashNoShard: 0, Rate: 25, Pushes: 35},
			}),
			"ingester1": newRateClient([]*logproto.StreamRate{
				{Tenant: "tenant 1", StreamHash: 0, StreamHashNoShard: 0, Rate: 35, Pushes: 10},
				{Tenant: "tenant 2", StreamHash: 0, StreamHashNoShard: 0, Rate: 35, Pushes: 10},
			}),
			"ingester2": newRateClient([]*logproto.StreamRate{
				{Tenant: "tenant 1", StreamHash: 0, StreamHashNoShard: 0, Rate: 15, Pushes: 25},
				{Tenant: "tenant 2", StreamHash: 0, StreamHashNoShard: 0, Rate: 15, Pushes: 25},
			}),
		}

		require.NoError(t, tc.rateStore.instrumentedUpdateAllRates(context.Background()))

		requireRatesAndPushesEqual(t, 35, 10, tc.rateStore, "tenant 1", 0)
		requireRatesAndPushesEqual(t, 35, 10, tc.rateStore, "tenant 2", 0)
	})

	t.Run("it aggregates rates but gets the max number of pushes over shards", func(t *testing.T) {
		tc := setup(true)
		tc.ring.replicationSet = ring.ReplicationSet{
			Instances: []ring.InstanceDesc{
				{Addr: "ingester0"},
			},
		}

		tc.clientPool.clients = map[string]client.PoolClient{
			"ingester0": newRateClient([]*logproto.StreamRate{
				{Tenant: "tenant 1", StreamHash: 1, StreamHashNoShard: 0, Rate: 25, Pushes: 10},
				{Tenant: "tenant 1", StreamHash: 2, StreamHashNoShard: 0, Rate: 35, Pushes: 20},
				{Tenant: "tenant 1", StreamHash: 3, StreamHashNoShard: 0, Rate: 15, Pushes: 30},
				{Tenant: "tenant 2", StreamHash: 1, StreamHashNoShard: 0, Rate: 25, Pushes: 10},
				{Tenant: "tenant 2", StreamHash: 2, StreamHashNoShard: 0, Rate: 35, Pushes: 20},
				{Tenant: "tenant 2", StreamHash: 3, StreamHashNoShard: 0, Rate: 15, Pushes: 30},
			}),
		}

		require.NoError(t, tc.rateStore.instrumentedUpdateAllRates(context.Background()))

		requireRatesAndPushesEqual(t, 75, 30, tc.rateStore, "tenant 1", 0)
		requireRatesAndPushesEqual(t, 75, 30, tc.rateStore, "tenant 2", 0)
	})

	t.Run("it does nothing if no one has enabled sharding", func(t *testing.T) {
		tc := setup(false)
		tc.ring.replicationSet = ring.ReplicationSet{
			Instances: []ring.InstanceDesc{
				{Addr: "ingester0"},
			},
		}

		tc.clientPool.clients = map[string]client.PoolClient{
			"ingester0": newRateClient([]*logproto.StreamRate{
				{Tenant: "tenant 1", StreamHash: 1, StreamHashNoShard: 0, Rate: 25},
			}),
		}

		require.NoError(t, tc.rateStore.instrumentedUpdateAllRates(context.Background()))
		requireRatesAndPushesEqual(t, 0, 0, tc.rateStore, "tenant 1", 0)
	})

	t.Run("it clears the rate after an interval", func(t *testing.T) {
		tc := setup(true)
		tc.ring.replicationSet = ring.ReplicationSet{
			Instances: []ring.InstanceDesc{
				{Addr: "ingester0"},
			},
		}

		tc.clientPool.clients = map[string]client.PoolClient{
			"ingester0": newRateClient([]*logproto.StreamRate{
				{Tenant: "tenant 1", StreamHash: 1, StreamHashNoShard: 0, Rate: 25},
			}, 1),
		}

		tc.rateStore.rateKeepAlive = 1 * time.Millisecond

		require.NoError(t, tc.rateStore.instrumentedUpdateAllRates(context.Background()))
		rate, _ := tc.rateStore.RateFor("tenant 1", 0)
		require.EqualValues(t, 25, rate)

		tc.ring.replicationSet = ring.ReplicationSet{}
		time.Sleep(10 * time.Millisecond)

		require.NoError(t, tc.rateStore.instrumentedUpdateAllRates(context.Background()))
		rate, _ = tc.rateStore.RateFor("tenant 1", 0)
		require.EqualValues(t, 0, rate)
	})

	t.Run("it adjusts the rate and pushes according to a weighted average", func(t *testing.T) {
		tc := setup(true)
		tc.ring.replicationSet = ring.ReplicationSet{
			Instances: []ring.InstanceDesc{
				{Addr: "ingester0"},
			},
		}

		tc.clientPool.clients = map[string]client.PoolClient{
			"ingester0": newRateClient([]*logproto.StreamRate{
				{Tenant: "tenant 1", StreamHash: 1, StreamHashNoShard: 0, Rate: 25, Pushes: 25},
			}, 1),
		}

		require.NoError(t, tc.rateStore.instrumentedUpdateAllRates(context.Background()))
		rate, pushRate := tc.rateStore.RateFor("tenant 1", 0)
		require.EqualValues(t, 25, rate)
		require.EqualValues(t, 25, pushRate)

		tc.clientPool.clients = map[string]client.PoolClient{
			"ingester0": newRateClient([]*logproto.StreamRate{
				{Tenant: "tenant 1", StreamHash: 1, StreamHashNoShard: 0, Rate: 50, Pushes: 50},
			}, 1),
		}

		require.NoError(t, tc.rateStore.instrumentedUpdateAllRates(context.Background()))
		afterNewRate, afterNewPushRate := tc.rateStore.RateFor("tenant 1", 0)
		require.EqualValues(t, weightedMovingAverage(50, 25), afterNewRate)
		require.EqualValues(t, weightedMovingAverageF(50, 25), afterNewPushRate)

		tc.ring.replicationSet = ring.ReplicationSet{} // No more data from ingesters

		require.NoError(t, tc.rateStore.instrumentedUpdateAllRates(context.Background()))
		afterNoUpdate, afterNoUpdatePushRate := tc.rateStore.RateFor("tenant 1", 0)
		require.EqualValues(t, weightedMovingAverage(0, afterNewRate), afterNoUpdate)
		require.EqualValues(t, weightedMovingAverageF(0, afterNewPushRate), afterNoUpdatePushRate)
	})

	t.Run("the push rate can be less that 1", func(t *testing.T) {
		tc := setup(true)
		tc.ring.replicationSet = ring.ReplicationSet{
			Instances: []ring.InstanceDesc{
				{Addr: "ingester0"},
			},
		}

		tc.clientPool.clients = map[string]client.PoolClient{
			"ingester0": newRateClient([]*logproto.StreamRate{
				{Tenant: "tenant 1", StreamHash: 1, StreamHashNoShard: 0, Rate: 25, Pushes: 1},
			}, 1),
		}

		require.NoError(t, tc.rateStore.instrumentedUpdateAllRates(context.Background()))
		_, pushRate := tc.rateStore.RateFor("tenant 1", 0)
		require.EqualValues(t, 1, pushRate)

		tc.ring.replicationSet = ring.ReplicationSet{} // No more data from ingesters

		require.NoError(t, tc.rateStore.instrumentedUpdateAllRates(context.Background()))
		_, afterNewPushRate := tc.rateStore.RateFor("tenant 1", 0)
		require.EqualValues(t, weightedMovingAverageF(0, 1), afterNewPushRate)
	})
}

var benchErr error

func BenchmarkRateStore(b *testing.B) {
	tc := setup(true)
	tc.ring.replicationSet = ring.ReplicationSet{
		Instances: []ring.InstanceDesc{
			{Addr: "ingester0"},
		},
	}

	rates := make([]*logproto.StreamRate, 200000)
	for i := 0; i < 200000; i++ {
		rates[i] = &logproto.StreamRate{Tenant: fmt.Sprintf("tenant %d", i%2), StreamHash: uint64(i % 3), StreamHashNoShard: uint64(i % 4), Rate: rand.Int63()}
	}

	tc.clientPool.clients = map[string]client.PoolClient{
		"ingester0": newRateClient(rates),
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		benchErr = tc.rateStore.updateAllRates(context.Background())
	}
}

func requireRatesAndPushesEqual(t *testing.T, expectedRate int64, expectedPushes float64, r *rateStore, tenant string, streamhash uint64) {
	actualRate, actualPushes := r.RateFor(tenant, streamhash)
	require.Equal(t, expectedRate, actualRate)
	require.Equal(t, expectedPushes, actualPushes)
}

func BenchmarkAggregateByShard(b *testing.B) {
	rs := &rateStore{rates: make(map[string]map[uint64]expiringRate)}
	rates := make(map[string]map[uint64]*logproto.StreamRate)
	rates["fake"] = make(map[uint64]*logproto.StreamRate)
	for i := 0; i < 1000; i++ {
		rates["fake"][uint64(i)] = &logproto.StreamRate{StreamHash: uint64(i), StreamHashNoShard: 12345}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rs.aggregateByShard(context.TODO(), rates)
	}
}

func newFakeRing() *fakeRing {
	return &fakeRing{}
}

type fakeRing struct {
	ring.ReadRing

	replicationSet ring.ReplicationSet
	err            error
}

func (r *fakeRing) GetAllHealthy(_ ring.Operation) (ring.ReplicationSet, error) {
	return r.replicationSet, r.err
}

func newFakeClientPool() *fakeClientPool {
	return &fakeClientPool{
		clients: make(map[string]client.PoolClient),
	}
}

type fakeClientPool struct {
	clients map[string]client.PoolClient
	err     error
}

func (p *fakeClientPool) GetClientFor(addr string) (client.PoolClient, error) {
	return p.clients[addr], p.err
}

func newRateClient(rates []*logproto.StreamRate, maxResponses ...int) client.PoolClient {
	var maxResp int
	if len(maxResponses) > 0 {
		maxResp = maxResponses[0]
	}

	return client2.ClosableHealthAndIngesterClient{
		StreamDataClient: &fakeStreamDataClient{resp: &logproto.StreamRatesResponse{StreamRates: rates}, maxResponses: maxResp},
	}
}

type fakeStreamDataClient struct {
	resp         *logproto.StreamRatesResponse
	err          error
	maxResponses int
	callCount    int
}

func (c *fakeStreamDataClient) GetStreamRates(_ context.Context, _ *logproto.StreamRatesRequest, _ ...grpc.CallOption) (*logproto.StreamRatesResponse, error) {
	if c.maxResponses > 0 && c.callCount > c.maxResponses {
		return nil, c.err
	}
	c.callCount++
	return c.resp, c.err
}

type fakeOverrides struct {
	Limits
	enabled bool
}

func (c *fakeOverrides) AllByUserID() map[string]*validation.Limits {
	return map[string]*validation.Limits{
		"ingester0": {
			ShardStreams: shardstreams.Config{
				Enabled: c.enabled,
			},
		},
	}
}

func (c *fakeOverrides) ShardStreams(_ string) shardstreams.Config {
	return shardstreams.Config{
		Enabled: c.enabled,
	}
}

type testContext struct {
	ring       *fakeRing
	clientPool *fakeClientPool
	rateStore  *rateStore
}

func setup(shardingEnabled bool) *testContext {
	ring := newFakeRing()
	cp := newFakeClientPool()
	cfg := RateStoreConfig{MaxParallelism: 5, IngesterReqTimeout: time.Second, StreamRateUpdateInterval: 10 * time.Millisecond}

	return &testContext{
		ring:       ring,
		clientPool: cp,
		rateStore:  NewRateStore(cfg, ring, cp, &fakeOverrides{enabled: shardingEnabled}, nil),
	}
}
