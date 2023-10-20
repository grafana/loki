package bloomgateway

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
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
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/config"
	lokiring "github.com/grafana/loki/pkg/util/ring"
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

func TestBloomGateway_StartStopService(t *testing.T) {

	ss := NewNoopStrategy()
	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()

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

		gw, err := New(cfg, schemaCfg, storageCfg, ss, cm, logger, reg)
		require.NoError(t, err)

		err = services.StartAndAwaitRunning(context.Background(), gw)
		require.NoError(t, err)

		err = services.StopAndAwaitTerminated(context.Background(), gw)
		require.NoError(t, err)
	})
}

func TestBloomGateway_FilterChunkRefs(t *testing.T) {
	tenantID := "test"

	ss := NewNoopStrategy()
	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()

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
		gw, err := New(cfg, schemaCfg, storageCfg, ss, cm, logger, reg)
		require.NoError(t, err)

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
		gw, err := New(cfg, schemaCfg, storageCfg, ss, cm, logger, reg)
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
		}

		ctx := user.InjectOrgID(context.Background(), tenantID)
		_, err = gw.FilterChunkRefs(ctx, req)
		require.Error(t, err)
		require.Equal(t, "expected chunk refs from tenant test, got tenant other: invalid tenant in chunk refs", err.Error())
	})

}
