package builder

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfig_RegisterFlags(t *testing.T) {
	var cfg Config
	f := flag.NewFlagSet("test", flag.PanicOnError)
	cfg.RegisterFlags(f)
	cfg.Topic = "test"
	require.NoError(t, cfg.Validate())

	require.Equal(t, 1, cfg.FlushConcurrency)

	require.Equal(t, 1<<20, int(cfg.LogsobjBuilder.TargetPageSize))
	require.Equal(t, 8, cfg.LogsobjBuilder.EstimatedCompressionRatio)
	require.Equal(t, "8", f.Lookup("dataobj.builder.logsobj-builder.estimated-compression-ratio").DefValue)

	require.Equal(t, 128<<10, int(cfg.IndexobjBuilder.TargetPageSize))
	require.Equal(t, 1, cfg.IndexobjBuilder.EstimatedCompressionRatio)
	require.Equal(t, "1", f.Lookup("dataobj.builder.indexobj-builder.estimated-compression-ratio").DefValue)

	require.NoError(t, f.Parse([]string{"-dataobj.builder.indexobj-builder.estimated-compression-ratio=4"}))
	require.Equal(t, 4, cfg.IndexobjBuilder.EstimatedCompressionRatio)
}

func TestConfig_Validate(t *testing.T) {
	newValidConfig := func() Config {
		var cfg Config
		cfg.RegisterFlags(flag.NewFlagSet("test", flag.PanicOnError))
		cfg.Topic = "test"
		return cfg
	}

	t.Run("accepts a flush concurrency above 1", func(t *testing.T) {
		cfg := newValidConfig()
		cfg.FlushConcurrency = 4
		require.NoError(t, cfg.Validate())
	})

	t.Run("rejects a flush concurrency of 0", func(t *testing.T) {
		cfg := newValidConfig()
		cfg.FlushConcurrency = 0
		require.EqualError(t, cfg.Validate(), "flush concurrency must be at least 1")
	})
}
