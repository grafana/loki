package builder

import (
	"flag"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/stretchr/testify/require"
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
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir:    "/tmp/test",
				Logline:       LoglineConfig{NgramLength: DefaultNgramLength, DocumentInterval: DefaultDocumentInterval, IndexVersion: "v3"},
				FlushOnIdle:   DefaultIdleFlushTimeout,
				FlushOnMaxAge: DefaultMaxBuilderAge,
			},
			wantError: false,
		},
		{
			name: "ring fields default when omitted",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
			},
			wantError: false,
		},
		{
			name: "missing consumer group applies default",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
			},
			wantError: false, // Validation now applies default consumer group
		},
		{
			name: "missing topic",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
			},
			wantError: true,
			errorMsg:  "the Kafka topic has not been configured",
		},
		{
			name: "missing scratch_dir",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
			},
			wantError: true,
			errorMsg:  "scratch_dir is required",
		},
		{
			name: "path traversal in scratch_dir",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/../etc/passwd",
			},
			wantError: true,
			errorMsg:  "scratch_dir contains path traversal",
		},
		{
			name: "applies defaults for zero values",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
				// Zero values for sizes and durations
			},
			wantError: false,
		},
		{
			name: "valid with custom bucket interval",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
				Logline: LoglineConfig{
					NgramLength:      DefaultNgramLength,
					DocumentInterval: 200 * time.Millisecond,
				},
			},
			wantError: false,
		},
		{
			name: "bucket interval too small",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
				Logline: LoglineConfig{
					DocumentInterval: 500 * time.Microsecond, // Too small
				},
			},
			wantError: true,
			errorMsg:  "document_interval must be at least",
		},
		{
			name: "bucket interval too large",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
				Logline: LoglineConfig{
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
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
				Logline: LoglineConfig{
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
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
				Logline: LoglineConfig{
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
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir:          "/tmp/test",
				PostingsBufferPairs: 1024,
			},
			wantError: true,
			errorMsg:  "postings_buffer_pairs must be at least",
		},
		{
			name: "postings_spill_watermark above 0.95 rejected",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir:             "/tmp/test",
				PostingsSpillWatermark: 0.96,
			},
			wantError: true,
			errorMsg:  "postings_spill_watermark must be > 0 and <= 0.95",
		},
		{
			name: "negative postings_spill_watermark rejected",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir:             "/tmp/test",
				PostingsSpillWatermark: -0.5,
			},
			wantError: true,
			errorMsg:  "postings_spill_watermark must be > 0 and <= 0.95",
		},
		{
			name: "ngram_length above radix-sort limit rejected",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
				Logline: LoglineConfig{
					NgramLength: 7,
				},
			},
			wantError: true,
			errorMsg:  "ngram_length must be between 1 and 6",
		},
		{
			name: "ngram_length negative rejected",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
				Logline: LoglineConfig{
					NgramLength: -1,
				},
			},
			wantError: true,
			errorMsg:  "ngram_length must be between 1 and 6",
		},
		{
			name: "bucket interval must divide 24h",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
				Logline: LoglineConfig{
					DocumentInterval: 7 * time.Millisecond, // In bounds, but 24h % 7ms != 0
				},
			},
			wantError: true,
			errorMsg:  "document_interval must evenly divide 24h",
		},
		{
			name: "applies default bucket interval",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
				// Zero bucket interval should apply default
			},
			wantError: false,
		},
		{
			name: "shard_count > 1 requires algorithm",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
				Logline:    LoglineConfig{ShardCount: 4, ShardAlgorithm: ""},
			},
			wantError: true,
			errorMsg:  "shard_algorithm must be set when shard_count > 1",
		},
		{
			name: "shard_count > 1 with unknown algorithm",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
				Logline:    LoglineConfig{ShardCount: 4, ShardAlgorithm: "unknown_algo"},
			},
			wantError: true,
			errorMsg:  "unknown shard algorithm",
		},
		{
			name: "shard_count negative is invalid",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
				Logline:    LoglineConfig{ShardCount: -1},
			},
			wantError: true,
			errorMsg:  "shard_count must be >= 0",
		},
		{
			name: "shard_count=0 with no algorithm is valid (unsharded)",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
				Logline:    LoglineConfig{ShardCount: 0, ShardAlgorithm: ""},
			},
			wantError: false,
		},
		{
			name: "shard_count=4 with first_byte algorithm is valid",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
				Logline:    LoglineConfig{ShardCount: 4, ShardAlgorithm: "first_byte"},
			},
			wantError: false,
		},
		{
			name: "shard_count=256 is the maximum and is valid",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
				Logline: LoglineConfig{
					ShardCount:     256,
					ShardAlgorithm: "murmur3_mix"},
			},
			wantError: false,
		},
		{
			name: "shard_count=257 exceeds the uint8 shard ceiling",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
				Logline: LoglineConfig{
					ShardCount:     257,
					ShardAlgorithm: "murmur3_mix"},
			},
			wantError: true,
			errorMsg:  "exceeds the builder maximum of 256",
		},
		{
			name: "shard_count=10 with murmur3_mix algorithm is valid",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir: "/tmp/test",
				Logline:    LoglineConfig{ShardCount: 10, ShardAlgorithm: "murmur3_mix"},
			},
			wantError: false,
		},
		{
			name: "extract_threads zero defaults to 1 (serial path)",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir:     "/tmp/test",
				ExtractThreads: 0,
			},
			wantError: false,
		},
		{
			name: "extract_threads at the maximum is valid",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir:     "/tmp/test",
				ExtractThreads: MaxExtractThreads,
			},
			wantError: false,
		},
		{
			name: "extract_threads above the maximum rejected",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
				ScratchDir:     "/tmp/test",
				ExtractThreads: 5,
			},
			wantError: true,
			errorMsg:  "extract_threads must be between 1 and 4",
		},
		{
			name: "extract_threads negative rejected",
			settings: Config{
				Kafka: kafka.Config{
					ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
					Topic:                      "test-topic",
					ConsumerGroup:              "test-group",
					ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
				},
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
				if tt.settings.Logline.NgramLength == 0 {
					require.Equal(t, DefaultNgramLength, tt.settings.Logline.NgramLength)
				}
				if tt.settings.Logline.DocumentInterval == 0 {
					require.Equal(t, DefaultDocumentInterval, tt.settings.Logline.DocumentInterval)
				}
			}
		})
	}
}

// TestConfig_ExtractThreads_ValidRangeAndDefault pins the extract_threads
// contract: omitted (0) defaults to the serial path, every value in 1..4 is
// accepted, and the default constant stays 1 — the catchup pipeline must never
// become the default by accident.
func TestConfig_ExtractThreads_ValidRangeAndDefault(t *testing.T) {
	base := func() Config {
		return Config{
			Kafka: kafka.Config{
				ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
				Topic:                      "test-topic",
				ConsumerGroup:              "test-group",
				ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
			},
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
