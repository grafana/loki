package indexshipper

import (
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	validConfig := func() Config {
		cfg := Config{}
		flagext.DefaultValues(&cfg)
		return cfg
	}

	t.Run("default config is valid", func(t *testing.T) {
		cfg := validConfig()
		require.NoError(t, cfg.Validate())
	})

	t.Run("negative index_gateway_client.max_in_flight_requests fails", func(t *testing.T) {
		cfg := validConfig()
		cfg.IndexGatewayClientConfig.MaxInFlightRequests = -1

		err := cfg.Validate()
		require.Error(t, err)
		require.ErrorContains(t, err, "shipper.index-gateway-client")
		require.ErrorContains(t, err, "max_in_flight_requests must not be negative")
	})

	t.Run("negative shadow_index_gateway_client.max_in_flight_requests fails", func(t *testing.T) {
		cfg := validConfig()
		cfg.ShadowIndexGatewayClientConfig.MaxInFlightRequests = -1

		err := cfg.Validate()
		require.Error(t, err)
		require.ErrorContains(t, err, "shipper.shadow-index-gateway-client")
		require.ErrorContains(t, err, "max_in_flight_requests must not be negative")
	})
}
