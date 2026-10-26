package querytee

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/runtimeconfig"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/validation"
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

	t.Run("resolves the stream-first limit from the flag, the limits_config block and the tenant overrides", func(t *testing.T) {
		const unrelatedLimit = "dataobj_query_start_time: \"2026-01-01\""

		testCases := []struct {
			name         string
			flagValue    *bool
			configYAML   string
			runtimeYAML  string
			wantByTenant map[string]bool
		}{
			{
				name:         "disables the limit for every tenant when neither the flag nor a file sets it",
				flagValue:    nil,
				configYAML:   "limits_config:\n  " + unrelatedLimit + "\n",
				runtimeYAML:  "overrides:\n  tenant-a:\n    " + unrelatedLimit + "\n",
				wantByTenant: map[string]bool{"other": false, "tenant-a": false},
			},
			{
				name:         "keeps the limits_config value when the flag is not set",
				flagValue:    nil,
				configYAML:   "limits_config:\n  stream_first_execution_enabled: true\n",
				runtimeYAML:  "overrides:\n  tenant-a:\n    " + unrelatedLimit + "\n",
				wantByTenant: map[string]bool{"other": true, "tenant-a": true},
			},
			{
				name:         "enables the limit for every tenant when only the flag sets it",
				flagValue:    new(true),
				configYAML:   "limits_config:\n  " + unrelatedLimit + "\n",
				runtimeYAML:  "overrides:\n  tenant-a:\n    " + unrelatedLimit + "\n",
				wantByTenant: map[string]bool{"other": true, "tenant-a": true},
			},
			{
				name:         "prefers a flag that enables the limit over the limits_config value",
				flagValue:    new(true),
				configYAML:   "limits_config:\n  stream_first_execution_enabled: false\n",
				runtimeYAML:  "overrides:\n  tenant-a:\n    " + unrelatedLimit + "\n",
				wantByTenant: map[string]bool{"other": true, "tenant-a": true},
			},
			{
				name:         "prefers a flag that disables the limit over the limits_config value",
				flagValue:    new(false),
				configYAML:   "limits_config:\n  stream_first_execution_enabled: true\n",
				runtimeYAML:  "overrides:\n  tenant-a:\n    " + unrelatedLimit + "\n",
				wantByTenant: map[string]bool{"other": false, "tenant-a": false},
			},
			{
				name:         "prefers the tenant override over a flag that enables the limit",
				flagValue:    new(true),
				configYAML:   "limits_config:\n  " + unrelatedLimit + "\n",
				runtimeYAML:  "overrides:\n  tenant-a:\n    stream_first_execution_enabled: false\n",
				wantByTenant: map[string]bool{"other": true, "tenant-a": false},
			},
			{
				name:         "prefers the tenant override over a flag that disables the limit",
				flagValue:    new(false),
				configYAML:   "limits_config:\n  stream_first_execution_enabled: true\n",
				runtimeYAML:  "overrides:\n  tenant-a:\n    stream_first_execution_enabled: true\n",
				wantByTenant: map[string]bool{"other": false, "tenant-a": true},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				dir := t.TempDir()
				writeFile(t, filepath.Join(dir, "config.yaml"), tc.configYAML)
				writeFile(t, filepath.Join(dir, "runtime.yaml"), tc.runtimeYAML)

				cfg := LimitsConfig{
					ConfigFile:                  filepath.Join(dir, "config.yaml"),
					RuntimeConfig:               runtimeconfig.Config{LoadPath: flagext.StringSliceCSV{filepath.Join(dir, "runtime.yaml")}, ReloadPeriod: time.Minute},
					StreamFirstExecutionEnabled: tc.flagValue,
				}
				lokiLimits, err := LoadLokiLimits(t.Context(), cfg, log.NewNopLogger(), prometheus.NewRegistry())
				require.NoError(t, err)

				for tenant, want := range tc.wantByTenant {
					require.Equal(t, want, lokiLimits.Overrides.StreamFirstExecutionEnabled(tenant), tenant)
				}
				require.NoError(t, lokiLimits.Stop(t.Context()))
			})
		}
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

func TestLimitsConfig_RegisterFlags(t *testing.T) {
	const streamFirstFlag = "querier.stream-first-execution-enabled"

	t.Run("registers the stream-first flag under the name of the Loki flag", func(t *testing.T) {
		var lokiLimits validation.Limits
		lokiFlags := flag.NewFlagSet("loki", flag.ContinueOnError)
		lokiLimits.RegisterFlags(lokiFlags)

		var cfg LimitsConfig
		flags := flag.NewFlagSet("querytee", flag.ContinueOnError)
		cfg.RegisterFlags(flags)

		require.NotNil(t, lokiFlags.Lookup(streamFirstFlag))
		require.NotNil(t, flags.Lookup(streamFirstFlag))
	})

	t.Run("leaves stream-first unset when the flag is not set", func(t *testing.T) {
		cfg := parseLimitsFlags(t)

		require.Nil(t, cfg.StreamFirstExecutionEnabled)
	})

	t.Run("enables stream-first when the flag is true", func(t *testing.T) {
		cfg := parseLimitsFlags(t, "-"+streamFirstFlag+"=true")

		require.Equal(t, new(true), cfg.StreamFirstExecutionEnabled)
	})

	t.Run("disables stream-first when the flag is false", func(t *testing.T) {
		cfg := parseLimitsFlags(t, "-"+streamFirstFlag+"=false")

		require.Equal(t, new(false), cfg.StreamFirstExecutionEnabled)
	})

	t.Run("enables stream-first when the flag has no value", func(t *testing.T) {
		cfg := parseLimitsFlags(t, "-"+streamFirstFlag)

		require.Equal(t, new(true), cfg.StreamFirstExecutionEnabled)
	})

	t.Run("returns an error when the flag value is not a boolean", func(t *testing.T) {
		var cfg LimitsConfig
		flags := flag.NewFlagSet("querytee", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		cfg.RegisterFlags(flags)

		err := flags.Parse([]string{"-" + streamFirstFlag + "=maybe"})

		require.EqualError(t, err, `invalid boolean value "maybe" for -querier.stream-first-execution-enabled: strconv.ParseBool: parsing "maybe": invalid syntax`)
		require.Nil(t, cfg.StreamFirstExecutionEnabled)
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func parseLimitsFlags(t *testing.T, args ...string) LimitsConfig {
	t.Helper()

	var cfg LimitsConfig
	flags := flag.NewFlagSet("querytee", flag.ContinueOnError)
	cfg.RegisterFlags(flags)
	require.NoError(t, flags.Parse(args))
	return cfg
}
