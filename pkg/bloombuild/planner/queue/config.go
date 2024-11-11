package queue

import (
	"flag"
	"github.com/grafana/loki/v3/pkg/queue"
)

type Config struct {
	MaxQueuedTasksPerTenant int `yaml:"max_queued_tasks_per_tenant"`
}

// RegisterFlagsWithPrefix registers flags for the bloom-planner configuration.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.IntVar(&cfg.MaxQueuedTasksPerTenant, prefix+".max-tasks-per-tenant", 30000, "Maximum number of tasks to queue per tenant.")
}

func (cfg *Config) Validate() error {
	return nil
}

type Limits = queue.Limits
