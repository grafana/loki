package indexshipper

import (
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name:   "defaults",
			mutate: func(*Config) {},
		},
		{
			name:    "index gateway client is validated",
			mutate:  func(cfg *Config) { cfg.IndexGatewayClientConfig.MaxRetries = -1 },
			wantErr: "shipper.index-gateway-client: index gateway client max-retries",
		},
		{
			name:    "shadow index gateway client is validated",
			mutate:  func(cfg *Config) { cfg.ShadowIndexGatewayClientConfig.MaxInFlightRequests = -1 },
			wantErr: "shipper.shadow-index-gateway-client: index gateway client max-in-flight-requests",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{}
			flagext.DefaultValues(&cfg)
			tc.mutate(&cfg)

			err := cfg.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}
