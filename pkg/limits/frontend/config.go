package frontend

import (
	"flag"
	"fmt"
	"time"

	"github.com/grafana/dskit/ring"

	limits_client "github.com/grafana/loki/v3/pkg/limits/client"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// Config contains the config for an ingest-limits-frontend.
type Config struct {
	ClientConfig               limits_client.Config  `yaml:"client_config"`
	LifecyclerConfig           ring.LifecyclerConfig `yaml:"lifecycler,omitempty"`
	RecheckPeriod              time.Duration         `yaml:"recheck_period"`
	NumPartitions              int                   `yaml:"num_partitions"`
	AssignedPartitionsCacheTTL time.Duration         `yaml:"assigned_partitions_cache_ttl"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.ClientConfig.RegisterFlagsWithPrefix("ingest-limits-frontend", f)
	cfg.LifecyclerConfig.RegisterFlagsWithPrefix("ingest-limits-frontend.", f, util_log.Logger)
	f.DurationVar(&cfg.RecheckPeriod, "ingest-limits-frontend.recheck-period", 10*time.Second, "The period to recheck per tenant ingestion rate limit configuration.")
	f.IntVar(&cfg.NumPartitions, "ingest-limits-frontend.num-partitions", 64, "The number of partitions to use for the ring.")
	f.DurationVar(&cfg.AssignedPartitionsCacheTTL, "ingest-limits-frontend.assigned-partitions-cache-ttl", 1*time.Minute, "The TTL for the assigned partitions cache. 0 disables the cache.")
}

func (cfg *Config) Validate() error {
	if err := cfg.ClientConfig.GRPCClientConfig.Validate(); err != nil {
		return fmt.Errorf("invalid gRPC client config: %w", err)
	}
	return nil
}
