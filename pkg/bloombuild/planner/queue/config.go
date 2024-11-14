package queue

import (
	"flag"
	"fmt"

	"github.com/grafana/loki/v3/pkg/queue"
)

type Config struct {
	MaxQueuedTasksPerTenant int    `yaml:"max_queued_tasks_per_tenant"`
	StoreTasksOnDisk        bool   `yaml:"store_tasks_on_disk"`
	TasksDiskDirectory      string `yaml:"tasks_disk_directory"`
	CleanTasksDirectory     bool   `yaml:"clean_tasks_directory"`
}

// RegisterFlagsWithPrefix registers flags for the bloom-planner configuration.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.IntVar(&cfg.MaxQueuedTasksPerTenant, prefix+".max-tasks-per-tenant", 30000, "Maximum number of tasks to queue per tenant.")
	f.BoolVar(&cfg.StoreTasksOnDisk, prefix+".store-tasks-on-disk", false, "Whether to store tasks on disk.")
	f.StringVar(&cfg.TasksDiskDirectory, prefix+".tasks-disk-directory", "/tmp/bloom-planner-queue", "Directory to store tasks on disk.")
	f.BoolVar(&cfg.CleanTasksDirectory, prefix+".clean-tasks-directory", false, "Whether to clean the tasks directory on startup.")
}

func (cfg *Config) Validate() error {
	if cfg.StoreTasksOnDisk && cfg.TasksDiskDirectory == "" {
		return fmt.Errorf("tasks_disk_directory must be set when store_tasks_on_disk is true")
	}

	return nil
}

type Limits = queue.Limits
