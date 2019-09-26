package cfg

import (
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	yamlSource := dYAML([]byte(`
server:
  port: 2000
  timeout: 60h
tls:
  key: YAML
`))
	fs := flag.NewFlagSet("testParse", flag.PanicOnError)
	flagSource := dFlags(fs, []string{"-verbose", "-server.port=21"})

	var c Data
	err := dParse(&c, dDefaults(fs), yamlSource, flagSource)
	require.NoError(t, err)

	require.Equal(t, Data{
		Verbose: true,
		Server: Server{
			Port:    21,
			Timeout: 60 * time.Hour,
		},
		TLS: TLS{
			Cert: "DEFAULTCERT",
			Key:  "YAML",
		},
	}, c)
}
