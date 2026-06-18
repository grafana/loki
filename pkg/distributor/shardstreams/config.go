package shardstreams

import (
	"flag"
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/util/flagext"
)

type Config struct {
	Enabled bool `yaml:"enabled" json:"enabled" doc:"description=Automatically shard streams to keep them under the per-stream rate limit. Sharding is dictated by the desired rate."`

	TimeShardingEnabled bool `yaml:"time_sharding_enabled" json:"time_sharding_enabled" doc:"description=Automatically shard streams by adding a __time_shard__ label, with values calculated from the log timestamps divided by MaxChunkAge/2. This allows the out-of-order ingestion of very old logs. If both flags are enabled, time-based sharding will happen before rate-based sharding."`

	TimeShardingIgnoreRecent time.Duration `yaml:"time_sharding_ignore_recent" json:"time_sharding_ignore_recent" doc:"description=Logs with timestamps that are newer than this value will not be time-sharded."`

	LoggingEnabled bool `yaml:"logging_enabled" json:"logging_enabled" doc:"description=Whether to log sharding streams behavior or not. Not recommended for production environments."`

	// DesiredRate is the threshold used to shard the stream into smaller pieces.
	// Expected to be in bytes.
	DesiredRate flagext.ByteSize `yaml:"desired_rate" json:"desired_rate" doc:"description=Threshold used to cut a new shard. Default (1536KB) means if a rate is above 1536KB/s, it will be sharded into two streams."`
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, fs *flag.FlagSet) {
	fs.BoolVar(&cfg.Enabled, prefix+".enabled", true, "Automatically shard streams to keep them under the per-stream rate limit")
	fs.BoolVar(&cfg.TimeShardingEnabled, prefix+".time-sharding-enabled", false, "Automatically shard streams by time (in MaxChunkAge/2 buckets), to allow out-of-order ingestion of very old logs.")
	fs.DurationVar(&cfg.TimeShardingIgnoreRecent, prefix+".time-sharding-ignore-recent", 40*time.Minute, "Logs with timestamps that are newer than this value will not be time-sharded.")
	fs.BoolVar(&cfg.LoggingEnabled, prefix+".logging-enabled", false, "Enable logging when sharding streams")
	cfg.DesiredRate.Set("1536KB") //nolint:errcheck
	fs.Var(&cfg.DesiredRate, prefix+".desired-rate", "threshold used to cut a new shard. Default (1536KB) means if a rate is above 1536KB/s, it will be sharded.")
}

// PerPolicyConfigOverride holds optional per-policy overrides for the sharding Config. Each field is a
// pointer so that a nil field inherits the value from the base (tenant) Config — this lets a
// policy change just one setting (e.g. toggle time sharding) without restating the rest.
type PerPolicyConfigOverride struct {
	Enabled                  *bool             `yaml:"enabled" json:"enabled" doc:"description=Override shard_streams.enabled for a specific policy."`
	DesiredRate              *flagext.ByteSize `yaml:"desired_rate" json:"desired_rate" doc:"description=Override shard_streams.desired_rate for a specific policy."`
	TimeShardingEnabled      *bool             `yaml:"time_sharding_enabled" json:"time_sharding_enabled" doc:"description=Override shard_streams.time_sharding_enabled for a specific policy."`
	TimeShardingIgnoreRecent *model.Duration   `yaml:"time_sharding_ignore_recent" json:"time_sharding_ignore_recent" doc:"description=Override shard_streams.time_sharding_ignore_recent for a specific policy."`
	LoggingEnabled           *bool             `yaml:"logging_enabled" json:"logging_enabled" doc:"description=Override shard_streams.logging_enabled for a specific policy."`
}

// ApplyTo returns base with only the override's non-nil fields replaced. A nil override (or one
// with all-nil fields) returns base unchanged.
func (o *PerPolicyConfigOverride) ApplyTo(base Config) Config {
	if o == nil {
		return base
	}
	if o.Enabled != nil {
		base.Enabled = *o.Enabled
	}
	if o.DesiredRate != nil {
		base.DesiredRate = *o.DesiredRate
	}
	if o.TimeShardingEnabled != nil {
		base.TimeShardingEnabled = *o.TimeShardingEnabled
	}
	if o.TimeShardingIgnoreRecent != nil {
		base.TimeShardingIgnoreRecent = time.Duration(*o.TimeShardingIgnoreRecent)
	}
	if o.LoggingEnabled != nil {
		base.LoggingEnabled = *o.LoggingEnabled
	}
	return base
}
