package rulestore

import (
	"flag"
	"fmt"
	"reflect"
	"strings"

	"github.com/grafana/dskit/flagext"

	"github.com/grafana/loki/v3/pkg/ruler/rulestore/local"
	"github.com/grafana/loki/v3/pkg/storage/bucket"
)

// Config configures a rule store.
type Config struct {
	bucket.Config `yaml:",inline"`
	Backend       string       `yaml:"backend"`
	Local         local.Config `yaml:"local"`
}

// RegisterFlags registers the backend storage config.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	prefix := "ruler-storage."

	cfg.ExtraBackends = []string{local.Name}
	cfg.Local.RegisterFlagsWithPrefix(prefix, f)
	f.StringVar(&cfg.Backend, prefix+"backend", "filesystem", fmt.Sprintf("Backend storage to use. Supported backends are: local, %s", strings.Join(bucket.SupportedBackends, ", ")))
	cfg.RegisterFlagsWithPrefix(prefix, f)
}

// IsDefaults returns true if the storage options have not been set.
func (cfg *Config) IsDefaults() bool {
	defaults := Config{}
	flagext.DefaultValues(&defaults)

	return reflect.DeepEqual(*cfg, defaults)
}
