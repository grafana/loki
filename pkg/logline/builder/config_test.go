package builder

import (
	"flag"
	"strings"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/kafka"
)

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		settings  Config
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid settings",
			settings: Config{
				Kafka:         KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir:    "/tmp/test",
				Index:         IndexConfig{NgramLength: DefaultNgramLength, DocumentInterval: DefaultDocumentInterval, Version: "v3"},
				FlushOnIdle:   DefaultIdleFlushTimeout,
				FlushOnMaxAge: DefaultMaxBuilderAge,
			},
			wantError: false,
		},
		{
			name: "ring fields default when omitted",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
			},
			wantError: false,
		},
		{
			name: "missing consumer group applies default",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic"},
				ScratchDir: "/tmp/test",
			},
			wantError: false, // Validation now applies default consumer group
		},
		{
			name: "missing topic",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
			},
			wantError: true,
			errorMsg:  "the Kafka topic has not been configured",
		},
		{
			name: "missing scratch_dir",
			settings: Config{
				Kafka: KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
			},
			wantError: true,
			errorMsg:  "scratch_dir is required",
		},
		{
			name: "path traversal in scratch_dir",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/../etc/passwd",
			},
			wantError: true,
			errorMsg:  "scratch_dir contains path traversal",
		},
		{
			name: "applies defaults for zero values",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
				// Zero values for sizes and durations
			},
			wantError: false,
		},
		{
			name: "valid with custom bucket interval",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
				Index: IndexConfig{
					NgramLength:      DefaultNgramLength,
					DocumentInterval: 200 * time.Millisecond,
				},
			},
			wantError: false,
		},
		{
			name: "bucket interval too small",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
				Index: IndexConfig{
					DocumentInterval: 500 * time.Microsecond, // Too small
				},
			},
			wantError: true,
			errorMsg:  "document_interval must be at least",
		},
		{
			name: "bucket interval too large",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
				Index: IndexConfig{
					DocumentInterval: 2 * time.Hour, // Too large
				},
			},
			wantError: true,
			errorMsg:  "document_interval must be at most",
		},
		{
			// 10ms → window end = docIDEpoch + 2^32 × 10ms ≈ 2027-05-13,
			// which stopped offering a year of headroom in 2026-05 and only
			// loses ground from there (the epoch is fixed), so this case is
			// stable: rejected now and forever after.
			name: "document_interval with insufficient fixed-epoch headroom rejected",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
				Index: IndexConfig{
					DocumentInterval: 10 * time.Millisecond,
				},
			},
			wantError: true,
			errorMsg:  "less than the required",
		},
		{
			// 1ms → window end = docIDEpoch + 2^32 × 1ms ≈ 2026-02-19: the
			// window is effectively already exhausted, and live timestamps
			// past its end would panic at ingest.
			name: "document_interval with too-short docID window rejected",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
				Index: IndexConfig{
					DocumentInterval: 1 * time.Millisecond,
				},
			},
			wantError: true,
			errorMsg:  "less than the required",
		},
		{
			// Below 2^16 pairs the buffer degenerates into constant radix
			// passes and tiny-run spills.
			name: "postings_buffer_pairs below floor rejected",
			settings: Config{
				Kafka:               KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir:          "/tmp/test",
				PostingsBufferPairs: 1024,
			},
			wantError: true,
			errorMsg:  "postings_buffer_pairs must be at least",
		},
		{
			name: "postings_spill_watermark above 0.95 rejected",
			settings: Config{
				Kafka:                  KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir:             "/tmp/test",
				PostingsSpillWatermark: 0.96,
			},
			wantError: true,
			errorMsg:  "postings_spill_watermark must be > 0 and <= 0.95",
		},
		{
			name: "negative postings_spill_watermark rejected",
			settings: Config{
				Kafka:                  KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir:             "/tmp/test",
				PostingsSpillWatermark: -0.5,
			},
			wantError: true,
			errorMsg:  "postings_spill_watermark must be > 0 and <= 0.95",
		},
		{
			name: "ngram_length above radix-sort limit rejected",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
				Index: IndexConfig{
					NgramLength: 7,
				},
			},
			wantError: true,
			errorMsg:  "ngram_length must be between 1 and 6",
		},
		{
			name: "ngram_length negative rejected",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
				Index: IndexConfig{
					NgramLength: -1,
				},
			},
			wantError: true,
			errorMsg:  "ngram_length must be between 1 and 6",
		},
		{
			name: "bucket interval must divide 24h",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
				Index: IndexConfig{
					DocumentInterval: 7 * time.Millisecond, // In bounds, but 24h % 7ms != 0
				},
			},
			wantError: true,
			errorMsg:  "document_interval must evenly divide 24h",
		},
		{
			name: "applies default bucket interval",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
				// Zero bucket interval should apply default
			},
			wantError: false,
		},
		{
			name: "shard_count > 1 requires algorithm",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
				Index:      IndexConfig{ShardCount: 4, ShardAlgorithm: ""},
			},
			wantError: true,
			errorMsg:  "shard_algorithm must be set when shard_count > 1",
		},
		{
			name: "shard_count > 1 with unknown algorithm",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
				Index:      IndexConfig{ShardCount: 4, ShardAlgorithm: "unknown_algo"},
			},
			wantError: true,
			errorMsg:  "unknown shard algorithm",
		},
		{
			name: "shard_count negative is invalid",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
				Index:      IndexConfig{ShardCount: -1},
			},
			wantError: true,
			errorMsg:  "shard_count must be >= 0",
		},
		{
			name: "shard_count=0 with no algorithm is valid (unsharded)",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
				Index:      IndexConfig{ShardCount: 0, ShardAlgorithm: ""},
			},
			wantError: false,
		},
		{
			name: "shard_count=4 with first_byte algorithm is valid",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
				Index:      IndexConfig{ShardCount: 4, ShardAlgorithm: "first_byte"},
			},
			wantError: false,
		},
		{
			name: "shard_count=256 is the maximum and is valid",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
				Index: IndexConfig{
					ShardCount:     256,
					ShardAlgorithm: "murmur3_mix"},
			},
			wantError: false,
		},
		{
			name: "shard_count=257 exceeds the uint8 shard ceiling",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
				Index: IndexConfig{
					ShardCount:     257,
					ShardAlgorithm: "murmur3_mix"},
			},
			wantError: true,
			errorMsg:  "exceeds the builder maximum of 256",
		},
		{
			name: "shard_count=10 with murmur3_mix algorithm is valid",
			settings: Config{
				Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir: "/tmp/test",
				Index:      IndexConfig{ShardCount: 10, ShardAlgorithm: "murmur3_mix"},
			},
			wantError: false,
		},
		{
			name: "extract_threads zero defaults to 1 (serial path)",
			settings: Config{
				Kafka:          KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir:     "/tmp/test",
				ExtractThreads: 0,
			},
			wantError: false,
		},
		{
			name: "extract_threads at the maximum is valid",
			settings: Config{
				Kafka:          KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir:     "/tmp/test",
				ExtractThreads: MaxExtractThreads,
			},
			wantError: false,
		},
		{
			name: "extract_threads above the maximum rejected",
			settings: Config{
				Kafka:          KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir:     "/tmp/test",
				ExtractThreads: 5,
			},
			wantError: true,
			errorMsg:  "extract_threads must be between 1 and 4",
		},
		{
			name: "extract_threads negative rejected",
			settings: Config{
				Kafka:          KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
				ScratchDir:     "/tmp/test",
				ExtractThreads: -1,
			},
			wantError: true,
			errorMsg:  "extract_threads must be between 1 and 4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.settings.Validate()
			if tt.wantError {
				require.Error(t, err, "expected error but got nil")
				if tt.errorMsg != "" {
					require.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
				// Verify defaults were applied when zero values provided
				if tt.settings.Index.NgramLength == 0 {
					require.Equal(t, DefaultNgramLength, tt.settings.Index.NgramLength)
				}
				if tt.settings.Index.DocumentInterval == 0 {
					require.Equal(t, DefaultDocumentInterval, tt.settings.Index.DocumentInterval)
				}
				require.NotEmpty(t, tt.settings.Kafka.ConsumerGroupName)
				require.NotEmpty(t, tt.settings.Kafka.ClientID)
				require.NotZero(t, tt.settings.Kafka.SessionTimeout)
				require.NotEmpty(t, tt.settings.Kafka.InstanceID)
			}
		})
	}
}

// TestKafkaApplyDefaultsFrom pins which fields the builder inherits from Loki's
// root kafka_config and which it must not.
func TestKafkaApplyDefaultsFrom(t *testing.T) {
	root := kafka.Config{
		ReaderConfig: kafka.ClientConfig{
			Address:  "root-broker:9092",
			ClientID: "root-client",
		},
		Topic:         "ingest",
		DialTimeout:   7 * time.Second,
		SASLUsername:  "root-user",
		SASLPassword:  flagext.SecretWithValue("root-pass"),
		ConsumerGroup: "ingester-partition-zone-a",
	}

	t.Run("inherits everything except the consumer group", func(t *testing.T) {
		var cfg KafkaConfig
		cfg.ApplyDefaultsFrom(root)

		require.Equal(t, "root-broker:9092", cfg.Address)
		require.Equal(t, "root-client", cfg.ClientID)
		require.Equal(t, "ingest", cfg.Topic)
		require.Equal(t, 7*time.Second, cfg.DialTimeout)
		require.Equal(t, "root-user", cfg.SASLUsername)
		require.Equal(t, "root-pass", cfg.SASLPassword.String())

		require.Empty(t, cfg.ConsumerGroupName, "consumer group must not be inherited")
		require.NoError(t, cfg.Validate())
		require.Equal(t, DefaultConsumerGroupName, cfg.ConsumerGroupName)
	})

	t.Run("explicit fields win over the root config", func(t *testing.T) {
		cfg := KafkaConfig{
			Address:           "logline-broker:9092",
			Topic:             "logline-ingest",
			ClientID:          "logline-client",
			DialTimeout:       time.Second,
			ConsumerGroupName: "logline-builders",
		}
		cfg.ApplyDefaultsFrom(root)

		require.Equal(t, "logline-broker:9092", cfg.Address)
		require.Equal(t, "logline-ingest", cfg.Topic)
		require.Equal(t, "logline-client", cfg.ClientID)
		require.Equal(t, time.Second, cfg.DialTimeout)
		require.Equal(t, "logline-builders", cfg.ConsumerGroupName)
	})

	t.Run("deprecated bare address and client id are honoured", func(t *testing.T) {
		var cfg KafkaConfig
		cfg.ApplyDefaultsFrom(kafka.Config{
			Address:  "bare-broker:9092",
			ClientID: "bare-client",
			Topic:    "ingest",
		})

		require.Equal(t, "bare-broker:9092", cfg.Address)
		require.Equal(t, "bare-client", cfg.ClientID)
	})

	t.Run("sasl is inherited as a pair", func(t *testing.T) {
		// A username set here must not be paired with the root password: that
		// combination was never configured anywhere.
		cfg := KafkaConfig{Address: "b:9092", Topic: "t", SASLUsername: "logline-user"}
		cfg.ApplyDefaultsFrom(root)

		require.Equal(t, "logline-user", cfg.SASLUsername)
		require.Empty(t, cfg.SASLPassword.String())
		require.ErrorIs(t, cfg.Validate(), kafka.ErrInconsistentSASLUsernameAndPassword)
	})
}

func TestKafkaConfigFlags(t *testing.T) {
	var cfg Config
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg.RegisterFlags(fs)

	for _, name := range []string{
		"logline-index-builder.kafka.address",
		"logline-index-builder.kafka.topic",
		"logline-index-builder.kafka.client-id",
		"logline-index-builder.kafka.dial-timeout",
		"logline-index-builder.kafka.sasl-username",
		"logline-index-builder.kafka.sasl-password",
		"logline-index-builder.kafka.consumer-group-name",
		"logline-index-builder.kafka.session-timeout",
		"logline-index-builder.kafka.instance-id",
	} {
		require.NotNil(t, fs.Lookup(name), "missing flag %s", name)
	}

	// The builder must not register anything in the root -kafka.* namespace:
	// that config is shared with the ingesters.
	fs.VisitAll(func(f *flag.Flag) {
		require.False(t, strings.HasPrefix(f.Name, "kafka."), "registered root kafka flag %s", f.Name)
	})

	// Cluster-level flags default to zero so ApplyDefaultsFrom can tell unset
	// from deliberately set. The group-membership ones carry real defaults.
	require.Empty(t, cfg.Kafka.Address)
	require.Empty(t, cfg.Kafka.Topic)
	require.Zero(t, cfg.Kafka.DialTimeout)
	require.Equal(t, DefaultConsumerGroupName, cfg.Kafka.ConsumerGroupName)
	require.Equal(t, DefaultKafkaSessionTimeout, cfg.Kafka.SessionTimeout)
}

// TestConfig_ExtractThreads_ValidRangeAndDefault pins the extract_threads
// contract: omitted (0) defaults to the serial path, every value in 1..4 is
// accepted, and the default constant stays 1 — the catchup pipeline must never
// become the default by accident.
func TestConfig_ExtractThreads_ValidRangeAndDefault(t *testing.T) {
	base := func() Config {
		return Config{
			Kafka:      KafkaConfig{Address: "localhost:9092", Topic: "test-topic", ConsumerGroupName: "test-group"},
			ScratchDir: "/tmp/test",
		}
	}

	require.Equal(t, 1, DefaultExtractThreads, "the parallel pipeline must never be the default")

	cfg := base()
	require.NoError(t, cfg.Validate())
	require.Equal(t, 1, cfg.ExtractThreads, "omitted extract_threads must default to the serial path")

	for w := 1; w <= MaxExtractThreads; w++ {
		cfg := base()
		cfg.ExtractThreads = w
		require.NoError(t, cfg.Validate(), "extract_threads=%d must be valid", w)
		require.Equal(t, w, cfg.ExtractThreads)
	}
}

// TestConfig_RegisterFlags_AppliesRingWaitDefault ensures the partition-ring
// startup wait default is set by RegisterFlags.
func TestConfig_RegisterFlags_AppliesRingWaitDefault(t *testing.T) {
	var cfg Config
	fs := flag.NewFlagSet("", flag.PanicOnError)
	cfg.RegisterFlags(fs)

	require.Equal(t, 60*time.Second, cfg.WaitRingPopulatedTimeout,
		"RegisterFlags must default wait_ring_populated_timeout to 60s; "+
			"otherwise YAML-only config loads fail Validate() with "+
			"\"wait_ring_populated_timeout must be > 0\"")
}
