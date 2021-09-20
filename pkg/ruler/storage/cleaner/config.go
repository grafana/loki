// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package cleaner

import (
	"time"
)

// Config specifies the configurable settings of the WAL cleaner
type Config struct {
	MinAge time.Duration `yaml:"min_age,omitempty"`
	Period time.Duration `yaml:"period,omitempty"`
}
