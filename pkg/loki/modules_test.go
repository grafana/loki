package loki

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/compactor"
	dataobjconfig "github.com/grafana/loki/v3/pkg/dataobj/config"
	"github.com/grafana/loki/v3/pkg/indexgateway"
	loglinestore "github.com/grafana/loki/v3/pkg/logline/store"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	bloomshipperconfig "github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper"
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

func TestIndexGatewayClientConfig(t *testing.T) {
	dir := t.TempDir()
	t.Run("IndexGateway client is enabled when running querier target", func(t *testing.T) {
		cfg := minimalWorkingConfig(t, dir, Querier)
		c, err := New(cfg)
		require.NoError(t, err)

		services, err := c.ModuleManager.InitModuleServices(Querier)
		defer func() {
			for _, service := range services {
				service.StopAsync()
			}
		}()

		require.NoError(t, err)
		assert.False(t, c.Cfg.StorageConfig.TSDBShipperConfig.IndexGatewayClientConfig.Disabled)
	})
}

const localhost = "localhost"

// TestIndexGatewayInterceptorsOrderedBeforeServer is a regression test for a
// bug where initServer was called before initIndexGatewayInterceptors, resulting
// in the interceptors not being effective.
func TestIndexGatewayInterceptorsOrderedBeforeServer(t *testing.T) {
	dir := t.TempDir()
	cfg := minimalWorkingConfig(t, dir, IndexGateway)
	c, err := New(cfg)
	require.NoError(t, err)

	require.Contains(t, c.ModuleManager.DependenciesForModule(Server), IndexGatewayInterceptors,
		"Server must list IndexGatewayInterceptors as a dependency when target=index-gateway")
}

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
		TSDBShipperConfig: indexshipper.Config{
			ActiveIndexDirectory: filepath.Join(dir, "index"),
			CacheLocation:        filepath.Join(dir, "cache"),
			Mode:                 indexshipper.ModeWriteOnly,
			ResyncInterval:       24 * time.Hour,
			IndexReaderMode:      indexshipper.DefaultIndexReaderMode,
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
				IndexType:  types.IndexTypeTSDB,
				ObjectType: types.StorageTypeFileSystem,
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Period: time.Hour * 24,
					}},
				Schema: "v11",
				From: config.DayTime{
					Time: model.Now(),
				},
			},
		},
	}

	cfg.Common.InstanceAddr = localhost
	cfg.MemberlistKV.AdvertiseAddr = localhost
	cfg.Ingester.LifecyclerConfig.Addr = localhost
	cfg.Distributor.DistributorRing.InstanceAddr = localhost
	cfg.IndexGateway.Mode = indexgateway.SimpleMode
	cfg.IndexGateway.Ring.InstanceAddr = localhost
	cfg.CompactorConfig.CompactorRing.InstanceAddr = localhost
	cfg.CompactorConfig.WorkingDirectory = filepath.Join(dir, "compactor")

	cfg.Ruler.Ring.InstanceAddr = localhost
	// "local" matches the ruler's local rule-store backend (pkg/ruler/rulestore/local.Name)
	cfg.Ruler.StoreConfig.Type = "local"
	cfg.Ruler.StoreConfig.Local.Directory = dir

	cfg.Common.CompactorAddress = "http://localhost:0"
	cfg.Common.PathPrefix = dir

	for _, transformer := range cfgTransformers {
		if transformer != nil {
			transformer(&cfg)
		}
	}

	return cfg
}

func TestInitPatternIngesterTee(t *testing.T) {
	tests := []struct {
		name           string
		patternEnabled bool
		kafkaIngestion bool
	}{
		{name: "disabled", patternEnabled: false},
		{name: "disabled when the pattern ingester consumes from Kafka", patternEnabled: true, kafkaIngestion: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			loki := &Loki{}
			loki.Cfg.Pattern.Enabled = tc.patternEnabled
			loki.Cfg.Pattern.KafkaIngestion.Enabled = tc.kafkaIngestion

			svc, err := loki.initPatternIngesterTee()
			require.NoError(t, err)
			require.Nil(t, svc)
			require.Nil(t, loki.Tee)
		})
	}
}

// Puts both servers and an ingester in the service map, then checks only the
// ingester comes back - if the servers wait on each other, neither shuts down.
func TestServerServicesToWaitFor(t *testing.T) {
	ingester := services.NewIdleService(nil, nil)
	l := &Loki{serviceMap: map[string]services.Service{
		Server:         services.NewIdleService(nil, nil),
		InternalServer: services.NewIdleService(nil, nil),
		Ingester:       ingester,
	}}

	require.Equal(t, []services.Service{ingester}, l.serverServicesToWaitFor())
}

func TestLoglineStoreConfigBorrowsIngesterWindow(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		queryIngestersWithin time.Duration
		want                 time.Duration
	}{
		{name: "querier value", queryIngestersWithin: 3 * time.Hour, want: 3 * time.Hour},
		{name: "unset falls back to default", queryIngestersWithin: 0, want: loglinestore.DefaultQueryIngestersWithin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := &Loki{}
			l.Cfg.Logline.Store.MinDate = "2026-01-01"
			l.Cfg.Querier.QueryIngestersWithin = tc.queryIngestersWithin

			cfg := l.loglineStoreConfig()
			require.Equal(t, tc.want, cfg.QueryIngestersWithin)
			require.Equal(t, "2026-01-01", cfg.MinDate)
			// The shared section stays untouched.
			require.Zero(t, l.Cfg.Logline.Store.QueryIngestersWithin)
		})
	}
}

func TestCompactorAddress(t *testing.T) {
	const (
		ownGRPCAddress = "10.0.0.1:9095"
		mainGRPC       = "loki-main:9095"
		compactorHTTP  = "http://compactor:3100"
	)

	for _, tc := range []struct {
		name        string
		target      []string
		mode        string
		grpcAddress string
		httpAddress string
		want        string
		wantGRPC    bool
		wantErr     bool
	}{
		{
			name:     "all targets with the compactor disabled uses the own address",
			target:   []string{All},
			mode:     compactor.HorizontalScalingModeDisabled,
			want:     ownGRPCAddress,
			wantGRPC: true,
		},
		{
			name:        "all targets with the main compactor uses the own address",
			target:      []string{All},
			mode:        compactor.HorizontalScalingModeMain,
			grpcAddress: mainGRPC,
			want:        ownGRPCAddress,
			wantGRPC:    true,
		},
		{
			name:        "all targets with a worker compactor uses the configured address",
			target:      []string{All},
			mode:        compactor.HorizontalScalingModeWorker,
			grpcAddress: mainGRPC,
			want:        mainGRPC,
			wantGRPC:    true,
		},
		{
			name:    "all targets with a worker compactor requires a configured address",
			target:  []string{All},
			mode:    compactor.HorizontalScalingModeWorker,
			wantErr: true,
		},
		{
			name:        "querier prefers the configured gRPC address",
			target:      []string{Querier},
			mode:        compactor.HorizontalScalingModeDisabled,
			grpcAddress: mainGRPC,
			httpAddress: compactorHTTP,
			want:        mainGRPC,
			wantGRPC:    true,
		},
		{
			name:        "querier falls back to the configured HTTP address",
			target:      []string{Querier},
			mode:        compactor.HorizontalScalingModeDisabled,
			httpAddress: compactorHTTP,
			want:        compactorHTTP,
		},
		{
			name:    "querier requires a configured address",
			target:  []string{Querier},
			mode:    compactor.HorizontalScalingModeDisabled,
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var loki Loki
			loki.Cfg.Target = tc.target
			loki.Cfg.Server.GRPCListenAddress = "10.0.0.1"
			loki.Cfg.Server.GRPCListenPort = 9095
			loki.Cfg.CompactorConfig.HorizontalScalingMode = tc.mode
			loki.Cfg.Common.CompactorGRPCAddress = tc.grpcAddress
			loki.Cfg.Common.CompactorAddress = tc.httpAddress

			got, isGRPC, err := loki.compactorAddress()
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.wantGRPC, isGRPC)
		})
	}
}

func TestInitDataObjCache(t *testing.T) {
	embeddedBackend := cache.Config{
		Prefix:        "dataobj.metadata-cache.",
		EmbeddedCache: cache.EmbeddedCacheConfig{Enabled: true, MaxSizeMB: 1, TTL: time.Minute},
	}

	t.Run("builds no cache when data objects are off, even with the memory cache flag set", func(t *testing.T) {
		useFreshRegistry(t)
		loki := &Loki{}
		loki.Cfg.DataObj = dataobjconfig.Config{Enabled: false, LogsMetadataMemoryCache: true, MetadataCache: embeddedBackend}

		svc, err := loki.initDataObjCache()

		require.NoError(t, err)
		require.Nil(t, svc)
		require.Nil(t, loki.dataObjLogsMemoryCache)
		require.Nil(t, loki.dataObjLogsCache)
	})

	t.Run("builds only the memory cache and no service when the flag is set and no backend is configured", func(t *testing.T) {
		useFreshRegistry(t)
		loki := &Loki{}
		loki.Cfg.DataObj = dataobjconfig.Config{Enabled: true, LogsMetadataMemoryCache: true}

		svc, err := loki.initDataObjCache()

		require.NoError(t, err)
		require.Nil(t, svc)
		require.NotNil(t, loki.dataObjLogsMemoryCache)
		require.Nil(t, loki.dataObjLogsCache)
		require.Nil(t, loki.dataObjIndexCache)
		require.Nil(t, loki.dataObjTOCCache)
	})

	t.Run("builds the memory cache and the backend caches when both are configured", func(t *testing.T) {
		useFreshRegistry(t)
		loki := &Loki{}
		loki.Cfg.DataObj = dataobjconfig.Config{Enabled: true, LogsMetadataMemoryCache: true, MetadataCache: embeddedBackend}

		svc, err := loki.initDataObjCache()

		require.NoError(t, err)
		require.NotNil(t, svc)
		require.NotNil(t, loki.dataObjLogsMemoryCache)
		require.NotNil(t, loki.dataObjLogsCache)
		require.NotNil(t, loki.dataObjIndexCache)
		require.NotNil(t, loki.dataObjTOCCache)
	})

	t.Run("a memory miss asks the logs cache of the backend, and a memory hit does not", func(t *testing.T) {
		reg := useFreshRegistry(t)
		loki := &Loki{}
		loki.Cfg.DataObj = dataobjconfig.Config{Enabled: true, LogsMetadataMemoryCache: true, MetadataCache: embeddedBackend}
		_, err := loki.initDataObjCache()
		require.NoError(t, err)
		load := func(context.Context) ([]byte, error) { return []byte("block"), nil }

		_, err = loki.dataObjLogsMemoryCache.GetOrLoadMetadataRegion(t.Context(), "obj", load)
		require.NoError(t, err)
		_, err = loki.dataObjLogsMemoryCache.GetOrLoadMetadataRegion(t.Context(), "obj", load)
		require.NoError(t, err)

		require.Equal(t, float64(1), counterValue(t, reg, "loki_dataobj_metadata_cache_misses_total", "logs_memory"))
		require.Equal(t, float64(1), counterValue(t, reg, "loki_dataobj_metadata_cache_hits_total", "logs_memory"))
		require.Equal(t, float64(1), counterValue(t, reg, "loki_dataobj_metadata_cache_misses_total", "logs"), "only the memory miss reached the backend")
		require.Zero(t, counterValue(t, reg, "loki_dataobj_metadata_cache_hits_total", "logs"))
	})

	t.Run("builds no memory cache when the flag is off", func(t *testing.T) {
		useFreshRegistry(t)
		loki := &Loki{}
		loki.Cfg.DataObj = dataobjconfig.Config{Enabled: true, MetadataCache: embeddedBackend}

		svc, err := loki.initDataObjCache()

		require.NoError(t, err)
		require.NotNil(t, svc)
		require.Nil(t, loki.dataObjLogsMemoryCache)
		require.NotNil(t, loki.dataObjLogsCache)
	})
}

// useFreshRegistry gives the test its own default registerer, so a cache built by one test does not
// collide with the metrics of another.
func useFreshRegistry(t *testing.T) *prometheus.Registry {
	t.Helper()

	previous := prometheus.DefaultRegisterer
	reg := prometheus.NewRegistry()
	prometheus.DefaultRegisterer = reg
	t.Cleanup(func() { prometheus.DefaultRegisterer = previous })
	return reg
}

// counterValue returns the value of the counter series of name that carries the given kind label.
func counterValue(t *testing.T, reg *prometheus.Registry, name, kind string) float64 {
	t.Helper()

	families, err := reg.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == "kind" && label.GetValue() == kind {
					return metric.GetCounter().GetValue()
				}
			}
		}
	}
	t.Fatalf("no series %q with kind %q", name, kind)
	return 0
}
