package streamsharding

import (
	"flag"
	"time"
)

// Config controls the ingester's own time-bucketing of out-of-order/backfilled
// entries within a single stream. This is a separate mechanism from the
// distributor's shardstreams.Config.TimeShardingEnabled, which shards a stream
// into multiple distinct streams via a synthetic __time_shard__ label. This
// config instead lets one logical stream keep several concurrently open,
// time-bucketed chunks in the ingester, so old and new entries for the same
// stream always land on the same ingester(s) and are never counted as
// separate streams.
type Config struct {
	Enabled bool `yaml:"enabled" json:"enabled" doc:"description=Allow the ingester to accept out-of-order/backfilled logs for a stream by keeping multiple time-bucketed chunks open concurrently, instead of rejecting entries older than half of max_chunk_age relative to the stream's most recent entry."`

	IgnoreRecent time.Duration `yaml:"ignore_recent" json:"ignore_recent" doc:"description=Entries with timestamps newer than this value are never time-bucketed; they always go to the stream's current (live) chunk."`

	MaxOpenBuckets int `yaml:"max_open_buckets" json:"max_open_buckets" doc:"description=Maximum number of concurrently open time-buckets per stream. Entries that would open a new bucket beyond this limit are rejected."`
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, fs *flag.FlagSet) {
	fs.BoolVar(&cfg.Enabled, prefix+".enabled", false, "Allow the ingester to accept out-of-order/backfilled logs for a stream by keeping multiple time-bucketed chunks open concurrently.")
	fs.DurationVar(&cfg.IgnoreRecent, prefix+".ignore-recent", 40*time.Minute, "Logs with timestamps newer than this value are never time-bucketed.")
	fs.IntVar(&cfg.MaxOpenBuckets, prefix+".max-open-buckets", 16, "Maximum number of concurrently open time-buckets per stream.")
}
