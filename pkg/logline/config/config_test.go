package config

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v4"
)

func TestValidateIsNoOpWhenDisabled(t *testing.T) {
	// The store requires a min_date that has no default, so a disabled
	// logline section must not fail a config that never mentions logline.
	var cfg Config
	require.NoError(t, cfg.Validate())
}

func TestValidateRequiresStoreWhenEnabled(t *testing.T) {
	cfg := Config{Enabled: true}
	require.ErrorContains(t, cfg.Validate(), "min_date")

	cfg.Store.MinDate = "2026-06-02"
	require.NoError(t, cfg.Validate())
	require.Equal(t, defaultNgramLength, cfg.QueryFrontend.NgramLength)
	require.Equal(t, defaultMaxHintParallel, cfg.QueryFrontend.MaxHintParallel)
}

func TestRegisterFlagsUsesLoglineNamespace(t *testing.T) {
	var cfg Config
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg.RegisterFlags(fs)

	for _, name := range []string{
		"logline.enabled",
		"logline-store.min-date",
		"logline-query-frontend.ngram-length",
		"logline-query-frontend.shard-planning.enabled",
	} {
		require.NotNil(t, fs.Lookup(name), "missing flag %s", name)
	}

	// Loki owns the query-frontend flag namespace; logline must stay out of it.
	fs.VisitAll(func(f *flag.Flag) {
		require.NotContains(t, []string{"query-frontend.ngram-length", "query-frontend.dry-run"}, f.Name)
	})
}

func TestUnmarshalSection(t *testing.T) {
	const in = `
enabled: true
store:
  min_date: "2026-06-02"
query_frontend:
  dry_run: true
  max_hint_parallel: 512
`
	var cfg Config
	require.NoError(t, yaml.Unmarshal([]byte(in), &cfg))
	require.True(t, cfg.Enabled)
	require.Equal(t, "2026-06-02", cfg.Store.MinDate)
	require.True(t, cfg.QueryFrontend.DryRun)
	require.Equal(t, 512, cfg.QueryFrontend.MaxHintParallel)

	// shard_planning was absent, so its UnmarshalYAML never ran and the
	// struct is still zero. Validate is what turns it on.
	require.False(t, cfg.QueryFrontend.ShardPlanning.Enabled)
	require.NoError(t, cfg.Validate())
	require.True(t, cfg.QueryFrontend.ShardPlanning.Enabled)
	require.Equal(t, DefaultShardPlanningMinReductionRatio, cfg.QueryFrontend.ShardPlanning.MinTimeReductionRatio)
}
