package metastore

import (
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	const storageLag = 3 * time.Hour

	t.Run("accepts the defaults of the flags", func(t *testing.T) {
		var cfg Config
		cfg.RegisterFlags(flag.NewFlagSet("test", flag.PanicOnError))
		cfg.TOCResolver.Enabled = true

		require.NoError(t, cfg.Validate(storageLag))
	})

	t.Run("returns the error of the ToC resolver config", func(t *testing.T) {
		cfg := Config{TOCResolver: testResolverConfig()}
		cfg.TOCResolver.RefreshInterval = 15 * time.Minute
		cfg.TOCResolver.ActiveWindow = 15 * time.Minute

		require.EqualError(t, cfg.Validate(storageLag), "twice the refresh interval of the ToC resolver (30m0s) must not exceed a tenth of the storage lag (3h0m0s)")
	})
}

func TestTOCResolverConfig_Validate(t *testing.T) {
	const storageLag = 3 * time.Hour

	enabled := func(mutate func(*TOCResolverConfig)) TOCResolverConfig {
		cfg := testResolverConfig()
		mutate(&cfg)
		return cfg
	}

	t.Run("accepts the defaults of the flags with the default storage lag", func(t *testing.T) {
		var cfg Config
		cfg.RegisterFlags(flag.NewFlagSet("test", flag.PanicOnError))
		cfg.TOCResolver.Enabled = true

		require.NoError(t, cfg.TOCResolver.Validate(storageLag))
	})

	t.Run("accepts any settings when the resolver is disabled", func(t *testing.T) {
		require.NoError(t, TOCResolverConfig{}.Validate(storageLag))
	})

	t.Run("accepts twice the refresh interval at a tenth of the storage lag", func(t *testing.T) {
		cfg := enabled(func(c *TOCResolverConfig) {
			c.RefreshInterval = 9 * time.Minute
			c.ActiveWindow = 9 * time.Minute
		})
		require.NoError(t, cfg.Validate(storageLag))
	})

	t.Run("rejects twice the refresh interval above a tenth of the storage lag", func(t *testing.T) {
		cfg := enabled(func(c *TOCResolverConfig) {
			c.RefreshInterval = 9*time.Minute + time.Second
			c.ActiveWindow = 10 * time.Minute
		})
		require.EqualError(t, cfg.Validate(storageLag), "twice the refresh interval of the ToC resolver (18m2s) must not exceed a tenth of the storage lag (3h0m0s)")
	})

	t.Run("rejects a refresh interval that is not positive", func(t *testing.T) {
		cfg := enabled(func(c *TOCResolverConfig) { c.RefreshInterval = 0 })
		require.EqualError(t, cfg.Validate(storageLag), "the refresh interval of the ToC resolver (0s) must be at least 1s")
	})

	t.Run("rejects a refresh interval below one second", func(t *testing.T) {
		cfg := enabled(func(c *TOCResolverConfig) {
			c.RefreshInterval = 999 * time.Millisecond
			c.ActiveWindow = time.Minute
		})
		require.EqualError(t, cfg.Validate(storageLag), "the refresh interval of the ToC resolver (999ms) must be at least 1s")
	})

	t.Run("accepts a refresh interval of one second", func(t *testing.T) {
		cfg := enabled(func(c *TOCResolverConfig) { c.RefreshInterval = time.Second })
		require.NoError(t, cfg.Validate(storageLag))
	})

	t.Run("rejects an active window shorter than the refresh interval", func(t *testing.T) {
		cfg := enabled(func(c *TOCResolverConfig) { c.ActiveWindow = c.RefreshInterval - time.Second })
		require.EqualError(t, cfg.Validate(storageLag), "the active window of the ToC resolver (59s) must not be shorter than its refresh interval (1m0s)")
	})

	t.Run("rejects a size limit of zero", func(t *testing.T) {
		cfg := enabled(func(c *TOCResolverConfig) { c.MaxMemoryCacheBytes = 0 })
		require.EqualError(t, cfg.Validate(storageLag), "the max memory cache bytes of the ToC resolver must be positive")
	})
}
