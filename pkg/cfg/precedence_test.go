package cfg

import (
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file checks precedence rules are correctly working
// The default precedence recommended by this package is the following:
// flag defaults < yaml < user-set flags
//
// The following tests make sure that this is indeed correct

const y = `
verbose: true
tls:
  cert: YAML
server:
  port: 1234
`

func TestYAMLOverDefaults(t *testing.T) {
	data := Data{}
	fs := flag.NewFlagSet(t.Name(), flag.PanicOnError)
	err := Unmarshal(&data,
		dDefaults(fs),
		dYAML([]byte(y)),
	)

	require.NoError(t, err)
	assert.Equal(t, Data{
		Verbose: true, // yaml
		Server: Server{
			Port:    1234,             // yaml
			Timeout: 60 * time.Second, // default
		},
		TLS: TLS{
			Cert: "YAML",       // yaml
			Key:  "DEFAULTKEY", // default
		},
	}, data)
}

func TestFlagOverYAML(t *testing.T) {
	data := Data{}
	fs := flag.NewFlagSet(t.Name(), flag.PanicOnError)

	err := Unmarshal(&data,
		dDefaults(fs),
		dYAML([]byte(y)),
		dFlags(fs, []string{"-verbose=false", "-tls.cert=CLI"}),
	)

	require.NoError(t, err)
	assert.Equal(t, Data{
		Verbose: false, // flag
		Server: Server{
			Port:    1234,             // yaml
			Timeout: 60 * time.Second, // default
		},
		TLS: TLS{
			Cert: "CLI",        // flag
			Key:  "DEFAULTKEY", // default
		},
	}, data)
}
