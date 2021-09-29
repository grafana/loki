package loki

import (
	"flag"
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/util/cfg"
)

func Test_CommonConfig(t *testing.T) {
	// defaultYamlConfig := `---
	// server:
	// port: 8080
	// `
	testContext := func(configFileString string, args []string) (ConfigWrapper, ConfigWrapper) {
		config := ConfigWrapper{}
		fs := flag.NewFlagSet(t.Name(), flag.PanicOnError)

		file, err := ioutil.TempFile("", "config.yaml")
		require.NoError(t, err)
		_, err = file.WriteString(configFileString)
		require.NoError(t, err)

		configFileArgs := []string{"-config.file", file.Name()}
		if args == nil {
			args = configFileArgs
		} else {
			args = append(args, configFileArgs...)
		}
		cfg.DynamicUnmarshal(&config, args, fs)

		defaults := ConfigWrapper{}
		freshFlags := flag.NewFlagSet(t.Name(), flag.PanicOnError)
		err = cfg.DefaultUnmarshal(&defaults, args, freshFlags)
		require.NoError(t, err)

		return config, defaults
	}

	t.Run("common base directory config", func(t *testing.T) {
		t.Run("does not override defaults for file paths when not provided", func(t *testing.T) {
			configFileString := `---`
			config, defaults := testContext(configFileString, nil)

			assert.EqualValues(t, defaults.Ruler.RulePath, config.Ruler.RulePath)
			assert.EqualValues(t, defaults.Ingester.WAL.Dir, config.Ingester.WAL.Dir)
		})

		t.Run("when provided, rewrites all default file paths to use common prefix", func(t *testing.T) {
			configFileString := `---
common:
  path_prefix: /opt/loki`
			config, _ := testContext(configFileString, nil)

			assert.EqualValues(t, "/opt/loki/rules", config.Ruler.RulePath)
			assert.EqualValues(t, "/opt/loki/wal", config.Ingester.WAL.Dir)
		})

		t.Run("does not rewrite custom (non-default) paths passed via config file", func(t *testing.T) {
			configFileString := `---
common:
  path_prefix: /opt/loki
ruler:
  rule_path: /etc/ruler/rules`
			config, _ := testContext(configFileString, nil)

			assert.EqualValues(t, "/etc/ruler/rules", config.Ruler.RulePath)
			assert.EqualValues(t, "/opt/loki/wal", config.Ingester.WAL.Dir)
		})

		t.Run("does not rewrite custom (non-default) paths passed via the command line", func(t *testing.T) {
			configFileString := `---
common:
  path_prefix: /opt/loki`
			config, _ := testContext(configFileString, []string{"-ruler.rule-path", "/etc/ruler/rules"})

			assert.EqualValues(t, "/etc/ruler/rules", config.Ruler.RulePath)
			assert.EqualValues(t, "/opt/loki/wal", config.Ingester.WAL.Dir)
		})
	})

}
