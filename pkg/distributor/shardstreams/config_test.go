package shardstreams

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/flagext"
)

func TestPerPolicyConfigOverride_ApplyTo(t *testing.T) {
	base := Config{
		Enabled:                  true,
		DesiredRate:              flagext.ByteSize(1536 * 1024),
		TimeShardingEnabled:      false,
		TimeShardingIgnoreRecent: 40 * time.Minute,
		LoggingEnabled:           false,
	}

	boolPtr := func(b bool) *bool { return &b }
	bytePtr := func(b flagext.ByteSize) *flagext.ByteSize { return &b }
	durPtr := func(d model.Duration) *model.Duration { return &d }

	// withBase returns a copy of base with fn applied, for readable per-field expectations.
	withBase := func(fn func(c *Config)) Config {
		c := base
		fn(&c)
		return c
	}

	for _, tc := range []struct {
		name     string
		override *PerPolicyConfigOverride
		expected Config
	}{
		{
			name:     "nil override inherits base",
			override: nil,
			expected: base,
		},
		{
			name:     "empty override inherits base",
			override: &PerPolicyConfigOverride{},
			expected: base,
		},
		{
			name:     "only time sharding overridden, rest inherited",
			override: &PerPolicyConfigOverride{TimeShardingEnabled: boolPtr(true)},
			expected: withBase(func(c *Config) { c.TimeShardingEnabled = true }),
		},
		{
			name:     "only desired rate overridden, rest inherited",
			override: &PerPolicyConfigOverride{DesiredRate: bytePtr(flagext.ByteSize(512 * 1024))},
			expected: withBase(func(c *Config) { c.DesiredRate = flagext.ByteSize(512 * 1024) }),
		},
		{
			name: "all fields overridden",
			override: &PerPolicyConfigOverride{
				Enabled:                  boolPtr(false),
				DesiredRate:              bytePtr(flagext.ByteSize(512 * 1024)),
				TimeShardingEnabled:      boolPtr(true),
				TimeShardingIgnoreRecent: durPtr(model.Duration(10 * time.Minute)),
				LoggingEnabled:           boolPtr(true),
			},
			expected: Config{
				Enabled:                  false,
				DesiredRate:              flagext.ByteSize(512 * 1024),
				TimeShardingEnabled:      true,
				TimeShardingIgnoreRecent: 10 * time.Minute,
				LoggingEnabled:           true,
			},
		},
		{
			name:     "zero-value fields replace, not inherit",
			override: &PerPolicyConfigOverride{Enabled: boolPtr(false), DesiredRate: bytePtr(0)},
			expected: withBase(func(c *Config) {
				c.Enabled = false
				c.DesiredRate = 0
			}),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.override.ApplyTo(base))
		})
	}
}
