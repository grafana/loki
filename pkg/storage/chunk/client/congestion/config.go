package congestion

import (
	"flag"
	"fmt"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
)

type Config struct {
	Enabled    bool             `yaml:"enabled"`
	Controller ControllerConfig `yaml:"controller"`
	Retry      RetrierConfig    `yaml:"retry"`
	Hedge      HedgerConfig     `yaml:"hedging"`
}

func (c *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	prefix = fmt.Sprintf("%s%s", prefix, "congestion-control.")
	f.BoolVar(&c.Enabled, prefix+"enabled", false, "Use storage congestion control (default: disabled).")

	c.Controller.RegisterFlagsWithPrefix(prefix, f)
	c.Retry.RegisterFlagsWithPrefix(prefix+"retry.", f)
	c.Hedge.RegisterFlagsWithPrefix(prefix+"hedge.", f)
}

type AIMD struct {
	Start         uint    `yaml:"start"`
	UpperBound    uint    `yaml:"upper_bound"`
	BackoffFactor float64 `yaml:"backoff_factor"`
}

type ControllerConfig struct {
	Strategy string `yaml:"strategy"`
	AIMD     AIMD   `yaml:"aimd"`
}

func (c *ControllerConfig) RegisterFlags(f *flag.FlagSet) {
	c.RegisterFlagsWithPrefix("", f)
}

// nolint:goconst
func (c *ControllerConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&c.Strategy, prefix+"strategy", "", "Congestion control strategy to use (default: none, options: 'aimd').")
	f.UintVar(&c.AIMD.Start, prefix+"strategy.aimd.start", 2000, "AIMD starting throughput window size: how many requests can be sent per second (default: 2000).")
	f.UintVar(&c.AIMD.UpperBound, prefix+"strategy.aimd.upper-bound", 10000, "AIMD maximum throughput window size: upper limit of requests sent per second (default: 10000).")
	f.Float64Var(&c.AIMD.BackoffFactor, prefix+"strategy.aimd.backoff-factor", 0.5, "AIMD backoff factor when upstream service is throttled to decrease number of requests sent per second (default: 0.5).")
}

type RetrierConfig struct {
	Strategy string `yaml:"strategy"`
	Limit    int    `yaml:"limit"`
}

func (c *RetrierConfig) RegisterFlags(f *flag.FlagSet) {
	c.RegisterFlagsWithPrefix("", f)
}

func (c *RetrierConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&c.Strategy, prefix+"strategy", "", "Congestion control retry strategy to use (default: none, options: 'limited').")
	f.IntVar(&c.Limit, prefix+"strategy.limited.limit", 2, "Maximum number of retries allowed.")
}

type HedgerConfig struct {
	hedging.Config

	Strategy string `yaml:"strategy"`
}

func (c *HedgerConfig) RegisterFlags(f *flag.FlagSet) {
	c.RegisterFlagsWithPrefix("", f)
}

func (c *HedgerConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&c.Strategy, prefix+"strategy", "", "Congestion control hedge strategy to use (default: none, options: 'limited').")
	// TODO hedge configs
}
