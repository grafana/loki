package bloomgateway

import (
	"flag"
	"time"
)

// PoolConfig is config for creating a Pool.
// It has the same fields as "github.com/grafana/dskit/ring/client.PoolConfig" so it can be cast.
type PoolConfig struct {
	CheckInterval             time.Duration `yaml:"check_interval"`
	HealthCheckEnabled        bool          `yaml:"enable_health_check"`
	HealthCheckTimeout        time.Duration `yaml:"health_check_timeout"`
	MaxConcurrentHealthChecks int           `yaml:"-"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *PoolConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.DurationVar(&cfg.CheckInterval, prefix+"check-interval", 10*time.Second, "How frequently to clean up clients for servers that have gone away or are unhealthy.")
	f.BoolVar(&cfg.HealthCheckEnabled, prefix+"enable-health-check", true, "Run a health check on each server during periodic cleanup.")
	f.DurationVar(&cfg.HealthCheckTimeout, prefix+"health-check-timeout", 1*time.Second, "Timeout for the health check if health check is enabled.")
}

func (cfg *PoolConfig) Validate() error {
	return nil
}
