package shardstreams

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{{
		// The zero value of a Config built in code rather than from flags or
		// YAML must be valid.
		name: "zero value",
		cfg:  Config{},
	}, {
		name: "mode disabled",
		cfg:  Config{LimitsServiceStreamShardingMode: LimitsServiceStreamShardingModeDisabled},
	}, {
		name: "mode shadow",
		cfg:  Config{LimitsServiceStreamShardingMode: LimitsServiceStreamShardingModeShadow},
	}, {
		name: "mode live",
		cfg:  Config{LimitsServiceStreamShardingMode: LimitsServiceStreamShardingModeLive},
	}, {
		name:    "unknown mode",
		cfg:     Config{LimitsServiceStreamShardingMode: "enabled"},
		wantErr: `invalid limits_service_stream_sharding_mode "enabled": must be one of "disabled", "shadow", "live"`,
	}, {
		name:    "negative rate window",
		cfg:     Config{LimitsServiceStreamShardingRateWindow: -time.Second},
		wantErr: "invalid limits_service_stream_sharding_rate_window -1s: must not be negative",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.cfg.Validate()
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, test.wantErr)
		})
	}
}
