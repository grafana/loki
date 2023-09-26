package ruler

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRemoteWriteConfig(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ExitOnError)

	r := RemoteWriteConfig{}
	r.RegisterFlags(fs)

	// assert new multi clients config is backward compatible if with single tenant.
	// if not provided
	assert.NotNil(t, r.Clients)
}
