//go:build integration

package integration

import (
	"context"
	"fmt"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/loki/v3/pkg/storage"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	bloomshipperconfig "github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util/mempool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"
)

func TestBloomBuilding(t *testing.T) {
	const (
		nSeries        = 10 //1000
		nLogsPerSeries = 50
	)

	clu := cluster.New(level.DebugValue(), cluster.SchemaWithTSDB, func(c *cluster.Cluster) {
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
	start := recordStartTime()
	tCompactor := clu.AddComponent(
		"compactor",
		"-target=compactor",
		"-compactor.compaction-interval=10m",
		"-compactor.run-once=true",
	)
	require.NoError(t, clu.Run())

	// Wait for compaction to finish.
	cliCompactor := client.New(tenantID, "", tCompactor.HTTPURL())
	checkCompactionFinished(t, cliCompactor, start)

	// Now create the bloom planner and builders
	start = recordStartTime()
	tBloomPlanner := clu.AddComponent(
		"bloom-planner",
		"-target=bloom-planner",
		"-bloom-build.enabled=true",
		"-bloom-build.enable=true",
		"-bloom-build.planner.interval=10m",
		"-bloom-build.planner.min-table-offset=0",
	)
	require.NoError(t, clu.Run())
	tBloomBuilder := clu.AddComponent(
		"bloom-builder",
		"-target=bloom-builder",
		"-bloom-build.enabled=true",
		"-bloom-build.enable=true",
		"-bloom-build.builder.planner-address="+tBloomPlanner.GRPCURL(),
	)
	require.NoError(t, clu.Run())

	// Wait for bloom build to finish
	cliPlanner := client.New(tenantID, "", tBloomPlanner.HTTPURL())
	checkBloomBuildFinished(t, cliPlanner, start)

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
	start = recordStartTime()
	require.NoError(t, tCompactor.Restart())
	cliCompactor = client.New(tenantID, "", tCompactor.HTTPURL())
	checkCompactionFinished(t, cliCompactor, start)

	// Restart bloom planner to trigger bloom build
	start = recordStartTime()
	require.NoError(t, tBloomPlanner.Restart())

	// TODO(salvacorts): Implement retry on builder so we don't need to restart it.
	tBloomBuilder.AddFlags("-bloom-build.builder.planner-address=" + tBloomPlanner.GRPCURL())
	require.NoError(t, tBloomBuilder.Restart())

	// Wait for bloom build to finish
	cliPlanner = client.New(tenantID, "", tBloomPlanner.HTTPURL())
	checkBloomBuildFinished(t, cliPlanner, start)

	// Check that all series (both previous and new ones) pushed are present in the metas and blocks.
	// This check ensures up to 1 meta per series, which tests deletion of old metas.
	checkSeriesInBlooms(t, now, tenantID, bloomStore, series)
}

func recordStartTime() time.Time {
	start := time.Now()
	time.Sleep(1 * time.Second) // Gauge seconds has second precision, so we need to wait a bit.
	return start
}

func checkCompactionFinished(t *testing.T, cliCompactor *client.Client, start time.Time) {
	require.Eventually(t, func() bool {
		metrics, err := cliCompactor.Metrics()
		require.NoError(t, err)

		metricName := "loki_boltdb_shipper_compact_tables_operation_last_successful_run_timestamp_seconds"
		val, _, err := extractMetric(metricName, metrics)
		require.NoError(t, err)

		lastRun := time.Unix(int64(val), 0)
		return lastRun.After(start)
	}, 30*time.Second, 1*time.Second)
}

func checkBloomBuildFinished(t *testing.T, cliPlanner *client.Client, start time.Time) {
	require.Eventually(t, func() bool {
		metrics, err := cliPlanner.Metrics()
		require.NoError(t, err)

		metricName := "loki_bloomplanner_build_last_successful_run_timestamp_seconds"
		val, _, err := extractMetric(metricName, metrics)
		require.NoError(t, err)

		lastRun := time.Unix(int64(val), 0)
		return lastRun.After(start)
	}, 30*time.Second, 1*time.Second)
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
