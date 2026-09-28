package shardstreams

import (
	"flag"
	"fmt"
	"time"

	"github.com/grafana/loki/v3/pkg/util/flagext"
)

// Values for Config.LimitsServiceStreamShardingMode.
const (
	// LimitsServiceStreamShardingModeDisabled leaves stream sharding entirely
	// to the distributor's local rate store.
	LimitsServiceStreamShardingModeDisabled = "disabled"

	// LimitsServiceStreamShardingModeShadow also asks the ingest-limits
	// service for a shard count, for comparison only. The local rate store
	// still decides how streams are sharded.
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

	// LimitsServiceStreamShardingMode controls whether the ingest-limits
	// service is asked for a shard count as well. In shadow mode its answer
	// is only compared against the local rate store's.
	LimitsServiceStreamShardingMode string `yaml:"limits_service_stream_sharding_mode" json:"limits_service_stream_sharding_mode" doc:"description=Experimental. Whether the ingest-limits service is asked for a shard count for this tenant. One of 'disabled' (default, unchanged behavior) or 'shadow' (ask the limits service and compare its answer against the local rate store's; the local rate store still decides how streams are sharded)."`

	// LimitsServiceStreamShardingRateWindow is the window the ingest-limits
	// service averages this tenant's stream rates over when deciding shard
	// counts. The distributor's local rate store ignores it. It is clamped at
	// use to the service's [bucket_size, rate_window], as the per-stream rate
	// bucket ring holds no more than rate_window of history.
	LimitsServiceStreamShardingRateWindow time.Duration `yaml:"limits_service_stream_sharding_rate_window" json:"limits_service_stream_sharding_rate_window" doc:"description=Experimental. The window the ingest-limits service averages this tenant's stream rates over when deciding shard counts. A shorter window reacts to shorter bursts, closer to the distributor's local rate store, which measures a one second window. 0 (default) uses the ingest-limits service's own rate_window. Clamped to the service's [bucket_size, rate_window], so raise the service-wide rate_window to allow a longer window here. The local rate store ignores this."`
}

// Validate returns an error if cfg is invalid.
func (cfg *Config) Validate() error {
	if cfg.LimitsServiceStreamShardingRateWindow < 0 {
		return fmt.Errorf("invalid limits_service_stream_sharding_rate_window %s: must not be negative", cfg.LimitsServiceStreamShardingRateWindow)
	}
	switch cfg.LimitsServiceStreamShardingMode {
	// The empty string is the zero value of the field, so a Config built in
	// code rather than from flags or YAML does not fail validation for never
	// having set it.
	case "", LimitsServiceStreamShardingModeDisabled, LimitsServiceStreamShardingModeShadow:
		return nil
	default:
		return fmt.Errorf("invalid limits_service_stream_sharding_mode %q: must be one of %q, %q",
			cfg.LimitsServiceStreamShardingMode,
			LimitsServiceStreamShardingModeDisabled, LimitsServiceStreamShardingModeShadow)
	}
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, fs *flag.FlagSet) {
	fs.BoolVar(&cfg.Enabled, prefix+".enabled", true, "Automatically shard streams to keep them under the per-stream rate limit")
	fs.BoolVar(&cfg.TimeShardingEnabled, prefix+".time-sharding-enabled", false, "Automatically shard streams by time (in MaxChunkAge/2 buckets), to allow out-of-order ingestion of very old logs.")
	fs.DurationVar(&cfg.TimeShardingIgnoreRecent, prefix+".time-sharding-ignore-recent", 40*time.Minute, "Logs with timestamps that are newer than this value will not be time-sharded.")
	fs.BoolVar(&cfg.LoggingEnabled, prefix+".logging-enabled", false, "Enable logging when sharding streams")
	cfg.DesiredRate.Set("1536KB") //nolint:errcheck
	fs.Var(&cfg.DesiredRate, prefix+".desired-rate", "threshold used to cut a new shard. Default (1536KB) means if a rate is above 1536KB/s, it will be sharded.")
	fs.StringVar(&cfg.LimitsServiceStreamShardingMode, prefix+".limits-service-stream-sharding-mode", LimitsServiceStreamShardingModeDisabled, "Experimental. One of 'disabled' or 'shadow'. Whether the ingest-limits service is asked for a shard count, for comparison against the local rate store.")
	fs.DurationVar(&cfg.LimitsServiceStreamShardingRateWindow, prefix+".limits-service-stream-sharding-rate-window", 0, "Experimental. The window the ingest-limits service averages stream rates over when deciding shard counts. A shorter window reacts to shorter bursts. 0 uses the ingest-limits service's own rate_window. Clamped to the service's [bucket_size, rate_window].")
}
