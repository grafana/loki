package store

import (
	"flag"
	"fmt"
	"time"
)

const (
	DefaultBucketPrefix          = "logline/"
	DefaultPollInterval          = 15 * time.Minute
	DefaultPollConcurrency       = 50
	DefaultRetentionDuration     = 7 * 24 * time.Hour // 7 days
	DefaultCompactionGracePeriod = 24 * time.Hour
	DefaultQueryIngestersWithin  = 1 * time.Hour
)

// Config holds configuration for the index Store.
type Config struct {
	BucketPrefix          string        `yaml:"bucket_prefix"`
	PollInterval          time.Duration `yaml:"poll_interval"`
	PollConcurrency       int           `yaml:"poll_concurrency"`
	RetentionDuration     time.Duration `yaml:"retention_duration"`
	CompactionGracePeriod time.Duration `yaml:"compaction_grace_period"`
	// MinDate is the first date partition (YYYY-MM-DD) considered trustworthy
	// for query-path reads. Required when logline is enabled.
	MinDate              string        `yaml:"min_date"`
	QueryIngestersWithin time.Duration `yaml:"-"`
}

// RegisterFlags registers configuration flags with the given FlagSet.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	if f == nil {
		f = flag.CommandLine
	}
	f.StringVar(&c.BucketPrefix, "logline-store.bucket-prefix", DefaultBucketPrefix,
		"Path prefix for all objects in the bucket")
	f.DurationVar(&c.PollInterval, "logline-store.poll-interval", DefaultPollInterval,
		"How often to poll object storage for new indexes")
	f.IntVar(&c.PollConcurrency, "logline-store.poll-concurrency", DefaultPollConcurrency,
		"Number of concurrent meta.json fetches during poll")
	f.DurationVar(&c.RetentionDuration, "logline-store.retention-duration", DefaultRetentionDuration,
		"Age after which index files are eligible for deletion")
	f.DurationVar(&c.CompactionGracePeriod, "logline-store.compaction-grace-period", DefaultCompactionGracePeriod,
		"How long to retain compacted (source) indexes after compaction before deletion")
	f.StringVar(&c.MinDate, "logline-store.min-date", "",
		"Earliest trusted index date partition (YYYY-MM-DD) for query-path reads; required when logline is enabled")
}

// Validate checks constraints and applies defaults for zero-valued fields.
func (c *Config) Validate() error {
	if c.PollInterval < 0 {
		return fmt.Errorf("poll_interval must be non-negative, got %v", c.PollInterval)
	}
	if c.PollConcurrency < 0 {
		return fmt.Errorf("poll_concurrency must be non-negative, got %d", c.PollConcurrency)
	}
	if c.RetentionDuration < 0 {
		return fmt.Errorf("retention_duration must be non-negative, got %v", c.RetentionDuration)
	}
	if c.CompactionGracePeriod < 0 {
		return fmt.Errorf("compaction_grace_period must be non-negative, got %v", c.CompactionGracePeriod)
	}
	if c.MinDate == "" {
		return fmt.Errorf("min_date is required (YYYY-MM-DD)")
	}
	if parsed, err := time.Parse("2006-01-02", c.MinDate); err != nil || parsed.Format("2006-01-02") != c.MinDate {
		return fmt.Errorf("min_date must be YYYY-MM-DD, got %q", c.MinDate)
	}
	if c.BucketPrefix == "" {
		c.BucketPrefix = DefaultBucketPrefix
	}
	if c.PollInterval == 0 {
		c.PollInterval = DefaultPollInterval
	}
	if c.PollConcurrency == 0 {
		c.PollConcurrency = DefaultPollConcurrency
	}
	if c.RetentionDuration == 0 {
		c.RetentionDuration = DefaultRetentionDuration
	}
	if c.CompactionGracePeriod == 0 {
		c.CompactionGracePeriod = DefaultCompactionGracePeriod
	}
	return nil
}
