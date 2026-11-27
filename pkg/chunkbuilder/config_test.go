package chunkbuilder

import (
	"errors"
	"flag"
	"math"
	"strconv"
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

// tooLargePartitionID is a variable, so that converting it to int compiles on
// 32-bit platforms too.
var tooLargePartitionID int64 = math.MaxInt32 + 1

func TestConfig_Validate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(cfg *Config)
		err    string
		// only64 skips the case where int has 32 bits.
		only64 bool
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
			modify: func(cfg *Config) { cfg.PartitionID = int(tooLargePartitionID) },
			err:    "the partition ID must fit in 32 bits",
			only64: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.only64 && strconv.IntSize < 64 {
				t.Skip("an int cannot hold a partition ID beyond 32 bits")
			}
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
			name:       "derives the partition from a hostname that contains local",
			configured: -1,
			hostname:   "chunk-builder-locality-3",
			expected:   3,
		},
		{
			name:       "fails when the hostname has no ordinal suffix",
			configured: -1,
			hostname:   "chunk-builder",
			err:        "hostname chunk-builder has no ordinal suffix: set -chunk-builder.partition-id",
		},
		{
			name:       "fails for a local hostname without an ordinal suffix",
			configured: -1,
			hostname:   "laptop.local",
			err:        "hostname laptop.local has no ordinal suffix: set -chunk-builder.partition-id",
		},
		{
			name:       "fails when the ordinal suffix does not fit in 32 bits",
			configured: -1,
			hostname:   "chunk-builder-4294967296",
			err:        `the ordinal suffix of hostname chunk-builder-4294967296 is not a partition: strconv.ParseInt: parsing "4294967296": value out of range`,
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
