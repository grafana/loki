package cleaner

import (
	"time"
)

// TODO: add YAML unmarshaling here with defaults
//       -> tried before but for some reason the UnmarshalYAML function was not being called

// Config specifies the configurable settings of the WAL cleaner
type Config struct {
	MinAge time.Duration `yaml:"min_age,omitempty"`
	Period time.Duration `yaml:"period,omitempty"`
}