package cfg

import (
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cfgFile1 = `
server:
  port: 2000
  timeout: 60h
tls:
  cert: cert
  key: key
`
const cfgFile2 = `
server:
  port: 3000
  timeout: 60h
tls:
  cert: cert
  key: key
 `

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
	err := dParse(&data,
		dDefaults(fs),
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
	err := dParse(&data,
		dDefaults(fs),
		yamlSource,
		flagSource,
	)
	require.Error(t, err)
	require.Equal(t, err.Error(), "yaml: unmarshal errors:\n  line 2: field servers not found in type cfg.Data\n  line 6: field keey not found in type cfg.TLS")
}

func TestParseWithMultipleYAML(t *testing.T) {
	data := Data{}
	cfgFiles := []string{cfgFile1, cfgFile2}
	for _, file := range cfgFiles {
		err := dYAML([]byte(file))(&data)
		require.NoError(t, err)
	}
	expectedData := Data{
		Verbose: false,
		Server: Server{
			Port:    3000,
			Timeout: 60 * time.Hour,
		},
		TLS: TLS{
			Cert: "cert",
			Key:  "key",
		},
	}
	require.Equal(t, expectedData, data, nil)
}
