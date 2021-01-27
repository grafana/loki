package distributor

import (
	"flag"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/cortexproject/cortex/pkg/ring"
	ring_client "github.com/cortexproject/cortex/pkg/ring/client"
)

var clients = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: "cortex",
	Name:      "distributor_ingester_clients",
	Help:      "The current number of ingester clients.",
})

// PoolConfig is config for creating a Pool.
type PoolConfig struct {
	ClientCleanupPeriod  time.Duration `yaml:"client_cleanup_period"`
	HealthCheckIngesters bool          `yaml:"health_check_ingesters"`
	RemoteTimeout        time.Duration `yaml:"-"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *PoolConfig) RegisterFlags(f *flag.FlagSet) {
	f.DurationVar(&cfg.ClientCleanupPeriod, "distributor.client-cleanup-period", 15*time.Second, "How frequently to clean up clients for ingesters that have gone away.")
	f.BoolVar(&cfg.HealthCheckIngesters, "distributor.health-check-ingesters", true, "Run a health check on each ingester client during periodic cleanup.")
}

func NewPool(cfg PoolConfig, ring ring.ReadRing, factory ring_client.PoolFactory, logger log.Logger) *ring_client.Pool {
	poolCfg := ring_client.PoolConfig{
		CheckInterval:      cfg.ClientCleanupPeriod,
		HealthCheckEnabled: cfg.HealthCheckIngesters,
		HealthCheckTimeout: cfg.RemoteTimeout,
	}

	return ring_client.NewPool("ingester", poolCfg, ring_client.NewRingServiceDiscovery(ring), factory, clients, logger)
}
