package cfg

import (
	"flag"
	"os"
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testCfg struct {
	v int
}

func (cfg *testCfg) RegisterFlags(_ *flag.FlagSet) {
	cfg.v++
}

func (cfg *testCfg) Clone() flagext.Registerer {
	return func(cfg testCfg) flagext.Registerer {
		return &cfg
	}(*cfg)
}

func TestConfigFileLoaderDoesNotMutate(t *testing.T) {
	cfg := &testCfg{}
	err := ConfigFileLoader(nil, "something", true)(cfg)
	require.Nil(t, err)
	require.Equal(t, 0, cfg.v)

	cfg.RegisterFlags(nil)
	require.Equal(t, 1, cfg.v)
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

func TestConfigFileLoader(t *testing.T) {
	t.Run("no config.file arg returns nil", func(t *testing.T) {
		var dst Data
		err := ConfigFileLoader([]string{"-server.port", "9090"}, "config.file", true)(&dst)
		require.NoError(t, err)
		assert.Equal(t, 0, dst.Server.Port) // untouched
	})

	t.Run("loads values from config file", func(t *testing.T) {
		path := writeConfigFile(t, "server:\n  port: 1234\n")
		var dst Data
		err := ConfigFileLoader([]string{"-config.file", path}, "config.file", false)(&dst)
		require.NoError(t, err)
		assert.Equal(t, 1234, dst.Server.Port)
	})

	t.Run("strict mode rejects unknown fields", func(t *testing.T) {
		path := writeConfigFile(t, "unknown_field: true\n")
		var dst Data
		err := ConfigFileLoader([]string{"-config.file", path}, "config.file", true)(&dst)
		require.Error(t, err)
	})

	t.Run("non-strict mode ignores unknown fields", func(t *testing.T) {
		path := writeConfigFile(t, "unknown_field: true\nserver:\n  port: 5678\n")
		var dst Data
		err := ConfigFileLoader([]string{"-config.file", path}, "config.file", false)(&dst)
		require.NoError(t, err)
		assert.Equal(t, 5678, dst.Server.Port)
	})

	t.Run("expands environment variables when config.expand-env is set", func(t *testing.T) {
		t.Setenv("TEST_PORT", "4321")
		path := writeConfigFile(t, "server:\n  port: ${TEST_PORT}\n")
		var dst Data
		err := ConfigFileLoader([]string{"-config.file", path, "-config.expand-env=true"}, "config.file", false)(&dst)
		require.NoError(t, err)
		assert.Equal(t, 4321, dst.Server.Port)
	})

	t.Run("comma-separated paths: uses first existing file", func(t *testing.T) {
		path := writeConfigFile(t, "server:\n  port: 7777\n")
		var dst Data
		err := ConfigFileLoader([]string{"-config.file", "/does/not/exist," + path}, "config.file", false)(&dst)
		require.NoError(t, err)
		assert.Equal(t, 7777, dst.Server.Port)
	})

	t.Run("returns error when no path in the list exists", func(t *testing.T) {
		var dst Data
		err := ConfigFileLoader([]string{"-config.file", "/no/such/file.yaml"}, "config.file", false)(&dst)
		require.Error(t, err)
	})

	t.Run("unknown flags alongside config.file are silently ignored", func(t *testing.T) {
		path := writeConfigFile(t, "server:\n  port: 2222\n")
		var dst Data
		err := ConfigFileLoader([]string{"-config.file", path, "-enterprise.only.flag", "value"}, "config.file", false)(&dst)
		require.NoError(t, err)
		assert.Equal(t, 2222, dst.Server.Port)
	})

	t.Run("unknown flag with value before config.file is skipped correctly", func(t *testing.T) {
		path := writeConfigFile(t, "server:\n  port: 3333\n")
		var dst Data
		err := ConfigFileLoader([]string{"-target", "ingester", "-config.file", path}, "config.file", false)(&dst)
		require.NoError(t, err)
		assert.Equal(t, 3333, dst.Server.Port)
	})

	t.Run("unknown flag with value between config.file and config.expand-env is skipped correctly", func(t *testing.T) {
		t.Setenv("TEST_PORT", "4444")
		path := writeConfigFile(t, "server:\n  port: ${TEST_PORT}\n")
		var dst Data
		err := ConfigFileLoader([]string{"-config.file", path, "-target", "ingester", "-config.expand-env=true"}, "config.file", false)(&dst)
		require.NoError(t, err)
		assert.Equal(t, 4444, dst.Server.Port)
	})

	t.Run("malformed flag syntax does not cause infinite loop", func(t *testing.T) {
		path := writeConfigFile(t, "server:\n  port: 5555\n")
		var dst Data
		err := ConfigFileLoader([]string{"---foo", "-config.file", path}, "config.file", false)(&dst)
		require.NoError(t, err)
		assert.Equal(t, 5555, dst.Server.Port)
	})
}
