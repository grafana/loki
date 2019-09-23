package cfg

import (
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	yamlSource := func() Source {
		return dYAML([]byte(`
server:
  port: 2000
  timeout: 60h
tls:
  key: YAML
`))
	}

	flagSource := func(fs *flag.FlagSet) Source {
		return dFlags(fs, []string{"-verbose", "-server.port=21"})
	}

	var c Data
	err := dParse(&c, yamlSource, flagSource)
	require.NoError(t, err)

	require.Equal(t, Data{
		Verbose: true,
		Server: Server{
			Port:    21,
			Timeout: 60 * time.Hour,
		},
		TLS: TLS{
			Cert: "CERT",
			Key:  "YAML",
		},
	}, c)
}
