package bloomgateway

import (
	"context"
	"fmt"
	"math/rand"
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
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/config"
	lokiring "github.com/grafana/loki/pkg/util/ring"
	"github.com/grafana/loki/pkg/validation"
)

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
	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()
	limits := newLimits()

	cm := storage.NewClientMetrics()
	t.Cleanup(cm.Unregister)

	p := config.PeriodConfig{
		From: parseDayTime("2023-09-01"),
		IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{
				Prefix: "index_",
				Period: 24 * time.Hour,
			},
		},
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
			WorkerConcurrency:       4,
			MaxOutstandingPerTenant: 1024,
		}

		gw, err := New(cfg, schemaCfg, storageCfg, limits, cm, logger, reg)
		require.NoError(t, err)

		err = services.StartAndAwaitRunning(context.Background(), gw)
		require.NoError(t, err)

		// Wait for workers to connect to queue
		time.Sleep(50 * time.Millisecond)
		require.Equal(t, float64(cfg.WorkerConcurrency), gw.queue.GetConnectedConsumersMetric())

		err = services.StopAndAwaitTerminated(context.Background(), gw)
		require.NoError(t, err)
	})
}

func TestBloomGateway_FilterChunkRefs(t *testing.T) {
	tenantID := "test"
	logger := log.NewLogfmtLogger(os.Stderr)
	reg := prometheus.NewRegistry()
	limits := newLimits()

	cm := storage.NewClientMetrics()
	t.Cleanup(cm.Unregister)

	p := config.PeriodConfig{
		From: parseDayTime("2023-09-01"),
		IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{
				Prefix: "index_",
				Period: 24 * time.Hour,
			},
		},
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
		WorkerConcurrency:       4,
		MaxOutstandingPerTenant: 1024,
	}

	t.Run("shipper error is propagated", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		gw, err := New(cfg, schemaCfg, storageCfg, limits, cm, logger, reg)
		require.NoError(t, err)

		now := mktime("2023-10-03 10:00")

		// replace store implementation and re-initialize workers and sub-services
		_, metas, queriers, data := createBlocks(t, tenantID, 10, now.Add(-1*time.Hour), now, 0x0000, 0x0fff)

		mockStore := newMockBloomStore(queriers, metas)
		mockStore.err = errors.New("request failed")
		gw.bloomStore = mockStore

		err = gw.initServices()
		require.NoError(t, err)

		err = services.StartAndAwaitRunning(context.Background(), gw)
		require.NoError(t, err)
		t.Cleanup(func() {
			err = services.StopAndAwaitTerminated(context.Background(), gw)
			require.NoError(t, err)
		})

		chunkRefs := createQueryInputFromBlockData(t, tenantID, data, 100)

		// saturate workers
		// then send additional request
		for i := 0; i < gw.cfg.WorkerConcurrency+1; i++ {
			req := &logproto.FilterChunkRefRequest{
				From:    now.Add(-24 * time.Hour),
				Through: now,
				Refs:    groupRefs(t, chunkRefs),
				Filters: []syntax.LineFilter{
					{Ty: labels.MatchEqual, Match: "does not match"},
				},
			}

			ctx, cancelFn := context.WithTimeout(context.Background(), 10*time.Second)
			ctx = user.InjectOrgID(ctx, tenantID)
			t.Cleanup(cancelFn)

			res, err := gw.FilterChunkRefs(ctx, req)
			require.ErrorContainsf(t, err, "request failed", "%+v", res)
		}
	})

	t.Run("request cancellation does not result in channel locking", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		gw, err := New(cfg, schemaCfg, storageCfg, limits, cm, logger, reg)
		require.NoError(t, err)

		now := mktime("2024-01-25 10:00")

		// replace store implementation and re-initialize workers and sub-services
		_, metas, queriers, data := createBlocks(t, tenantID, 10, now.Add(-1*time.Hour), now, 0x0000, 0x0fff)

		mockStore := newMockBloomStore(queriers, metas)
		mockStore.delay = 2000 * time.Millisecond
		gw.bloomStore = mockStore

		err = gw.initServices()
		require.NoError(t, err)

		err = services.StartAndAwaitRunning(context.Background(), gw)
		require.NoError(t, err)
		t.Cleanup(func() {
			err = services.StopAndAwaitTerminated(context.Background(), gw)
			require.NoError(t, err)
		})

		chunkRefs := createQueryInputFromBlockData(t, tenantID, data, 100)

		// saturate workers
		// then send additional request
		for i := 0; i < gw.cfg.WorkerConcurrency+1; i++ {
			req := &logproto.FilterChunkRefRequest{
				From:    now.Add(-24 * time.Hour),
				Through: now,
				Refs:    groupRefs(t, chunkRefs),
				Filters: []syntax.LineFilter{
					{Ty: labels.MatchEqual, Match: "does not match"},
				},
			}

			ctx, cancelFn := context.WithTimeout(context.Background(), 500*time.Millisecond)
			ctx = user.InjectOrgID(ctx, tenantID)
			t.Cleanup(cancelFn)

			res, err := gw.FilterChunkRefs(ctx, req)
			require.ErrorContainsf(t, err, context.DeadlineExceeded.Error(), "%+v", res)
		}
	})

	t.Run("returns unfiltered chunk refs if no filters provided", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		gw, err := New(cfg, schemaCfg, storageCfg, limits, cm, logger, reg)
		require.NoError(t, err)

		err = services.StartAndAwaitRunning(context.Background(), gw)
		require.NoError(t, err)
		t.Cleanup(func() {
			err = services.StopAndAwaitTerminated(context.Background(), gw)
			require.NoError(t, err)
		})

		now := mktime("2023-10-03 10:00")

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

	t.Run("gateway tracks active users", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		gw, err := New(cfg, schemaCfg, storageCfg, limits, cm, logger, reg)
		require.NoError(t, err)

		err = services.StartAndAwaitRunning(context.Background(), gw)
		require.NoError(t, err)
		t.Cleanup(func() {
			err = services.StopAndAwaitTerminated(context.Background(), gw)
			require.NoError(t, err)
		})

		now := mktime("2023-10-03 10:00")

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
				Filters: []syntax.LineFilter{
					{Ty: labels.MatchEqual, Match: "foo"},
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
		gw, err := New(cfg, schemaCfg, storageCfg, limits, cm, logger, reg)
		require.NoError(t, err)

		now := mktime("2023-10-03 10:00")

		// replace store implementation and re-initialize workers and sub-services
		_, metas, queriers, data := createBlocks(t, tenantID, 10, now.Add(-1*time.Hour), now, 0x0000, 0x0fff)

		gw.bloomStore = newMockBloomStore(queriers, metas)
		err = gw.initServices()
		require.NoError(t, err)

		err = services.StartAndAwaitRunning(context.Background(), gw)
		require.NoError(t, err)
		t.Cleanup(func() {
			err = services.StopAndAwaitTerminated(context.Background(), gw)
			require.NoError(t, err)
		})

		chunkRefs := createQueryInputFromBlockData(t, tenantID, data, 10)

		t.Run("no match - return empty response", func(t *testing.T) {
			inputChunkRefs := groupRefs(t, chunkRefs)
			req := &logproto.FilterChunkRefRequest{
				From:    now.Add(-8 * time.Hour),
				Through: now,
				Refs:    inputChunkRefs,
				Filters: []syntax.LineFilter{
					{Ty: labels.MatchEqual, Match: "does not match"},
				},
			}
			ctx := user.InjectOrgID(context.Background(), tenantID)
			res, err := gw.FilterChunkRefs(ctx, req)
			require.NoError(t, err)

			expectedResponse := &logproto.FilterChunkRefResponse{
				ChunkRefs: []*logproto.GroupedChunkRefs{},
			}
			require.Equal(t, expectedResponse, res)
		})

		t.Run("match - return filtered", func(t *testing.T) {
			inputChunkRefs := groupRefs(t, chunkRefs)
			// Hack to get search string for a specific series
			// see MkBasicSeriesWithBlooms() in pkg/storage/bloom/v1/test_util.go
			// each series has 1 chunk
			// each chunk has multiple strings, from int(fp) to int(nextFp)-1
			x := rand.Intn(len(inputChunkRefs))
			fp := inputChunkRefs[x].Fingerprint
			chks := inputChunkRefs[x].Refs
			line := fmt.Sprintf("%04x:%04x", int(fp), 0) // first line

			t.Log("x=", x, "fp=", fp, "line=", line)

			req := &logproto.FilterChunkRefRequest{
				From:    now.Add(-8 * time.Hour),
				Through: now,
				Refs:    inputChunkRefs,
				Filters: []syntax.LineFilter{
					{Ty: labels.MatchEqual, Match: line},
				},
			}
			ctx := user.InjectOrgID(context.Background(), tenantID)
			res, err := gw.FilterChunkRefs(ctx, req)
			require.NoError(t, err)

			expectedResponse := &logproto.FilterChunkRefResponse{
				ChunkRefs: []*logproto.GroupedChunkRefs{
					{
						Fingerprint: fp,
						Refs:        chks,
						Tenant:      tenantID,
					},
				},
			}
			require.Equal(t, expectedResponse, res)
		})

	})
}

func TestBloomGateway_RemoveNotMatchingChunks(t *testing.T) {
	g := &Gateway{
		logger: log.NewNopLogger(),
	}
	t.Run("removing chunks partially", func(t *testing.T) {
		req := &logproto.FilterChunkRefRequest{
			Refs: []*logproto.GroupedChunkRefs{
				{Fingerprint: 0x00, Tenant: "fake", Refs: []*logproto.ShortRef{
					{Checksum: 0x1},
					{Checksum: 0x2},
					{Checksum: 0x3},
					{Checksum: 0x4},
					{Checksum: 0x5},
				}},
			},
		}
		res := v1.Output{
			Fp: 0x00, Removals: v1.ChunkRefs{
				{Checksum: 0x2},
				{Checksum: 0x4},
			},
		}
		expected := &logproto.FilterChunkRefRequest{
			Refs: []*logproto.GroupedChunkRefs{
				{Fingerprint: 0x00, Tenant: "fake", Refs: []*logproto.ShortRef{
					{Checksum: 0x1},
					{Checksum: 0x3},
					{Checksum: 0x5},
				}},
			},
		}
		n := g.removeNotMatchingChunks(req, res)
		require.Equal(t, 2, n)
		require.Equal(t, expected, req)
	})

	t.Run("removing all chunks removed fingerprint ref", func(t *testing.T) {
		req := &logproto.FilterChunkRefRequest{
			Refs: []*logproto.GroupedChunkRefs{
				{Fingerprint: 0x00, Tenant: "fake", Refs: []*logproto.ShortRef{
					{Checksum: 0x1},
					{Checksum: 0x2},
					{Checksum: 0x3},
				}},
			},
		}
		res := v1.Output{
			Fp: 0x00, Removals: v1.ChunkRefs{
				{Checksum: 0x1},
				{Checksum: 0x2},
				{Checksum: 0x2},
			},
		}
		expected := &logproto.FilterChunkRefRequest{
			Refs: []*logproto.GroupedChunkRefs{},
		}
		n := g.removeNotMatchingChunks(req, res)
		require.Equal(t, 3, n)
		require.Equal(t, expected, req)
	})

}
