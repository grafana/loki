package configs

import (
	"flag"

	"github.com/cortexproject/cortex/pkg/configs/api"
	"github.com/cortexproject/cortex/pkg/configs/db"
)

type Config struct {
	DB  db.Config  `yaml:"database"`
	API api.Config `yaml:"api"`
}

// RegisterFlags adds the flags required to configure this to the given FlagSet.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.DB.RegisterFlags(f)
	cfg.API.RegisterFlags(f)
}
