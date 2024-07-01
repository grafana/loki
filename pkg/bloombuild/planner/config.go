package planner

import (
	"flag"
	"fmt"
	"time"
)

// Config configures the bloom-planner component.
type Config struct {
	PlanningInterval        time.Duration `yaml:"planning_interval"`
	MinTableOffset          int           `yaml:"min_table_offset"`
	MaxTableOffset          int           `yaml:"max_table_offset"`
	MaxQueuedTasksPerTenant int           `yaml:"max_queued_tasks_per_tenant"`
}

// RegisterFlagsWithPrefix registers flags for the bloom-planner configuration.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.DurationVar(&cfg.PlanningInterval, prefix+".interval", 8*time.Hour, "Interval at which to re-run the bloom creation planning.")
	f.IntVar(&cfg.MinTableOffset, prefix+".min-table-offset", 1, "Newest day-table offset (from today, inclusive) to build blooms for. Increase to lower cost by not re-writing data to object storage too frequently since recent data changes more often at the cost of not having blooms available as quickly.")
	// TODO(owen-d): ideally we'd set this per tenant based on their `reject_old_samples_max_age` setting,
	// but due to how we need to discover tenants, we can't do that yet. Tenant+Period discovery is done by
	// iterating the table periods in object storage and looking for tenants within that period.
	// In order to have this done dynamically, we'd need to account for tenant specific overrides, which are also
	// dynamically reloaded.
	// I'm doing it the simple way for now.
	f.IntVar(&cfg.MaxTableOffset, prefix+".max-table-offset", 2, "Oldest day-table offset (from today, inclusive) to compact. This can be used to lower cost by not trying to compact older data which doesn't change. This can be optimized by aligning it with the maximum `reject_old_samples_max_age` setting of any tenant.")
	f.IntVar(&cfg.MaxQueuedTasksPerTenant, prefix+".max-tasks-per-tenant", 30000, "Maximum number of tasks to queue per tenant.")
}

func (cfg *Config) Validate() error {
	if cfg.MinTableOffset > cfg.MaxTableOffset {
		return fmt.Errorf("min-table-offset (%d) must be less than or equal to max-table-offset (%d)", cfg.MinTableOffset, cfg.MaxTableOffset)
	}

	return nil
}

type Limits interface {
	BloomCreationEnabled(tenantID string) bool
	BloomSplitSeriesKeyspaceBy(tenantID string) int
	BloomBuildMaxBuilders(tenantID string) int
	BuilderResponseTimeout(tenantID string) time.Duration
	BloomTaskMaxRetries(tenantID string) int
}

type QueueLimits struct {
	limits Limits
}

func NewQueueLimits(limits Limits) *QueueLimits {
	return &QueueLimits{limits: limits}
}

// MaxConsumers is used to compute how many of the available builders are allowed to handle tasks for a given tenant.
// 0 is returned when neither limits are applied. 0 means all builders can be used.
func (c *QueueLimits) MaxConsumers(tenantID string, allConsumers int) int {
	if c == nil || c.limits == nil {
		return 0
	}

	maxBuilders := c.limits.BloomBuildMaxBuilders(tenantID)
	if maxBuilders == 0 {
		return 0
	}

	return min(allConsumers, maxBuilders)
}
