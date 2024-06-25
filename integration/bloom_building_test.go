//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"
	"github.com/grafana/loki/v3/pkg/storage"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	bloomshipperconfig "github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util/mempool"
)

func TestBloomBuilding(t *testing.T) {
	const (
		nSeries        = 10 //1000
		nLogsPerSeries = 50
		nBuilders      = 5
	)

	clu := cluster.New(nil, cluster.SchemaWithTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer("v13")
	})
	defer func() {
		require.NoError(t, clu.Cleanup())
	}()

	// First run distributor and ingester and write some data across many series.
	tDistributor := clu.AddComponent(
		"distributor",
		"-target=distributor",
	)
	tIngester := clu.AddComponent(
		"ingester",
		"-target=ingester",
		"-ingester.flush-on-shutdown=true",
	)
	require.NoError(t, clu.Run())

	tenantID := "fake"
	now := time.Now()
	cliDistributor := client.New(tenantID, "", tDistributor.HTTPURL())
	cliIngester := client.New(tenantID, "", tIngester.HTTPURL())
	cliIngester.Now = now

	// We now ingest some logs across many series.
	series := make([]labels.Labels, 0, nSeries)
	for i := 0; i < nSeries; i++ {
		lbs := labels.FromStrings("job", fmt.Sprintf("job-%d", i))
		series = append(series, lbs)

		for j := 0; j < nLogsPerSeries; j++ {
			require.NoError(t, cliDistributor.PushLogLine(fmt.Sprintf("log line %d", j), now, nil, lbs.Map()))
		}
	}

	// restart ingester which should flush the chunks and index
	require.NoError(t, tIngester.Restart())

	// Start compactor and wait for compaction to finish.
	tCompactor := clu.AddComponent(
		"compactor",
		"-target=compactor",
		"-compactor.compaction-interval=10m",
		"-compactor.run-once=true",
	)
	require.NoError(t, clu.Run())

	// Wait for compaction to finish.
	time.Sleep(5 * time.Second)

	// Now create the bloom planner and builders
	tBloomPlanner := clu.AddComponent(
		"bloom-planner",
		"-target=bloom-planner",
		"-bloom-build.enabled=true",
		"-bloom-build.enable=true",
		"-bloom-build.planner.interval=10m",
		"-bloom-build.planner.min-table-offset=0",
	)
	require.NoError(t, clu.Run())

	// Add several builders
	builders := make([]*cluster.Component, 0, nBuilders)
	for i := 0; i < nBuilders; i++ {
		builder := clu.AddComponent(
			"bloom-builder",
			"-target=bloom-builder",
			"-bloom-build.enabled=true",
			"-bloom-build.enable=true",
			"-bloom-build.builder.planner-address="+tBloomPlanner.GRPCURL(),
		)
		builders = append(builders, builder)
	}
	require.NoError(t, clu.Run())

	// Wait for bloom build to finish
	time.Sleep(5 * time.Second)

	// Create bloom client to fetch metas and blocks.
	bloomStore := createBloomStore(t, tBloomPlanner.ClusterSharedPath())

	// Check that all series pushed are present in the metas and blocks.
	checkSeriesInBlooms(t, now, tenantID, bloomStore, series)

	// Push some more logs so TSDBs need to be updated.
	for i := 0; i < nSeries; i++ {
		lbs := labels.FromStrings("job", fmt.Sprintf("job-new-%d", i))
		series = append(series, lbs)

		for j := 0; j < nLogsPerSeries; j++ {
			require.NoError(t, cliDistributor.PushLogLine(fmt.Sprintf("log line %d", j), now, nil, lbs.Map()))
		}
	}

	// restart ingester which should flush the chunks and index
	require.NoError(t, tIngester.Restart())

	// Restart compactor and wait for compaction to finish so TSDBs are updated.
	require.NoError(t, tCompactor.Restart())
	time.Sleep(5 * time.Second)

	// Restart bloom planner to trigger bloom build
	require.NoError(t, tBloomPlanner.Restart())

	// TODO(salvacorts): Implement retry on builder so we don't need to restart them.
	for _, tBloomBuilder := range builders {
		tBloomBuilder.AddFlags("-bloom-build.builder.planner-address=" + tBloomPlanner.GRPCURL())
		require.NoError(t, tBloomBuilder.Restart())
	}

	// Wait for bloom build to finish
	time.Sleep(5 * time.Second)

	// Check that all series (both previous and new ones) pushed are present in the metas and blocks.
	// This check ensures up to 1 meta per series, which tests deletion of old metas.
	checkSeriesInBlooms(t, now, tenantID, bloomStore, series)
}

func createBloomStore(t *testing.T, sharedPath string) *bloomshipper.BloomStore {
	logger := log.NewNopLogger()
	//logger := log.NewLogfmtLogger(os.Stdout)

	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From: parseDayTime("2023-09-01"),
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_tsdb_",
						Period: 24 * time.Hour,
					},
				},
				IndexType:  types.TSDBType,
				ObjectType: types.StorageTypeFileSystem,
				Schema:     "v13",
				RowShards:  16,
			},
		},
	}
	storageCfg := storage.Config{
		BloomShipperConfig: bloomshipperconfig.Config{
			WorkingDirectory:    []string{sharedPath + "/bloom-store-test"},
			DownloadParallelism: 1,
			BlocksCache: bloomshipperconfig.BlocksCacheConfig{
				SoftLimit: flagext.Bytes(10 << 20),
				HardLimit: flagext.Bytes(20 << 20),
				TTL:       time.Hour,
			},
		},
		FSConfig: local.FSConfig{
			Directory: sharedPath + "/fs-store-1",
		},
	}

	reg := prometheus.NewPedanticRegistry()
	metasCache := cache.NewNoopCache()
	blocksCache := bloomshipper.NewFsBlocksCache(storageCfg.BloomShipperConfig.BlocksCache, reg, logger)

	store, err := bloomshipper.NewBloomStore(schemaCfg.Configs, storageCfg, storage.ClientMetrics{}, metasCache, blocksCache, &mempool.SimpleHeapAllocator{}, reg, logger)
	require.NoError(t, err)

	return store
}

func checkSeriesInBlooms(
	t *testing.T,
	now time.Time,
	tenantID string,
	bloomStore *bloomshipper.BloomStore,
	series []labels.Labels,
) {
	for _, lbs := range series {
		seriesFP := model.Fingerprint(lbs.Hash())

		metas, err := bloomStore.FetchMetas(context.Background(), bloomshipper.MetaSearchParams{
			TenantID: tenantID,
			Interval: bloomshipper.NewInterval(model.TimeFromUnix(now.Add(-24*time.Hour).Unix()), model.TimeFromUnix(now.Unix())),
			Keyspace: v1.NewBounds(seriesFP, seriesFP),
		})
		require.NoError(t, err)

		// Only one meta should be present.
		require.Len(t, metas, 1)

		var relevantBlocks []bloomshipper.BlockRef
		for _, block := range metas[0].Blocks {
			if block.Cmp(uint64(seriesFP)) != v1.Overlap {
				continue
			}
			relevantBlocks = append(relevantBlocks, block)
		}

		// Only one block should be relevant.
		require.Len(t, relevantBlocks, 1)

		queriers, err := bloomStore.FetchBlocks(context.Background(), relevantBlocks)
		require.NoError(t, err)
		require.Len(t, queriers, 1)
		querier := queriers[0]

		require.NoError(t, querier.Seek(seriesFP))
		require.Equal(t, seriesFP, querier.At().Series.Fingerprint)
	}
}

func parseDayTime(s string) config.DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return config.DayTime{
		Time: model.TimeFromUnix(t.Unix()),
	}
}
