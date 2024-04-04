package bloomgateway

import (
	"context"
	"flag"
	"sort"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"

	"github.com/grafana/loki/v3/pkg/util/discovery"
	"github.com/grafana/loki/v3/pkg/util/jumphash"
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

type JumpHashClientPool struct {
	*client.Pool
	*jumphash.Selector

	done   chan struct{}
	logger log.Logger
}

func NewJumpHashClientPool(pool *client.Pool, dnsProvider *discovery.DNS, updateInterval time.Duration, logger log.Logger) *JumpHashClientPool {
	selector := jumphash.DefaultSelector()
	selector.SetServers(dnsProvider.Addresses()...)

	p := &JumpHashClientPool{
		Pool:     pool,
		Selector: selector,
		done:     make(chan struct{}),
		logger:   logger,
	}
	go p.discoveryLoop(dnsProvider, updateInterval)

	return p
}

func (p *JumpHashClientPool) Start() {
	ctx := context.Background()
	services.StartAndAwaitRunning(ctx, p.Pool)
}

func (p *JumpHashClientPool) Stop() {
	ctx := context.Background()
	services.StopAndAwaitTerminated(ctx, p.Pool)
	close(p.done)
}

func (p *JumpHashClientPool) discoveryLoop(provider *discovery.DNS, updateInterval time.Duration) {
	if provider == nil {
		return
	}

	ticker := time.NewTicker(updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			servers := provider.Addresses()
			// ServerList deterministically maps keys to _index_ of the server list.
			// Since DNS returns records in different order each time, we sort to
			// guarantee best possible match between nodes.
			sort.Strings(servers)
			err := p.SetServers(servers...)
			if err != nil {
				level.Warn(p.logger).Log("msg", "error updating memcache servers", "err", err)
			}
		}
	}
}
