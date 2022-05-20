package loki

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper"
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
		t.Run(target, func(t *testing.T) {
			prepareGlobalMetricsRegistry(t)

			cfg := Config{}
			cfg.SchemaConfig = config.SchemaConfig{
				Configs: []config.PeriodConfig{
					{
						IndexType:  config.StorageTypeInMemory,
						ObjectType: config.StorageTypeFileSystem,
						RowShards:  16,
						Schema:     "v11",
						From: config.DayTime{
							Time: model.Now(),
						},
					},
				},
			}
			flagext.DefaultValues(&cfg)
			// Set to 0 to find any free port.
			cfg.Server.HTTPListenPort = 0
			cfg.Server.GRPCListenPort = 0
			cfg.Target = []string{target}

			// Must be set, otherwise MultiKV config provider will not be set.
			cfg.RuntimeConfig.LoadPath = filepath.Join(dir, "config.yaml")

			// This would be overwritten by the default values setting.
			cfg.StorageConfig = storage.Config{
				FSConfig: local.FSConfig{Directory: dir},
				BoltDBShipperConfig: shipper.Config{
					SharedStoreType:      config.StorageTypeFileSystem,
					ActiveIndexDirectory: dir,
					CacheLocation:        dir,
					Mode:                 shipper.ModeWriteOnly},
			}
			cfg.Ruler.Config.StoreConfig.Type = config.StorageTypeLocal
			cfg.Ruler.Config.StoreConfig.Local.Directory = dir

			c, err := New(cfg)
			require.NoError(t, err)

			_, err = c.ModuleManager.InitModuleServices(cfg.Target...)
			require.NoError(t, err)
			defer c.Server.Stop()

			checkFn(t, c.Cfg)
		})
	}
}
