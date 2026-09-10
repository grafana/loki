package builder

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/grafana/loki/v3/pkg/kafka"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/shard"
)

const (
	DefaultFlushOnMaxBytes  = 20 * 1024 * 1024 * 1024
	DefaultNgramLength      = 6
	DefaultDocumentInterval = 100 * time.Millisecond

	DefaultIdleFlushTimeout   = 5 * time.Minute
	DefaultMaxBuilderAge      = 30 * time.Minute
	DefaultFlushCheckInterval = 5 * time.Minute

	// DefaultPostingsBufferPairs is the postings buffer capacity in
	// (ngram, docID) pairs. Resident memory is ~24 B per pair (SoA buffer +
	// radix scratch, allocated up front), so the default costs ~460 MiB.
	DefaultPostingsBufferPairs = 20_000_000
	// DefaultPostingsSpillWatermark is the deduped-head fraction of the
	// postings buffer that triggers spilling a sorted run to scratch disk.
	DefaultPostingsSpillWatermark = 0.70

	// minPostingsBufferPairs floors postings_buffer_pairs. Below this the
	// buffer degenerates into constant radix passes and per-batch spills of
	// tiny runs — the merge fan-in explodes and ingest throughput collapses.
	minPostingsBufferPairs = 1 << 16

	// DefaultExtractThreads is the serial (non-pipelined) ingest path. The
	// parallel extract pipeline is an incident/catchup mode, never the default.
	DefaultExtractThreads = 1
	// MaxExtractThreads caps the extract pipeline fan-out. Each extract
	// goroutine owns a full postings buffer (~480 MiB resident at the default
	// postings_buffer_pairs), so the memory floor scales linearly with this
	// value; 4 is already ~1.9 GiB of sort buffers plus the flush-merge
	// ceiling downstream.
	MaxExtractThreads = 4

	// DefaultKafkaSessionTimeout is the consumer-group session timeout used
	// when KafkaSessionTimeout is unset. It must be long enough that a
	// normal pod restart completes before the broker considers the member
	// dead (and triggers a rebalance), but short enough that a genuinely
	// dead pod is evicted promptly so its partitions get reassigned.
	DefaultKafkaSessionTimeout = 2 * time.Minute

	// MinDocumentInterval is 1ms (prevents excessive document-bucket count)
	MinDocumentInterval = 1 * time.Millisecond

	// MaxDocumentInterval is 1 hour (ensures reasonable time resolution)
	MaxDocumentInterval = 1 * time.Hour
)

// Config holds configuration for the logline index builder service.
type Config struct {
	// Kafka is Loki's root kafka_config, injected by the module wiring rather
	// than configured here, so there is only one kafka section in the config
	// file and only one set of -kafka.* flags.
	Kafka   kafka.Config  `yaml:"-"`
	Logline LoglineConfig `yaml:"logline"`

	FlushOnIdle   time.Duration `yaml:"flush_on_idle"`
	FlushOnMaxAge time.Duration `yaml:"flush_on_max_age"`
	// FlushOnMaxBytes bounds the run-file bytes spilled by the ACTIVE builder;
	// it is the dominant flush trigger under sustained throughput. It is not
	// the peak scratch footprint: during a flush, the swapped-out builder's
	// runs (up to this threshold) coexist with its merged .lidx output and the
	// fresh builder's new runs, so the scratch volume needs ~2-3x this value
	// free or a full volume becomes a zero-progress crash loop.
	FlushOnMaxBytes    uint64        `yaml:"flush_on_max_bytes"`
	FlushCheckInterval time.Duration `yaml:"flush_check_interval"`

	// PostingsBufferPairs is the capacity of the in-memory postings buffer in
	// (ngram, docID) pairs. Resident memory scales linearly — each pair costs
	// 24 B across the SoA buffer and its radix scratch, all allocated up front
	// (~460 MiB at the default), and a builder swap briefly holds two buffers.
	// There is deliberately no upper bound beyond that memory math: sizing is
	// a GOMEMLIMIT budget decision.
	PostingsBufferPairs int `yaml:"postings_buffer_pairs"`
	// PostingsSpillWatermark is the deduped-head fraction of the postings
	// buffer that triggers spilling a sorted run to scratch disk. Higher packs
	// runs denser (fewer, larger runs feed the merge); above 0.95 the tail has
	// too little room left to make integration progress.
	PostingsSpillWatermark float64 `yaml:"postings_spill_watermark"`

	// ExtractThreads is the number of parallel n-gram extract goroutines
	// (1-4). 1 (the default) is the serial production ingest path — no queue,
	// no goroutines. Values 2-4 enable the catchup pipeline: decoded streams
	// are fed through a bounded queue to N competing extract goroutines, each
	// owning its own postings buffer. Incident/catchup mode only: the resident
	// memory floor and CPU demand scale ~linearly with N. See
	// pkg/logline/builder/AGENTS.md.
	ExtractThreads int `yaml:"extract_threads"`

	// InstanceID is the Kafka static-membership identifier (kgo.InstanceID).
	// Set to the pod name so restarts inside the consumer-group session timeout
	// rejoin without triggering a rebalance. Defaults to os.Hostname() if empty.
	InstanceID string `yaml:"instance_id"`
	ScratchDir string `yaml:"scratch_dir"`

	// KafkaSessionTimeout is the consumer-group session timeout.
	// Combined with kgo.InstanceID it controls how long
	// a pod can be absent (restart, brief network blip) before the broker
	// evicts it from the group and triggers a rebalance. Defaults to
	// DefaultKafkaSessionTimeout. The broker's group.max.session.timeout.ms
	// must be at least this value or JoinGroup is rejected.
	KafkaSessionTimeout time.Duration `yaml:"kafka_session_timeout"`

	// WaitRingPopulatedTimeout bounds how long the builder will wait at
	// startup for the partition ring to be populated (PartitionsCount > 0)
	// before failing service startup. This is intentionally a hard failure:
	// if it's misconfigured (wrong KV key, wrong cluster label, ring not yet
	// provisioned), the pod must crashloop.
	WaitRingPopulatedTimeout time.Duration `yaml:"wait_ring_populated_timeout"`

	// disableStaticMembership skips kgo.InstanceID when creating the Kafka
	// client. It exists solely because kfake (the in-memory Kafka used by
	// unit tests) rejects every JoinGroup that carries an InstanceID with
	// INVALID_GROUP_ID. Tests set this to true; production code must not.
	disableStaticMembership bool `yaml:"-"`
}

type LoglineConfig struct {
	NgramLength      int           `yaml:"ngram_length"`
	DocumentInterval time.Duration `yaml:"document_interval"`
	ShardCount       int           `yaml:"shard_count"`
	ShardAlgorithm   string        `yaml:"shard_algorithm"`
	// IndexVersion is the format version written to meta.json.
	// Defaults to logline.CurrentVersion ("v3").
	IndexVersion string `yaml:"index_version"`
	// DensityThreshold filters n-grams covering more than this fraction of a
	// full day's documents (24h / DocumentInterval). 0 = use the format
	// version's built-in default (v3 default: 0.20).
	DensityThreshold float64 `yaml:"density_threshold"`
}

// RegisterFlags registers configuration flags for BuilderSettings.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	if f == nil {
		f = flag.CommandLine
	}

	// Builder-specific flags
	f.IntVar(&c.Logline.NgramLength, "logline-index-builder.ngram-length", DefaultNgramLength,
		"N-gram length for feature extraction")
	f.DurationVar(&c.Logline.DocumentInterval, "logline-index-builder.document-interval", DefaultDocumentInterval,
		"Time range each document covers (e.g., 100ms, 1s)")
	f.IntVar(&c.Logline.ShardCount, "logline-index-builder.shard-count", 0,
		"Number of ngram shards per date bucket. 0 or 1 disables sharding (single file per date).")
	f.StringVar(&c.Logline.ShardAlgorithm, "logline-index-builder.shard-algorithm", "murmur3_mix",
		"Shard algorithm for ngram routing. Valid values: first_byte, murmur3_mix.")
	f.StringVar(&c.Logline.IndexVersion, "logline-index-builder.index-version", "",
		"Index format version string written to meta.json (defaults to current version)")
	f.Float64Var(&c.Logline.DensityThreshold, "logline-index-builder.density-threshold", 0,
		"Filter n-grams covering more than this fraction of a full day's documents (0 = use format default; v3 default is 0.20)")
	f.DurationVar(&c.FlushOnIdle, "logline-index-builder.flush-on-idle", DefaultIdleFlushTimeout,
		"Duration of inactivity before flushing")
	f.DurationVar(&c.FlushOnMaxAge, "logline-index-builder.flush-on-max-age", DefaultMaxBuilderAge,
		"Maximum age of the builder before flushing")
	f.DurationVar(&c.FlushCheckInterval, "logline-index-builder.flush-check-interval", DefaultFlushCheckInterval,
		"Interval for periodic flush checks independent of the poll loop")
	f.IntVar(&c.PostingsBufferPairs, "logline-index-builder.postings-buffer-pairs", DefaultPostingsBufferPairs,
		"Capacity of the in-memory postings buffer in (ngram, docID) pairs. Resident memory is ~24 bytes per pair, allocated up front (~460 MiB at the default), and a builder swap briefly holds two buffers — budget GOMEMLIMIT accordingly. Minimum 65536.")
	f.Float64Var(&c.PostingsSpillWatermark, "logline-index-builder.postings-spill-watermark", DefaultPostingsSpillWatermark,
		"Fraction of the postings buffer the sorted, deduped head must reach before a run is spilled to scratch disk. Higher packs runs denser (fewer, larger runs feed the merge). Must be > 0 and <= 0.95.")

	f.IntVar(&c.ExtractThreads, "logline-index-builder.extract-threads", DefaultExtractThreads,
		"Number of parallel n-gram extract goroutines (1-4). Incident/catchup mode only: values above 1 multiply the resident sort-buffer floor "+
			"(~480 MiB per goroutine at the default postings_buffer_pairs) and CPU demand for higher ingest throughput. Default 1 is the serial production path.")
	f.StringVar(&c.InstanceID, "logline-index-builder.instance-id", "",
		"Kafka static-membership ID (defaults to os.Hostname() if empty). "+
			"Pod restarts within session_timeout rejoin without rebalance.")
	f.DurationVar(&c.KafkaSessionTimeout, "logline-index-builder.kafka-session-timeout", DefaultKafkaSessionTimeout,
		"Kafka consumer-group session timeout. A pod absent for longer than this is evicted from the group and its partitions rebalanced. "+
			"Must not exceed broker's group.max.session.timeout.ms.")
	f.StringVar(&c.ScratchDir, "logline-index-builder.scratch-dir", "./data/partial-indexes",
		"Directory where intermediate .lidx files are written")
	f.Uint64Var(&c.FlushOnMaxBytes, "logline-index-builder.flush-on-max-bytes", DefaultFlushOnMaxBytes,
		"Full-flush trigger based on cumulative bytes of run files spilled to scratch disk by the active builder. "+
			"Peak scratch usage reaches 2-3x this value during a flush (retiring builder's runs + its merged .lidx output + the fresh builder's runs), "+
			"so size the scratch volume with that headroom.")

	f.DurationVar(&c.WaitRingPopulatedTimeout, "logline-index-builder.wait-ring-populated-timeout", 60*time.Second,
		"Maximum time to wait at startup for the partition ring to be populated. "+
			"Service startup fails if the ring is still empty after this — there is no silent fallback.")
}

// Validate validates the configuration and applies defaults.
func (c *Config) Validate() error {
	// Kafka is Loki's root kafka_config, injected by the module wiring and
	// validated there as a whole. Only the fields this builder consumes are
	// checked here.
	if c.Kafka.ReaderConfig.Address == "" && c.Kafka.Address == "" {
		return fmt.Errorf("invalid kafka config: %w", kafka.ErrMissingKafkaAddress)
	}
	if c.Kafka.Topic == "" {
		return fmt.Errorf("invalid kafka config: %w", kafka.ErrMissingKafkaTopic)
	}
	if (c.Kafka.SASLUsername == "") != (c.Kafka.SASLPassword.String() == "") {
		return fmt.Errorf("invalid kafka config: %w", kafka.ErrInconsistentSASLUsernameAndPassword)
	}

	if c.Kafka.ConsumerGroup == "" {
		c.Kafka.ConsumerGroup = "logline-index-builder"
	}

	if c.ScratchDir == "" {
		return fmt.Errorf("scratch_dir is required")
	}

	if strings.Contains(c.ScratchDir, "..") {
		return fmt.Errorf("scratch_dir contains path traversal: %s", c.ScratchDir)
	}

	if c.FlushOnIdle < 0 {
		return fmt.Errorf("idle_flush_timeout must be non-negative, got %v", c.FlushOnIdle)
	}

	if c.FlushOnMaxAge < 0 {
		return fmt.Errorf("max_builder_age must be non-negative, got %v", c.FlushOnMaxAge)
	}

	if c.FlushOnMaxBytes == 0 {
		c.FlushOnMaxBytes = DefaultFlushOnMaxBytes
	}

	if c.PostingsBufferPairs == 0 {
		c.PostingsBufferPairs = DefaultPostingsBufferPairs
	}
	if c.PostingsBufferPairs < minPostingsBufferPairs {
		return fmt.Errorf("postings_buffer_pairs must be at least %d, got %d", minPostingsBufferPairs, c.PostingsBufferPairs)
	}

	if c.PostingsSpillWatermark == 0 {
		c.PostingsSpillWatermark = DefaultPostingsSpillWatermark
	}
	if c.PostingsSpillWatermark < 0 || c.PostingsSpillWatermark > 0.95 {
		return fmt.Errorf("postings_spill_watermark must be > 0 and <= 0.95, got %v", c.PostingsSpillWatermark)
	}

	if c.ExtractThreads == 0 {
		c.ExtractThreads = DefaultExtractThreads
	}
	if c.ExtractThreads < 1 || c.ExtractThreads > MaxExtractThreads {
		return fmt.Errorf("extract_threads must be between 1 and %d (each extract goroutine holds a full postings buffer, ~480 MiB at the default postings_buffer_pairs), got %d",
			MaxExtractThreads, c.ExtractThreads)
	}

	if c.Logline.NgramLength == 0 {
		c.Logline.NgramLength = DefaultNgramLength
	}

	// radixSortByNgram orders only the first 6 key bytes and assumes bytes 6-7
	// are zero. A 7- or 8-byte ngram would mis-sort runs, and the flush merge
	// would then wedge the retry loop at the writer's ascending-term check;
	// anything longer extracts zero ngrams and silently commits empty cycles.
	if c.Logline.NgramLength < 1 || c.Logline.NgramLength > 6 {
		return fmt.Errorf("ngram_length must be between 1 and 6 (the radix sort orders only the first 6 ngram bytes), got %d", c.Logline.NgramLength)
	}

	if c.Logline.DocumentInterval == 0 {
		c.Logline.DocumentInterval = DefaultDocumentInterval
	}

	if c.Logline.IndexVersion == "" {
		c.Logline.IndexVersion = logline.CurrentVersion
	}

	// Default density threshold: 0.20 (terms in >20% of a day's docs become sentinels).
	if c.Logline.DensityThreshold == 0 {
		c.Logline.DensityThreshold = 0.20
	}

	// The index-defining settings (version, interval, sharding) are validated
	// in one place only, after the defaults above have been applied.
	if err := validateLoglineIndexSettings(c.Logline); err != nil {
		return err
	}

	if c.FlushOnIdle == 0 {
		c.FlushOnIdle = DefaultIdleFlushTimeout
	}

	if c.FlushOnMaxAge == 0 {
		c.FlushOnMaxAge = DefaultMaxBuilderAge
	}

	if c.FlushCheckInterval == 0 {
		c.FlushCheckInterval = DefaultFlushCheckInterval
	}

	if c.KafkaSessionTimeout == 0 {
		c.KafkaSessionTimeout = DefaultKafkaSessionTimeout
	}
	if c.KafkaSessionTimeout < 0 {
		return fmt.Errorf("kafka_session_timeout must be non-negative, got %v", c.KafkaSessionTimeout)
	}

	// InstanceID defaults to os.Hostname() so each pod gets a stable static-
	// membership identity for kgo.InstanceID without explicit configuration.
	if c.InstanceID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("failed to get hostname for instance ID: %w", err)
		}
		c.InstanceID = hostname
	}

	if c.WaitRingPopulatedTimeout == 0 {
		c.WaitRingPopulatedTimeout = 60 * time.Second
	}
	if c.WaitRingPopulatedTimeout < 0 {
		return fmt.Errorf("wait_ring_populated_timeout must be > 0, got %v", c.WaitRingPopulatedTimeout)
	}
	return nil
}

// validateLoglineIndexSettings checks the index-defining settings (version,
// interval, sharding) as a unit. It is the single owner of these checks;
// Config.Validate applies defaults and delegates here.
func validateLoglineIndexSettings(settings LoglineConfig) error {
	if settings.IndexVersion == "" {
		return fmt.Errorf("index_version is required")
	}
	if err := logline.ValidateVersion(settings.IndexVersion); err != nil {
		return fmt.Errorf("invalid index_version: %w", err)
	}
	if settings.DocumentInterval < MinDocumentInterval {
		return fmt.Errorf("document_interval must be at least %v, got %v", MinDocumentInterval, settings.DocumentInterval)
	}
	if settings.DocumentInterval > MaxDocumentInterval {
		return fmt.Errorf("document_interval must be at most %v, got %v", MaxDocumentInterval, settings.DocumentInterval)
	}
	// The flat-buffer builder derives each index's date from its epoch-tick
	// docID as day = absBucket / (24h / interval). That is only consistent with
	// the calendar date when the interval evenly divides 24h; otherwise ticks
	// drift across day boundaries and files land under the wrong date.
	if (24*time.Hour)%settings.DocumentInterval != 0 {
		return fmt.Errorf("document_interval must evenly divide 24h, got %s", settings.DocumentInterval)
	}
	// The docID is an epoch tick: a uint32 count of document buckets from the
	// FIXED docIDEpoch (2026-01-01), so the interval fixes the END of the
	// representable window: epoch + 2^32 ticks. Timestamps outside the window
	// PANIC at ingest, so the window end must stay comfortably ahead of the
	// present: require epoch + 2^32 × interval ≥ now + minDocIDFutureRunway
	// (1 year). This rule is deliberately time-DEPENDENT — with a fixed epoch
	// the window consumes its headroom as calendar time passes, and the config
	// must be rejected at startup well before live traffic starts panicking.
	// At the default 100ms interval the window ends 2039-08-12, so the rule
	// holds until ~2038-08.
	if windowEnd, minEnd := docIDWindowEnd(settings.DocumentInterval), time.Now().Add(minDocIDFutureRunway); windowEnd.Before(minEnd) {
		return fmt.Errorf("document_interval %v yields a docID window ending %s (fixed epoch %s + 2^32 ticks), less than the required %v from now; use a larger interval",
			settings.DocumentInterval, windowEnd.UTC().Format(time.RFC3339), docIDEpoch.Format(time.RFC3339), minDocIDFutureRunway)
	}
	if settings.ShardCount < 0 {
		return fmt.Errorf("shard_count must be >= 0, got %d", settings.ShardCount)
	}
	// Shard values are carried as uint8 through the spill reorder
	// (postingsBuffer.shardScratch), so 256 shards is a hard ceiling.
	if settings.ShardCount > 256 {
		return fmt.Errorf("shard count %d exceeds the builder maximum of 256", settings.ShardCount)
	}
	if settings.ShardCount > 1 {
		if settings.ShardAlgorithm == "" {
			return fmt.Errorf("shard_algorithm must be set when shard_count > 1")
		}
		if _, err := shard.New(settings.ShardAlgorithm); err != nil {
			return fmt.Errorf("invalid shard config: %w", err)
		}
	}
	return nil
}
