package congestion

import "github.com/grafana/loki/pkg/storage/chunk/client/hedging"

type Config struct {
	Controller ControllerConfig `yaml:"controller"`
	Retry      RetrierConfig    `yaml:"retry"`
	Hedge      HedgerConfig     `yaml:"hedging"`
}

type ControllerConfig struct {
	Strategy string `yaml:"strategy"`
	AIMD     struct {
		LowerBound    uint    `yaml:"lower_bound"`
		UpperBound    uint    `yaml:"upper_bound"`
		BackoffFactor float64 `yaml:"backoff_factor"`
	} `yaml:"aimd"`
}

type RetrierConfig struct {
	Strategy string `yaml:"strategy"`
	Limit    int    `yaml:"limit"`
}

type HedgerConfig struct {
	hedging.Config

	Strategy string `yaml:"strategy"`
}

// TODO(dannyk): register flags
