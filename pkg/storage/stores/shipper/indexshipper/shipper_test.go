package indexshipper

import (
	"flag"
	"testing"
	"time"

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
			mutate:  func(cfg *Config) { cfg.IndexGatewayClientConfig.MaxRetries = -2 },
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

func TestPostingsCacheFlags(t *testing.T) {
	var cfg Config
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg.RegisterFlags(flags)

	require.Equal(t, time.Hour, cfg.PostingsCache.DefaultValidity)
	require.NoError(t, flags.Parse([]string{
		"-shipper.postings-cache.default-validity=2m",
		"-shipper.postings-cache.background.write-back-concurrency=3",
	}))
	require.Equal(t, 2*time.Minute, cfg.PostingsCache.DefaultValidity)
	require.Equal(t, 3, cfg.PostingsCache.Background.WriteBackGoroutines)
}
