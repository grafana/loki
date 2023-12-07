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

func mktime(s string) model.Time {
	ts, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		panic(err)
	}
	return model.TimeFromUnix(ts.Unix())
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
			WorkerConcurrency:       4,
			MaxOutstandingPerTenant: 1024,
		}

		gw, err := New(cfg, schemaCfg, storageCfg, limits, ss, cm, logger, reg)
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
		WorkerConcurrency:       4,
		MaxOutstandingPerTenant: 1024,
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
		gw, err := New(cfg, schemaCfg, storageCfg, limits, ss, cm, logger, reg)
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
		for _, tc := range []struct {
			name  string
			value bool
		}{
			{"sequentially", true},
			{"callback", false},
		} {
			t.Run(tc.name, func(t *testing.T) {

				reg := prometheus.NewRegistry()
				gw, err := New(cfg, schemaCfg, storageCfg, limits, ss, cm, logger, reg)
				require.NoError(t, err)

				now := mktime("2023-10-03 10:00")

				// replace store implementation and re-initialize workers and sub-services
				bqs, data := createBlockQueriers(t, 5, now.Add(-8*time.Hour), now, 0, 1024)
				gw.bloomStore = newMockBloomStore(bqs)
				gw.workerConfig.processBlocksSequentially = tc.value
				err = gw.initServices()
				require.NoError(t, err)

				t.Log("process blocks in worker sequentially", gw.workerConfig.processBlocksSequentially)

				err = services.StartAndAwaitRunning(context.Background(), gw)
				require.NoError(t, err)
				t.Cleanup(func() {
					err = services.StopAndAwaitTerminated(context.Background(), gw)
					require.NoError(t, err)
				})

				chunkRefs := createQueryInputFromBlockData(t, tenantID, data, 100)

				t.Run("no match - return empty response", func(t *testing.T) {
					inputChunkRefs := groupRefs(t, chunkRefs)
					req := &logproto.FilterChunkRefRequest{
						From:    now.Add(-8 * time.Hour),
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
						ChunkRefs: []*logproto.GroupedChunkRefs{},
					}
					require.Equal(t, expectedResponse, res)
				})

				t.Run("match - return filtered", func(t *testing.T) {
					inputChunkRefs := groupRefs(t, chunkRefs)
					// hack to get indexed key for a specific series
					// the indexed key range for a series is defined as
					// i * keysPerSeries ... i * keysPerSeries + keysPerSeries - 1
					// where i is the nth series in a block
					// fortunately, i is also used as Checksum for the single chunk of a series
					// see mkBasicSeriesWithBlooms() in pkg/storage/bloom/v1/test_util.go
					key := inputChunkRefs[0].Refs[0].Checksum*1000 + 500

					req := &logproto.FilterChunkRefRequest{
						From:    now.Add(-8 * time.Hour),
						Through: now,
						Refs:    inputChunkRefs,
						Filters: []*logproto.LineFilterExpression{
							{Operator: 1, Match: fmt.Sprint(key)},
						},
					}
					ctx := user.InjectOrgID(context.Background(), tenantID)
					res, err := gw.FilterChunkRefs(ctx, req)
					require.NoError(t, err)

					expectedResponse := &logproto.FilterChunkRefResponse{
						ChunkRefs: inputChunkRefs[:1],
					}
					require.Equal(t, expectedResponse, res)
				})

			})
		}

	})
}

func createBlockQueriers(t *testing.T, numBlocks int, from, through model.Time, minFp, maxFp model.Fingerprint) ([]bloomshipper.BlockQuerierWithFingerprintRange, [][]v1.SeriesWithBloom) {
	t.Helper()
	step := (maxFp - minFp) / model.Fingerprint(numBlocks)
	bqs := make([]bloomshipper.BlockQuerierWithFingerprintRange, 0, numBlocks)
	series := make([][]v1.SeriesWithBloom, 0, numBlocks)
	for i := 0; i < numBlocks; i++ {
		fromFp := minFp + (step * model.Fingerprint(i))
		throughFp := fromFp + step - 1
		// last block needs to include maxFp
		if i == numBlocks-1 {
			throughFp = maxFp
		}
		blockQuerier, data := v1.MakeBlockQuerier(t, fromFp, throughFp, from, through)
		bq := bloomshipper.BlockQuerierWithFingerprintRange{
			BlockQuerier: blockQuerier,
			MinFp:        fromFp,
			MaxFp:        throughFp,
		}
		bqs = append(bqs, bq)
		series = append(series, data)
	}
	return bqs, series
}

func newMockBloomStore(bqs []bloomshipper.BlockQuerierWithFingerprintRange) *mockBloomStore {
	return &mockBloomStore{bqs: bqs}
}

type mockBloomStore struct {
	bqs []bloomshipper.BlockQuerierWithFingerprintRange
}

var _ bloomshipper.Store = &mockBloomStore{}

// GetBlockQueriersForBlockRefs implements bloomshipper.Store.
func (s *mockBloomStore) GetBlockQueriersForBlockRefs(_ context.Context, _ string, _ []bloomshipper.BlockRef) ([]bloomshipper.BlockQuerierWithFingerprintRange, error) {
	return s.bqs, nil
}

// GetBlockRefs implements bloomshipper.Store.
func (s *mockBloomStore) GetBlockRefs(_ context.Context, tenant string, _, _ time.Time) ([]bloomshipper.BlockRef, error) {
	blocks := make([]bloomshipper.BlockRef, 0, len(s.bqs))
	for i := range s.bqs {
		blocks = append(blocks, bloomshipper.BlockRef{
			Ref: bloomshipper.Ref{
				MinFingerprint: uint64(s.bqs[i].MinFp),
				MaxFingerprint: uint64(s.bqs[i].MaxFp),
				TenantID:       tenant,
			},
		})
	}
	return blocks, nil
}

// GetBlockQueriers implements bloomshipper.Store.
func (s *mockBloomStore) GetBlockQueriers(_ context.Context, _ string, _, _ time.Time, _ []uint64) ([]bloomshipper.BlockQuerierWithFingerprintRange, error) {
	return s.bqs, nil
}

func (s *mockBloomStore) Stop() {}

// ForEach implements bloomshipper.Store.
func (s *mockBloomStore) ForEach(_ context.Context, _ string, _ []bloomshipper.BlockRef, callback bloomshipper.ForEachBlockCallback) error {
	shuffled := make([]bloomshipper.BlockQuerierWithFingerprintRange, len(s.bqs))
	_ = copy(shuffled, s.bqs)

	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	for _, bq := range shuffled {
		// ignore errors in the mock
		_ = callback(bq.BlockQuerier, uint64(bq.MinFp), uint64(bq.MaxFp))
	}
	return nil
}

func createQueryInputFromBlockData(t *testing.T, tenant string, data [][]v1.SeriesWithBloom, nthSeries int) []*logproto.ChunkRef {
	t.Helper()
	n := 0
	res := make([]*logproto.ChunkRef, 0)
	for i := range data {
		for j := range data[i] {
			if n%nthSeries == 0 {
				chk := data[i][j].Series.Chunks[0]
				res = append(res, &logproto.ChunkRef{
					Fingerprint: uint64(data[i][j].Series.Fingerprint),
					UserID:      tenant,
					From:        chk.Start,
					Through:     chk.End,
					Checksum:    chk.Checksum,
				})
			}
			n++
		}
	}
	return res
}
