package shardstreams

import (
	"flag"
	"fmt"
	"time"

	"github.com/grafana/loki/v3/pkg/util/flagext"
)

// Values for Config.LimitsServiceStreamShardingMode.
const (
	// LimitsServiceStreamShardingModeDisabled stream sharding
	// is driven entirely by the distributor's local rate store.
	LimitsServiceStreamShardingModeDisabled = "disabled"

	// LimitsServiceStreamShardingModeShadow computes a shard-count
	// recommendation via the ingest-limits service's CheckLimitsAndShard RPC
	// for comparison/observability only. Actual sharding is still driven by
	// the local rate store.
	LimitsServiceStreamShardingModeShadow = "shadow"
)

type Config struct {
	Enabled bool `yaml:"enabled" json:"enabled" doc:"description=Automatically shard streams to keep them under the per-stream rate limit. Sharding is dictated by the desired rate."`

	TimeShardingEnabled bool `yaml:"time_sharding_enabled" json:"time_sharding_enabled" doc:"description=Automatically shard streams by adding a __time_shard__ label, with values calculated from the log timestamps divided by MaxChunkAge/2. This allows the out-of-order ingestion of very old logs. If both flags are enabled, time-based sharding will happen before rate-based sharding."`

	TimeShardingIgnoreRecent time.Duration `yaml:"time_sharding_ignore_recent" json:"time_sharding_ignore_recent" doc:"description=Logs with timestamps that are newer than this value will not be time-sharded."`

	LoggingEnabled bool `yaml:"logging_enabled" json:"logging_enabled" doc:"description=Whether to log sharding streams behavior or not. Not recommended for production environments."`

	// DesiredRate is the threshold used to shard the stream into smaller pieces.
	// Expected to be in bytes.
	DesiredRate flagext.ByteSize `yaml:"desired_rate" json:"desired_rate" doc:"description=Threshold used to cut a new shard. Default (1536KB) means if a rate is above 1536KB/s, it will be sharded into two streams."`

	// LimitsServiceStreamShardingMode controls whether shard-count decisions
	// are computed by the ingest-limits service instead of the distributor's
	// local rate store.
	LimitsServiceStreamShardingMode string `yaml:"limits_service_stream_sharding_mode" json:"limits_service_stream_sharding_mode" doc:"description=Experimental. Controls whether the ingest-limits service is asked for a shard-count recommendation for observability purposes. One of 'disabled' (default, unchanged behavior) or 'shadow' (compute via the limits service for comparison only; actual sharding is still driven by the local rate store)."`
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, fs *flag.FlagSet) {
	fs.BoolVar(&cfg.Enabled, prefix+".enabled", true, "Automatically shard streams to keep them under the per-stream rate limit")
	fs.BoolVar(&cfg.TimeShardingEnabled, prefix+".time-sharding-enabled", false, "Automatically shard streams by time (in MaxChunkAge/2 buckets), to allow out-of-order ingestion of very old logs.")
	fs.DurationVar(&cfg.TimeShardingIgnoreRecent, prefix+".time-sharding-ignore-recent", 40*time.Minute, "Logs with timestamps that are newer than this value will not be time-sharded.")
	fs.BoolVar(&cfg.LoggingEnabled, prefix+".logging-enabled", false, "Enable logging when sharding streams")
	cfg.DesiredRate.Set("1536KB") //nolint:errcheck
	fs.Var(&cfg.DesiredRate, prefix+".desired-rate", "threshold used to cut a new shard. Default (1536KB) means if a rate is above 1536KB/s, it will be sharded.")
	fs.StringVar(&cfg.LimitsServiceStreamShardingMode, prefix+".limits-service-stream-sharding-mode", LimitsServiceStreamShardingModeDisabled, "Experimental. One of 'disabled' or 'shadow'. Controls whether the ingest-limits service is asked for a shard-count recommendation for observability purposes.")
}

// Validate returns an error if cfg is invalid.
func (cfg *Config) Validate() error {
	switch cfg.LimitsServiceStreamShardingMode {
	// The empty string is accepted as equivalent to "disabled" -- the Go
	// zero value for this field -- so that Config values built directly
	// (e.g. in tests, or before flag defaults are applied) don't fail
	// validation just for never having set this field explicitly.
	case "", LimitsServiceStreamShardingModeDisabled, LimitsServiceStreamShardingModeShadow:
		return nil
	default:
		return fmt.Errorf("invalid limits_service_stream_sharding_mode %q: must be one of %q, %q",
			cfg.LimitsServiceStreamShardingMode,
			LimitsServiceStreamShardingModeDisabled, LimitsServiceStreamShardingModeShadow)
	}
}
