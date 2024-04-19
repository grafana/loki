package shardstreams

import (
	"flag"

	"github.com/grafana/loki/v3/pkg/util/flagext"
)

type Config struct {
	Enabled        bool `yaml:"enabled" json:"enabled"`
	LoggingEnabled bool `yaml:"logging_enabled" json:"logging_enabled"`

	// DesiredRate is the threshold used to shard the stream into smaller pieces.
	// Expected to be in bytes.
	DesiredRate flagext.ByteSize `yaml:"desired_rate" json:"desired_rate"`
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, fs *flag.FlagSet) {
	fs.BoolVar(&cfg.Enabled, prefix+".enabled", true, "Automatically shard streams to keep them under the per-stream rate limit")
	fs.BoolVar(&cfg.LoggingEnabled, prefix+".logging-enabled", false, "Enable logging when sharding streams")
	cfg.DesiredRate.Set("1536KB") //nolint:errcheck
	fs.Var(&cfg.DesiredRate, prefix+".desired-rate", "threshold used to cut a new shard. Default (1536KB) means if a rate is above 1536KB/s, it will be sharded.")
}
