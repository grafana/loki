package config

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"
)

func TestConfig_QueryReadBudget(t *testing.T) {
	newFlagSet := func(cfg *Config) *flag.FlagSet {
		fs := flag.NewFlagSet(t.Name(), flag.ContinueOnError)
		cfg.RegisterFlags(fs)
		return fs
	}

	t.Run("has no limit by default", func(t *testing.T) {
		var cfg Config
		require.NoError(t, newFlagSet(&cfg).Parse(nil))

		require.Zero(t, cfg.QueryReadBudget)
	})

	t.Run("reads a size from the flag", func(t *testing.T) {
		var cfg Config
		require.NoError(t, newFlagSet(&cfg).Parse([]string{"-dataobj.query-read-budget=256MiB"}))

		require.Equal(t, uint64(256<<20), uint64(cfg.QueryReadBudget))
	})

	t.Run("reads a size from the yaml key", func(t *testing.T) {
		var cfg Config
		require.NoError(t, newFlagSet(&cfg).Parse(nil))

		require.NoError(t, yaml.Unmarshal([]byte("query_read_budget: 1GiB\n"), &cfg))

		require.Equal(t, uint64(1<<30), uint64(cfg.QueryReadBudget))
	})
}
