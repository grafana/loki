package consumer

import (
	"flag"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
)

type Config struct {
	logsobj.BuilderConfig
	UploaderConfig uploader.Config `yaml:"uploader"`
	FlushInterval  time.Duration   `yaml:"flush_interval"`
	IdleTimeout    time.Duration   `yaml:"idle_timeout"`
}

func (cfg *Config) Validate() error {
	if err := cfg.UploaderConfig.Validate(); err != nil {
		return err
	}
	return cfg.BuilderConfig.Validate()
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("dataobj-consumer.", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.BuilderConfig.RegisterFlagsWithPrefix(prefix, f)
	cfg.UploaderConfig.RegisterFlagsWithPrefix(prefix, f)
	f.DurationVar(
		&cfg.FlushInterval,
		prefix+"flush-interval",
		60*time.Minute,
		"The rate at which data objects are built. For example, if set to 15 minutes, you can expect the consumer to build at least one data object per partition every 15 minutes. However, data objects can be built more often if the partition reaches the target object size before the flush interval.",
	)
	f.DurationVar(
		&cfg.IdleTimeout,
		prefix+"idle-timeout",
		60*time.Minute,
		"The maximum amount of time to wait before declaring a partition as idle. When a partition is marked as idle its in-progress data object will be flushed.",
	)
}
