package indexgateway

import (
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
		{name: "defaults are valid", mutate: func(*Config) {}},
		{name: "zero max concurrent and zero timeout are valid", mutate: func(c *Config) {
			c.MaxConcurrent = 0
			c.MaxConcurrentQueueTimeout = 0
		}},
		{name: "positive max concurrent and timeout are valid", mutate: func(c *Config) {
			c.MaxConcurrent = 200
			c.MaxConcurrentQueueTimeout = 5 * time.Second
		}},
		{name: "negative max concurrent", mutate: func(c *Config) {
			c.MaxConcurrent = -1
		}, wantErr: "index gateway max concurrent must be greater than or equal to 0"},
		{name: "negative queue timeout", mutate: func(c *Config) {
			c.MaxConcurrentQueueTimeout = -time.Second
		}, wantErr: "index gateway max concurrent queue timeout must be greater than or equal to 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg Config
			flagext.DefaultValues(&cfg)
			tc.mutate(&cfg)

			err := cfg.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}
