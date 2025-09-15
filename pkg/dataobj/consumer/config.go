package consumer

import (
	"flag"
	"time"

	"github.com/grafana/dskit/ring"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type Config struct {
	BuilderConfig    logsobj.BuilderConfig `yaml:"builder,omitempty"`
	LifecyclerConfig ring.LifecyclerConfig `yaml:"lifecycler,omitempty"`
	UploaderConfig   uploader.Config       `yaml:"uploader"`
	IdleFlushTimeout time.Duration         `yaml:"idle_flush_timeout"`
}

func (cfg *Config) Validate() error {
	if err := cfg.BuilderConfig.Validate(); err != nil {
		return err
	}
	if err := cfg.LifecyclerConfig.Validate(); err != nil {
		return err
	}
	if err := cfg.UploaderConfig.Validate(); err != nil {
		return err
	}
	return nil
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("dataobj-consumer.", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.BuilderConfig.RegisterFlagsWithPrefix(prefix, f)
	cfg.LifecyclerConfig.RegisterFlagsWithPrefix(prefix, f, util_log.Logger)
	cfg.UploaderConfig.RegisterFlagsWithPrefix(prefix, f)

	f.DurationVar(&cfg.IdleFlushTimeout, prefix+"idle-flush-timeout", 60*60*time.Second, "The maximum amount of time to wait in seconds before flushing an object that is no longer receiving new writes")
}
