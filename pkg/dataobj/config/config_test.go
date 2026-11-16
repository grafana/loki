package config

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

func TestConfig_Validate(t *testing.T) {
	t.Run("accepts the default values without a Kafka topic when Kafka ingestion is disabled", func(t *testing.T) {
		cfg := enabledConfig()

		require.NoError(t, cfg.Validate(false))
	})

	t.Run("rejects the default values without a Kafka topic when Kafka ingestion is enabled", func(t *testing.T) {
		cfg := enabledConfig()

		require.EqualError(t, cfg.Validate(true), "topic is required")
	})

	t.Run("accepts the default values with a Kafka topic when Kafka ingestion is enabled", func(t *testing.T) {
		cfg := enabledConfig()
		cfg.Builder.Topic = "dataobj"

		require.NoError(t, cfg.Validate(true))
	})

	t.Run("rejects an invalid uploader config when Kafka ingestion is disabled", func(t *testing.T) {
		cfg := enabledConfig()
		cfg.Uploader.SHAPrefixSize = 0

		require.EqualError(t, cfg.Validate(false), "SHAPrefixSize must be greater than 0")
	})

	t.Run("accepts an invalid config when data objects are disabled", func(t *testing.T) {
		cfg := enabledConfig()
		cfg.Enabled = false
		cfg.Uploader.SHAPrefixSize = 0

		require.NoError(t, cfg.Validate(true))
	})

	t.Run("accepts the metadata prefetcher with a metadata cache", func(t *testing.T) {
		cfg := enabledConfig()
		cfg.MetadataPrefetcher.Enabled = true
		cfg.MetadataCache.Cache = cache.NewMockCache()

		require.NoError(t, cfg.Validate(false))
	})

	t.Run("rejects the metadata prefetcher without a metadata cache", func(t *testing.T) {
		cfg := enabledConfig()
		cfg.MetadataPrefetcher.Enabled = true

		require.EqualError(t, cfg.Validate(false), "the metadata prefetcher needs the metadata cache: configure dataobj.metadata_cache")
	})

	t.Run("rejects an invalid config of the metadata prefetcher", func(t *testing.T) {
		cfg := enabledConfig()
		cfg.MetadataPrefetcher.Enabled = true
		cfg.MetadataPrefetcher.Compression = "gzip"
		cfg.MetadataCache.Cache = cache.NewMockCache()

		require.EqualError(t, cfg.Validate(false), `unsupported metadata prefetcher compression "gzip"`)
	})
}

// enabledConfig returns a config with the default values and data objects enabled.
func enabledConfig() Config {
	var cfg Config
	cfg.RegisterFlags(flag.NewFlagSet("test", flag.PanicOnError))
	cfg.Enabled = true
	return cfg
}
