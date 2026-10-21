package builder

import (
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfig_RegisterFlags(t *testing.T) {
	var cfg Config
	f := flag.NewFlagSet("test", flag.PanicOnError)
	cfg.RegisterFlags(f)
	cfg.Topic = "test"
	require.NoError(t, cfg.Validate())

	require.Equal(t, 1, cfg.FlushConcurrency)
	require.Equal(t, 1, cfg.SortConcurrency)

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
	for _, tc := range []struct {
		name             string
		flushConcurrency int
		wantErr          string
	}{
		{name: "accepts a flush concurrency of 1", flushConcurrency: 1},
		{name: "accepts a flush concurrency above 1", flushConcurrency: 4},
		{name: "rejects a flush concurrency of 0", flushConcurrency: 0, wantErr: "flush concurrency must be at least 1"},
		{name: "rejects a negative flush concurrency", flushConcurrency: -1, wantErr: "flush concurrency must be at least 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg Config
			cfg.RegisterFlags(flag.NewFlagSet("test", flag.PanicOnError))
			cfg.Topic = "test"
			cfg.FlushConcurrency = tc.flushConcurrency

			err := cfg.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tc.wantErr)
		})
	}

	newConfig := func(t *testing.T, args ...string) Config {
		t.Helper()
		var cfg Config
		f := flag.NewFlagSet("test", flag.PanicOnError)
		cfg.RegisterFlags(f)
		require.NoError(t, f.Parse(append([]string{"-dataobj.builder.topic=test"}, args...)))
		return cfg
	}

	t.Run("accepts the default required replicas refresh interval", func(t *testing.T) {
		cfg := newConfig(t)
		require.NoError(t, cfg.Validate())
		require.Equal(t, 30*time.Second, cfg.RequiredReplicasRefreshInterval)
	})

	t.Run("rejects a zero required replicas refresh interval", func(t *testing.T) {
		cfg := newConfig(t, "-dataobj.builder.required-replicas-refresh-interval=0s")
		require.Error(t, cfg.Validate())
	})

	t.Run("accepts a sort concurrency above 1", func(t *testing.T) {
		cfg := newConfig(t, "-dataobj.builder.sort-concurrency=4")
		require.NoError(t, cfg.Validate())
		require.Equal(t, 4, cfg.SortConcurrency)
	})

	t.Run("rejects a sort concurrency of 0", func(t *testing.T) {
		cfg := newConfig(t, "-dataobj.builder.sort-concurrency=0")
		require.EqualError(t, cfg.Validate(), "sort concurrency must be at least 1")
	})
}
