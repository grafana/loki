package http

import (
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v2"
)

// defaultConfig should match the default flag values defined in RegisterFlagsWithPrefix.
var defaultConfig = Config{
	IdleConnTimeout:       90 * time.Second,
	ResponseHeaderTimeout: 2 * time.Minute,
	InsecureSkipVerify:    false,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
	MaxIdleConns:          100,
	MaxIdleConnsPerHost:   100,
	MaxConnsPerHost:       0,
}

func TestConfig(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		config         string
		expectedConfig Config
		expectedErr    error
	}{
		"default config": {
			config:         "",
			expectedConfig: defaultConfig,
			expectedErr:    nil,
		},
		"custom config": {
			config: `
idle_conn_timeout: 2s
response_header_timeout: 3s
insecure_skip_verify: true
tls_handshake_timeout: 4s
expect_continue_timeout: 5s
max_idle_connections: 6
max_idle_connections_per_host: 7
max_connections_per_host: 8
`,
			expectedConfig: Config{
				IdleConnTimeout:       2 * time.Second,
				ResponseHeaderTimeout: 3 * time.Second,
				InsecureSkipVerify:    true,
				TLSHandshakeTimeout:   4 * time.Second,
				ExpectContinueTimeout: 5 * time.Second,
				MaxIdleConns:          6,
				MaxIdleConnsPerHost:   7,
				MaxConnsPerHost:       8,
			},
			expectedErr: nil,
		},
		"invalid type": {
			config:         `max_idle_connections: foo`,
			expectedConfig: defaultConfig,
			expectedErr:    &yaml.TypeError{Errors: []string{"line 1: cannot unmarshal !!str `foo` into int"}},
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			cfg := Config{}
			flagext.DefaultValues(&cfg)

			err := yaml.Unmarshal([]byte(testData.config), &cfg)
			require.Equal(t, testData.expectedErr, err)
			require.Equal(t, testData.expectedConfig, cfg)
		})
	}
}
