package bloomcompactor

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	storageconfig "github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper/config"
	util_log "github.com/grafana/loki/pkg/util/log"
	lokiring "github.com/grafana/loki/pkg/util/ring"
	"github.com/grafana/loki/pkg/validation"
)

var testTime = parseDayTime("2024-12-31").ModelTime()

func TestRetention(t *testing.T) {
	for _, tc := range []struct {
		name          string
		ownsRetention bool
		cfg           RetentionConfig
		lim           mockRetentionLimits
		prePopulate   func(t *testing.T, schemaCfg storageconfig.SchemaConfig, bloomStore *bloomshipper.BloomStore)
		expectErr     bool
		check         func(t *testing.T, bloomStore *bloomshipper.BloomStore)
	}{
		{
			name:          "retention disabled",
			ownsRetention: true,
			cfg: RetentionConfig{
				Enabled: false,
			},
			lim: mockRetentionLimits{
				retention: map[string]time.Duration{
					"1": 30 * 24 * time.Hour,
					"2": 200 * 24 * time.Hour,
					"3": 500 * 24 * time.Hour,
				},
			},
			prePopulate: func(t *testing.T, schemaCfg storageconfig.SchemaConfig, bloomStore *bloomshipper.BloomStore) {
				putMetasForLastNDays(t, schemaCfg, bloomStore, "1", testTime, 200)
				putMetasForLastNDays(t, schemaCfg, bloomStore, "2", testTime, 50)
				putMetasForLastNDays(t, schemaCfg, bloomStore, "3", testTime, 500)
			},
			check: func(t *testing.T, bloomStore *bloomshipper.BloomStore) {
				metas := getGroupedMetasForLastNDays(t, bloomStore, "1", testTime, 500)
				require.Equal(t, 1, len(metas))
				require.Equal(t, 200, len(metas[0]))
				metas = getGroupedMetasForLastNDays(t, bloomStore, "2", testTime, 500)
				require.Equal(t, 1, len(metas))
				require.Equal(t, 50, len(metas[0]))
				metas = getGroupedMetasForLastNDays(t, bloomStore, "3", testTime, 500)
				require.Equal(t, 1, len(metas))
				require.Equal(t, 500, len(metas[0]))
			},
		},
		{
			name:          "compactor does not own retention",
			ownsRetention: false,
			cfg: RetentionConfig{
				Enabled: true,
			},
			lim: mockRetentionLimits{
				retention: map[string]time.Duration{
					"1": 30 * 24 * time.Hour,
					"2": 200 * 24 * time.Hour,
					"3": 500 * 24 * time.Hour,
				},
			},
			prePopulate: func(t *testing.T, schemaCfg storageconfig.SchemaConfig, bloomStore *bloomshipper.BloomStore) {
				putMetasForLastNDays(t, schemaCfg, bloomStore, "1", testTime, 200)
				putMetasForLastNDays(t, schemaCfg, bloomStore, "2", testTime, 50)
				putMetasForLastNDays(t, schemaCfg, bloomStore, "3", testTime, 500)
			},
			check: func(t *testing.T, bloomStore *bloomshipper.BloomStore) {
				metas := getGroupedMetasForLastNDays(t, bloomStore, "1", testTime, 500)
				require.Equal(t, 1, len(metas))
				require.Equal(t, 200, len(metas[0]))
				metas = getGroupedMetasForLastNDays(t, bloomStore, "2", testTime, 500)
				require.Equal(t, 1, len(metas))
				require.Equal(t, 50, len(metas[0]))
				metas = getGroupedMetasForLastNDays(t, bloomStore, "3", testTime, 500)
				require.Equal(t, 1, len(metas))
				require.Equal(t, 500, len(metas[0]))
			},
		},
		{
			name:          "limited retention lookback",
			ownsRetention: true,
			cfg: RetentionConfig{
				Enabled:         true,
				MaxLookbackDays: 100,
			},
			lim: mockRetentionLimits{
				retention: map[string]time.Duration{
					"1": 30 * 24 * time.Hour,
					"2": 20 * 24 * time.Hour,
					"3": 200 * 24 * time.Hour,
					"4": 400 * 24 * time.Hour,
				},
				streamRetention: map[string][]validation.StreamRetention{
					"1": {
						{
							Period: model.Duration(30 * 24 * time.Hour),
						},
						{
							Period: model.Duration(40 * 24 * time.Hour),
						},
					},
					"2": {
						{
							Period: model.Duration(10 * 24 * time.Hour),
						},
					},
				},
			},
			prePopulate: func(t *testing.T, schemaCfg storageconfig.SchemaConfig, bloomStore *bloomshipper.BloomStore) {
				putMetasForLastNDays(t, schemaCfg, bloomStore, "1", testTime, 200)
				putMetasForLastNDays(t, schemaCfg, bloomStore, "2", testTime, 50)
				putMetasForLastNDays(t, schemaCfg, bloomStore, "3", testTime, 500)
				putMetasForLastNDays(t, schemaCfg, bloomStore, "4", testTime, 500)
			},
			check: func(t *testing.T, bloomStore *bloomshipper.BloomStore) {
				// Tenant 1 has 40 days of retention, and we wrote 200 days of metas
				// We should get two groups: 0th-40th and 101th-200th
				metas := getGroupedMetasForLastNDays(t, bloomStore, "1", testTime, 500)
				require.Equal(t, 2, len(metas))
				require.Equal(t, 41, len(metas[0])) // 0-40th day
				require.Equal(t, 99, len(metas[1])) // 101th-200th day

				// Tenant 2 has 20 days of retention, and we wrote 50 days of metas
				// We should get one group: 0th-20th
				metas = getGroupedMetasForLastNDays(t, bloomStore, "2", testTime, 500)
				require.Equal(t, 1, len(metas))
				require.Equal(t, 21, len(metas[0])) // 0th-20th

				// Tenant 3 has 200 days of retention, and we wrote 500 days of metas
				// Since the manager looks up to 100 days, we shouldn't have deleted any metas
				metas = getGroupedMetasForLastNDays(t, bloomStore, "3", testTime, 500)
				require.Equal(t, 1, len(metas))
				require.Equal(t, 500, len(metas[0])) // 0th-500th

				// Tenant 4 has 400 days of retention, and we wrote 500 days of metas
				// Since the manager looks up to 100 days, we shouldn't have deleted any metas
				metas = getGroupedMetasForLastNDays(t, bloomStore, "4", testTime, 500)
				require.Equal(t, 1, len(metas))
				require.Equal(t, 500, len(metas[0])) // 0th-500th
			},
		},
		{
			name:          "unlimited retention lookback",
			ownsRetention: true,
			cfg: RetentionConfig{
				Enabled: true,
			},
			lim: mockRetentionLimits{
				retention: map[string]time.Duration{
					"1": 30 * 24 * time.Hour,
					"2": 20 * 24 * time.Hour,
					"3": 200 * 24 * time.Hour,
					"4": 400 * 24 * time.Hour,
				},
				streamRetention: map[string][]validation.StreamRetention{
					"1": {
						{
							Period: model.Duration(30 * 24 * time.Hour),
						},
						{
							Period: model.Duration(40 * 24 * time.Hour),
						},
					},
					"2": {
						{
							Period: model.Duration(10 * 24 * time.Hour),
						},
					},
				},
			},
			prePopulate: func(t *testing.T, schemaCfg storageconfig.SchemaConfig, bloomStore *bloomshipper.BloomStore) {
				putMetasForLastNDays(t, schemaCfg, bloomStore, "1", testTime, 200)
				putMetasForLastNDays(t, schemaCfg, bloomStore, "2", testTime, 50)
				putMetasForLastNDays(t, schemaCfg, bloomStore, "3", testTime, 500)
				putMetasForLastNDays(t, schemaCfg, bloomStore, "4", testTime, 500)
			},
			check: func(t *testing.T, bloomStore *bloomshipper.BloomStore) {
				// Tenant 1 has 40 days of retention, and we wrote 200 days of metas
				// We should get one groups: 0th-40th
				metas := getGroupedMetasForLastNDays(t, bloomStore, "1", testTime, 500)
				require.Equal(t, 1, len(metas))
				require.Equal(t, 41, len(metas[0])) // 0-40th day

				// Tenant 2 has 20 days of retention, and we wrote 50 days of metas
				// We should get one group: 0th-20th
				metas = getGroupedMetasForLastNDays(t, bloomStore, "2", testTime, 500)
				require.Equal(t, 1, len(metas))
				require.Equal(t, 21, len(metas[0])) // 0th-20th

				// Tenant 3 has 200 days of retention, and we wrote 500 days of metas
				// We should get one group: 0th-200th
				metas = getGroupedMetasForLastNDays(t, bloomStore, "3", testTime, 500)
				require.Equal(t, 1, len(metas))
				require.Equal(t, 201, len(metas[0])) // 0th-500th

				// Tenant 4 has 400 days of retention, and we wrote 500 days of metas
				// Since the manager looks up to 100 days, we shouldn't have deleted any metas
				metas = getGroupedMetasForLastNDays(t, bloomStore, "4", testTime, 500)
				require.Equal(t, 1, len(metas))
				require.Equal(t, 401, len(metas[0])) // 0th-500th
			},
		},
		{
			name:          "hit no tenants in table",
			ownsRetention: true,
			cfg: RetentionConfig{
				Enabled: true,
			},
			lim: mockRetentionLimits{
				retention: map[string]time.Duration{
					"1": 30 * 24 * time.Hour,
				},
			},
			prePopulate: func(t *testing.T, schemaCfg storageconfig.SchemaConfig, bloomStore *bloomshipper.BloomStore) {
				// Place metas with a gap of 50 days. [0th-100th], [151th-200th]
				putMetasForLastNDays(t, schemaCfg, bloomStore, "1", testTime, 100)
				putMetasForLastNDays(t, schemaCfg, bloomStore, "1", testTime.Add(-150*24*time.Hour), 50)
			},
			check: func(t *testing.T, bloomStore *bloomshipper.BloomStore) {
				// We should get two groups: 0th-30th and 151th-200th
				metas := getGroupedMetasForLastNDays(t, bloomStore, "1", testTime, 500)
				require.Equal(t, 2, len(metas))
				require.Equal(t, 31, len(metas[0])) // 0th-30th day
				require.Equal(t, 50, len(metas[1])) // 151th-200th day
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bloomStore, schema, _, err := NewMockBloomStore(t)
			require.NoError(t, err)

			rm := NewRetentionManager(
				tc.cfg,
				tc.lim,
				bloomStore,
				mockSharding{
					ownsRetention: tc.ownsRetention,
				},
				NewMetrics(nil, v1.NewMetrics(nil)),
				util_log.Logger,
			)
			rm.now = func() model.Time {
				return testTime
			}

			tc.prePopulate(t, schema, bloomStore)

			err = rm.Apply(context.Background())
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			tc.check(t, bloomStore)
		})
	}
}

func TestRetentionRunsOncePerDay(t *testing.T) {
	bloomStore, schema, _, err := NewMockBloomStore(t)
	require.NoError(t, err)

	rm := NewRetentionManager(
		RetentionConfig{
			Enabled: true,
		},
		mockRetentionLimits{
			retention: map[string]time.Duration{
				"1": 30 * 24 * time.Hour,
			},
		},
		bloomStore,
		mockSharding{
			ownsRetention: true,
		},
		NewMetrics(nil, v1.NewMetrics(nil)),
		util_log.Logger,
	)
	rm.now = func() model.Time {
		return testTime
	}

	// Write metas for the last 100 days and run retention
	putMetasForLastNDays(t, schema, bloomStore, "1", testTime, 100)
	err = rm.Apply(context.Background())
	require.NoError(t, err)

	// We should get only the first 30 days of metas
	metas := getGroupedMetasForLastNDays(t, bloomStore, "1", testTime, 100)
	require.Equal(t, 1, len(metas))
	require.Equal(t, 31, len(metas[0])) // 0th-30th day

	// Write metas again and run retention. Since we already ran retention at now()'s day,
	// Apply should be a noop, and therefore we should be able to get all the 100 days of metas
	putMetasForLastNDays(t, schema, bloomStore, "1", testTime, 100)
	err = rm.Apply(context.Background())
	require.NoError(t, err)

	metas = getGroupedMetasForLastNDays(t, bloomStore, "1", testTime, 100)
	require.Equal(t, 1, len(metas))
	require.Equal(t, 100, len(metas[0]))

	// We now change the now() time to be the next day, retention should run again
	rm.now = func() model.Time {
		return testTime.Add(24 * time.Hour)
	}
	err = rm.Apply(context.Background())
	require.NoError(t, err)

	// We should only see the first 30 days of metas
	metas = getGroupedMetasForLastNDays(t, bloomStore, "1", testTime, 100)
	require.Equal(t, 1, len(metas))
	require.Equal(t, 30, len(metas[0])) // 0th-30th day
}

func TestOwnsRetention(t *testing.T) {
	for _, tc := range []struct {
		name          string
		numCompactors int
	}{
		{
			name:          "single compactor",
			numCompactors: 1,
		},
		{
			name:          "multiple compactors",
			numCompactors: 100,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ringManagers []*lokiring.RingManager
			for i := 0; i < tc.numCompactors; i++ {
				var cfg Config
				cfg.RegisterFlags(flag.NewFlagSet("ring", flag.PanicOnError))
				cfg.Ring.KVStore.Store = "inmemory"
				cfg.Ring.InstanceID = fmt.Sprintf("bloom-compactor-%d", i)
				cfg.Ring.InstanceAddr = fmt.Sprintf("localhost-%d", i)

				ringManager, err := lokiring.NewRingManager("bloom-compactor", lokiring.ServerMode, cfg.Ring, 1, cfg.Ring.NumTokens, util_log.Logger, prometheus.NewRegistry())
				require.NoError(t, err)
				require.NoError(t, ringManager.StartAsync(context.Background()))

				ringManagers = append(ringManagers, ringManager)
			}
			t.Cleanup(func() {
				// Stop all rings and wait for them to stop.
				for _, ringManager := range ringManagers {
					ringManager.StopAsync()
					require.Eventually(t, func() bool {
						return ringManager.State() == services.Terminated
					}, 1*time.Minute, 100*time.Millisecond)
				}
			})

			// Wait for all rings to see each other.
			for _, ringManager := range ringManagers {
				require.Eventually(t, func() bool {
					running := ringManager.State() == services.Running
					discovered := ringManager.Ring.InstancesCount() == tc.numCompactors
					return running && discovered
				}, 1*time.Minute, 100*time.Millisecond)
			}

			var shardings []retentionSharding
			for _, ringManager := range ringManagers {
				shardings = append(shardings, newFirstTokenRetentionSharding(ringManager.Ring, ringManager.RingLifecycler))
			}

			var ownsRetention int
			for _, sharding := range shardings {
				owns, err := sharding.OwnsRetention()
				require.NoError(t, err)
				if owns {
					ownsRetention++
				}
			}

			require.Equal(t, 1, ownsRetention)
		})
	}
}

func putMetasForLastNDays(t *testing.T, schemaCfg storageconfig.SchemaConfig, bloomStore *bloomshipper.BloomStore, tenant string, start model.Time, days int) {
	const metasPerDay = 2

	startDay := storageconfig.NewDayTime(start)
	endDay := storageconfig.NewDayTime(startDay.Add(-time.Duration(days) * 24 * time.Hour))
	for day := startDay; day.After(endDay); day = day.Dec() {
		period, err := schemaCfg.SchemaForTime(day.ModelTime())
		require.NoError(t, err)

		dayTable := storageconfig.NewDayTable(day, period.IndexTables.Prefix)
		bloomClient, err := bloomStore.Client(dayTable.ModelTime())
		require.NoErrorf(t, err, "failed to get bloom client for day %d: %s", day, err)

		for i := 0; i < metasPerDay; i++ {
			err = bloomClient.PutMeta(context.Background(), bloomshipper.Meta{
				MetaRef: bloomshipper.MetaRef{
					Ref: bloomshipper.Ref{
						TenantID:  tenant,
						TableName: dayTable.String(),
						Bounds:    v1.NewBounds(model.Fingerprint(i*100), model.Fingerprint(i*100+100)),
					},
				},
				Blocks: []bloomshipper.BlockRef{},
			})
			require.NoError(t, err)
		}
	}
}

// getMetasForLastNDays returns groups of continuous metas for the last N days.
func getGroupedMetasForLastNDays(t *testing.T, bloomStore *bloomshipper.BloomStore, tenant string, start model.Time, days int) [][][]bloomshipper.Meta {
	metasGrouped := make([][][]bloomshipper.Meta, 0)
	currentGroup := make([][]bloomshipper.Meta, 0)

	startDay := storageconfig.NewDayTime(start)
	endDay := storageconfig.NewDayTime(startDay.Add(-time.Duration(days) * 24 * time.Hour))

	for day := startDay; day.After(endDay); day = day.Dec() {
		metas, err := bloomStore.FetchMetas(context.Background(), bloomshipper.MetaSearchParams{
			TenantID: tenant,
			Interval: bloomshipper.NewInterval(day.Bounds()),
			Keyspace: v1.NewBounds(0, math.MaxUint64),
		})
		require.NoError(t, err)
		if len(metas) == 0 {
			// We have reached the end of the metas group: cut a new group
			if len(currentGroup) > 0 {
				metasGrouped = append(metasGrouped, currentGroup)
				currentGroup = make([][]bloomshipper.Meta, 0)
			}
			continue
		}
		currentGroup = append(currentGroup, metas)
	}

	// Append the last group if it's not empty
	if len(currentGroup) > 0 {
		metasGrouped = append(metasGrouped, currentGroup)
	}

	return metasGrouped
}

func NewMockBloomStore(t *testing.T) (*bloomshipper.BloomStore, storageconfig.SchemaConfig, string, error) {
	workDir := t.TempDir()
	return NewMockBloomStoreWithWorkDir(t, workDir)
}

func NewMockBloomStoreWithWorkDir(t *testing.T, workDir string) (*bloomshipper.BloomStore, storageconfig.SchemaConfig, string, error) {
	schemaCfg := storageconfig.SchemaConfig{
		Configs: []storageconfig.PeriodConfig{
			{
				ObjectType: storageconfig.StorageTypeFileSystem,
				From: storageconfig.DayTime{
					Time: testTime.Add(-2 * 365 * 24 * time.Hour), // -2 year
				},
				IndexTables: storageconfig.IndexPeriodicTableConfig{
					PeriodicTableConfig: storageconfig.PeriodicTableConfig{
						Period: 24 * time.Hour,
						Prefix: "schema_a_table_",
					}},
			},
			{
				ObjectType: storageconfig.StorageTypeFileSystem,
				From: storageconfig.DayTime{
					Time: testTime.Add(-365 * 24 * time.Hour), // -1 year
				},
				IndexTables: storageconfig.IndexPeriodicTableConfig{
					PeriodicTableConfig: storageconfig.PeriodicTableConfig{
						Period: 24 * time.Hour,
						Prefix: "schema_b_table_",
					}},
			},
		},
	}

	storageConfig := storage.Config{
		FSConfig: local.FSConfig{
			Directory: workDir,
		},
		BloomShipperConfig: config.Config{
			WorkingDirectory: workDir + "/bloomshipper",
			BlocksDownloadingQueue: config.DownloadingQueueConfig{
				WorkersCount: 1,
			},
			BlocksCache: cache.EmbeddedCacheConfig{
				MaxSizeItems: 1000,
				TTL:          1 * time.Hour,
			},
		},
	}

	reg := prometheus.NewPedanticRegistry()
	metrics := storage.NewClientMetrics()
	t.Cleanup(metrics.Unregister)
	logger := log.NewLogfmtLogger(os.Stderr)

	metasCache := cache.NewMockCache()
	blocksCache := bloomshipper.NewBlocksCache(storageConfig.BloomShipperConfig.BlocksCache, prometheus.NewPedanticRegistry(), logger)

	store, err := bloomshipper.NewBloomStore(schemaCfg.Configs, storageConfig, metrics, metasCache, blocksCache, reg, logger)
	if err == nil {
		t.Cleanup(store.Stop)
	}

	return store, schemaCfg, workDir, err
}

type mockRetentionLimits struct {
	retention       map[string]time.Duration
	streamRetention map[string][]validation.StreamRetention
}

func (m mockRetentionLimits) RetentionPeriod(tenant string) time.Duration {
	return m.retention[tenant]
}

func (m mockRetentionLimits) StreamRetention(tenant string) []validation.StreamRetention {
	return m.streamRetention[tenant]
}

type mockSharding struct {
	ownsRetention bool
}

func (m mockSharding) OwnsRetention() (bool, error) {
	return m.ownsRetention, nil
}
