package loki

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/indexgateway"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	bloomshipperconfig "github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/boltdb"
	"github.com/grafana/loki/v3/pkg/storage/types"
)

func Test_calculateMaxLookBack(t *testing.T) {
	type args struct {
		pc                config.PeriodConfig
		maxLookBackConfig time.Duration
		minDuration       time.Duration
	}
	tests := []struct {
		name    string
		args    args
		want    time.Duration
		wantErr bool
	}{
		{
			name: "default",
			args: args{
				pc: config.PeriodConfig{
					ObjectType: "filesystem",
				},
				maxLookBackConfig: 0,
				minDuration:       time.Hour,
			},
			want:    time.Hour,
			wantErr: false,
		},
		{
			name: "infinite",
			args: args{
				pc: config.PeriodConfig{
					ObjectType: "filesystem",
				},
				maxLookBackConfig: -1,
				minDuration:       time.Hour,
			},
			want:    -1,
			wantErr: false,
		},
		{
			name: "invalid store type",
			args: args{
				pc: config.PeriodConfig{
					ObjectType: "gcs",
				},
				maxLookBackConfig: -1,
				minDuration:       time.Hour,
			},
			want:    0,
			wantErr: true,
		},
		{
			name: "less than minDuration",
			args: args{
				pc: config.PeriodConfig{
					ObjectType: "filesystem",
				},
				maxLookBackConfig: 1 * time.Hour,
				minDuration:       2 * time.Hour,
			},
			want:    0,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calculateMaxLookBack(tt.args.pc, tt.args.maxLookBackConfig, tt.args.minDuration)
			if (err != nil) != tt.wantErr {
				t.Errorf("calculateMaxLookBack() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("calculateMaxLookBack() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func prepareGlobalMetricsRegistry(t *testing.T) {
	oldReg, oldGat := prometheus.DefaultRegisterer, prometheus.DefaultGatherer

	reg := prometheus.NewRegistry()
	prometheus.DefaultRegisterer, prometheus.DefaultGatherer = reg, reg

	t.Cleanup(func() {
		prometheus.DefaultRegisterer, prometheus.DefaultGatherer = oldReg, oldGat
	})
}

func TestMultiKVSetup(t *testing.T) {
	dir := t.TempDir()

	for target, checkFn := range map[string]func(t *testing.T, c Config){
		All: func(t *testing.T, c Config) {
			require.NotNil(t, c.CompactorConfig.CompactorRing.KVStore.Multi.ConfigProvider)
			require.NotNil(t, c.Distributor.DistributorRing.KVStore.Multi.ConfigProvider)
			require.NotNil(t, c.IndexGateway.Ring.KVStore.Multi.ConfigProvider)
			require.NotNil(t, c.Ingester.LifecyclerConfig.RingConfig.KVStore.Multi.ConfigProvider)
			require.NotNil(t, c.QueryScheduler.SchedulerRing.KVStore.Multi.ConfigProvider)
			require.NotNil(t, c.Ruler.Ring.KVStore.Multi.ConfigProvider)
		},

		Compactor: func(t *testing.T, c Config) {
			require.NotNil(t, c.CompactorConfig.CompactorRing.KVStore.Multi.ConfigProvider)
		},

		Distributor: func(t *testing.T, c Config) {
			require.NotNil(t, c.Ingester.LifecyclerConfig.RingConfig.KVStore.Multi.ConfigProvider)
		},

		IndexGateway: func(t *testing.T, c Config) {
			require.NotNil(t, c.IndexGateway.Ring.KVStore.Multi.ConfigProvider)
		},

		Ingester: func(t *testing.T, c Config) {
			require.NotNil(t, c.Ingester.LifecyclerConfig.RingConfig.KVStore.Multi.ConfigProvider)
		},

		QueryScheduler: func(t *testing.T, c Config) {
			require.NotNil(t, c.QueryScheduler.SchedulerRing.KVStore.Multi.ConfigProvider)
		},

		Ruler: func(t *testing.T, c Config) {
			require.NotNil(t, c.Ruler.Ring.KVStore.Multi.ConfigProvider)
		},
	} {
		t.Run(target, func(test *testing.T) {
			prepareGlobalMetricsRegistry(test)

			cfg := minimalWorkingConfig(test, dir, target)
			cfg.RuntimeConfig.LoadPath = []string{filepath.Join(dir, "config.yaml")}
			c, err := New(cfg)
			require.NoError(test, err)

			_, err = c.ModuleManager.InitModuleServices(cfg.Target...)
			require.NoError(test, err)
			defer c.Server.Stop()

			checkFn(test, c.Cfg)
		})
	}
}

func TestIndexGatewayRingMode_when_TargetIsLegacyReadOrBackend(t *testing.T) {
	dir := t.TempDir()

	type ringModeTestCase struct {
		name        string
		transformer func(cfg *Config)
		target      string
	}

	for _, tc := range []ringModeTestCase{
		{
			name:   "leagcy read",
			target: Read,
			transformer: func(cfg *Config) {
				cfg.LegacyReadTarget = true
			},
		},
		{
			name:   "backend",
			target: Backend,
			transformer: func(cfg *Config) {
				cfg.LegacyReadTarget = false
			},
		},
	} {
		t.Run(fmt.Sprintf("IndexGateway always set to ring mode when running as part of %s", tc.name), func(t *testing.T) {
			cfg := minimalWorkingConfig(t, dir, tc.target, tc.transformer)
			c, err := New(cfg)
			require.NoError(t, err)

			services, err := c.ModuleManager.InitModuleServices(Read)
			defer func() {
				for _, service := range services {
					service.StopAsync()
				}
			}()

			require.NoError(t, err)
			assert.Equal(t, c.Cfg.IndexGateway.Mode, indexgateway.RingMode)
		})
	}

	type indexModeTestCase struct {
		name        string
		target      string
		transformer func(cfg *Config)
	}

	for _, tc := range []indexModeTestCase{
		{
			name:   "index gateway",
			target: IndexGateway,
		},
		{
			name:   "new read target",
			target: Read,
			transformer: func(cfg *Config) {
				cfg.LegacyReadTarget = false
			},
		},
	} {
		t.Run(fmt.Sprintf("When target is %s", tc.name), func(t *testing.T) {
			t.Run("IndexGateway config respects configured simple mode", func(t *testing.T) {
				cfg := minimalWorkingConfig(t, dir, IndexGatewayRing, tc.transformer)
				cfg.IndexGateway.Mode = indexgateway.SimpleMode
				c, err := New(cfg)
				require.NoError(t, err)

				services, err := c.ModuleManager.InitModuleServices(IndexGateway)
				defer func() {
					for _, service := range services {
						service.StopAsync()
					}
				}()

				require.NoError(t, err)
				assert.Equal(t, c.Cfg.IndexGateway.Mode, indexgateway.SimpleMode)
			})

			t.Run("IndexGateway config respects configured ring mode", func(t *testing.T) {
				cfg := minimalWorkingConfig(t, dir, IndexGatewayRing)
				cfg.IndexGateway.Mode = indexgateway.RingMode
				c, err := New(cfg)
				require.NoError(t, err)

				services, err := c.ModuleManager.InitModuleServices(IndexGateway)
				defer func() {
					for _, service := range services {
						service.StopAsync()
					}
				}()

				require.NoError(t, err)
				assert.Equal(t, c.Cfg.IndexGateway.Mode, indexgateway.RingMode)
			})
		})
	}
}

func TestIndexGatewayClientConfig(t *testing.T) {
	dir := t.TempDir()

	t.Run("IndexGateway client is enabled when running querier target", func(t *testing.T) {
		cfg := minimalWorkingConfig(t, dir, Querier)
		cfg.SchemaConfig.Configs[0].IndexType = types.BoltDBShipperType
		cfg.SchemaConfig.Configs[0].IndexTables.Period = 24 * time.Hour
		c, err := New(cfg)
		require.NoError(t, err)

		services, err := c.ModuleManager.InitModuleServices(Querier)
		defer func() {
			for _, service := range services {
				service.StopAsync()
			}
		}()

		require.NoError(t, err)
		assert.False(t, c.Cfg.StorageConfig.BoltDBShipperConfig.IndexGatewayClientConfig.Disabled)
		assert.False(t, c.Cfg.StorageConfig.TSDBShipperConfig.IndexGatewayClientConfig.Disabled)
	})

	t.Run("IndexGateway client is disabled when running legacy read target", func(t *testing.T) {
		cfg := minimalWorkingConfig(t, dir, Read, func(cfg *Config) {
			cfg.LegacyReadTarget = true
		})
		cfg.SchemaConfig.Configs[0].IndexType = types.BoltDBShipperType
		cfg.SchemaConfig.Configs[0].IndexTables.Period = 24 * time.Hour
		cfg.CompactorConfig.WorkingDirectory = dir
		c, err := New(cfg)
		require.NoError(t, err)

		services, err := c.ModuleManager.InitModuleServices(Read)
		defer func() {
			for _, service := range services {
				service.StopAsync()
			}
		}()

		require.NoError(t, err)
		assert.True(t, c.Cfg.StorageConfig.BoltDBShipperConfig.IndexGatewayClientConfig.Disabled)
		assert.True(t, c.Cfg.StorageConfig.TSDBShipperConfig.IndexGatewayClientConfig.Disabled)
	})

	t.Run("IndexGateway client is enabled when running new read target", func(t *testing.T) {
		cfg := minimalWorkingConfig(t, dir, Read, func(cfg *Config) {
			cfg.LegacyReadTarget = false
		})
		cfg.SchemaConfig.Configs[0].IndexType = types.BoltDBShipperType
		cfg.SchemaConfig.Configs[0].IndexTables.Period = 24 * time.Hour
		cfg.CompactorConfig.WorkingDirectory = dir
		c, err := New(cfg)
		require.NoError(t, err)

		services, err := c.ModuleManager.InitModuleServices(Read)
		defer func() {
			for _, service := range services {
				service.StopAsync()
			}
		}()

		require.NoError(t, err)
		assert.False(t, c.Cfg.StorageConfig.BoltDBShipperConfig.IndexGatewayClientConfig.Disabled)
		assert.False(t, c.Cfg.StorageConfig.TSDBShipperConfig.IndexGatewayClientConfig.Disabled)
	})

	t.Run("IndexGateway client is disabled when running backend target", func(t *testing.T) {
		cfg := minimalWorkingConfig(t, dir, Backend, func(cfg *Config) {
			cfg.LegacyReadTarget = false
		})
		cfg.SchemaConfig.Configs[0].IndexType = types.BoltDBShipperType
		cfg.SchemaConfig.Configs[0].IndexTables.Period = 24 * time.Hour
		cfg.CompactorConfig.WorkingDirectory = dir
		c, err := New(cfg)
		require.NoError(t, err)

		services, err := c.ModuleManager.InitModuleServices(Read)
		defer func() {
			for _, service := range services {
				service.StopAsync()
			}
		}()

		require.NoError(t, err)
		assert.True(t, c.Cfg.StorageConfig.BoltDBShipperConfig.IndexGatewayClientConfig.Disabled)
		assert.True(t, c.Cfg.StorageConfig.TSDBShipperConfig.IndexGatewayClientConfig.Disabled)
	})
}

const localhost = "localhost"

func minimalWorkingConfig(t *testing.T, dir, target string, cfgTransformers ...func(*Config)) Config {
	prepareGlobalMetricsRegistry(t)

	cfg := Config{}
	flagext.DefaultValues(&cfg)

	// Set to 0 to find any free port.
	cfg.Server.HTTPListenPort = 0
	cfg.Server.GRPCListenPort = 0
	cfg.Target = []string{target}

	// This would be overwritten by the default values setting.
	cfg.StorageConfig = storage.Config{
		FSConfig: local.FSConfig{Directory: dir},
		BloomShipperConfig: bloomshipperconfig.Config{
			WorkingDirectory:    []string{filepath.Join(dir, "blooms")},
			DownloadParallelism: 1,
		},
		BoltDBShipperConfig: boltdb.IndexCfg{
			Config: indexshipper.Config{
				ActiveIndexDirectory: filepath.Join(dir, "index"),
				CacheLocation:        filepath.Join(dir, "cache"),
				Mode:                 indexshipper.ModeWriteOnly,
				ResyncInterval:       24 * time.Hour,
			},
		},
	}

	// Disable some caches otherwise we'll get errors if we don't configure them
	cfg.QueryRange.CacheLabelResults = false
	cfg.QueryRange.CacheSeriesResults = false
	cfg.QueryRange.CacheIndexStatsResults = false
	cfg.QueryRange.CacheVolumeResults = false

	cfg.SchemaConfig = config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				IndexType:  types.BoltDBShipperType,
				ObjectType: types.StorageTypeFileSystem,
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Period: time.Hour * 24,
					}},
				RowShards: 16,
				Schema:    "v11",
				From: config.DayTime{
					Time: model.Now(),
				},
			},
		},
	}

	cfg.Common.InstanceAddr = localhost
	cfg.Ingester.LifecyclerConfig.Addr = localhost
	cfg.Distributor.DistributorRing.InstanceAddr = localhost
	cfg.IndexGateway.Mode = indexgateway.SimpleMode
	cfg.IndexGateway.Ring.InstanceAddr = localhost
	cfg.CompactorConfig.CompactorRing.InstanceAddr = localhost
	cfg.CompactorConfig.WorkingDirectory = filepath.Join(dir, "compactor")

	cfg.Ruler.Config.Ring.InstanceAddr = localhost
	cfg.Ruler.Config.StoreConfig.Type = types.StorageTypeLocal
	cfg.Ruler.Config.StoreConfig.Local.Directory = dir

	cfg.Common.CompactorAddress = "http://localhost:0"
	cfg.Common.PathPrefix = dir

	for _, transformer := range cfgTransformers {
		if transformer != nil {
			transformer(&cfg)
		}
	}

	return cfg
}
