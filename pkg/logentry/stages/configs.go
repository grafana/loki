package stages

import (
	"github.com/mitchellh/mapstructure"
)

const (
	MetricTypeCounter   = "counter"
	MetricTypeGauge     = "gauge"
	MetricTypeHistogram = "histogram"

	StageTypeJSON  = "json"
	StageTypeRegex = "regex"
)

// MetricConfig is a single metrics configuration.
type MetricConfig struct {
	MetricType  string    `mapstructure:"type"`
	Description string    `mapstructure:"description"`
	Source      *string   `mapstructure:"source"`
	Buckets     []float64 `mapstructure:"buckets"`
}

// MetricsConfig is a set of configured metrics.
type MetricsConfig map[string]MetricConfig

// TimestampConfig configures timestamp extraction
type TimestampConfig struct {
	Source *string `mapstructure:"source"`
	Format string  `mapstructure:"format"`
}

// LabelConfig configures a labels value extraction
type LabelConfig struct {
	Source *string `mapstructure:"source"`
}

// OutputConfig configures output value extraction
type OutputConfig struct {
	Source *string `mapstructure:"source"`
}

// StageConfig configures the log entry parser to extract value from
type StageConfig struct {
	Timestamp  *TimestampConfig        `mapstructure:"timestamp"`
	Output     *OutputConfig           `mapstructure:"output"`
	Labels     map[string]*LabelConfig `mapstructure:"labels"`
	Metrics    MetricsConfig           `mapstructure:"metrics"`
	Match      string                  `mapstructure:"match"`
	Expression string                  `mapstructure:"expression"`
}

// NewConfig creates a new config from an interface using mapstructure
func NewConfig(config interface{}) (*StageConfig, error) {
	cfg := &StageConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}
