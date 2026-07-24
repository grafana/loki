package indexgateway

import (
	"flag"
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
)

func defaultTestConfig(t *testing.T) Config {
	t.Helper()
	var cfg Config
	fs := flag.NewFlagSet(t.Name(), flag.PanicOnError)
	cfg.RegisterFlags(fs)
	require.NoError(t, fs.Parse(nil))
	return cfg
}

func TestConfig_Validate(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mutate      func(*Config)
		expectedErr string
	}{
		{
			name:   "defaults are valid",
			mutate: func(*Config) {},
		},
		{
			name: "positive limits are valid",
			mutate: func(cfg *Config) {
				cfg.SaturationControl.CPUUtilizationLimit = 0.8
				cfg.SaturationControl.SchedulerBacklogLimit = 1.0
			},
		},
		{
			name:        "negative CPU utilization limit is rejected",
			mutate:      func(cfg *Config) { cfg.SaturationControl.CPUUtilizationLimit = -1 },
			expectedErr: "CPU utilization limit",
		},
		{
			name: "negative memory utilization limit is rejected",
			mutate: func(cfg *Config) {
				var b flagext.Bytes
				require.NoError(t, b.Set("-1GiB"))
				cfg.SaturationControl.MemoryUtilizationLimit = b
			},
			expectedErr: "memory utilization limit",
		},
		{
			name:        "negative scheduler backlog limit is rejected",
			mutate:      func(cfg *Config) { cfg.SaturationControl.SchedulerBacklogLimit = -1 },
			expectedErr: "scheduler backlog limit",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaultTestConfig(t)
			tc.mutate(&cfg)
			err := cfg.Validate()
			if tc.expectedErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.expectedErr)
			}
		})
	}
}

func TestConfig_LimiterEnabledHelpers(t *testing.T) {
	cfg := defaultTestConfig(t)
	require.False(t, cfg.SaturationControl.UtilizationLimiterEnabled())
	require.False(t, cfg.SaturationControl.SchedulerLimiterEnabled())

	cfg.SaturationControl.CPUUtilizationLimit = 0.8
	require.True(t, cfg.SaturationControl.UtilizationLimiterEnabled())
	require.False(t, cfg.SaturationControl.SchedulerLimiterEnabled())

	cfg.SaturationControl.CPUUtilizationLimit = 0
	require.NoError(t, cfg.SaturationControl.MemoryUtilizationLimit.Set("1GiB"))
	require.True(t, cfg.SaturationControl.UtilizationLimiterEnabled())

	cfg.SaturationControl.MemoryUtilizationLimit = 0
	cfg.SaturationControl.SchedulerBacklogLimit = 1.0
	require.False(t, cfg.SaturationControl.UtilizationLimiterEnabled())
	require.True(t, cfg.SaturationControl.SchedulerLimiterEnabled())
}
