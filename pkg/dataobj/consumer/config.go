package consumer

import (
	"flag"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
)

type Config struct {
	dataobj.BuilderConfig
	UploaderConfig uploader.Config `yaml:"uploader"`
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
}
