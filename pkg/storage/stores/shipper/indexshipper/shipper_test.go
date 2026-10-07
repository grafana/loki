package indexshipper

import (
	"strings"
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
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
		{
			name: "in-memory index disabled ignores its other settings",
			mutate: func(cfg *Config) {
				cfg.InMemoryIndex.MaxBytes = 0
				cfg.InMemoryIndex.Placement = "bogus"
			},
		},
		{
			name: "in-memory index enabled",
			mutate: func(cfg *Config) {
				cfg.InMemoryIndex.Enabled = true
				cfg.InMemoryIndex.MaxBytes = 1 << 20
			},
		},
		{
			name:    "in-memory index enabled without a budget",
			mutate:  func(cfg *Config) { cfg.InMemoryIndex.Enabled = true },
			wantErr: "shipper.in-memory-index.max-bytes must be greater than zero",
		},
		{
			name: "in-memory index with an unknown placement",
			mutate: func(cfg *Config) {
				cfg.InMemoryIndex.Enabled = true
				cfg.InMemoryIndex.MaxBytes = 1 << 20
				cfg.InMemoryIndex.Placement = "bogus"
			},
			wantErr: `invalid shipper.in-memory-index.placement "bogus"`,
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

func TestNewReaderOptions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Config)
		want   tsdbindex.ReaderOptions
	}{
		{
			name:   "disabled returns the mmap mode unchanged",
			mutate: func(*Config) {},
			want:   tsdbindex.MmapOptions{},
		},
		{
			name: "disabled returns the stream mode unchanged",
			mutate: func(cfg *Config) {
				cfg.IndexReaderMode = IndexReaderModeStream
				cfg.StreamingIndexMaxIdleFileHandles = 3
			},
			want: tsdbindex.StreamOptions{MaxIdleFileHandles: 3},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{}
			flagext.DefaultValues(&cfg)
			tc.mutate(&cfg)

			reg := prometheus.NewRegistry()
			opts, err := NewReaderOptions(cfg, reg)
			require.NoError(t, err)
			require.Equal(t, tc.want, opts)

			// Nothing is registered while the tier is disabled.
			families, err := reg.Gather()
			require.NoError(t, err)
			require.Empty(t, families)
		})
	}

	t.Run("enabled wraps the configured mode", func(t *testing.T) {
		cfg := Config{}
		flagext.DefaultValues(&cfg)
		cfg.IndexReaderMode = IndexReaderModeStream
		cfg.InMemoryIndex.Enabled = true
		cfg.InMemoryIndex.MaxBytes = 1 << 20

		reg := prometheus.NewRegistry()
		opts, err := NewReaderOptions(cfg, reg)
		require.NoError(t, err)
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
# HELP loki_tsdb_shipper_in_memory_index_budget_bytes Configured maximum bytes of TSDB index files held in memory.
# TYPE loki_tsdb_shipper_in_memory_index_budget_bytes gauge
loki_tsdb_shipper_in_memory_index_budget_bytes 1.048576e+06
`), "loki_tsdb_shipper_in_memory_index_budget_bytes"))
		inMemory, ok := opts.(tsdbindex.InMemoryOptions)
		require.True(t, ok, "got %T", opts)
		require.NotNil(t, inMemory.Budget)
		require.NotNil(t, inMemory.Placement)
		require.Equal(t, tsdbindex.StreamOptions{MaxIdleFileHandles: cfg.StreamingIndexMaxIdleFileHandles}, inMemory.Fallback)
	})

	t.Run("enabled rejects an invalid config", func(t *testing.T) {
		cfg := Config{}
		flagext.DefaultValues(&cfg)
		cfg.InMemoryIndex.Enabled = true

		_, err := NewReaderOptions(cfg, nil)
		require.ErrorContains(t, err, "max-bytes must be greater than zero")
	})
}
