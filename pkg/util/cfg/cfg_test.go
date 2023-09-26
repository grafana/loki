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

func TestParse(t *testing.T) {
	yamlSource := dYAMLStrict([]byte(`
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
	yamlSource := dYAMLStrict([]byte(`
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
	testContext := func(yamlString string, args []string) TestConfigWrapper {
		file, err := os.CreateTemp("", "config.yaml")
		defer func() {
			os.Remove(file.Name())
		}()
		require.NoError(t, err)

		_, err = file.WriteString(yamlString)
		require.NoError(t, err)

		configFileArgs := []string{"-config.file", file.Name()}
		if args == nil {
			args = configFileArgs
		} else {
			args = append(args, configFileArgs...)
		}

		var config TestConfigWrapper
		flags := flag.NewFlagSet(t.Name(), flag.PanicOnError)
		err = DefaultUnmarshal(&config, args, flags)
		require.NoError(t, err)

		return config
	}
	t.Run("with an empty config file and no command line args, defaults are used", func(t *testing.T) {
		configFileString := `---
required: foo`
		config := testContext(configFileString, nil)

		assert.Equal(t, "Jerry", config.Name)
		assert.Equal(t, true, config.Role.Sings)
		assert.Equal(t, "guitar", config.Role.Instrument)
	})

	t.Run("values provided in config file take precedence over defaults", func(t *testing.T) {
		configFileString := `---
required: foo
name: Phil`
		config := testContext(configFileString, nil)

		assert.Equal(t, "Phil", config.Name)
		assert.Equal(t, true, config.Role.Sings)
	})

	t.Run("partial structs can be provided in the config file, with defaults filling zeros", func(t *testing.T) {
		configFileString := `---
required: foo
name: Phil
role:
  instrument: bass`
		config := testContext(configFileString, nil)

		assert.Equal(t, "Phil", config.Name)
		assert.Equal(t, "bass", config.Role.Instrument)
		//zero value overridden by default
		assert.Equal(t, true, config.Role.Sings)
	})

	t.Run("values can be explicitly zeroed out in config file", func(t *testing.T) {
		configFileString := `---
required: foo
name: Mickey
role:
  sings: false
  instrument: drums`
		config := testContext(configFileString, nil)

		assert.Equal(t, "Mickey", config.Name)
		assert.Equal(t, "drums", config.Role.Instrument)
		assert.Equal(t, false, config.Role.Sings)
	})

	t.Run("values passed by command line take precedence", func(t *testing.T) {
		configFileString := `---
name: Mickey
role:
  sings: false`

		args := []string{"-name", "Bob", "-role.sings=true", "-role.instrument", "piano"}
		config := testContext(configFileString, args)

		assert.Equal(t, "Bob", config.Name)
		assert.Equal(t, true, config.Role.Sings)
		assert.Equal(t, "piano", config.Role.Instrument)
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
	//Add a parameter that will always be there, as the yaml parser exhibits
	//weird behavior when a config file is completely empty
	Required string `yaml:"required"`
	Name     string `yaml:"name"`
	Role     Role   `yaml:"role"`
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
	f.BoolVar(&c.Sings, "role.sings", true, "Do they sing?")
	f.StringVar(&c.Instrument, "role.instrument", "guitar", "What instrument do they play?")
}
