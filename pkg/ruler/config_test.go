package ruler

import (
	"flag"
	"net/url"
	"testing"

	common_config "github.com/prometheus/common/config"
	"github.com/prometheus/prometheus/config"
	"github.com/stretchr/testify/require"

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

func TestCloneRemoteWriteConfigWithCredentials(t *testing.T) {
	var remoteURL, _ = url.Parse("http://remote-write")

	r := RemoteWriteConfig{
		Clients: map[string]config.RemoteWriteConfig{
			"default": {
				URL: &common_config.URL{URL: remoteURL},
				HTTPClientConfig: common_config.HTTPClientConfig{
					Authorization: &common_config.Authorization{
						Credentials: "1234",
					},
				},
			},
		},
	}

	c, err := r.Clone()
	require.NoError(t, err)
	assert.Equal(t, "1234", string(c.Clients["default"].HTTPClientConfig.Authorization.Credentials))
}
