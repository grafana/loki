package client

import (
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/stretchr/testify/require"

	"gopkg.in/yaml.v2"
)

var clientConfig = Config{}

var clientDefaultConfig = (`
url: http://localhost:3100/loki/api/v1/push
`)

var clientCustomConfig = `
url: http://localhost:3100/loki/api/v1/push
backoff_config:
  max_retries: 20
  min_period: 5s
  max_period: 1m
batchwait: 5s
batchsize: 204800
timeout: 5s
`

func Test_Config(t *testing.T) {
	u, err := url.Parse("http://localhost:3100/loki/api/v1/push")
	require.NoError(t, err)
	tests := []struct {
		configValues   string
		expectedConfig Config
	}{
		{
			clientDefaultConfig,
			Config{
				URL: flagext.URLValue{
					URL: u,
				},
				BackoffConfig: util.BackoffConfig{
					MaxBackoff: MaxBackoff,
					MaxRetries: MaxRetries,
					MinBackoff: MinBackoff,
				},
				BatchSize: BatchSize,
				BatchWait: BatchWait,
				Timeout:   Timeout,
			},
		},
		{
			clientCustomConfig,
			Config{
				URL: flagext.URLValue{
					URL: u,
				},
				BackoffConfig: util.BackoffConfig{
					MaxBackoff: 1 * time.Minute,
					MaxRetries: 20,
					MinBackoff: 5 * time.Second,
				},
				BatchSize: 100 * 2048,
				BatchWait: 5 * time.Second,
				Timeout:   5 * time.Second,
			},
		},
	}
	for _, tc := range tests {
		err := yaml.Unmarshal([]byte(tc.configValues), &clientConfig)
		require.NoError(t, err)

		if !reflect.DeepEqual(tc.expectedConfig, clientConfig) {
			t.Errorf("Configs does not match, expected: %v, received: %v", tc.expectedConfig, clientConfig)
		}
	}
}
