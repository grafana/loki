package client

import (
	"errors"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/config"
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
basic_auth:
  username: promtail
enable_http2: false
`

var clientInvalidHTTPConfig = `
url: http://localhost:3100/loki/api/v1/push
bearer_token: tkn
bearer_token_file: tkn_file
`

func Test_Config(t *testing.T) {
	u, err := url.Parse("http://localhost:3100/loki/api/v1/push")
	require.NoError(t, err)
	tests := []struct {
		configValues   string
		expectedConfig Config
		expectedErr    error
	}{
		{
			clientDefaultConfig,
			Config{
				URL: flagext.URLValue{
					URL: u,
				},
				BackoffConfig: backoff.Config{
					MaxBackoff: MaxBackoff,
					MaxRetries: MaxRetries,
					MinBackoff: MinBackoff,
				},
				BatchSize: BatchSize,
				BatchWait: BatchWait,
				Timeout:   Timeout,
				Client:    config.DefaultHTTPClientConfig,
			},
			nil,
		},
		{
			clientCustomConfig,
			Config{
				URL: flagext.URLValue{
					URL: u,
				},
				BackoffConfig: backoff.Config{
					MaxBackoff: 1 * time.Minute,
					MaxRetries: 20,
					MinBackoff: 5 * time.Second,
				},
				BatchSize: 100 * 2048,
				BatchWait: 5 * time.Second,
				Timeout:   5 * time.Second,
				Client: config.HTTPClientConfig{
					BasicAuth: &config.BasicAuth{
						Username: "promtail",
					},
					FollowRedirects: true,
				},
			},
			nil,
		},
		{
			clientInvalidHTTPConfig,
			Config{},
			errors.New("at most one of bearer_token & bearer_token_file must be configured"),
		},
	}
	for _, tc := range tests {
		err := yaml.Unmarshal([]byte(tc.configValues), &clientConfig)

		if tc.expectedErr != nil {
			require.Error(t, err)
			require.Equal(t, tc.expectedErr, err)
		} else {
			require.NoError(t, err)

			if !reflect.DeepEqual(tc.expectedConfig, clientConfig) {
				t.Errorf("Configs do not match, expected: %v, got: %v", tc.expectedConfig, clientConfig)
			}
		}
	}
}
