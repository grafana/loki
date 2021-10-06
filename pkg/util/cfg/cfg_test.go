package cfg

import (
	"flag"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func TestParse(t *testing.T) {
	yamlSource := dYAML([]byte(`
server:
  port: 2000
  timeout: 60h
tls:
  key: YAML
`))

	fs := flag.NewFlagSet(t.Name(), flag.PanicOnError)
	flagSource := dFlags(fs, []string{"-verbose", "-server.port=21"})

	data := Data{}
	err := Unmarshal(&data,
		Defaults(fs),
		yamlSource,
		flagSource,
	)
	require.NoError(t, err)

	assert.Equal(t, Data{
		Verbose: true, // flag
		Server: Server{
			Port:    21,             // flag
			Timeout: 60 * time.Hour, // defaults
		},
		TLS: TLS{
			Cert: "DEFAULTCERT", // defaults
			Key:  "YAML",        // yaml
		},
	}, data)
}

func TestParseWithInvalidYAML(t *testing.T) {
	yamlSource := dYAML([]byte(`
servers:
  ports: 2000
  timeoutz: 60h
tls:
  keey: YAML
`))

	fs := flag.NewFlagSet(t.Name(), flag.PanicOnError)
	flagSource := dFlags(fs, []string{"-verbose", "-server.port=21"})

	data := Data{}
	err := Unmarshal(&data,
		Defaults(fs),
		yamlSource,
		flagSource,
	)
	require.Error(t, err)
	require.Equal(t, err.Error(), "yaml: unmarshal errors:\n  line 2: field servers not found in type cfg.Data\n  line 6: field keey not found in type cfg.TLS")
}

func TestDefaultUnmarshal(t *testing.T) {
	t.Run("with an empty config file and no command line args, defaults are use", func(t *testing.T) {
		file, err := ioutil.TempFile("", "config.yaml")
		defer func() {
			os.Remove(file.Name())
		}()
		require.NoError(t, err)

		configFileString := `---`
		_, err = file.WriteString(configFileString)
		require.NoError(t, err)
		config := TestConfigWrapper{}

		flags := flag.NewFlagSet(t.Name(), flag.PanicOnError)
		args := []string{"-config.file", file.Name()}
		DefaultUnmarshal(&config, args, flags)

		assert.Equal(t, "Jerry", config.Name)
		assert.Equal(t, true, config.Role.Sings)
		assert.Equal(t, "guitar", config.Role.Instrument)
	})

	t.Run("values provided in config file take precedence over defaults", func(t *testing.T) {
		file, err := ioutil.TempFile("", "config.yaml")
		defer func() {
			os.Remove(file.Name())
		}()
		require.NoError(t, err)

		configFileString := `---
name: Phil`
		_, err = file.WriteString(configFileString)
		require.NoError(t, err)

		buf, err := ioutil.ReadFile(file.Name())
		require.NoError(t, err)

		strictYamlConfig := TestConfigWrapper{}
		yaml.UnmarshalStrict(buf, &strictYamlConfig)
		assert.Equal(t, "Phil", strictYamlConfig.Name)

		config := TestConfigWrapper{}

		flags := flag.NewFlagSet(t.Name(), flag.PanicOnError)
		args := []string{"-config.file", file.Name()}
		DefaultUnmarshal(&config, args, flags)

		assert.Equal(t, "Phil", config.Name)
		assert.Equal(t, true, config.Role.Sings)
	})
}

type TestConfigWrapper struct {
	TestConfig `yaml:",inline"`
	ConfigFile string
}

func (c *TestConfigWrapper) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&c.ConfigFile, "config.file", "", "yaml file to load")
	c.TestConfig.RegisterFlags(f)
}

func (c *TestConfigWrapper) Clone() flagext.Registerer {
	return func(c TestConfigWrapper) *TestConfigWrapper {
		return &c
	}(*c)
}

type TestConfig struct {
	Name string `yaml:"name"`
	Role Role   `yaml:"role"`
}

func (c *TestConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&c.Name, "name", "Jerry", "Favorite band member")
	c.Role.RegisterFlags(f)
}

type Role struct {
	Sings      bool   `yaml:"sings"`
	Instrument string `yaml:"instrument"`
}

func (c *Role) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.Sings, "sings", true, "Do they sing?")
	f.StringVar(&c.Instrument, "instrument", "guitar", "What instrument do they play?")
}
