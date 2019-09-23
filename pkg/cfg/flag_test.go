package cfg

import (
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaults checks whether `FlagDefaults()` correctly sets values from flag defaults
func TestDefaults(t *testing.T) {
	var d Data
	err := Defaults(&flag.FlagSet{})(&d)
	require.NoError(t, err)
	assert.Equal(t, Data{
		Verbose: false,
		Server: Server{
			Port:    80,
			Timeout: 60 * time.Second,
		},
		TLS: TLS{
			Cert: "CERT",
			Key:  "KEY",
		},
	}, d)
}
