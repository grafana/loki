package rulestore

import (
	"flag"

	"github.com/cortexproject/cortex/pkg/configs/client"
	"github.com/cortexproject/cortex/pkg/storage/bucket"
)

const (
	ConfigDB = "configdb"

	Name   = "ruler-storage"
	prefix = "ruler-storage."
)

// Config configures a rule store.
type Config struct {
	bucket.Config `yaml:",inline"`
	ConfigDB      client.Config `yaml:"configdb"`
}

// RegisterFlags registers the backend storage config.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.ExtraBackends = []string{ConfigDB}
	cfg.ConfigDB.RegisterFlagsWithPrefix(prefix, f)
	cfg.RegisterFlagsWithPrefix(prefix, f)
}
