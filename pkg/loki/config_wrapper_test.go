package loki

import (
	"flag"
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/util/cfg"
)

func Test_CommonConfig(t *testing.T) {
	testContext := func(configFileString string, args []string) (ConfigWrapper, ConfigWrapper) {
		config := ConfigWrapper{}
		fs := flag.NewFlagSet(t.Name(), flag.PanicOnError)

		file, err := ioutil.TempFile("", "config.yaml")
		defer func() {
			os.Remove(file.Name())
		}()

		require.NoError(t, err)
		_, err = file.WriteString(configFileString)
		require.NoError(t, err)

		configFileArgs := []string{"-config.file", file.Name()}
		if args == nil {
			args = configFileArgs
		} else {
			args = append(args, configFileArgs...)
		}
		err = cfg.DynamicUnmarshal(&config, args, fs)
		require.NoError(t, err)

		defaults := ConfigWrapper{}
		freshFlags := flag.NewFlagSet(t.Name(), flag.PanicOnError)
		err = cfg.DefaultUnmarshal(&defaults, args, freshFlags)
		require.NoError(t, err)

		return config, defaults
	}

	t.Run("common path prefix config", func(t *testing.T) {
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

	t.Run("common memberlist config", func(t *testing.T) {
		// components with rings
		// * ingester
		// * distributor
		// * ruler

		t.Run("does not automatically configure memberlist when no top-level memberlist config is provided", func(t *testing.T) {
			configFileString := `---`
			config, defaults := testContext(configFileString, nil)

			assert.EqualValues(t, defaults.Ingester.LifecyclerConfig.RingConfig.KVStore.Store, config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)
			assert.EqualValues(t, defaults.Distributor.DistributorRing.KVStore.Store, config.Distributor.DistributorRing.KVStore.Store)
			assert.EqualValues(t, defaults.Ruler.Ring.KVStore.Store, config.Ruler.Ring.KVStore.Store)
		})

		t.Run("when top-level memberlist join_members are provided, all applicable rings are defaulted to use memberlist", func(t *testing.T) {
			configFileString := `---
memberlist:
  join_members:
    - foo.bar.example.com`

			config, _ := testContext(configFileString, nil)

			assert.EqualValues(t, memberlistStr, config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)
			assert.EqualValues(t, memberlistStr, config.Distributor.DistributorRing.KVStore.Store)
			assert.EqualValues(t, memberlistStr, config.Ruler.Ring.KVStore.Store)
		})

		t.Run("explicit ring configs provided via config file are preserved", func(t *testing.T) {
			configFileString := `---
memberlist:
  join_members:
    - foo.bar.example.com
distributor:
  ring:
    kvstore:
      store: etcd`

			config, _ := testContext(configFileString, nil)

			assert.EqualValues(t, "etcd", config.Distributor.DistributorRing.KVStore.Store)

			assert.EqualValues(t, memberlistStr, config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)
			assert.EqualValues(t, memberlistStr, config.Ruler.Ring.KVStore.Store)
		})

		t.Run("explicit ring configs provided via command line are preserved", func(t *testing.T) {
			configFileString := `---
memberlist:
  join_members:
    - foo.bar.example.com`

			config, _ := testContext(configFileString, []string{"-ruler.ring.store", "inmemory"})

			assert.EqualValues(t, "inmemory", config.Ruler.Ring.KVStore.Store)

			assert.EqualValues(t, memberlistStr, config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)
			assert.EqualValues(t, memberlistStr, config.Distributor.DistributorRing.KVStore.Store)
		})
	})
}

// Can't use a totally empty yaml file or it causes weird behavior in the unmarhsalling
const minimalConfig = `---
schema_config:
  configs:
    - from: 2021-08-01
      schema: v11
 
memberlist: 
  join_members: 
    - loki.loki-dev-single-binary.svc.cluster.local`

func TestDefaultUnmarshal(t *testing.T) {
	t.Run("with an empty config file and no command line args, defaults are use", func(t *testing.T) {
		file, err := ioutil.TempFile("", "config.yaml")
		defer func() {
			os.Remove(file.Name())
		}()
		require.NoError(t, err)

		_, err = file.WriteString(minimalConfig)
		require.NoError(t, err)
		var config ConfigWrapper

		flags := flag.NewFlagSet(t.Name(), flag.PanicOnError)
		args := []string{"-config.file", file.Name()}
		cfg.DefaultUnmarshal(&config, args, flags)

		assert.True(t, config.AuthEnabled)
		assert.Equal(t, 80, config.Server.HTTPListenPort)
		assert.Equal(t, 9095, config.Server.GRPCListenPort)
	})
}
