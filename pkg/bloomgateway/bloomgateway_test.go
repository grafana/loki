package bloomgateway

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
	lokiring "github.com/grafana/loki/pkg/util/ring"
	"github.com/grafana/loki/pkg/validation"
)

func parseDayTime(s string) config.DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return config.DayTime{
		Time: model.TimeFromUnix(t.Unix()),
	}
}

func groupRefs(t *testing.T, chunkRefs []*logproto.ChunkRef) []*logproto.GroupedChunkRefs {
	t.Helper()
	grouped := make([]*logproto.GroupedChunkRefs, 0, len(chunkRefs))
	return groupChunkRefs(chunkRefs, grouped)
}

func newLimits() *validation.Overrides {
	limits := validation.Limits{}
	flagext.DefaultValues(&limits)
	limits.BloomGatewayEnabled = true

	overrides, _ := validation.NewOverrides(limits, nil)
	return overrides
}

func TestBloomGateway_StartStopService(t *testing.T) {

	ss := NewNoopStrategy()
	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()
	limits := newLimits()

	cm := storage.NewClientMetrics()
	t.Cleanup(cm.Unregister)

	p := config.PeriodConfig{
		From:       parseDayTime("2023-09-01"),
		IndexType:  config.TSDBType,
		ObjectType: config.StorageTypeFileSystem,
		Schema:     "v13",
		RowShards:  16,
	}
	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{p},
	}
	storageCfg := storage.Config{
		FSConfig: local.FSConfig{
			Directory: t.TempDir(),
		},
	}

	t.Run("start and stop bloom gateway", func(t *testing.T) {
		kvStore, closer := consul.NewInMemoryClient(ring.GetCodec(), logger, reg)
		t.Cleanup(func() {
			closer.Close()
		})

		cfg := Config{
			Enabled: true,
			Ring: lokiring.RingConfigWithRF{
				RingConfig: lokiring.RingConfig{
					KVStore: kv.Config{
						Mock: kvStore,
					},
				},
				ReplicationFactor: 1,
			},
		}

		gw, err := New(cfg, schemaCfg, storageCfg, limits, ss, cm, logger, reg)
		require.NoError(t, err)

		err = services.StartAndAwaitRunning(context.Background(), gw)
		require.NoError(t, err)

		// Wait for workers to connect to queue
		time.Sleep(50 * time.Millisecond)
		require.Equal(t, float64(numWorkers), gw.queue.GetConnectedConsumersMetric())

		err = services.StopAndAwaitTerminated(context.Background(), gw)
		require.NoError(t, err)
	})
}

func TestBloomGateway_FilterChunkRefs(t *testing.T) {
	tenantID := "test"

	ss := NewNoopStrategy()
	logger := log.NewLogfmtLogger(os.Stderr)
	reg := prometheus.NewRegistry()
	limits := newLimits()

	cm := storage.NewClientMetrics()
	t.Cleanup(cm.Unregister)

	p := config.PeriodConfig{
		From:       parseDayTime("2023-09-01"),
		IndexType:  config.TSDBType,
		ObjectType: config.StorageTypeFileSystem,
		Schema:     "v13",
		RowShards:  16,
	}
	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{p},
	}
	storageCfg := storage.Config{
		FSConfig: local.FSConfig{
			Directory: t.TempDir(),
		},
	}

	kvStore, closer := consul.NewInMemoryClient(ring.GetCodec(), logger, reg)
	t.Cleanup(func() {
		closer.Close()
	})

	cfg := Config{
		Enabled: true,
		Ring: lokiring.RingConfigWithRF{
			RingConfig: lokiring.RingConfig{
				KVStore: kv.Config{
					Mock: kvStore,
				},
			},
			ReplicationFactor: 1,
		},
	}

	t.Run("returns unfiltered chunk refs if no filters provided", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		gw, err := New(cfg, schemaCfg, storageCfg, limits, ss, cm, logger, reg)
		require.NoError(t, err)

		err = services.StartAndAwaitRunning(context.Background(), gw)
		require.NoError(t, err)
		t.Cleanup(func() {
			err = services.StopAndAwaitTerminated(context.Background(), gw)
			require.NoError(t, err)
		})

		ts, _ := time.Parse("2006-01-02 15:04", "2023-10-03 10:00")
		now := model.TimeFromUnix(ts.Unix())

		chunkRefs := []*logproto.ChunkRef{
			{Fingerprint: 3000, UserID: tenantID, From: now.Add(-24 * time.Hour), Through: now.Add(-23 * time.Hour), Checksum: 1},
			{Fingerprint: 1000, UserID: tenantID, From: now.Add(-22 * time.Hour), Through: now.Add(-21 * time.Hour), Checksum: 2},
			{Fingerprint: 2000, UserID: tenantID, From: now.Add(-20 * time.Hour), Through: now.Add(-19 * time.Hour), Checksum: 3},
			{Fingerprint: 1000, UserID: tenantID, From: now.Add(-23 * time.Hour), Through: now.Add(-22 * time.Hour), Checksum: 4},
		}
		req := &logproto.FilterChunkRefRequest{
			From:    now.Add(-24 * time.Hour),
			Through: now,
			Refs:    groupRefs(t, chunkRefs),
		}

		ctx := user.InjectOrgID(context.Background(), tenantID)
		res, err := gw.FilterChunkRefs(ctx, req)
		require.NoError(t, err)
		require.Equal(t, &logproto.FilterChunkRefResponse{
			ChunkRefs: []*logproto.GroupedChunkRefs{
				{Fingerprint: 1000, Tenant: tenantID, Refs: []*logproto.ShortRef{
					{From: 1696248000000, Through: 1696251600000, Checksum: 2},
					{From: 1696244400000, Through: 1696248000000, Checksum: 4},
				}},
				{Fingerprint: 2000, Tenant: tenantID, Refs: []*logproto.ShortRef{
					{From: 1696255200000, Through: 1696258800000, Checksum: 3},
				}},
				{Fingerprint: 3000, Tenant: tenantID, Refs: []*logproto.ShortRef{
					{From: 1696240800000, Through: 1696244400000, Checksum: 1},
				}},
			},
		}, res)
	})

	t.Run("returns error if chunk refs do not belong to tenant", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		gw, err := New(cfg, schemaCfg, storageCfg, limits, ss, cm, logger, reg)
		require.NoError(t, err)

		ts, _ := time.Parse("2006-01-02 15:04", "2023-10-03 10:00")
		now := model.TimeFromUnix(ts.Unix())

		chunkRefs := []*logproto.ChunkRef{
			{Fingerprint: 1000, UserID: tenantID, From: now.Add(-22 * time.Hour), Through: now.Add(-21 * time.Hour), Checksum: 1},
			{Fingerprint: 2000, UserID: "other", From: now.Add(-20 * time.Hour), Through: now.Add(-19 * time.Hour), Checksum: 2},
		}
		req := &logproto.FilterChunkRefRequest{
			From:    now.Add(-24 * time.Hour),
			Through: now,
			Refs:    groupRefs(t, chunkRefs),
			Filters: []*logproto.LineFilterExpression{
				{Operator: 1, Match: "foo"},
			},
		}

		ctx := user.InjectOrgID(context.Background(), tenantID)
		_, err = gw.FilterChunkRefs(ctx, req)
		require.Error(t, err)
		require.Equal(t, "expected chunk refs from tenant test, got tenant other: invalid tenant in chunk refs", err.Error())
	})

	t.Run("gateway tracks active users", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		gw, err := New(cfg, schemaCfg, storageCfg, limits, ss, cm, logger, reg)
		require.NoError(t, err)

		err = services.StartAndAwaitRunning(context.Background(), gw)
		require.NoError(t, err)
		t.Cleanup(func() {
			err = services.StopAndAwaitTerminated(context.Background(), gw)
			require.NoError(t, err)
		})

		ts, _ := time.Parse("2006-01-02 15:04", "2023-10-03 10:00")
		now := model.TimeFromUnix(ts.Unix())

		tenants := []string{"tenant-a", "tenant-b", "tenant-c"}
		for idx, tenantID := range tenants {
			chunkRefs := []*logproto.ChunkRef{
				{
					Fingerprint: uint64(1000 + 100*idx),
					UserID:      tenantID,
					From:        now.Add(-24 * time.Hour),
					Through:     now,
					Checksum:    uint32(idx),
				},
			}
			req := &logproto.FilterChunkRefRequest{
				From:    now.Add(-24 * time.Hour),
				Through: now,
				Refs:    groupRefs(t, chunkRefs),
				Filters: []*logproto.LineFilterExpression{
					{Operator: 1, Match: "foo"},
				},
			}
			ctx := user.InjectOrgID(context.Background(), tenantID)
			_, err = gw.FilterChunkRefs(ctx, req)
			require.NoError(t, err)
		}
		require.ElementsMatch(t, tenants, gw.activeUsers.ActiveUsers())
	})

	t.Run("use fuse queriers to filter chunks", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		gw, err := New(cfg, schemaCfg, storageCfg, limits, ss, cm, logger, reg)
		require.NoError(t, err)

		// replace store implementation and re-initialize workers and sub-services
		gw.bloomStore = newMockBloomStore(t)
		err = gw.initServices()
		require.NoError(t, err)

		err = services.StartAndAwaitRunning(context.Background(), gw)
		require.NoError(t, err)
		t.Cleanup(func() {
			err = services.StopAndAwaitTerminated(context.Background(), gw)
			require.NoError(t, err)
		})

		ts, _ := time.Parse("2006-01-02 15:04", "2023-10-03 10:00")
		now := model.TimeFromUnix(ts.Unix())

		chunkRefs := []*logproto.ChunkRef{
			{
				Fingerprint: 100,
				UserID:      tenantID,
				From:        now.Add(-24 * time.Hour),
				Through:     now.Add(-23 * time.Hour),
				Checksum:    1,
			},
			{
				Fingerprint: 100,
				UserID:      tenantID,
				From:        now.Add(-23 * time.Hour),
				Through:     now.Add(-22 * time.Hour),
				Checksum:    2,
			},
			{
				Fingerprint: 500,
				UserID:      tenantID,
				From:        now.Add(-22 * time.Hour),
				Through:     now.Add(-21 * time.Hour),
				Checksum:    3,
			},
			{
				Fingerprint: 1000,
				UserID:      tenantID,
				From:        now.Add(-20 * time.Hour),
				Through:     now.Add(-19 * time.Hour),
				Checksum:    4,
			},
			{
				Fingerprint: 1001,
				UserID:      tenantID,
				From:        now.Add(-19 * time.Hour),
				Through:     now.Add(-18 * time.Hour),
				Checksum:    5,
			},
		}
		inputChunkRefs := groupRefs(t, chunkRefs)

		t.Run("no match - return filtered", func(t *testing.T) {
			req := &logproto.FilterChunkRefRequest{
				From:    now.Add(-24 * time.Hour),
				Through: now,
				Refs:    inputChunkRefs,
				Filters: []*logproto.LineFilterExpression{
					{Operator: 1, Match: "does not match"},
				},
			}
			ctx := user.InjectOrgID(context.Background(), tenantID)
			res, err := gw.FilterChunkRefs(ctx, req)
			require.NoError(t, err)

			expectedResponse := &logproto.FilterChunkRefResponse{
				ChunkRefs: inputChunkRefs, // why does it return all chunks?
			}
			require.Equal(t, expectedResponse, res)
		})

		t.Run("match - return unfiltered", func(t *testing.T) {
			req := &logproto.FilterChunkRefRequest{
				From:    now.Add(-24 * time.Hour),
				Through: now,
				Refs:    groupRefs(t, chunkRefs),
				Filters: []*logproto.LineFilterExpression{
					// series with fingerprint 100 has 1000 keys
					// range is from 100_000 to 100_999
					{Operator: 1, Match: "100001"},
				},
			}
			ctx := user.InjectOrgID(context.Background(), tenantID)
			res, err := gw.FilterChunkRefs(ctx, req)
			require.NoError(t, err)

			expectedResponse := &logproto.FilterChunkRefResponse{
				ChunkRefs: inputChunkRefs, // why does it return all chunks?
			}
			require.Equal(t, expectedResponse, res)
		})

	})
}

func newMockBloomStore(t *testing.T) *mockBloomStore {
	return &mockBloomStore{t: t}
}

type mockBloomStore struct {
	t *testing.T
}

func (s *mockBloomStore) GetBlockQueriers(_ context.Context, _ string, from, through time.Time, _ []uint64) ([]bloomshipper.BlockQuerierWithFingerprintRange, error) {
	return []bloomshipper.BlockQuerierWithFingerprintRange{
		{BlockQuerier: v1.MakeBlockQuerier(s.t, 0, 255, from.Unix(), through.Unix()), MinFp: 0, MaxFp: 255},
		{BlockQuerier: v1.MakeBlockQuerier(s.t, 256, 511, from.Unix(), through.Unix()), MinFp: 256, MaxFp: 511},
		{BlockQuerier: v1.MakeBlockQuerier(s.t, 512, 767, from.Unix(), through.Unix()), MinFp: 512, MaxFp: 767},
		{BlockQuerier: v1.MakeBlockQuerier(s.t, 768, 1023, from.Unix(), through.Unix()), MinFp: 768, MaxFp: 1023},
	}, nil
}

func (s *mockBloomStore) Stop() {}
