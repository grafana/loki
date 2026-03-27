package consumer

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
)

// validBaseConfig returns a Config with valid BuilderConfig defaults, useful for testing
// IngestMode/Topic combinations without triggering BuilderConfig validation failures.
func validBaseConfig() Config {
	var cfg Config
	cfg.RegisterFlagsWithPrefix("test.", flag.NewFlagSet("test", flag.PanicOnError))
	cfg.Topic = "test-topic"
	return cfg
}

func TestConfig_ChannelSize(t *testing.T) {
	t.Run("default is 10000", func(t *testing.T) {
		cfg := validBaseConfig()
		require.Equal(t, 10000, cfg.ChannelSize)
	})

	t.Run("flag parsed", func(t *testing.T) {
		var cfg Config
		fs := flag.NewFlagSet("test", flag.PanicOnError)
		cfg.RegisterFlagsWithPrefix("test.", fs)
		require.NoError(t, fs.Parse([]string{"-test.channel-size=500"}))
		require.Equal(t, 500, cfg.ChannelSize)
	})
}

func TestConfig_Validate_IngestMode(t *testing.T) {
	t.Run("kafka mode with topic set", func(t *testing.T) {
		cfg := validBaseConfig()
		cfg.IngestMode = IngestModeKafka
		require.NoError(t, cfg.Validate())
	})

	t.Run("kafka mode with empty topic returns error", func(t *testing.T) {
		cfg := validBaseConfig()
		cfg.IngestMode = IngestModeKafka
		cfg.Topic = ""
		require.ErrorContains(t, cfg.Validate(), "topic is required")
	})

	t.Run("empty ingest_mode defaults to kafka behavior", func(t *testing.T) {
		cfg := validBaseConfig()
		cfg.IngestMode = ""
		cfg.Topic = ""
		require.ErrorContains(t, cfg.Validate(), "topic is required")
	})

	t.Run("inmemory mode with empty topic is valid", func(t *testing.T) {
		cfg := validBaseConfig()
		cfg.IngestMode = IngestModeInMemory
		cfg.Topic = ""
		require.NoError(t, cfg.Validate())
	})

	t.Run("inmemory mode with topic set is also valid", func(t *testing.T) {
		cfg := validBaseConfig()
		cfg.IngestMode = IngestModeInMemory
		require.NoError(t, cfg.Validate())
	})

	t.Run("unknown ingest_mode returns error", func(t *testing.T) {
		cfg := validBaseConfig()
		cfg.IngestMode = "unknown"
		err := cfg.Validate()
		require.ErrorContains(t, err, "unknown ingest_mode")
		require.ErrorContains(t, err, "kafka")
		require.ErrorContains(t, err, "inmemory")
	})
}
