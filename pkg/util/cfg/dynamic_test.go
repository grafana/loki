package cfg

import (
	"flag"
	"os"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_DynamicUnmarshal(t *testing.T) {
	defaultYamlConfig := `---
server:
  port: 8080
`

	testContext := func(mockApplyDynamicConfig Source, config string, args []string) DynamicConfig {
		data := NewDynamicConfig(mockApplyDynamicConfig)
		fs := flag.NewFlagSet(t.Name(), flag.PanicOnError)

		file, err := os.CreateTemp("", "config.yaml")
		require.NoError(t, err)
		_, err = file.WriteString(config)
		require.NoError(t, err)

		configFileArgs := []string{"-config.file", file.Name()}
		if args == nil {
			args = configFileArgs
		} else {
			args = append(args, configFileArgs...)
		}

		err = DynamicUnmarshal(&data, args, fs)
		require.NoError(t, err)
		return data
	}

	t.Run("parses defaults", func(t *testing.T) {
		data := testContext(nil, "", nil)

		assert.Equal(t, 80, data.Server.Port)
		assert.Equal(t, 60*time.Second, data.Server.Timeout)
	})

	t.Run("parses config from config.file", func(t *testing.T) {
		data := testContext(nil, defaultYamlConfig, nil)
		assert.Equal(t, 8080, data.Server.Port)
	})

	t.Run("calls ApplyDynamicConfig on provided DynamicCloneable", func(t *testing.T) {
		applyDynamicConfigCalled := false
		mockApplyDynamicConfig := func(_ Cloneable) error {
			applyDynamicConfigCalled = true
			return nil
		}
		data := testContext(mockApplyDynamicConfig, "", nil)
		assert.NotNil(t, data)
		assert.True(t, applyDynamicConfigCalled)
	})

	t.Run("makes config from file available to ApplyDynamicConfig", func(t *testing.T) {
		var configFromFile *DynamicConfig
		mockApplyDynamicConfig := func(dst Cloneable) error {
			var ok bool
			configFromFile, ok = dst.(*DynamicConfig)
			require.True(t, ok)
			return nil
		}

		data := testContext(mockApplyDynamicConfig, defaultYamlConfig, nil)
		assert.NotNil(t, data)
		assert.NotNil(t, configFromFile)
		assert.Equal(t, 8080, configFromFile.Server.Port)
	})

	t.Run("config from file take precedence over config applied in ApplyDynamicConfig", func(t *testing.T) {
		mockApplyDynamicConfig := func(dst Cloneable) error {
			config, ok := dst.(*DynamicConfig)
			require.True(t, ok)
			config.Server.Port = 9090
			return nil
		}

		data := testContext(mockApplyDynamicConfig, defaultYamlConfig, nil)
		assert.Equal(t, 8080, data.Server.Port)
	})

	t.Run("config from command line takes precedence over config applied in ApplyDynamicConfig and in file", func(t *testing.T) {
		mockApplyDynamicConfig := func(dst Cloneable) error {
			config, ok := dst.(*DynamicConfig)
			require.True(t, ok)
			config.Server.Port = 9090
			return nil
		}

		args := []string{
			"-server.port", "7070",
		}

		data := testContext(mockApplyDynamicConfig, defaultYamlConfig, args)
		assert.Equal(t, 7070, data.Server.Port)
	})
}

type DynamicConfig struct {
	Server             `yaml:"server"`
	ConfigFile         string
	applyDynamicConfig Source
}

func NewDynamicConfig(applyDynamicConfig Source) DynamicConfig {
	if applyDynamicConfig == nil {
		applyDynamicConfig = func(_ Cloneable) error {
			return nil
		}
	}
	return DynamicConfig{
		Server:             Server{},
		ConfigFile:         "",
		applyDynamicConfig: applyDynamicConfig,
	}
}

func (d *DynamicConfig) Clone() flagext.Registerer {
	return func(d DynamicConfig) *DynamicConfig {
		return &d
	}(*d)
}

func (d *DynamicConfig) ApplyDynamicConfig() Source {
	return d.applyDynamicConfig
}

func (d *DynamicConfig) RegisterFlags(fs *flag.FlagSet) {
	fs.IntVar(&d.Server.Port, "server.port", 80, "")
	fs.DurationVar(&d.Server.Timeout, "server.timeout", 60*time.Second, "")
	fs.StringVar(&d.ConfigFile, "config.file", "", "yaml file to load")
}
