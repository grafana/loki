package chunkbuilder

import (
	"errors"
	"flag"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfig_RegisterFlags(t *testing.T) {
	var cfg Config
	cfg.RegisterFlags(flag.NewFlagSet("test", flag.PanicOnError))

	require.False(t, cfg.Enabled)
	require.Empty(t, cfg.Topic)
	require.Equal(t, -1, cfg.PartitionID, "a partition ID from the hostname is the default")
	require.Equal(t, DefaultConsumerGroup, cfg.ConsumerGroup)
}

func TestConfig_Validate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(cfg *Config)
		err    string
	}{
		{
			name:   "valid with a topic and the defaults",
			modify: func(_ *Config) {},
		},
		{
			name:   "valid with an explicit partition ID",
			modify: func(cfg *Config) { cfg.PartitionID = 7 },
		},
		{
			name:   "valid with a partition ID from the hostname",
			modify: func(cfg *Config) { cfg.PartitionID = -1 },
		},
		{
			name:   "invalid without a topic",
			modify: func(cfg *Config) { cfg.Topic = "" },
			err:    "the topic is required",
		},
		{
			name:   "invalid without a consumer group",
			modify: func(cfg *Config) { cfg.ConsumerGroup = "" },
			err:    "the consumer group is required",
		},
		{
			name:   "invalid with a partition ID that does not fit in 32 bits",
			modify: func(cfg *Config) { cfg.PartitionID = math.MaxInt32 + 1 },
			err:    "the partition ID must fit in 32 bits",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg Config
			cfg.RegisterFlags(flag.NewFlagSet("test", flag.PanicOnError))
			cfg.Enabled = true
			cfg.Topic = "replay"
			tc.modify(&cfg)

			err := cfg.Validate()
			if tc.err == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tc.err)
		})
	}
}

func TestResolvePartitionID(t *testing.T) {
	errHostname := errors.New("no hostname")
	for _, tc := range []struct {
		name       string
		configured int
		hostname   string
		hostErr    error
		expected   int32
		err        string
	}{
		{
			name:       "uses the configured partition and ignores the hostname",
			configured: 5,
			hostErr:    errHostname,
			expected:   5,
		},
		{
			name:       "uses partition 0 when it is configured",
			configured: 0,
			hostname:   "chunk-builder-3",
			expected:   0,
		},
		{
			name:       "derives the partition from the ordinal suffix of the hostname",
			configured: -1,
			hostname:   "chunk-builder-3",
			expected:   3,
		},
		{
			name:       "derives the partition from a hostname with several dashes",
			configured: -1,
			hostname:   "chunk-builder-zone-a-12",
			expected:   12,
		},
		{
			name:       "fails when the hostname has no ordinal suffix",
			configured: -1,
			hostname:   "chunk-builder",
			err:        "failed to extract partition ID from hostname: chunk-builder does not contain a partition ID",
		},
		{
			name:       "fails when the hostname is unknown",
			configured: -1,
			hostErr:    errHostname,
			err:        "failed to get hostname: no hostname",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			partition, err := resolvePartitionID(tc.configured, func() (string, error) {
				return tc.hostname, tc.hostErr
			})
			if tc.err != "" {
				require.EqualError(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expected, partition)
		})
	}
}
