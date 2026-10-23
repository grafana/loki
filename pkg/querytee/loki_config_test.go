package querytee

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/runtimeconfig"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestLoadLokiLimits(t *testing.T) {
	const lokiConfigYAML = `
server:
  http_listen_port: 3100
limits_config:
  stream_first_execution_enabled: true
  dataobj_query_start_time: "2026-01-01"
`
	const runtimeConfigYAML = `
overrides:
  tenant-a:
    stream_first_execution_enabled: false
  tenant-b:
    dataobj_query_start_time: "2026-03-01"
configs:
  tenant-a:
    log_stream_creation: true
`

	startTime := func(year int, month time.Month) time.Time {
		return time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	}

	t.Run("loads no config when no file is set", func(t *testing.T) {
		lokiLimits, err := LoadLokiLimits(t.Context(), LimitsConfig{}, log.NewNopLogger(), prometheus.NewRegistry())
		require.NoError(t, err)
		require.Nil(t, lokiLimits)
		require.NoError(t, lokiLimits.Stop(t.Context()))
	})

	t.Run("reads the default limits and the tenant overrides", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "config.yaml"), lokiConfigYAML)
		writeFile(t, filepath.Join(dir, "runtime.yaml"), runtimeConfigYAML)

		cfg := LimitsConfig{
			ConfigFile:    filepath.Join(dir, "config.yaml"),
			RuntimeConfig: runtimeconfig.Config{LoadPath: flagext.StringSliceCSV{filepath.Join(dir, "runtime.yaml")}, ReloadPeriod: time.Minute},
		}
		lokiLimits, err := LoadLokiLimits(t.Context(), cfg, log.NewNopLogger(), prometheus.NewRegistry())
		require.NoError(t, err)

		require.True(t, lokiLimits.Overrides.StreamFirstExecutionEnabled("other"))
		require.Equal(t, startTime(2026, time.January), lokiLimits.Overrides.DataObjQueryStartTime("other"))

		require.False(t, lokiLimits.Overrides.StreamFirstExecutionEnabled("tenant-a"))
		require.Equal(t, startTime(2026, time.January), lokiLimits.Overrides.DataObjQueryStartTime("tenant-a"))
		require.True(t, lokiLimits.Overrides.StreamFirstExecutionEnabled("tenant-b"))
		require.Equal(t, startTime(2026, time.March), lokiLimits.Overrides.DataObjQueryStartTime("tenant-b"))
	})

	t.Run("applies a changed override after the runtime config reloads", func(t *testing.T) {
		dir := t.TempDir()
		runtimePath := filepath.Join(dir, "runtime.yaml")
		writeFile(t, runtimePath, runtimeConfigYAML)

		cfg := LimitsConfig{RuntimeConfig: runtimeconfig.Config{LoadPath: flagext.StringSliceCSV{runtimePath}, ReloadPeriod: 20 * time.Millisecond}}
		lokiLimits, err := LoadLokiLimits(t.Context(), cfg, log.NewNopLogger(), prometheus.NewRegistry())
		require.NoError(t, err)
		require.False(t, lokiLimits.Overrides.StreamFirstExecutionEnabled("tenant-a"))

		writeFile(t, runtimePath, "overrides:\n  tenant-a:\n    stream_first_execution_enabled: true\n")
		require.Eventually(t, func() bool {
			return lokiLimits.Overrides.StreamFirstExecutionEnabled("tenant-a")
		}, 5*time.Second, 10*time.Millisecond)
		require.NoError(t, lokiLimits.Stop(t.Context()))
	})

	t.Run("returns an error when the runtime config reload period is not positive", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "runtime.yaml"), runtimeConfigYAML)

		cfg := LimitsConfig{RuntimeConfig: runtimeconfig.Config{LoadPath: flagext.StringSliceCSV{filepath.Join(dir, "runtime.yaml")}}}
		_, err := LoadLokiLimits(t.Context(), cfg, log.NewNopLogger(), prometheus.NewRegistry())

		require.EqualError(t, err, "-runtime-config.reload-period must be positive, got 0s")
	})

	t.Run("returns an error when the runtime config file does not exist", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.yaml")
		cfg := LimitsConfig{RuntimeConfig: runtimeconfig.Config{LoadPath: flagext.StringSliceCSV{path}, ReloadPeriod: time.Minute}}

		_, err := LoadLokiLimits(t.Context(), cfg, log.NewNopLogger(), prometheus.NewRegistry())

		require.EqualError(t, err, fmt.Sprintf("loading the runtime config: failed to load runtime config: read %[1]q: open %[1]s: no such file or directory", path))
	})

	t.Run("returns an error when the config file does not exist", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.yaml")

		_, err := LoadLokiLimits(t.Context(), LimitsConfig{ConfigFile: path}, log.NewNopLogger(), prometheus.NewRegistry())

		require.EqualError(t, err, fmt.Sprintf("reading the Loki config file %[1]s: open %[1]s: no such file or directory", path))
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
