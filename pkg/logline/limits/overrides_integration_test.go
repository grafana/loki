package limits_test

import (
	"context"
	"flag"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/runtimeconfig"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loki"
	"github.com/grafana/loki/v3/pkg/validation"

	"github.com/grafana/loki/v3/pkg/logline/limits"
)

// validDefaults returns a validation.Limits with all fields populated to
// valid defaults (via RegisterFlags) so that per-tenant overrides inherit
// valid values for fields like DeletionMode that Validate() checks.
func validDefaults() validation.Limits {
	var l validation.Limits
	l.RegisterFlags(flag.NewFlagSet("test", flag.PanicOnError))
	return l
}

func writeRuntimeConfig(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestNewOverrides_RuntimeConfigReload(t *testing.T) {
	// Write initial runtime config with two tenants.
	configFile := t.TempDir() + "/runtime.yaml"
	writeRuntimeConfig(t, configFile, `
overrides:
  tenant-a:
    retention_period: 14d
  tenant-b:
    retention_period: 30d
`)

	// Global defaults: 7d retention.
	defaults := validDefaults()
	defaults.RetentionPeriod = model.Duration(7 * 24 * time.Hour)

	lokiCfg := loki.ConfigWrapper{}
	lokiCfg.LimitsConfig = defaults
	lokiCfg.RuntimeConfig = runtimeconfig.Config{
		ReloadPeriod: 100 * time.Millisecond,
		LoadPath:     []string{configFile},
	}

	reg := prometheus.NewRegistry()
	lim, ov, svc, err := limits.NewOverrides(lokiCfg, log.NewNopLogger(), reg)
	require.NoError(t, err)
	require.NotNil(t, svc, "runtime config service should be non-nil when file is configured")
	require.NotNil(t, ov)

	// Before the manager starts, limits reflect only the global defaults
	// because the runtime config file hasn't been loaded yet.
	require.Equal(t, 7*24*time.Hour, lim.RetentionPeriod())

	// Start the runtime config manager. starting() loads the file
	// synchronously and notifies the listener goroutine.
	ctx := context.Background()
	require.NoError(t, services.StartAndAwaitRunning(ctx, svc))
	t.Cleanup(func() {
		require.NoError(t, services.StopAndAwaitTerminated(ctx, svc))
	})

	// The listener goroutine recomputes asynchronously after the manager
	// notifies it. Poll briefly for the updated values.
	require.Eventually(t, func() bool {
		return lim.RetentionPeriod() == 30*24*time.Hour
	}, 2*time.Second, 10*time.Millisecond,
		"expected max retention=30d (tenant-b) after initial load")

	// --- Reload: change tenant-b to shorter values, add tenant-c with the longest. ---
	writeRuntimeConfig(t, configFile, `
overrides:
  tenant-a:
    retention_period: 14d
  tenant-b:
    retention_period: 20d
  tenant-c:
    retention_period: 60d
`)

	require.Eventually(t, func() bool {
		return lim.RetentionPeriod() == 60*24*time.Hour
	}, 2*time.Second, 10*time.Millisecond,
		"expected max retention=60d (tenant-c) after reload")

	// --- Reload: remove all tenant overrides. Limits should fall back to defaults. ---
	writeRuntimeConfig(t, configFile, `
overrides: {}
`)

	require.Eventually(t, func() bool {
		return lim.RetentionPeriod() == 7*24*time.Hour
	}, 2*time.Second, 10*time.Millisecond,
		"expected defaults (retention=7d) after removing all tenant overrides")
}

func TestNewOverrides_NoRuntimeConfig(t *testing.T) {
	defaults := validDefaults()
	defaults.RetentionPeriod = model.Duration(14 * 24 * time.Hour)

	lokiCfg := loki.ConfigWrapper{}
	lokiCfg.LimitsConfig = defaults
	// No RuntimeConfig.LoadPath set — no file polling.

	lim, ov, svc, err := limits.NewOverrides(lokiCfg, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	require.Nil(t, svc, "no service when no runtime config file")
	require.NotNil(t, ov)

	require.Equal(t, 14*24*time.Hour, lim.RetentionPeriod())
}
