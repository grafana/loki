// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.

package cleaner

import (
	"flag"
	"time"
)

// Config specifies the configurable settings of the WAL cleaner
type Config struct {
	MinAge time.Duration `yaml:"min_age,omitempty"`
	Period time.Duration `yaml:"period,omitempty"`
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	f.DurationVar(&c.MinAge, "ruler.wal-cleaner.min-age", DefaultCleanupAge, "The minimum age of a WAL to consider for cleaning.")
	f.DurationVar(&c.Period, "ruler.wal-cleaner.period", DefaultCleanupPeriod, "How often to run the WAL cleaner. 0 = disabled.")
}

func (c *Config) Validate() error {
	return nil
}
