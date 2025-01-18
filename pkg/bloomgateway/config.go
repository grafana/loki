package bloomgateway

import (
	"flag"
)

// Config configures the Bloom Gateway component.
type Config struct {
	// Enabled is the global switch to configures whether Bloom Gateways should be used to filter chunks.
	Enabled bool `yaml:"enabled"`
	// Client configures the Bloom Gateway client
	Client ClientConfig `yaml:"client,omitempty" doc:""`

	WorkerConcurrency       int  `yaml:"worker_concurrency"`
	BlockQueryConcurrency   int  `yaml:"block_query_concurrency"`
	MaxOutstandingPerTenant int  `yaml:"max_outstanding_per_tenant"`
	NumMultiplexItems       int  `yaml:"num_multiplex_tasks"`
	FetchBlocksAsync        bool `yaml:"fetch_blocks_async" doc:"hidden"`
}

// RegisterFlags registers flags for the Bloom Gateway configuration.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("bloom-gateway.", f)
}

// RegisterFlagsWithPrefix registers flags for the Bloom Gateway configuration with a common prefix.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, prefix+"enabled", false, "Flag to enable or disable the bloom gateway component globally.")
	f.IntVar(&cfg.WorkerConcurrency, prefix+"worker-concurrency", 4, "Number of workers to use for filtering chunks concurrently. Usually set to 1x number of CPU cores.")
	f.IntVar(&cfg.BlockQueryConcurrency, prefix+"block-query-concurrency", 8, "Number of blocks processed concurrently on a single worker. Usually set to 2x number of CPU cores.")
	f.IntVar(&cfg.MaxOutstandingPerTenant, prefix+"max-outstanding-per-tenant", 1024, "Maximum number of outstanding tasks per tenant.")
	f.IntVar(&cfg.NumMultiplexItems, prefix+"num-multiplex-tasks", 512, "How many tasks are multiplexed at once.")
	f.BoolVar(&cfg.FetchBlocksAsync, prefix+"fetch-blocks-async", true, "Whether blocks should be fetched asynchronously.")
	// TODO(chaudum): Figure out what the better place is for registering flags
	// -bloom-gateway.client.* or -bloom-gateway-client.*
	cfg.Client.RegisterFlags(f)
}

func (cfg *Config) Validate() error {
	if !cfg.Enabled {
		return nil
	}
	if err := cfg.Client.Validate(); err != nil {
		return err
	}
	return nil
}

type Limits interface {
	BloomGatewayEnabled(tenantID string) bool
}
