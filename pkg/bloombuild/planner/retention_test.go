package planner

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/bloombuild/planner/plannertest"
	"github.com/grafana/loki/v3/pkg/storage"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	storageconfig "github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util/mempool"
	"github.com/grafana/loki/v3/pkg/validation"
)

var testTime = plannertest.ParseDayTime("2024-12-31").ModelTime()

func TestRetention(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cfg         RetentionConfig
		lim         mockRetentionLimits
		prePopulate func(t *testing.T, schemaCfg storageconfig.SchemaConfig, bloomStore *bloomshipper.BloomStore)
		expectErr   bool
		check       func(t *testing.T, bloomStore *bloomshipper.BloomStore)
	}{
		{
			name: "retention disabled",
			cfg: RetentionConfig{
				Enabled:         false,
				MaxLookbackDays: 2 * 365,
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
			name: "unlimited retention",
			cfg: RetentionConfig{
				Enabled:         true,
				MaxLookbackDays: 2 * 365,
			},
			lim: mockRetentionLimits{
				retention: map[string]time.Duration{
					"1": 0,
				},
			},
			prePopulate: func(t *testing.T, schemaCfg storageconfig.SchemaConfig, bloomStore *bloomshipper.BloomStore) {
				putMetasForLastNDays(t, schemaCfg, bloomStore, "1", testTime, 200)
			},
			check: func(t *testing.T, bloomStore *bloomshipper.BloomStore) {
				metas := getGroupedMetasForLastNDays(t, bloomStore, "1", testTime, 500)
				require.Equal(t, 1, len(metas))
				require.Equal(t, 200, len(metas[0]))
			},
		},
		{
			name: "default retention",
			cfg: RetentionConfig{
				Enabled:         true,
				MaxLookbackDays: 2 * 365,
			},
			lim: mockRetentionLimits{
				defaultRetention: 30 * 24 * time.Hour,
			},
			prePopulate: func(t *testing.T, schemaCfg storageconfig.SchemaConfig, bloomStore *bloomshipper.BloomStore) {
				putMetasForLastNDays(t, schemaCfg, bloomStore, "1", testTime, 200)
			},
			check: func(t *testing.T, bloomStore *bloomshipper.BloomStore) {
				metas := getGroupedMetasForLastNDays(t, bloomStore, "1", testTime, 500)
				require.Equal(t, 1, len(metas))
				require.Equal(t, 31, len(metas[0]))
			},
		},
		{
			name: "retention lookback smaller than max retention",
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
				require.Equal(t, 41, len(metas[0]))  // 0-40th day
				require.Equal(t, 100, len(metas[1])) // 100th-200th day

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
			name: "retention lookback bigger than max retention",
			cfg: RetentionConfig{
				Enabled:         true,
				MaxLookbackDays: 2 * 365,
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
				require.Equal(t, 201, len(metas[0])) // 0th-200th

				// Tenant 4 has 400 days of retention, and we wrote 500 days of metas
				// Since the manager looks up to 100 days, we shouldn't have deleted any metas
				metas = getGroupedMetasForLastNDays(t, bloomStore, "4", testTime, 500)
				require.Equal(t, 1, len(metas))
				require.Equal(t, 401, len(metas[0])) // 0th-400th
			},
		},
		{
			name: "hit no tenants in table",
			cfg: RetentionConfig{
				Enabled:         true,
				MaxLookbackDays: 2 * 365,
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
			logger := log.NewNopLogger()
			//logger := log.NewLogfmtLogger(os.Stdout)

			bloomStore, schema, _, err := NewMockBloomStore(t, logger)
			require.NoError(t, err)

			rm := NewRetentionManager(
				tc.cfg,
				tc.lim,
				bloomStore,
				NewMetrics(nil, nil),
				logger,
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
	logger := log.NewNopLogger()
	//logger := log.NewLogfmtLogger(os.Stdout)

	bloomStore, schema, _, err := NewMockBloomStore(t, logger)
	require.NoError(t, err)

	rm := NewRetentionManager(
		RetentionConfig{
			Enabled:         true,
			MaxLookbackDays: 365,
		},
		mockRetentionLimits{
			retention: map[string]time.Duration{
				"1": 30 * 24 * time.Hour,
			},
		},
		bloomStore,
		NewMetrics(nil, nil),
		logger,
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

	// We now change the now() time to be a bit later in the day
	rm.now = func() model.Time {
		return testTime.Add(1 * time.Hour)
	}

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

func TestFindLongestRetention(t *testing.T) {
	for _, tc := range []struct {
		name              string
		globalRetention   time.Duration
		streamRetention   []validation.StreamRetention
		expectedRetention time.Duration
	}{
		{
			name:              "no retention",
			expectedRetention: 0,
		},
		{
			name:              "global retention",
			globalRetention:   30 * 24 * time.Hour,
			expectedRetention: 30 * 24 * time.Hour,
		},
		{
			name: "stream retention",
			streamRetention: []validation.StreamRetention{
				{
					Period: model.Duration(30 * 24 * time.Hour),
				},
			},
			expectedRetention: 30 * 24 * time.Hour,
		},
		{
			name: "two stream retention",
			streamRetention: []validation.StreamRetention{
				{
					Period: model.Duration(30 * 24 * time.Hour),
				},
				{
					Period: model.Duration(40 * 24 * time.Hour),
				},
			},
			expectedRetention: 40 * 24 * time.Hour,
		},
		{
			name:            "stream retention bigger than global",
			globalRetention: 20 * 24 * time.Hour,
			streamRetention: []validation.StreamRetention{
				{
					Period: model.Duration(30 * 24 * time.Hour),
				},
				{
					Period: model.Duration(40 * 24 * time.Hour),
				},
			},
			expectedRetention: 40 * 24 * time.Hour,
		},
		{
			name:            "global retention bigger than stream",
			globalRetention: 40 * 24 * time.Hour,
			streamRetention: []validation.StreamRetention{
				{
					Period: model.Duration(20 * 24 * time.Hour),
				},
				{
					Period: model.Duration(30 * 24 * time.Hour),
				},
			},
			expectedRetention: 40 * 24 * time.Hour,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			retention := findLongestRetention(tc.globalRetention, tc.streamRetention)
			require.Equal(t, tc.expectedRetention, retention)
		})
	}
}

func TestSmallestRetention(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		limits               RetentionLimits
		expectedRetention    time.Duration
		expectedHasRetention bool
	}{
		{
			name:              "no retention",
			limits:            mockRetentionLimits{},
			expectedRetention: 0,
		},
		{
			name: "default global retention",
			limits: mockRetentionLimits{
				defaultRetention: 30 * 24 * time.Hour,
			},
			expectedRetention: 30 * 24 * time.Hour,
		},
		{
			name: "default stream retention",
			limits: mockRetentionLimits{
				defaultStreamRetention: []validation.StreamRetention{
					{
						Period: model.Duration(30 * 24 * time.Hour),
					},
				},
			},
			expectedRetention: 30 * 24 * time.Hour,
		},
		{
			name: "tenant configured unlimited",
			limits: mockRetentionLimits{
				retention: map[string]time.Duration{
					"1": 0,
				},
				defaultRetention: 30 * 24 * time.Hour,
			},
			expectedRetention: 30 * 24 * time.Hour,
		},
		{
			name: "no default one tenant",
			limits: mockRetentionLimits{
				retention: map[string]time.Duration{
					"1": 30 * 24 * time.Hour,
				},
				streamRetention: map[string][]validation.StreamRetention{
					"1": {
						{
							Period: model.Duration(40 * 24 * time.Hour),
						},
					},
				},
			},
			expectedRetention: 40 * 24 * time.Hour,
		},
		{
			name: "no default two tenants",
			limits: mockRetentionLimits{
				retention: map[string]time.Duration{
					"1": 30 * 24 * time.Hour,
					"2": 20 * 24 * time.Hour,
				},
				streamRetention: map[string][]validation.StreamRetention{
					"1": {
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
			expectedRetention: 20 * 24 * time.Hour,
		},
		{
			name: "default bigger than tenant",
			limits: mockRetentionLimits{
				retention: map[string]time.Duration{
					"1": 10 * 24 * time.Hour,
				},
				streamRetention: map[string][]validation.StreamRetention{
					"1": {
						{
							Period: model.Duration(20 * 24 * time.Hour),
						},
					},
				},
				defaultRetention: 40 * 24 * time.Hour,
				defaultStreamRetention: []validation.StreamRetention{
					{
						Period: model.Duration(30 * 24 * time.Hour),
					},
				},
			},
			expectedRetention: 20 * 24 * time.Hour,
		},
		{
			name: "tenant bigger than default",
			limits: mockRetentionLimits{
				retention: map[string]time.Duration{
					"1": 30 * 24 * time.Hour,
				},
				streamRetention: map[string][]validation.StreamRetention{
					"1": {
						{
							Period: model.Duration(40 * 24 * time.Hour),
						},
					},
				},
				defaultRetention: 10 * 24 * time.Hour,
				defaultStreamRetention: []validation.StreamRetention{
					{
						Period: model.Duration(20 * 24 * time.Hour),
					},
				},
			},
			expectedRetention: 20 * 24 * time.Hour,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defaultLim := tc.limits.DefaultLimits()
			defaultRetention := findLongestRetention(time.Duration(defaultLim.RetentionPeriod), defaultLim.StreamRetention)
			tenantsRetention := retentionByTenant(tc.limits)

			retention := smallestEnabledRetention(defaultRetention, tenantsRetention)
			require.Equal(t, tc.expectedRetention, retention)
		})
	}
}

func TestRetentionConfigValidate(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cfg       RetentionConfig
		expectErr bool
	}{
		{
			name: "enabled and valid",
			cfg: RetentionConfig{
				Enabled:         true,
				MaxLookbackDays: 2 * 365,
			},
			expectErr: false,
		},
		{
			name: "invalid max lookback days",
			cfg: RetentionConfig{
				Enabled:         true,
				MaxLookbackDays: 0,
			},
			expectErr: true,
		},
		{
			name: "disabled and invalid",
			cfg: RetentionConfig{
				Enabled:         false,
				MaxLookbackDays: 0,
			},
			expectErr: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
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

// getGroupedMetasForLastNDays returns groups of continuous metas for the last N days.
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

func NewMockBloomStore(t *testing.T, logger log.Logger) (*bloomshipper.BloomStore, storageconfig.SchemaConfig, string, error) {
	workDir := t.TempDir()
	return NewMockBloomStoreWithWorkDir(t, workDir, logger)
}

func NewMockBloomStoreWithWorkDir(t *testing.T, workDir string, logger log.Logger) (*bloomshipper.BloomStore, storageconfig.SchemaConfig, string, error) {
	schemaCfg := storageconfig.SchemaConfig{
		Configs: []storageconfig.PeriodConfig{
			{
				ObjectType: types.StorageTypeFileSystem,
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
				ObjectType: types.StorageTypeFileSystem,
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
			WorkingDirectory:    []string{workDir},
			DownloadParallelism: 1,
			BlocksCache: config.BlocksCacheConfig{
				SoftLimit:     1 << 20,
				HardLimit:     2 << 20,
				TTL:           time.Hour,
				PurgeInterval: time.Hour,
			},
		},
	}

	reg := prometheus.NewPedanticRegistry()
	metrics := storage.NewClientMetrics()
	t.Cleanup(metrics.Unregister)

	metasCache := cache.NewMockCache()
	blocksCache := bloomshipper.NewFsBlocksCache(storageConfig.BloomShipperConfig.BlocksCache, prometheus.NewPedanticRegistry(), logger)

	store, err := bloomshipper.NewBloomStore(schemaCfg.Configs, storageConfig, metrics, metasCache, blocksCache, &mempool.SimpleHeapAllocator{}, reg, logger)
	if err == nil {
		t.Cleanup(store.Stop)
	}

	return store, schemaCfg, workDir, err
}

type mockRetentionLimits struct {
	retention              map[string]time.Duration
	streamRetention        map[string][]validation.StreamRetention
	defaultRetention       time.Duration
	defaultStreamRetention []validation.StreamRetention
}

func (m mockRetentionLimits) RetentionPeriod(tenant string) time.Duration {
	return m.retention[tenant]
}

func (m mockRetentionLimits) StreamRetention(tenant string) []validation.StreamRetention {
	return m.streamRetention[tenant]
}

func (m mockRetentionLimits) AllByUserID() map[string]*validation.Limits {
	tenants := make(map[string]*validation.Limits, len(m.retention))

	for tenant, retention := range m.retention {
		if _, ok := tenants[tenant]; !ok {
			tenants[tenant] = &validation.Limits{}
		}
		tenants[tenant].RetentionPeriod = model.Duration(retention)
	}

	for tenant, streamRetention := range m.streamRetention {
		if _, ok := tenants[tenant]; !ok {
			tenants[tenant] = &validation.Limits{}
		}
		tenants[tenant].StreamRetention = streamRetention
	}

	return tenants
}

func (m mockRetentionLimits) DefaultLimits() *validation.Limits {
	return &validation.Limits{
		RetentionPeriod: model.Duration(m.defaultRetention),
		StreamRetention: m.defaultStreamRetention,
	}
}
