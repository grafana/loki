package bloomgateway

import (
	"flag"

	"github.com/grafana/loki/pkg/util/ring"
)

// Config configures the Bloom Gateway component.
type Config struct {
	// Ring configures the ring store used to save and retrieve the different Bloom Gateway instances.
	// In case it isn't explicitly set, it follows the same behavior of the other rings (ex: using the common configuration
	// section and the ingester configuration by default).
	Ring ring.RingConfigWithRF `yaml:"ring,omitempty" doc:"description=Defines the ring to be used by the bloom gateway servers and clients. In case this isn't configured, this block supports inheriting configuration from the common ring section."`
	// Enabled is the global switch to configures whether Bloom Gateways should be used to filter chunks.
	Enabled bool `yaml:"enabled"`
	// Client configures the Bloom Gateway client
	Client ClientConfig `yaml:"client,omitempty" doc:""`

	WorkerConcurrency       int `yaml:"worker_concurrency"`
	MaxOutstandingPerTenant int `yaml:"max_outstanding_per_tenant"`
}

// RegisterFlags registers flags for the Bloom Gateway configuration.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("bloom-gateway.", f)
}

// RegisterFlagsWithPrefix registers flags for the Bloom Gateway configuration with a common prefix.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.Ring.RegisterFlagsWithPrefix(prefix, "collectors/", f)
	f.BoolVar(&cfg.Enabled, prefix+"enabled", false, "Flag to enable or disable the bloom gateway component globally.")
	f.IntVar(&cfg.WorkerConcurrency, prefix+"worker-concurrency", 4, "Number of workers to use for filtering chunks concurrently.")
	f.IntVar(&cfg.MaxOutstandingPerTenant, prefix+"max-outstanding-per-tenant", 1024, "Maximum number of outstanding tasks per tenant.")
	// TODO(chaudum): Figure out what the better place is for registering flags
	// -bloom-gateway.client.* or -bloom-gateway-client.*
	cfg.Client.RegisterFlags(f)
}

type Limits interface {
	CacheLimits
	BloomGatewayShardSize(tenantID string) int
	BloomGatewayEnabled(tenantID string) bool
	BloomGatewayBlocksDownloadingParallelism(tenantID string) int
}
