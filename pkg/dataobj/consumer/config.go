package consumer

import (
	"errors"
	"flag"
	"time"

	"github.com/grafana/dskit/ring"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	dataobj_uploader "github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/kafka/partitionring"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type Config struct {
	logsobj.BuilderConfig
	LifecyclerConfig    ring.LifecyclerConfig   `yaml:"lifecycler,omitempty"`
	PartitionRingConfig partitionring.Config    `yaml:"partition_ring" category:"experimental"`
	UploaderConfig      dataobj_uploader.Config `yaml:"uploader"`
	IdleFlushTimeout    time.Duration           `yaml:"idle_flush_timeout"`
	MaxBuilderAge       time.Duration           `yaml:"max_builder_age"`

	// This is temporary until we move to kafkav2.
	Topic string `yaml:"topic"`
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
	if cfg.Topic == "" {
		return errors.New("topic is required")
	}
	return nil
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("dataobj-consumer.", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.BuilderConfig.RegisterFlagsWithPrefix(prefix, f)
	cfg.LifecyclerConfig.RegisterFlagsWithPrefix(prefix, f, util_log.Logger)
	cfg.PartitionRingConfig.RegisterFlagsWithPrefix(prefix, f)
	cfg.UploaderConfig.RegisterFlagsWithPrefix(prefix, f)

	f.StringVar(&cfg.Topic, prefix+"topic", "", "The name of the Kafka topic")
	f.DurationVar(&cfg.IdleFlushTimeout, prefix+"idle-flush-timeout", 60*60*time.Second, "The maximum amount of time to wait in seconds before flushing an object that is no longer receiving new writes")
	f.DurationVar(&cfg.MaxBuilderAge, prefix+"max-builder-age", time.Hour, "The maximum amount of time to accumulate data in a builder before flushing it. Defaults to 1 hour.")
}
