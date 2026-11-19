package logline

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/grafana/dskit/flagext"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

const (
	DefaultMaxJobSize          = 2 << 30 // 2 GiB
	DefaultJobTimeout          = 2 * time.Hour
	DefaultDownloadConcurrency = 4
)

// Postings encodings a compactor can write merged indexes with.
const (
	// PostingsEncodingDeltaVarInt writes delta+varint blocks compressed with
	// zstd. It is the default.
	PostingsEncodingDeltaVarInt = "delta-varint"
	// PostingsEncodingEliasFano writes Elias-Fano blocks uncompressed, each
	// followed by a checksum. Only readers that support it can open the
	// merged indexes.
	PostingsEncodingEliasFano = "elias-fano"
)

// CompactionConfig is the logline.compaction section. Compaction merges
// indexes of the same date and shard into fewer, larger ones. The section
// covers how merge jobs are planned, how they are handed out, and how a merge
// runs.
type CompactionConfig struct {
	// MaxJobSize is the largest total size of the indexes merged by one job.
	// An index larger than this is never compacted.
	MaxJobSize flagext.Bytes `yaml:"max_job_size"`
	// JobTimeout is how long a job may run before it is reassigned.
	JobTimeout time.Duration `yaml:"job_timeout"`
	// JobStateDir holds the in-flight job state. It must be durable: losing it
	// lets the same indexes be merged twice by different jobs.
	JobStateDir string `yaml:"job_state_dir"`

	// SchedulerAddress is the base URL jobs are requested from.
	SchedulerAddress string `yaml:"scheduler_address"`
	// CompactorID identifies the process running a job. Defaults to the hostname.
	CompactorID string `yaml:"compactor_id"`
	// DownloadConcurrency is how many source indexes one job opens in parallel.
	DownloadConcurrency int `yaml:"download_concurrency"`
	// PostingsEncoding is how merged indexes encode postings. Sources are read
	// in whatever encoding they were written with.
	PostingsEncoding string `yaml:"postings_encoding"`
}

func (c *CompactionConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	c.MaxJobSize = DefaultMaxJobSize
	f.Var(&c.MaxJobSize, prefix+".max-job-size",
		"Maximum total size of the indexes merged by one compaction job. Indexes larger than this are never compacted.")
	f.DurationVar(&c.JobTimeout, prefix+".job-timeout", DefaultJobTimeout,
		"How long a compaction job may run before it is considered failed and reassigned.")
	f.StringVar(&c.JobStateDir, prefix+".job-state-dir", "",
		"Directory holding the state of in-flight compaction jobs. Required by the logline-scheduler target. It must be on durable storage, otherwise a restart can merge the same indexes twice.")
	f.StringVar(&c.SchedulerAddress, prefix+".scheduler-address", "",
		"Base URL compaction jobs are requested from, for example http://logline-scheduler:3100. Required by the logline-compactor target.")
	f.StringVar(&c.CompactorID, prefix+".compactor-id", "",
		"Identifier of this compactor. Defaults to the hostname.")
	f.IntVar(&c.DownloadConcurrency, prefix+".download-concurrency", DefaultDownloadConcurrency,
		"Number of source indexes a compaction job opens in parallel.")
	f.StringVar(&c.PostingsEncoding, prefix+".postings-encoding", PostingsEncodingDeltaVarInt,
		"Postings encoding of merged indexes, delta-varint or elias-fano. Every component that reads indexes must support elias-fano before it is enabled. Sources are read in whatever encoding they were written with, so the setting can be changed at any time.")
}

// ValidateScheduler checks the settings used to plan and hand out jobs. It
// applies defaults for zero values, so it must run before the config is used.
func (c *CompactionConfig) ValidateScheduler() error {
	if c.JobTimeout == 0 {
		c.JobTimeout = DefaultJobTimeout
	}

	if c.MaxJobSize == 0 {
		return errors.New("max_job_size must be greater than 0")
	}
	// The planner sums index sizes as int64.
	if uint64(c.MaxJobSize) > math.MaxInt64 {
		return fmt.Errorf("max_job_size is too large, got %d", c.MaxJobSize)
	}
	if c.JobTimeout < 0 {
		return fmt.Errorf("job_timeout must be positive, got %v", c.JobTimeout)
	}
	if c.JobStateDir == "" {
		return errors.New("job_state_dir is required")
	}
	return nil
}

// ValidateCompactor checks the settings used to run jobs. It applies defaults
// for zero values, so it must run before the config is used.
func (c *CompactionConfig) ValidateCompactor() error {
	if c.SchedulerAddress == "" {
		return errors.New("scheduler_address is required")
	}
	if c.DownloadConcurrency == 0 {
		c.DownloadConcurrency = DefaultDownloadConcurrency
	}
	if c.DownloadConcurrency < 0 {
		return fmt.Errorf("download_concurrency must be positive, got %d", c.DownloadConcurrency)
	}
	if c.PostingsEncoding == "" {
		c.PostingsEncoding = PostingsEncodingDeltaVarInt
	}
	if _, err := c.WriterPostingsEncoding(); err != nil {
		return err
	}
	if c.CompactorID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("compactor_id not set and failed to get hostname: %w", err)
		}
		c.CompactorID = hostname
	}
	return nil
}

// WriterPostingsEncoding returns the format encoding merged indexes are
// written with.
func (c *CompactionConfig) WriterPostingsEncoding() (format.PostingsEncoding, error) {
	switch c.PostingsEncoding {
	case "", PostingsEncodingDeltaVarInt:
		return format.PostingsEncodingFastDeltaVarIntBlocked, nil
	case PostingsEncodingEliasFano:
		return format.PostingsEncodingFastEliasFanoBlocked, nil
	default:
		return 0, fmt.Errorf("postings_encoding must be %q or %q, got %q",
			PostingsEncodingDeltaVarInt, PostingsEncodingEliasFano, c.PostingsEncoding)
	}
}
