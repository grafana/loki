package consumer

import (
	"flag"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
)

type Config struct {
	dataobj.BuilderConfig
	UploaderConfig   uploader.Config `yaml:"uploader"`
	IdleFlushTimeout time.Duration   `yaml:"idle_flush_timeout"`
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

	f.DurationVar(&cfg.IdleFlushTimeout, prefix+"idle-flush-timeout", 60*60*time.Second, "The maximum amount of time to wait in seconds before flushing an object that is no longer receiving new writes")
}
