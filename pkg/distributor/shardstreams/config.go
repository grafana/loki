package shardstreams

import (
	"flag"

	"github.com/grafana/loki/v3/pkg/util/flagext"
)

type Config struct {
	Enabled bool `yaml:"enabled" json:"enabled" doc:"description=Automatically shard streams to keep them under the per-stream rate limit. Sharding is dictated by the desired rate."`

	LoggingEnabled bool `yaml:"logging_enabled" json:"logging_enabled" doc:"description=Whether to log sharding streams behavior or not. Not recommended for production environments."`

	// DesiredRate is the threshold used to shard the stream into smaller pieces.
	// Expected to be in bytes.
	DesiredRate flagext.ByteSize `yaml:"desired_rate" json:"desired_rate" doc:"description=Threshold used to cut a new shard. Default (1536KB) means if a rate is above 1536KB/s, it will be sharded into two streams."`
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, fs *flag.FlagSet) {
	fs.BoolVar(&cfg.Enabled, prefix+".enabled", true, "Automatically shard streams to keep them under the per-stream rate limit")
	fs.BoolVar(&cfg.LoggingEnabled, prefix+".logging-enabled", false, "Enable logging when sharding streams")
	cfg.DesiredRate.Set("1536KB") //nolint:errcheck
	fs.Var(&cfg.DesiredRate, prefix+".desired-rate", "threshold used to cut a new shard. Default (1536KB) means if a rate is above 1536KB/s, it will be sharded.")
}
