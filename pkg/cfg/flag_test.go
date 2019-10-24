package cfg

import (
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaults checks that defaults are correctly obtained from a
// flagext.Registerer
func TestDefaults(t *testing.T) {
	data := Data{}
	fs := flag.NewFlagSet(t.Name(), flag.PanicOnError)

	err := Unmarshal(&data,
		dDefaults(fs),
	)

	require.NoError(t, err)
	assert.Equal(t, Data{
		Verbose: false,
		Server: Server{
			Port:    80,
			Timeout: 60 * time.Second,
		},
		TLS: TLS{
			Cert: "DEFAULTCERT",
			Key:  "DEFAULTKEY",
		},
	}, data)
}

// TestFlags checks that defaults and flag values (they can't be separated) are
// correctly obtained from the command line
func TestFlags(t *testing.T) {
	data := Data{}
	fs := flag.NewFlagSet(t.Name(), flag.PanicOnError)
	err := Unmarshal(&data,
		dDefaults(fs),
		dFlags(fs, []string{"-server.timeout=10h", "-verbose"}),
	)
	require.NoError(t, err)

	assert.Equal(t, Data{
		Verbose: true,
		Server: Server{
			Port:    80,
			Timeout: 10 * time.Hour,
		},
		TLS: TLS{
			Cert: "DEFAULTCERT",
			Key:  "DEFAULTKEY",
		},
	}, data)
}
