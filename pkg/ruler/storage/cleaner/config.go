package cleaner

import (
	"time"
)

// Config specifies the configurable settings of the WAL cleaner
type Config struct {
	MinAge time.Duration `yaml:"min_age,omitempty"`
	Period time.Duration `yaml:"period,omitempty"`
}