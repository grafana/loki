package indexshipper

import (
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
)

func TestConfig_Validate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name:   "defaults",
			mutate: func(*Config) {},
		},
		{
			name:    "index gateway client is validated",
			mutate:  func(cfg *Config) { cfg.IndexGatewayClientConfig.MaxRetries = -2 },
			wantErr: "shipper.index-gateway-client: index gateway client max-retries",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{}
			flagext.DefaultValues(&cfg)
			tc.mutate(&cfg)

			err := cfg.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestConfig_ForPeriod(t *testing.T) {
	unsharded := config.PeriodConfig{IndexTables: config.IndexPeriodicTableConfig{PathPrefix: "index/"}}
	sharded := config.PeriodConfig{IndexTables: config.IndexPeriodicTableConfig{PathPrefix: "index/", ShardedPathPrefix: "index_sharded/"}}
	filterTenants := func(_ string, tenants []string) ([]string, error) { return tenants, nil }
	filterFiles := func(_, _ string, files []storage.IndexFile) ([]storage.IndexFile, error) { return files, nil }

	t.Run("without per-index ownership every period reads path_prefix", func(t *testing.T) {
		for _, p := range []config.PeriodConfig{unsharded, sharded} {
			cfg, prefix := Config{}.ForPeriod(p)
			require.Equal(t, "index/", prefix)
			require.False(t, cfg.PerIndexOwnership)
		}
	})

	owning := Config{
		PerIndexOwnership:         true,
		TenantFilter:              filterTenants,
		SkipInitialQueryReadiness: true,
		DropFilter:                filterTenants,
		FileFilter:                filterFiles,
	}

	t.Run("with per-index ownership a sharded period reads the sharded copy with the filters", func(t *testing.T) {
		cfg, prefix := owning.ForPeriod(sharded)
		require.Equal(t, "index_sharded/", prefix)
		require.True(t, cfg.PerIndexOwnership)
		require.NotNil(t, cfg.TenantFilter)
		require.True(t, cfg.SkipInitialQueryReadiness)
		require.NotNil(t, cfg.DropFilter)
		require.NotNil(t, cfg.FileFilter)
	})

	t.Run("with per-index ownership another period reads path_prefix without the filters", func(t *testing.T) {
		cfg, prefix := owning.ForPeriod(unsharded)
		require.Equal(t, "index/", prefix)
		require.False(t, cfg.PerIndexOwnership)
		require.Nil(t, cfg.TenantFilter)
		require.False(t, cfg.SkipInitialQueryReadiness)
		require.Nil(t, cfg.DropFilter)
		require.Nil(t, cfg.FileFilter)
	})
}
