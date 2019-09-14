package cfg

import (
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// NOTE: These tests CANNOT run in parallel, because of the global state in the
// `flag` package of the standard library.

// TestDefaults checks whether `FlagDefaults()` correctly sets values from flag defaults
func TestDefaults(t *testing.T) {
	var d Data
	err := FlagDefaults(&Data{}, nil)(&d)
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

// TestFlagsSetOnly checks that user-supplied flag values can be correctly distinguished from defaults
func TestFlagsSetOnly(t *testing.T) {
	flag.CommandLine = &flag.FlagSet{}
	var c Data

	var sharedMem []byte
	// c is not passed, to only see the output of dFlags() afterwards
	err := FlagDefaults(&Data{}, &sharedMem)(&Data{})
	require.NoError(t, err)

	err = dFlags([]string{"-verbose", "-server.timeout=12h"}, &Data{}, sharedMem)(&c)
	require.NoError(t, err)

	// check that defaults are correctly stripped away
	assert.Equal(t, Data{
		Verbose: true,
		Server: Server{
			Port:    0,
			Timeout: 12 * time.Hour,
		},
		TLS: TLS{
			Cert: "",
			Key:  "",
		},
	}, c)
}

// TestFlagsMerge checks that defaults and user-supplied values merge correctly
func TestFlagsMerge(t *testing.T) {
	flag.CommandLine = &flag.FlagSet{}

	var c Data
	var sharedMem []byte

	err := Unmarshal(&c,
		FlagDefaults(&Data{}, &sharedMem),
		dFlags([]string{"-verbose", "-server.timeout=12h"}, &Data{}, sharedMem),
	)
	require.NoError(t, err)
	assert.Equal(t, Data{
		Verbose: true,
		Server: Server{
			Port:    80,
			Timeout: 12 * time.Hour,
		},
		TLS: TLS{
			Cert: "CERT",
			Key:  "KEY",
		},
	}, c)
}
