package compactor

import (
	"flag"
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
)

// TestConfig_ValidateRejectsBadValues verifies that each new coordinator-loop
// knob is validated when Enabled is true. The pre-existing
// MaxRunningCompactionTasks and Scheduler.Endpoint validations are exercised
// in compactor_test.go.
func TestConfig_ValidateRejectsBadValues(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr error
	}{
		{"polling interval zero", func(c *Config) { c.PollingInterval = 0 }, errInvalidPollingInterval},
		{"polling interval negative", func(c *Config) { c.PollingInterval = -1 }, errInvalidPollingInterval},
		{"toc consolidate timeout zero", func(c *Config) { c.ToCConsolidateTimeout = 0 }, errInvalidToCConsolidateTimeout},
		{"max runs zero", func(c *Config) { c.MaxRunsPerTask = 0 }, errInvalidMaxRunsPerTask},
		{"max runs negative", func(c *Config) { c.MaxRunsPerTask = -1 }, errInvalidMaxRunsPerTask},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var cfg Config
			cfg.RegisterFlags(flag.NewFlagSet("test", flag.ContinueOnError))
			cfg.Enabled = true                       // validation is gated on Enabled
			cfg.Scheduler.Endpoint = defaultEndpoint // satisfy existing scheduler-endpoint check
			tc.mutate(&cfg)
			err := cfg.Validate()
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestConfig_LogMinCompactionSizeValidation(t *testing.T) {
	// Build a valid enabled config, driving LogMinCompactionSize through the flag
	// parser so this one test covers both parsing and validation.
	base := func(t *testing.T, args ...string) Config {
		t.Helper()
		var cfg Config
		fs := flag.NewFlagSet("test", flag.PanicOnError)
		cfg.RegisterFlagsWithPrefix("dataobj.compaction.", fs)
		require.NoError(t, fs.Parse(args))
		cfg.Enabled = true
		cfg.Scheduler.Endpoint = defaultEndpoint
		return cfg
	}

	t.Run("parsed value passes and is applied", func(t *testing.T) {
		cfg := base(t, "--dataobj.compaction.logs.min-compaction-size=8MB")
		require.Equal(t, flagext.Bytes(8*1024*1024), cfg.LogMinCompactionSize)
		require.NoError(t, cfg.Validate())
	})

	t.Run("zero fails", func(t *testing.T) {
		cfg := base(t, "--dataobj.compaction.logs.min-compaction-size=0")
		require.ErrorIs(t, cfg.Validate(), errInvalidLogMinCompactionSize)
	})
}
