package rulestore

import (
	"flag"

	"github.com/cortexproject/cortex/pkg/configs/client"
	"github.com/cortexproject/cortex/pkg/ruler/rulestore/configdb"
	"github.com/cortexproject/cortex/pkg/ruler/rulestore/local"
	"github.com/cortexproject/cortex/pkg/storage/bucket"
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
