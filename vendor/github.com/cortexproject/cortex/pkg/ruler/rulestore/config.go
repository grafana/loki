package rulestore

import (
	"flag"
	"reflect"

	"github.com/cortexproject/cortex/pkg/configs/client"
	"github.com/cortexproject/cortex/pkg/ruler/rulestore/configdb"
	"github.com/cortexproject/cortex/pkg/ruler/rulestore/local"
	"github.com/cortexproject/cortex/pkg/storage/bucket"
	"github.com/cortexproject/cortex/pkg/util/flagext"
)

// Config configures a rule store.
type Config struct {
	bucket.Config `yaml:",inline"`
	ConfigDB      client.Config `yaml:"configdb"`
	Local         local.Config  `yaml:"local"`
}

// RegisterFlags registers the backend storage config.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	prefix := "ruler-storage."

	cfg.ExtraBackends = []string{configdb.Name, local.Name}
	cfg.ConfigDB.RegisterFlagsWithPrefix(prefix, f)
	cfg.Local.RegisterFlagsWithPrefix(prefix, f)
	cfg.RegisterFlagsWithPrefix(prefix, f)
}

// IsDefaults returns true if the storage options have not been set.
func (cfg *Config) IsDefaults() bool {
	defaults := Config{}
	flagext.DefaultValues(&defaults)

	return reflect.DeepEqual(*cfg, defaults)
}
