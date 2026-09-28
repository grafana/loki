package index

import (
	"errors"
	"flag"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
)

type Config struct {
	logsobj.BuilderBaseConfig `yaml:",inline"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("dataobj-index-builder.", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	// Set defaults for base builder configuration
	_ = cfg.TargetPageSize.Set("128KB")   // smaller pages gives more opportunities to prune
	_ = cfg.TargetObjectSize.Set("512MB") // compressed
	_ = cfg.BufferSize.Set("128MB")
	_ = cfg.TargetSectionSize.Set("512MB") // uncompressed
	cfg.BuilderBaseConfig.RegisterFlagsWithPrefix(prefix, f)
	cfg.EstimatedCompressionRatio = 1
	if ecr := f.Lookup(prefix + "estimated-compression-ratio"); ecr != nil {
		// Hack to set the default value for the flag only for the index builder.
		ecr.DefValue = "1"
	}
}

// Validate validates the BuilderConfig.
func (cfg *Config) Validate() error {
	var errs []error

	if err := cfg.BuilderBaseConfig.Validate(); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}
